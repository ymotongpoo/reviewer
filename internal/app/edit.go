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

// SaveFile writes a text file after checking that it has not changed since it
// was opened. The normal watcher path then re-anchors comments and refreshes
// connected clients.
func (a *App) SaveFile(in SaveFileInput) error {
	if in.Path == "" {
		return badRequest("ファイルのパスがありません")
	}
	a.mu.Lock()
	err := a.Proj.Write(in.Path, in.Hash, []byte(in.Content))
	a.mu.Unlock()
	if err != nil {
		switch {
		case errors.Is(err, project.ErrOutside):
			return badRequest("編集できないパスです: %s", in.Path)
		case errors.Is(err, project.ErrNotText):
			return badRequest("テキストファイルではないか、大きすぎます")
		case errors.Is(err, fs.ErrNotExist):
			return notFound("ファイルがありません: %s", in.Path)
		case err.Error() == "file changed since it was opened":
			return &Error{Code: http.StatusConflict, Msg: "保存前にファイルが外部で変更されました。再読み込みして内容を確認してください"}
		default:
			return err
		}
	}
	a.HandleChanges([]string{in.Path}, false)
	return nil
}
