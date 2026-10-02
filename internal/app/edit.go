package app

import (
	"errors"
	"io/fs"
	"net/http"

	"github.com/ymotongpoo/reviewer/internal/project"
)

// SaveFileInput contains the content and hash shown to the user before editing.
type SaveFileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Hash    string `json:"hash"`
}

// SaveFileResult reports the hash of the content that was written so the
// client can adopt it without racing the watcher's refresh.
type SaveFileResult struct {
	OK   bool   `json:"ok"`
	Hash string `json:"hash"`
}

// SaveFile writes a text file after checking that it has not changed since it
// was opened. The normal watcher path then re-anchors comments and refreshes
// connected clients.
func (a *App) SaveFile(in SaveFileInput) (*SaveFileResult, error) {
	if in.Path == "" {
		return nil, badRequest("ファイルのパスがありません")
	}
	content := []byte(in.Content)
	a.mu.Lock()
	err := a.Proj.Write(in.Path, in.Hash, content)
	hash := project.HashBytes(content)
	if err == nil {
		// Keep the saved content as a blob so that new comments made against
		// the returned hash anchor to it, like content served by File. The
		// file is already written, so a failure here only loses that.
		_, _ = a.Store.PutBlob(content)
	}
	a.mu.Unlock()
	if err != nil {
		switch {
		case errors.Is(err, project.ErrOutside):
			return nil, badRequest("編集できないパスです: %s", in.Path)
		case errors.Is(err, project.ErrNotText):
			return nil, badRequest("テキストファイルではないか、大きすぎます")
		case errors.Is(err, fs.ErrNotExist):
			return nil, notFound("ファイルがありません: %s", in.Path)
		case errors.Is(err, project.ErrChanged):
			return nil, &Error{Code: http.StatusConflict, Msg: "保存前にファイルが外部で変更されました。外部の変更を確認してください"}
		default:
			return nil, err
		}
	}
	a.HandleChanges([]string{in.Path}, false)
	return &SaveFileResult{OK: true, Hash: hash}, nil
}
