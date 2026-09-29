// Command fakehermes serves a fake Hermes Agent API for trying reviewer's
// agent integration without Hermes. It answers every comment of the round.
//
//	HERMES_HOME=/tmp/fh fakehermes -key 0123456789abcdef0123 &
//	HERMES_HOME=/tmp/fh reviewer serve ./docs
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ymotongpoo/reviewer/internal/agent/hermes/hermestest"
)

var feedbackRe = regexp.MustCompile("`([^`]+/feedback\\.md)`")

func main() {
	addr := flag.String("addr", "127.0.0.1:8642", "listen address")
	key := flag.String("key", "0123456789abcdef0123", "API key")
	approval := flag.Bool("approval", true, "ask for an approval during each run")
	writeEnv := flag.Bool("write-env", true, "write $HERMES_HOME/.env so that reviewer detects this server")
	modifyDefault := os.Getenv("FAKEHERMES_MODIFY_TARGET") == "1" || strings.EqualFold(os.Getenv("FAKEHERMES_MODIFY_TARGET"), "true")
	modifyAnnotationTarget := flag.Bool("modify-annotation-target", modifyDefault, "modify one target file after an annotation request")
	flag.Parse()

	if home := os.Getenv("HERMES_HOME"); home != "" && *writeEnv {
		os.MkdirAll(home, 0o755)
		_, port, _ := splitHostPort(*addr)
		env := fmt.Sprintf("API_SERVER_ENABLED=true\nAPI_SERVER_KEY=%s\nAPI_SERVER_PORT=%s\n", *key, port)
		if err := os.WriteFile(filepath.Join(home, ".env"), []byte(env), 0o600); err != nil {
			log.Fatal(err)
		}
	}

	now := float64(time.Now().Unix())
	f := &hermestest.Fake{
		Key:                    *key,
		Keepalive:              time.Second,
		ModifyAnnotationTarget: *modifyAnnotationTarget,
		Sessions: []map[string]any{
			{"id": "20260922_021", "source": "discord", "title": "執筆候補1〜3番目をzenn.dev記事化", "last_active": now - 600, "preview": "記事の構成を…"},
			{"id": "20260918_054", "source": "discord", "title": "サイトの登壇履歴を確認", "last_active": now - 86400},
			{"id": "cli_1", "source": "cli", "title": "ローカルで作業", "last_active": now - 3600},
		},
		Script: func(input string) []hermestest.Step {
			if strings.Contains(input, "/instructions.md`") {
				return []hermestest.Step{
					{Event: "message.delta", Fields: map[string]any{"delta": "確認依頼を受け取りました。"}, Delay: 500 * time.Millisecond},
					{Event: "tool.started", Fields: map[string]any{"tool": "read_file", "preview": "instructions.md"}, Delay: 300 * time.Millisecond},
					{Event: "tool.completed", Fields: map[string]any{"tool": "read_file", "duration": 0.2, "error": false}, Delay: 300 * time.Millisecond},
					{Event: "message.delta", Fields: map[string]any{"delta": "annotations.json に指摘を書きました。"}, Delay: 400 * time.Millisecond},
				}
			}
			steps := []hermestest.Step{
				{Event: "message.delta", Fields: map[string]any{"delta": "レビューを受け取りました。"}, Delay: 500 * time.Millisecond},
				{Event: "tool.started", Fields: map[string]any{"tool": "read_file", "preview": "feedback.md"}, Delay: 300 * time.Millisecond},
				{Event: "tool.completed", Fields: map[string]any{"tool": "read_file", "duration": 0.2, "error": false}, Delay: 300 * time.Millisecond},
			}
			if *approval {
				steps = append(steps, hermestest.Step{Approval: true, Fields: map[string]any{"command": "git checkout -- drafts/", "description": "未保存の変更を破棄する"}})
			}
			steps = append(steps,
				hermestest.Step{Event: "tool.started", Fields: map[string]any{"tool": "patch", "preview": "articles/intro.md"}, Delay: 800 * time.Millisecond},
				hermestest.Step{Event: "tool.completed", Fields: map[string]any{"tool": "patch", "duration": 0.4, "error": false}, Delay: 300 * time.Millisecond},
				hermestest.Step{Event: "message.delta", Fields: map[string]any{"delta": "指摘に沿って修正し、"}, Delay: 400 * time.Millisecond},
				hermestest.Step{Event: "message.delta", Fields: map[string]any{"delta": "response.json に返答を書きました。"}, Delay: 400 * time.Millisecond},
			)
			return steps
		},
		OnRun: func(input, _ string) {
			if strings.Contains(input, "/instructions.md`") {
				return
			}
			m := feedbackRe.FindStringSubmatch(input)
			if m == nil {
				log.Printf("no feedback path in prompt")
				return
			}
			dir := filepath.Dir(m[1])
			var fb struct {
				Round    int `json:"round"`
				Comments []struct {
					ID string `json:"id"`
				} `json:"comments"`
			}
			b, err := os.ReadFile(filepath.Join(dir, "feedback.json"))
			if err != nil {
				log.Print(err)
				return
			}
			json.Unmarshal(b, &fb)
			var rs []map[string]string
			for _, c := range fb.Comments {
				rs = append(rs, map[string]string{"id": c.ID, "status": "addressed", "message": "（fakehermes）対応しました"})
			}
			out, _ := json.Marshal(map[string]any{"round": fb.Round, "responses": rs, "summary": "fakehermes がすべてのコメントに対応済みと返答しました"})
			if err := os.WriteFile(filepath.Join(dir, "response.json"), out, 0o644); err != nil {
				log.Print(err)
			}
		},
	}
	log.Printf("fake Hermes API on http://%s (key %s)", *addr, *key)
	log.Fatal(http.ListenAndServe(*addr, logRequests(f)))
}

func splitHostPort(addr string) (string, string, error) {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i], addr[i+1:], nil
		}
	}
	return addr, "8642", nil
}

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		h.ServeHTTP(w, r)
	})
}
