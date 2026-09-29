package hermes

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestMain lets the test binary act as `hermes mcp serve`.
func TestMain(m *testing.M) {
	if os.Getenv("FAKE_HERMES_MCP") == "1" {
		fakeMCPServer()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeMCPServer speaks just enough MCP. The Discord ids exceed float64
// precision on purpose.
func fakeMCPServer() {
	out := bufio.NewWriter(os.Stdout)
	sc := bufio.NewScanner(os.Stdin)
	reply := func(id any, result any) {
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
		out.Write(append(b, '\n'))
		out.Flush()
	}
	text := func(v any) map[string]any {
		b, _ := json.Marshal(v)
		return map[string]any{"content": []map[string]any{{"type": "text", "text": string(b)}}}
	}
	fmt.Fprintln(out, `{"jsonrpc":"2.0","method":"notifications/message","params":{"level":"info"}}`)
	out.Flush()
	for sc.Scan() {
		var msg struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		json.Unmarshal(sc.Bytes(), &msg)
		switch msg.Method {
		case "initialize":
			reply(msg.ID, map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}})
		case "tools/call":
			switch msg.Params.Name {
			case "conversations_list":
				reply(msg.ID, text(map[string]any{"count": 1, "conversations": []map[string]any{
					{"session_key": "agent:main:discord:thread:1", "session_id": "sess-1", "platform": "discord"}}}))
			case "conversation_get":
				// Raw JSON so the ids stay exact.
				reply(msg.ID, map[string]any{"content": []map[string]any{{"type": "text",
					"text": `{"platform":"discord","chat_id":"1549644000000000001","thread_id":1549644123456789012}`}}})
			case "messages_send":
				if f := os.Getenv("FAKE_HERMES_MCP_LOG"); f != "" {
					b, _ := json.Marshal(msg.Params.Arguments)
					os.WriteFile(f, b, 0o644)
				}
				reply(msg.ID, text(map[string]any{"success": true}))
			}
		}
	}
}

func TestNotifier(t *testing.T) {
	log := t.TempDir() + "/sent.json"
	t.Setenv("FAKE_HERMES_MCP", "1")
	t.Setenv("FAKE_HERMES_MCP_LOG", log)
	n := NewNotifier(os.Args[0], "")
	if err := n.Notify(context.Background(), "sess-1", "hello"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	var sent map[string]string
	json.Unmarshal(b, &sent)
	if sent["target"] != "discord:1549644000000000001:1549644123456789012" || sent["message"] != "hello" {
		t.Errorf("sent = %v", sent)
	}
	if err := n.Notify(context.Background(), "unknown", "x"); err == nil || !strings.Contains(err.Error(), "見つかりません") {
		t.Errorf("unknown session: %v", err)
	}
}
