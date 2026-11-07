package server

import (
	"github.com/ymotongpoo/reviewer/internal/app"
	"net/http"
)

func (p *Project) handlePDFMatch(w http.ResponseWriter, r *http.Request) {
	var in app.PDFInput
	if err := decode(r, &in); err != nil {
		writeError(w, err)
		return
	}
	out, err := p.App.MatchPDF(in)
	respond(w, map[string]any{"matches": out}, err)
}

func (p *Project) handlePDFComment(w http.ResponseWriter, r *http.Request) {
	var in app.PDFCommentInput
	if err := decode(r, &in); err != nil {
		writeError(w, err)
		return
	}
	out, err := p.App.ImportPDFComment(in)
	respond(w, out, err)
}
