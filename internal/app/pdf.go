package app

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ymotongpoo/reviewer/internal/pdfimport"
	"github.com/ymotongpoo/reviewer/internal/project"
	"github.com/ymotongpoo/reviewer/internal/store"
)

type PDFInput struct {
	DocumentHash string                `json:"documentHash"`
	Name         string                `json:"name"`
	Highlights   []pdfimport.Highlight `json:"highlights"`
}

type PDFMatch struct {
	Highlight  pdfimport.Highlight   `json:"highlight"`
	Candidates []pdfimport.Candidate `json:"candidates"`
	Automatic  bool                  `json:"automatic"`
	Truncated  bool                  `json:"truncated"`
	ExistingID string                `json:"existingId,omitempty"`
}

type PDFCommentInput struct {
	DocumentHash string              `json:"documentHash"`
	Name         string              `json:"name"`
	Highlight    pdfimport.Highlight `json:"highlight"`
	Candidate    pdfimport.Candidate `json:"candidate"`
}

func validatePDF(in PDFInput) error {
	b, err := hex.DecodeString(in.DocumentHash)
	if err != nil || len(b) != 32 || in.DocumentHash != strings.ToLower(in.DocumentHash) {
		return badRequest("PDFの識別情報が不正です")
	}
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 1024 {
		return badRequest("PDFのファイル名が不正です")
	}
	if len(in.Highlights) == 0 || len(in.Highlights) > 500 {
		return badRequest("コメント付きハイライトは1〜500件にしてください")
	}
	seen := map[string]bool{}
	for _, h := range in.Highlights {
		key := fmt.Sprintf("%d:%s", h.Page, h.ID)
		if h.ID == "" || len(h.ID) > 256 || h.Page < 1 || h.Page > 10000 || strings.TrimSpace(h.Body) == "" || len(h.Body) > 16384 || len(h.Quote) > 16384 || len(h.Before) > 2048 || len(h.After) > 2048 || len(h.Warning) > 1024 || seen[key] {
			return badRequest("PDFの注釈データが不正です")
		}
		seen[key] = true
	}
	return nil
}

func (a *App) pdfExisting(hash string, h pdfimport.Highlight) *store.Comment {
	for _, c := range a.Store.Comments() {
		if p := c.PDFSource; p != nil && p.DocumentHash == hash && p.Page == h.Page && p.AnnotationID == h.ID {
			return c
		}
	}
	return nil
}

func markdownPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".mdx":
		return true
	}
	return false
}

// MatchPDF is read-only: no comments or snapshots are persisted until import.
func (a *App) MatchPDF(in PDFInput) ([]PDFMatch, error) {
	if err := validatePDF(in); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.rescan(); err != nil {
		return nil, err
	}
	var indexes []*pdfimport.Index
	total := 0
	for _, f := range a.files {
		if !markdownPath(f.Path) {
			continue
		}
		b, err := a.readFile(f.Path)
		if err != nil {
			return nil, err
		}
		total += len(b)
		if total > 32<<20 {
			return nil, badRequest("対象原稿が32 MiBを超えています。章のディレクトリを指定してください")
		}
		indexes = append(indexes, pdfimport.NewIndex(f.Path, project.HashBytes(b), b))
	}
	out := make([]PDFMatch, 0, len(in.Highlights))
	for _, h := range in.Highlights {
		m := PDFMatch{Highlight: h, Candidates: []pdfimport.Candidate{}}
		if c := a.pdfExisting(in.DocumentHash, h); c != nil {
			m.ExistingID = c.ID
		} else {
			m.Candidates, m.Automatic, m.Truncated = pdfimport.Find(indexes, h)
		}
		out = append(out, m)
	}
	return out, nil
}

// ImportPDFComment revalidates the exact source snapshot and match. Repeated
// requests return the existing comment, including after a lost HTTP response.
func (a *App) ImportPDFComment(in PDFCommentInput) (*store.Comment, error) {
	if err := validatePDF(PDFInput{in.DocumentHash, in.Name, []pdfimport.Highlight{in.Highlight}}); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if c := a.pdfExisting(in.DocumentHash, in.Highlight); c != nil {
		return c, nil
	}
	if in.Highlight.Warning != "" || len(pdfimport.Normalize(in.Highlight.Quote)) == 0 {
		return nil, badRequest("本文を抽出できなかった注釈は取り込めません")
	}
	if err := a.rescan(); err != nil {
		return nil, err
	}
	known := false
	for _, f := range a.files {
		if f.Path == in.Candidate.Path && markdownPath(f.Path) {
			known = true
			break
		}
	}
	if !known {
		return nil, badRequest("対象のMarkdownがレビュー範囲にありません")
	}
	b, err := a.readFile(in.Candidate.Path)
	if err != nil {
		return nil, err
	}
	if project.HashBytes(b) != in.Candidate.Hash {
		return nil, conflict("原稿が変わっています。PDFの照合をやり直してください")
	}
	x := pdfimport.NewIndex(in.Candidate.Path, in.Candidate.Hash, b)
	cs, _, _ := pdfimport.Find([]*pdfimport.Index{x}, in.Highlight)
	for _, c := range cs {
		if c.Range.SamePosition(in.Candidate.Range) && c.Range.Text == in.Candidate.Range.Text {
			label := "suggestion"
			if !a.validLabel(label) {
				label = ""
			}
			// Quote filenames as text so Markdown punctuation in a local filename
			// cannot create a link or change the feedback's structure.
			name := strings.NewReplacer("\n", " ", "\r", " ", "`", "'").Replace(filepath.Base(in.Name))
			body := in.Highlight.Body + fmt.Sprintf("\n\nPDF: `%s`、%dページ\n\nPDFから抽出した引用（ハイライト周辺を含む）:\n> ", name, in.Highlight.Page) + strings.ReplaceAll(in.Highlight.Quote, "\n", "\n> ")
			return a.createComment(NewComment{Scope: store.ScopeLine, Path: c.Path, Label: label, Body: body, Hash: c.Hash, Range: &c.Range,
				pdfSource: &store.PDFSource{DocumentHash: in.DocumentHash, Name: in.Name, Page: in.Highlight.Page, AnnotationID: in.Highlight.ID, Quote: in.Highlight.Quote}})
		}
	}
	return nil, conflict("選択した箇所がPDFの引用と一致しません。照合をやり直してください")
}
