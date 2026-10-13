package t3

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
)

const mcpProtocolVersion = "2025-06-18"

// errUnauthorized and errSessionGone make call retry with a new token or
// MCP session.
var (
	errUnauthorized = errors.New("unauthorized")
	errSessionGone  = errors.New("mcp session gone")
)

// ToolError is a failure reported by a T3 Code tool.
type ToolError struct {
	Tool string
	Msg  string
}

func (e *ToolError) Error() string { return e.Msg }

type rpcResponse struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// call invokes a tool and decodes its structured result into out.
func (c *Client) call(ctx context.Context, tool string, args, out any) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = c.callOnce(ctx, tool, args, out)
		if !errors.Is(err, errUnauthorized) && !errors.Is(err, errSessionGone) {
			return err
		}
	}
	if errors.Is(err, errUnauthorized) {
		return errors.New("T3 Code に認証を拒否されました")
	}
	return err
}

func (c *Client) callOnce(ctx context.Context, tool string, args, out any) error {
	tok, err := c.token(ctx)
	if err != nil {
		return err
	}
	origin, sid, err := c.session(ctx, tok)
	if err != nil {
		if errors.Is(err, errUnauthorized) {
			c.invalidate(tok)
		}
		return err
	}
	raw, _, err := c.rpc(ctx, origin, tok, sid, "tools/call", map[string]any{"name": tool, "arguments": args})
	if errors.Is(err, errUnauthorized) {
		c.invalidate(tok)
		c.resetSession(sid)
		return err
	}
	if errors.Is(err, errSessionGone) {
		c.resetSession(sid)
		return err
	}
	if err != nil {
		return err
	}
	var res struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StructuredContent json.RawMessage `json:"structuredContent"`
		IsError           bool            `json:"isError"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("%s: %w", tool, err)
	}
	var text strings.Builder
	for _, ct := range res.Content {
		if ct.Type == "text" {
			text.WriteString(ct.Text)
		}
	}
	if res.IsError {
		return &ToolError{Tool: tool, Msg: toolErrorMessage(text.String())}
	}
	if out == nil {
		return nil
	}
	data := res.StructuredContent
	if len(data) == 0 || string(data) == "null" {
		data = []byte(text.String())
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%s: %w", tool, err)
	}
	return nil
}

// toolErrorMessage extracts the message of an error that may be JSON.
func toolErrorMessage(s string) string {
	var e struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if json.Unmarshal([]byte(s), &e) == nil {
		if e.Message != "" {
			return e.Message
		}
		if e.Error != "" {
			return e.Error
		}
	}
	return strings.TrimSpace(s)
}

// session returns an initialized MCP session.
func (c *Client) session(ctx context.Context, tok string) (origin, sid string, err error) {
	c.sessMu.Lock()
	defer c.sessMu.Unlock()
	origin, err = c.origin()
	if err != nil {
		return "", "", err
	}
	if c.sid != "" && c.sidOrigin == origin && c.sidToken == tok {
		return origin, c.sid, nil
	}
	_, hdr, err := c.rpc(ctx, origin, tok, "", "initialize", map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "reviewer", "version": "1"},
	})
	if err != nil {
		return "", "", err
	}
	sid = hdr.Get("Mcp-Session-Id")
	if err := c.notifyInitialized(ctx, origin, tok, sid); err != nil {
		return "", "", err
	}
	c.sid, c.sidOrigin, c.sidToken = sid, origin, tok
	return origin, sid, nil
}

func (c *Client) resetSession(sid string) {
	c.sessMu.Lock()
	defer c.sessMu.Unlock()
	if c.sid == sid {
		c.sid = ""
	}
}

func (c *Client) newRequest(ctx context.Context, origin, tok, sid string, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, origin+"/mcp", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", mcpProtocolVersion)
	if sid != "" {
		req.Header.Set("Mcp-Session-Id", sid)
	}
	return req, nil
}

func (c *Client) notifyInitialized(ctx context.Context, origin, tok, sid string) error {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	req, err := c.newRequest(ctx, origin, tok, sid, body)
	if err != nil {
		return err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	io.Copy(io.Discard, res.Body)
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("notifications/initialized: %s", res.Status)
	}
	return nil
}

// rpc sends a JSON-RPC request and returns its result. The server answers
// either with JSON or with an event stream carrying the response.
func (c *Client) rpc(ctx context.Context, origin, tok, sid, method string, params any) (json.RawMessage, http.Header, error) {
	id := c.nextID.Add(1)
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	req, err := c.newRequest(ctx, origin, tok, sid, body)
	if err != nil {
		return nil, nil, err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("T3 Code に接続できません: %w", err)
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusUnauthorized:
		return nil, nil, errUnauthorized
	case sid != "" && (res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusBadRequest && strings.Contains(strings.ToLower(peek(res.Body)), "session")):
		return nil, nil, errSessionGone
	case res.StatusCode/100 != 2:
		return nil, nil, fmt.Errorf("%s: %s: %s", method, res.Status, peek(res.Body))
	}
	var resp rpcResponse
	ct, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if ct == "text/event-stream" {
		resp, err = readEventResponse(res.Body, id)
	} else {
		err = json.NewDecoder(io.LimitReader(res.Body, 32<<20)).Decode(&resp)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", method, err)
	}
	if resp.Error != nil {
		return nil, nil, fmt.Errorf("%s: %s", method, resp.Error.Message)
	}
	return resp.Result, res.Header, nil
}

func peek(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, 512))
	return strings.TrimSpace(string(b))
}

// readEventResponse reads server-sent events until the response to id.
func readEventResponse(r io.Reader, id int64) (rpcResponse, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 32<<20)
	var data strings.Builder
	want := fmt.Sprint(id)
	for sc.Scan() {
		line := sc.Text()
		if v, ok := strings.CutPrefix(line, "data:"); ok {
			data.WriteString(strings.TrimPrefix(v, " "))
			continue
		}
		if line != "" || data.Len() == 0 {
			continue
		}
		var resp rpcResponse
		if json.Unmarshal([]byte(data.String()), &resp) == nil && string(resp.ID) == want {
			return resp, nil
		}
		data.Reset()
	}
	if err := sc.Err(); err != nil {
		return rpcResponse{}, err
	}
	return rpcResponse{}, errors.New("no response in event stream")
}
