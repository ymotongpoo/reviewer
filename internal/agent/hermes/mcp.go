package hermes

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// mcpClient is a minimal MCP client over the stdio of `hermes mcp serve`.
// It supports just what notifications need: initialize and tools/call.
type mcpClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  chan []byte
	nextID int
}

func startMCP(ctx context.Context, command, home string) (*mcpClient, error) {
	if command == "" {
		return nil, errors.New("hermes コマンドが見つかりません（設定の agent.hermes.command で指定してください）")
	}
	cmd := exec.CommandContext(ctx, command, "mcp", "serve")
	if home != "" {
		// Read the same state as the gateway, not a default home.
		cmd.Env = append(os.Environ(), "HERMES_HOME="+home)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("hermes mcp serve を起動できません: %w", err)
	}
	c := &mcpClient{cmd: cmd, stdin: stdin, lines: make(chan []byte, 16)}
	go func() {
		defer close(c.lines)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
		for sc.Scan() {
			b := append([]byte(nil), sc.Bytes()...)
			c.lines <- b
		}
	}()
	if _, err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "reviewer", "version": "1"},
	}); err != nil {
		c.close()
		return nil, err
	}
	if err := c.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}

func (c *mcpClient) send(msg any) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = c.stdin.Write(append(b, '\n'))
	return err
}

func (c *mcpClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.nextID++
	id := c.nextID
	if err := c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case line, ok := <-c.lines:
			if !ok {
				return nil, errors.New("hermes mcp serve が終了しました")
			}
			var msg struct {
				ID     *int            `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(line, &msg) != nil || msg.ID == nil || *msg.ID != id {
				continue // notifications, logs or other responses
			}
			if msg.Error != nil {
				return nil, fmt.Errorf("MCP %s: %s", method, msg.Error.Message)
			}
			return msg.Result, nil
		}
	}
}

// tool calls an MCP tool and decodes the JSON text it returns into out.
func (c *mcpClient) tool(ctx context.Context, name string, args map[string]any, out any) error {
	raw, err := c.call(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return err
	}
	var res struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return err
	}
	var text strings.Builder
	for _, c := range res.Content {
		if c.Type == "text" {
			text.WriteString(c.Text)
		}
	}
	if res.IsError {
		return fmt.Errorf("%s: %s", name, text.String())
	}
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(text.String()), &e) == nil && e.Error != "" {
		return fmt.Errorf("%s: %s", name, e.Error)
	}
	if out != nil {
		// Discord IDs exceed float64 precision: keep numbers as json.Number.
		dec := json.NewDecoder(strings.NewReader(text.String()))
		dec.UseNumber()
		return dec.Decode(out)
	}
	return nil
}

func (c *mcpClient) close() {
	c.stdin.Close()
	done := make(chan struct{})
	go func() { c.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		c.cmd.Process.Kill()
		<-done
	}
}

// Notifier posts into the platform conversation (e.g. the Discord thread)
// that a Hermes session belongs to, using the gateway's own bot.
type Notifier struct {
	Command string
	Home    string // HERMES_HOME for the subprocess

	mu      sync.Mutex
	targets map[string]string // session id -> "platform:chat_id[:thread_id]"
}

// NewNotifier creates a notifier that runs `<command> mcp serve` with
// HERMES_HOME set to home (when non-empty).
func NewNotifier(command, home string) *Notifier {
	return &Notifier{Command: command, Home: home, targets: map[string]string{}}
}

// Notify implements agent.Notifier.
func (n *Notifier) Notify(ctx context.Context, sessionID, text string) error {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	c, err := startMCP(ctx, n.Command, n.Home)
	if err != nil {
		return err
	}
	defer c.close()
	target, err := n.target(ctx, c, sessionID)
	if err != nil {
		return err
	}
	return c.tool(ctx, "messages_send", map[string]any{"target": target, "message": text}, nil)
}

func (n *Notifier) target(ctx context.Context, c *mcpClient, sessionID string) (string, error) {
	n.mu.Lock()
	t, ok := n.targets[sessionID]
	n.mu.Unlock()
	if ok {
		return t, nil
	}
	var list struct {
		Conversations []struct {
			SessionKey string `json:"session_key"`
			SessionID  string `json:"session_id"`
			Platform   string `json:"platform"`
		} `json:"conversations"`
	}
	if err := c.tool(ctx, "conversations_list", map[string]any{"limit": 200}, &list); err != nil {
		return "", err
	}
	key := ""
	for _, conv := range list.Conversations {
		if conv.SessionID == sessionID {
			key = conv.SessionKey
			break
		}
	}
	if key == "" {
		return "", fmt.Errorf("セッション %s の会話（チャンネル・スレッド）が見つかりません", sessionID)
	}
	var conv struct {
		Platform string `json:"platform"`
		ChatID   any    `json:"chat_id"`
		ThreadID any    `json:"thread_id"`
	}
	if err := c.tool(ctx, "conversation_get", map[string]any{"session_key": key}, &conv); err != nil {
		return "", err
	}
	chat, thread := idString(conv.ChatID), idString(conv.ThreadID)
	if conv.Platform == "" || chat == "" {
		return "", fmt.Errorf("セッション %s の送信先がわかりません", sessionID)
	}
	t = conv.Platform + ":" + chat
	if thread != "" {
		t += ":" + thread
	}
	n.mu.Lock()
	n.targets[sessionID] = t
	n.mu.Unlock()
	return t, nil
}

func idString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return fmt.Sprintf("%.0f", x)
	case json.Number:
		return x.String()
	}
	return ""
}
