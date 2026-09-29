package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ymotongpoo/reviewer/internal/agent"
	"github.com/ymotongpoo/reviewer/internal/anchor"
	annotationdoc "github.com/ymotongpoo/reviewer/internal/annotate"
	"github.com/ymotongpoo/reviewer/internal/config"
	"github.com/ymotongpoo/reviewer/internal/project"
	"github.com/ymotongpoo/reviewer/internal/store"
	"github.com/ymotongpoo/reviewer/internal/textutil"
)

const (
	AnnotationTargetNew     = "new"
	AnnotationTargetBound   = "bound"
	AnnotationTargetSession = "session"
)

// AnnotateInput starts an agent annotation request. An empty Paths means all
// reviewable files, and an empty Target means a new session.
type AnnotateInput struct {
	Preset    string   `json:"preset,omitempty"`
	Prompt    string   `json:"prompt,omitempty"`
	Paths     []string `json:"paths,omitempty"`
	Target    string   `json:"target,omitempty"`
	SessionID string   `json:"sessionId,omitempty"`
}

// AnnotateResult is returned after the agent run starts.
type AnnotateResult struct {
	Request  *store.AnnotationRequest `json:"request"`
	AgentRun *store.AgentRun          `json:"agentRun"`
}

// Annotate snapshots the target files, writes instructions.md, and starts an
// agent run.
func (a *App) Annotate(ctx context.Context, in AnnotateInput) (*AnnotateResult, error) {
	ag, err := a.agentOrErr()
	if err != nil {
		return nil, err
	}
	target := in.Target
	if target == "" {
		target = AnnotationTargetNew
	}
	prompt, preset, err := a.annotationPrompt(in.Preset, in.Prompt)
	if err != nil {
		return nil, err
	}
	sessionID, err := a.annotationSession(ctx, ag, target, in.SessionID)
	if err != nil {
		return nil, err
	}

	a.mu.Lock()
	if err := a.rescan(); err != nil {
		a.mu.Unlock()
		return nil, err
	}
	paths, files, err := a.annotationSnapshot(in.Paths)
	if err != nil {
		a.mu.Unlock()
		return nil, err
	}
	id := a.Store.NewRequestID()
	req := &store.AnnotationRequest{
		ID: id, Preset: preset, Prompt: prompt, Files: files, Target: target,
		SessionID: sessionID, CreatedAt: a.now(),
	}
	if err := a.Store.PutRequest(req); err != nil {
		a.mu.Unlock()
		return nil, err
	}
	absPaths := make([]string, len(paths))
	for i, path := range paths {
		absPaths[i] = filepath.Join(a.Proj.Root, filepath.FromSlash(path))
	}
	output := filepath.Join(a.Store.RequestDir(id), "annotations.json")
	instructions := annotationdoc.Instructions(annotationdoc.InstructionData{
		RequestID: id, Prompt: prompt, Files: absPaths, OutputPath: output,
	})
	instructionsPath, err := a.Store.WriteRequestFile(id, "instructions.md", instructions)
	a.mu.Unlock()
	if err != nil {
		return nil, err
	}

	if target == AnnotationTargetNew {
		if creator, ok := ag.(agent.SessionCreator); ok {
			name := preset
			if name == "" {
				name = "AI確認"
			}
			session, err := creator.CreateSession(ctx, fmt.Sprintf("reviewer: %s %s", id, name))
			if err != nil {
				return nil, &Error{Code: http.StatusBadGateway, Msg: "新しいセッションを作成できませんでした: " + err.Error()}
			}
			sessionID = session.ID
			a.mu.Lock()
			req.SessionID = sessionID
			err = a.Store.PutRequest(req)
			a.mu.Unlock()
			if err != nil {
				return nil, err
			}
		}
	}
	promptText := autoSentHeader + fmt.Sprintf("`%s` の確認依頼を読み、指示に従って annotations.json を書いてください。", instructionsPath)
	sum := sha256.Sum256([]byte(a.Proj.Root))
	key := fmt.Sprintf("reviewer-%s-%s", hex.EncodeToString(sum[:6]), strings.ToLower(id))
	run, err := ag.Start(ctx, agent.Request{SessionID: sessionID, Prompt: promptText, IdempotencyKey: key})
	if err != nil {
		return nil, &Error{Code: http.StatusBadGateway, Msg: "エージェントに送信できませんでした: " + err.Error()}
	}
	rec := &store.AgentRun{
		ID: run.ID(), Kind: ag.Kind(), Purpose: "annotate", Request: id,
		SessionID: sessionID, Status: store.RunRunning, StartedAt: a.now(),
	}
	a.saveRun(rec)
	a.follow(rec, run)
	if target != AnnotationTargetNew {
		msg := fmt.Sprintf("🔎 reviewer: %s の確認依頼を送りました。", id)
		if u := a.agents.baseURL; u != "" {
			msg += "\n進捗: " + u
		}
		go a.sendNotice(rec, msg)
	}
	return &AnnotateResult{Request: req, AgentRun: rec}, nil
}

func (a *App) annotationPrompt(name, prompt string) (string, string, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt != "" {
		return prompt, strings.TrimSpace(name), nil
	}
	if name == "" {
		name = config.BuiltinPresets()[0].Name
	}
	for _, p := range a.Presets() {
		if p.Name == name {
			return p.Prompt, p.Name, nil
		}
	}
	return "", "", badRequest("プリセットがありません: %s", name)
}

func (a *App) annotationSession(ctx context.Context, ag agent.Agent, target, explicit string) (string, error) {
	switch target {
	case AnnotationTargetNew:
		return "", nil
	case AnnotationTargetBound:
		a.mu.Lock()
		binding := a.Store.AgentBinding()
		a.mu.Unlock()
		if binding == nil || binding.Kind != ag.Kind() {
			return "", badRequest("送信先のセッションが選ばれていません")
		}
		return binding.SessionID, nil
	case AnnotationTargetSession:
		if explicit == "" {
			return "", badRequest("送信先のセッションを指定してください")
		}
		sessions, err := a.AgentSessions(ctx)
		if err != nil {
			return "", err
		}
		for _, session := range sessions {
			if session.ID == explicit {
				return explicit, nil
			}
		}
		return "", notFound("セッションが見つかりません: %s", explicit)
	default:
		return "", badRequest("不明な送信先です: %s", target)
	}
}

func (a *App) annotationSnapshot(requested []string) ([]string, map[string]string, error) {
	known := map[string]bool{}
	for _, f := range a.files {
		known[f.Path] = true
	}
	paths := append([]string(nil), requested...)
	if len(paths) == 0 {
		for _, f := range a.files {
			paths = append(paths, f.Path)
		}
	}
	sort.Strings(paths)
	paths = compactStrings(paths)
	if len(paths) == 0 {
		return nil, nil, badRequest("確認対象のファイルがありません")
	}
	files := make(map[string]string, len(paths))
	for _, path := range paths {
		if !known[path] {
			return nil, nil, badRequest("確認対象にできないファイルです: %s", path)
		}
		b, err := a.readFile(path)
		if err != nil {
			return nil, nil, err
		}
		h, err := a.Store.PutBlob(b)
		if err != nil {
			return nil, nil, err
		}
		files[path] = h
	}
	return paths, files, nil
}

func compactStrings(v []string) []string {
	if len(v) == 0 {
		return v
	}
	out := v[:1]
	for _, s := range v[1:] {
		if s != out[len(out)-1] {
			out = append(out, s)
		}
	}
	return out
}

// AnnotationRequestView includes the agent runs and events of a request.
type AnnotationRequestView struct {
	*store.AnnotationRequest
	Runs []AgentRunView `json:"runs"`
}

// AnnotationRequests lists all requests, newest first.
func (a *App) AnnotationRequests() []*store.AnnotationRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := a.Store.Requests()
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// AnnotationRequest returns one request with its runs.
func (a *App) AnnotationRequest(id string) (*AnnotationRequestView, error) {
	a.mu.Lock()
	req, err := a.Store.Request(id)
	a.mu.Unlock()
	if errors.Is(err, fs.ErrNotExist) {
		return nil, notFound("確認依頼がありません: %s", id)
	}
	if err != nil {
		return nil, err
	}
	return &AnnotationRequestView{AnnotationRequest: req, Runs: a.AnnotationAgentRuns(id)}, nil
}

// SetRequestHidden changes whether a request is shown with its annotations.
func (a *App) SetRequestHidden(id string, hidden bool) (*store.AnnotationRequest, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	req, err := a.Store.Request(id)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, notFound("確認依頼がありません: %s", id)
	}
	if err != nil {
		return nil, err
	}
	req.Hidden = hidden
	if err := a.Store.PutRequest(req); err != nil {
		return nil, err
	}
	a.notify(Event{Type: "annotate", Request: id})
	a.notify(Event{Type: "annotations", Request: id})
	return req, nil
}

// ImportAnnotations imports one annotations.json when it changed.
func (a *App) ImportAnnotations(id string) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.importAnnotations(id)
}

func (a *App) importAllAnnotations() {
	for _, req := range a.Store.Requests() {
		if _, err := a.importAnnotations(req.ID); err != nil {
			continue
		}
	}
}

func (a *App) importAnnotations(id string) (bool, error) {
	req, err := a.Store.Request(id)
	if err != nil {
		return false, err
	}
	b, err := os.ReadFile(filepath.Join(a.Store.RequestDir(id), "annotations.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	h := project.HashBytes(b)
	if req.Import != nil && req.Import.Hash == h {
		return false, nil
	}
	snapshots := map[string]annotationdoc.Snapshot{}
	for path, blob := range req.Files {
		content, err := a.Store.Blob(blob)
		if err != nil {
			continue
		}
		snapshots[path] = annotationdoc.Snapshot{Blob: blob, Lines: textutil.SplitLines(string(content))}
	}
	info := &store.AnnotationImport{Hash: h, ImportedAt: a.now()}
	resp, warnings, parseErr := annotationdoc.ParseResponse(b, id, snapshots)
	if parseErr != nil {
		info.Error = parseErr.Error()
	} else {
		for _, old := range a.Store.Annotations() {
			if old.Request == id && old.State == store.AnnotationPending {
				old.State = store.AnnotationDismissed
				old.UpdatedAt = a.now()
				if err := a.Store.PutAnnotation(old); err != nil {
					return false, err
				}
			}
		}
		info.Summary, info.Warnings = resp.Summary, warnings
		for _, parsed := range resp.Annotations {
			ann := a.storedAnnotation(id, parsed, snapshots[parsed.Path])
			if err := a.Store.PutAnnotation(ann); err != nil {
				return false, err
			}
			info.Count++
		}
	}
	req.Import = info
	if err := a.Store.PutRequest(req); err != nil {
		return false, err
	}
	a.reanchorAnnotations(nil)
	a.notify(Event{Type: "annotations", Request: id})
	a.notify(Event{Type: "annotate", Request: id})
	return true, nil
}

func (a *App) storedAnnotation(request string, p annotationdoc.Annotation, snap annotationdoc.Snapshot) *store.Annotation {
	now := a.now()
	ann := &store.Annotation{
		ID: a.Store.NewAnnotationID(), Request: request, Path: p.Path,
		OrigStart: p.StartLine, OrigEnd: p.EndLine, OrigBlob: snap.Blob,
		Severity: p.Severity, Confidence: p.Confidence, Label: p.Label,
		Body: p.Body, Suggestion: p.Suggestion, State: store.AnnotationPending,
		Evidence: []store.Evidence{}, CreatedAt: now, UpdatedAt: now,
	}
	for _, e := range p.Evidence {
		ann.Evidence = append(ann.Evidence, store.Evidence{URL: e.URL, Quote: e.Quote, Note: e.Note})
	}
	if p.Located {
		anc := anchor.New(snap.Lines, p.StartLine, p.EndLine)
		ann.Anchor = &anc
		ann.Loc = &store.Location{Start: p.StartLine, End: p.EndLine, State: anchor.Exact, Blob: snap.Blob, Anchor: anc}
	} else {
		anc := anchor.Anchor{Lines: append([]string(nil), p.Quote...), Hash: anchor.Hash(p.Quote)}
		ann.Anchor = &anc
		ann.Loc = &store.Location{Start: p.ReportedStart, End: p.ReportedEnd, State: anchor.Outdated, Blob: snap.Blob, Anchor: anc}
	}
	return ann
}

func (a *App) finishAnnotationRun(rec *store.AgentRun) {
	a.mu.Lock()
	req, err := a.Store.Request(rec.Request)
	var summary string
	hasResponse := false
	if err == nil {
		now := a.now()
		req.CompletedAt = &now
		req.ChangedPaths = a.changedRequestPaths(req)
		a.Store.PutRequest(req)
		a.importAnnotations(req.ID)
		req, _ = a.Store.Request(req.ID)
		if req != nil && req.Import != nil {
			hasResponse = req.Import.Error == "" && req.Import.ImportedAt.After(rec.StartedAt.Add(-time.Second))
			summary = req.Import.Summary
		}
	}
	a.mu.Unlock()
	a.agents.mu.Lock()
	rec.NoResponse = rec.Status == agent.StatusCompleted && !hasResponse
	status, errText, text, noResp := rec.Status, rec.Error, rec.Text, rec.NoResponse
	a.agents.mu.Unlock()
	a.saveRun(rec)
	a.notify(a.agentEvent(rec, nil))
	if req == nil || req.Target == AnnotationTargetNew {
		return
	}
	var msg string
	switch status {
	case agent.StatusCompleted:
		msg = fmt.Sprintf("✅ reviewer: %s の確認が終わりました。", rec.Request)
		if summary == "" {
			summary = firstLines(text, 300)
		}
		if summary != "" {
			msg += "\n> " + strings.ReplaceAll(summary, "\n", "\n> ")
		}
		if noResp {
			msg += "\n（annotations.json が書かれていないため、指摘はありません）"
		}
	case agent.StatusCancelled:
		msg = fmt.Sprintf("⏹ reviewer: %s の確認を停止しました。", rec.Request)
	default:
		msg = fmt.Sprintf("⚠️ reviewer: %s の確認が途中で終わりました。", rec.Request)
		if errText != "" {
			msg += "（" + errText + "）"
		}
	}
	a.sendNotice(rec, msg)
}

func (a *App) changedRequestPaths(req *store.AnnotationRequest) []string {
	var changed []string
	for path, before := range req.Files {
		b, err := a.Proj.Read(path)
		if err != nil || project.HashBytes(b) != before {
			changed = append(changed, path)
		}
	}
	sort.Strings(changed)
	return changed
}

// Annotations returns pending annotations whose requests are visible.
func (a *App) Annotations() []*store.Annotation {
	a.mu.Lock()
	defer a.mu.Unlock()
	hidden := map[string]bool{}
	for _, req := range a.Store.Requests() {
		hidden[req.ID] = req.Hidden
	}
	var out []*store.Annotation
	for _, ann := range a.Store.Annotations() {
		if ann.State == store.AnnotationPending && !hidden[ann.Request] {
			out = append(out, ann)
		}
	}
	return out
}

// AdoptAnnotation turns a pending annotation into a draft human comment.
func (a *App) AdoptAnnotation(id string, patch AnnotationAdoptPatch) (*store.Comment, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ann, ok := a.Store.Annotation(id)
	if !ok {
		return nil, notFound("AI指摘がありません: %s", id)
	}
	if ann.State != store.AnnotationPending {
		return nil, conflict("このAI指摘は採用できません: %s", ann.State)
	}
	label := ann.Label
	if !a.validLabel(label) {
		label = map[string]string{"critical": "must", "major": "must", "minor": "suggestion", "info": "question"}[ann.Severity]
	}
	if patch.Label != nil {
		label = *patch.Label
	}
	if !a.validLabel(label) {
		return nil, badRequest("不明なラベルです: %s", label)
	}
	body := annotationBody(ann)
	if patch.Body != nil {
		body = *patch.Body
	}
	newComment := NewComment{Scope: store.ScopeFile, Path: ann.Path, Label: label, Body: body}
	if ann.Loc != nil && ann.Loc.State != anchor.Outdated {
		newComment.Scope = store.ScopeLine
		newComment.Start, newComment.End, newComment.Hash = ann.Loc.Start, ann.Loc.End, ann.Loc.Blob
	} else {
		body = withUnlocatedQuote(body, ann)
		newComment.Body = body
	}
	c, err := a.createComment(newComment)
	if err != nil {
		return nil, err
	}
	ann.State, ann.AdoptedAs, ann.UpdatedAt = store.AnnotationAdopted, c.ID, a.now()
	if err := a.Store.PutAnnotation(ann); err != nil {
		return nil, err
	}
	a.notify(Event{Type: "annotations", Paths: nonEmpty(ann.Path), Request: ann.Request})
	return c, nil
}

// AnnotationAdoptPatch optionally replaces the generated comment fields.
type AnnotationAdoptPatch struct {
	Label *string `json:"label"`
	Body  *string `json:"body"`
}

func annotationBody(ann *store.Annotation) string {
	parts := []string{strings.TrimSpace(ann.Body)}
	if ann.Suggestion != "" {
		parts = append(parts, "```suggestion\n"+ann.Suggestion+"\n```")
	}
	if len(ann.Evidence) > 0 {
		var b strings.Builder
		b.WriteString("根拠\n\n")
		for i, e := range ann.Evidence {
			label := fmt.Sprintf("根拠 %d", i+1)
			if e.URL != "" {
				fmt.Fprintf(&b, "- [%s](%s)", label, e.URL)
			} else {
				fmt.Fprintf(&b, "- %s", label)
			}
			if e.Note != "" {
				fmt.Fprintf(&b, "（%s）", e.Note)
			}
			b.WriteByte('\n')
			if e.Quote != "" {
				for _, line := range strings.Split(e.Quote, "\n") {
					b.WriteString("  > " + line + "\n")
				}
			}
		}
		parts = append(parts, strings.TrimSpace(b.String()))
	}
	return strings.Join(parts, "\n\n")
}

func withUnlocatedQuote(body string, ann *store.Annotation) string {
	if ann.Anchor == nil || len(ann.Anchor.Lines) == 0 {
		return body
	}
	var b strings.Builder
	b.WriteString(strings.TrimSpace(body))
	b.WriteString("\n\n対象箇所（位置不明）\n\n")
	for _, line := range ann.Anchor.Lines {
		b.WriteString("> " + line + "\n")
	}
	return strings.TrimSpace(b.String())
}

// SetAnnotationState dismisses or restores a pending annotation.
func (a *App) SetAnnotationState(id, state string) (*store.Annotation, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ann, ok := a.Store.Annotation(id)
	if !ok {
		return nil, notFound("AI指摘がありません: %s", id)
	}
	if ann.State == store.AnnotationAdopted {
		return nil, conflict("採用済みのAI指摘の状態は変更できません")
	}
	if state != store.AnnotationPending && state != store.AnnotationDismissed {
		return nil, badRequest("この状態には変更できません: %s", state)
	}
	ann.State, ann.UpdatedAt = state, a.now()
	if err := a.Store.PutAnnotation(ann); err != nil {
		return nil, err
	}
	a.notify(Event{Type: "annotations", Paths: nonEmpty(ann.Path), Request: ann.Request})
	return ann, nil
}

// DiscardRequest dismisses all pending annotations of a request.
func (a *App) DiscardRequest(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, err := a.Store.Request(id); errors.Is(err, fs.ErrNotExist) {
		return notFound("確認依頼がありません: %s", id)
	} else if err != nil {
		return err
	}
	paths := []string{}
	for _, ann := range a.Store.Annotations() {
		if ann.Request != id || ann.State != store.AnnotationPending {
			continue
		}
		ann.State, ann.UpdatedAt = store.AnnotationDismissed, a.now()
		if err := a.Store.PutAnnotation(ann); err != nil {
			return err
		}
		paths = append(paths, ann.Path)
	}
	a.notify(Event{Type: "annotations", Paths: paths, Request: id})
	return nil
}

func (a *App) reanchorAnnotations(paths []string) {
	want := map[string]bool{}
	for _, path := range paths {
		want[path] = true
	}
	contents := map[string][]byte{}
	var changed []string
	for _, ann := range a.Store.Annotations() {
		if ann.State != store.AnnotationPending || ann.Loc == nil {
			continue
		}
		if paths != nil && !want[ann.Path] {
			continue
		}
		b, ok := contents[ann.Path]
		if !ok {
			b, _ = a.Proj.Read(ann.Path)
			contents[ann.Path] = b
		}
		prev := *ann.Loc
		if b == nil {
			ann.Loc.State = anchor.Outdated
		} else {
			h, err := a.Store.PutBlob(b)
			if err != nil || h == ann.Loc.Blob {
				continue
			}
			a.reanchorAnnotation(ann, textutil.SplitLines(string(b)), h)
		}
		if prev.State != ann.Loc.State || prev.Start != ann.Loc.Start || prev.End != ann.Loc.End || prev.Blob != ann.Loc.Blob {
			ann.UpdatedAt = a.now()
			a.Store.PutAnnotation(ann)
			changed = append(changed, ann.Path)
		}
	}
	if len(changed) > 0 {
		a.notify(Event{Type: "annotations", Paths: changed})
	}
}

func (a *App) reanchorAnnotation(ann *store.Annotation, newLines []string, hash string) {
	var oldLines []string
	if b, err := a.Store.Blob(ann.Loc.Blob); err == nil {
		oldLines = textutil.SplitLines(string(b))
	}
	res := anchor.Resolve(ann.Loc.Anchor, oldLines, ann.Loc.Start, ann.Loc.End, newLines, a.threshold())
	if res.State == anchor.Outdated && ann.Anchor != nil {
		var original []string
		if b, err := a.Store.Blob(ann.OrigBlob); err == nil {
			original = textutil.SplitLines(string(b))
		}
		res = anchor.Resolve(*ann.Anchor, original, ann.OrigStart, ann.OrigEnd, newLines, a.threshold())
	}
	if res.State == anchor.Outdated {
		ann.Loc.State = anchor.Outdated
		return
	}
	loc := anchor.New(newLines, res.Start, res.End)
	state := anchor.Exact
	switch {
	case ann.Anchor != nil && loc.Hash != ann.Anchor.Hash:
		state = anchor.Fuzzy
	case res.Start != ann.OrigStart:
		state = anchor.Moved
	}
	ann.Loc = &store.Location{Start: res.Start, End: res.End, State: state, Blob: hash, Anchor: loc}
}

// RequestDiff is the line diff of a target file since the request snapshot.
type RequestDiff struct {
	Request string        `json:"request"`
	Path    string        `json:"path"`
	Kind    string        `json:"kind"`
	Ops     []textutil.Op `json:"ops"`
}

// AnnotationRequestDiff returns how path changed since the request started.
func (a *App) AnnotationRequestDiff(id, path string) (*RequestDiff, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	req, err := a.Store.Request(id)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, notFound("確認依頼がありません: %s", id)
	}
	if err != nil {
		return nil, err
	}
	before, ok := req.Files[path]
	if !ok {
		return nil, notFound("確認依頼%sの対象にファイルがありません: %s", id, path)
	}
	after, kind := "", ChangeModified
	b, readErr := a.Proj.Read(path)
	if errors.Is(readErr, fs.ErrNotExist) {
		kind = ChangeDeleted
	} else if readErr != nil {
		return nil, readErr
	} else {
		after, err = a.Store.PutBlob(b)
		if err != nil {
			return nil, err
		}
		if before == after {
			kind = ChangeUnchanged
		}
	}
	return &RequestDiff{Request: id, Path: path, Kind: kind, Ops: textutil.LineDiff(a.blobLines(before), a.blobLines(after))}, nil
}

// PresetView identifies where a merged preset came from.
type PresetView struct {
	config.Preset
	Origin string `json:"origin"`
}

// Presets merges built-in, global, and project presets by name. Later origins
// override earlier ones while retaining a stable order.
func (a *App) Presets() []PresetView {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.presets()
}

func (a *App) presets() []PresetView {
	builtin := config.BuiltinPresets()
	var candidates []PresetView
	for _, p := range builtin {
		candidates = append(candidates, PresetView{Preset: p, Origin: "builtin"})
	}
	for i, p := range a.Cfg.Presets {
		if i < len(builtin) && p == builtin[i] {
			continue
		}
		candidates = append(candidates, PresetView{Preset: p, Origin: "global"})
	}
	if projectPresets, err := a.Store.ProjectPresets(); err == nil {
		for _, p := range projectPresets {
			candidates = append(candidates, PresetView{Preset: p, Origin: "project"})
		}
	}
	index := map[string]int{}
	out := []PresetView{}
	for _, p := range candidates {
		if i, ok := index[p.Name]; ok {
			out[i] = p
		} else {
			index[p.Name] = len(out)
			out = append(out, p)
		}
	}
	return out
}

// SaveProjectPresets replaces the editable project presets.
func (a *App) SaveProjectPresets(presets []config.Preset) ([]PresetView, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	seen := map[string]bool{}
	for i := range presets {
		presets[i].Name = strings.TrimSpace(presets[i].Name)
		presets[i].Prompt = strings.TrimSpace(presets[i].Prompt)
		if presets[i].Scope == "" {
			presets[i].Scope = "all"
		}
		if presets[i].Name == "" || presets[i].Prompt == "" {
			return nil, badRequest("プリセットの name と prompt は必須です")
		}
		if seen[presets[i].Name] {
			return nil, badRequest("プリセット名が重複しています: %s", presets[i].Name)
		}
		seen[presets[i].Name] = true
		switch presets[i].Scope {
		case "all", "selected", "current":
		default:
			return nil, badRequest("不明なプリセットの scope です: %s", presets[i].Scope)
		}
	}
	if err := a.Store.SaveProjectPresets(presets); err != nil {
		return nil, err
	}
	a.notify(Event{Type: "annotate"})
	return a.presets(), nil
}
