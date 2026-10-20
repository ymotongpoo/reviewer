package app

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/ymotongpoo/reviewer/internal/store"
)

const (
	MaxAttachmentSize            = 10 << 20
	maxAttachmentsPerComment     = 5
	maxAttachmentBytesPerEntry   = 10 << 20
	maxAttachmentBytesPerComment = 20 << 20
	maxAttachmentPixels          = 40_000_000
)

// UploadAttachment validates and stores one image in the session data directory.
func (a *App) UploadAttachment(name string, data []byte) (store.Attachment, error) {
	if len(data) == 0 || len(data) > MaxAttachmentSize {
		return store.Attachment{}, badRequest("画像は1バイト以上%d MiB以下にしてください", MaxAttachmentSize>>20)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return store.Attachment{}, badRequest("PNG、JPEG、GIFの画像を選んでください")
	}
	if cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > maxAttachmentPixels {
		return store.Attachment{}, badRequest("画像の縦横サイズが大きすぎます")
	}
	ext := map[string]string{"png": "png", "jpeg": "jpg", "gif": "gif"}[format]
	if ext == "" {
		return store.Attachment{}, badRequest("PNG、JPEG、GIFの画像を選んでください")
	}
	id, err := a.Store.PutAttachment(data, ext)
	if err != nil {
		return store.Attachment{}, err
	}
	return store.Attachment{ID: id, Name: cleanAttachmentName(name, ext), Size: int64(len(data))}, nil
}

// Attachment reads a stored image for the authenticated preview endpoint.
func (a *App) Attachment(id string) ([]byte, string, error) {
	b, mediaType, err := a.Store.ReadAttachment(id)
	if err != nil {
		return nil, "", notFound("画像がありません")
	}
	return b, mediaType, nil
}

func (a *App) validateAttachments(in []store.Attachment) ([]store.Attachment, error) {
	if len(in) > maxAttachmentsPerComment {
		return nil, badRequest("1件のコメントに添付できる画像は%d枚までです", maxAttachmentsPerComment)
	}
	out := make([]store.Attachment, 0, len(in))
	seen := make(map[string]bool, len(in))
	var total int64
	for _, att := range in {
		if seen[att.ID] {
			return nil, badRequest("同じ画像を重複して添付できません")
		}
		seen[att.ID] = true
		b, _, err := a.Store.ReadAttachment(att.ID)
		if err != nil {
			return nil, badRequest("添付画像が見つかりません")
		}
		if len(b) > maxAttachmentBytesPerEntry {
			return nil, badRequest("添付画像は1枚%d MiB以下にしてください", maxAttachmentBytesPerEntry>>20)
		}
		total += int64(len(b))
		if total > maxAttachmentBytesPerComment {
			return nil, badRequest("添付画像の合計は%d MiB以下にしてください", maxAttachmentBytesPerComment>>20)
		}
		ext := strings.TrimPrefix(filepath.Ext(att.ID), ".")
		att.Name = cleanAttachmentName(att.Name, ext)
		att.Size = int64(len(b))
		out = append(out, att)
	}
	return out, nil
}

func cleanAttachmentName(name, ext string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	var clean strings.Builder
	for _, r := range strings.TrimSpace(name) {
		if unicode.IsControl(r) || r == '/' || r == '\\' {
			continue
		}
		if clean.Len() >= 180 {
			break
		}
		clean.WriteRune(r)
	}
	name = strings.TrimSpace(clean.String())
	if name == "" || name == "." {
		return fmt.Sprintf("image.%s", ext)
	}
	return name
}
