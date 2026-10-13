package t3

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// redirectURI is registered for reviewer's OAuth client. The code is read
// from the approval response, so nothing listens there.
const redirectURI = "http://127.0.0.1/reviewer/t3-callback"

// renewBefore makes reviewer obtain a new token this long before expiry.
const renewBefore = 24 * time.Hour

// scopes returns the scopes T3 Code requires a pairing code to hold for
// approving a client with access.
func scopes(access string) []string {
	switch access {
	case "read-only":
		return []string{"orchestration:read"}
	case "full-access":
		return []string{"orchestration:read", "orchestration:operate", "settings:write", "source-control:write"}
	default:
		return []string{"orchestration:read", "orchestration:operate"}
	}
}

type cachedToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
	Access    string    `json:"access"`
	Home      string    `json:"home"`
}

func (t cachedToken) usable(o Options, now time.Time) bool {
	return t.Token != "" && t.Access == o.Access && t.Home == o.Home && now.Add(renewBefore).Before(t.ExpiresAt)
}

// token returns the MCP token, obtaining one when there is none.
func (c *Client) token(ctx context.Context) (string, error) {
	if c.opts.Token != "" {
		return c.opts.Token, nil
	}
	c.tokMu.Lock()
	defer c.tokMu.Unlock()
	now := time.Now()
	if c.tok.usable(c.opts, now) {
		return c.tok.Token, nil
	}
	if c.opts.TokenFile != "" {
		var t cachedToken
		if b, err := os.ReadFile(c.opts.TokenFile); err == nil && json.Unmarshal(b, &t) == nil && t.usable(c.opts, now) {
			c.tok = t
			return t.Token, nil
		}
	}
	origin, err := c.origin()
	if err != nil {
		return "", err
	}
	t, err := c.authorize(ctx, origin)
	if err != nil {
		return "", fmt.Errorf("T3 Code の認可を取得できませんでした: %w", err)
	}
	c.tok = t
	if c.opts.TokenFile != "" {
		if err := saveToken(c.opts.TokenFile, t); err != nil {
			return "", err
		}
	}
	return t.Token, nil
}

// invalidate drops tok after the server rejected it.
func (c *Client) invalidate(tok string) {
	c.tokMu.Lock()
	defer c.tokMu.Unlock()
	if c.tok.Token == tok {
		c.tok = cachedToken{}
		if c.opts.TokenFile != "" {
			os.Remove(c.opts.TokenFile)
		}
	}
}

func saveToken(path string, t cachedToken) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(t, "", "  ")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// authorize signs reviewer in as an MCP client. T3 Code's OAuth approval
// accepts a pairing code instead of a browser session; the t3 CLI issues
// one, so no browser is involved.
func (c *Client) authorize(ctx context.Context, origin string) (cachedToken, error) {
	code, err := c.pairingCode(ctx)
	if err != nil {
		return cachedToken{}, err
	}
	var client struct {
		ClientID string `json:"client_id"`
	}
	if err := c.postJSON(ctx, origin+"/oauth/mcp/register", map[string]any{
		"client_name": "reviewer", "redirect_uris": []string{redirectURI},
	}, &client); err != nil {
		return cachedToken{}, fmt.Errorf("register: %w", err)
	}
	verifier := randomString(32)
	sum := sha256.Sum256([]byte(verifier))
	authorization := map[string]string{
		"response_type":         "code",
		"client_id":             client.ClientID,
		"redirect_uri":          redirectURI,
		"code_challenge":        base64.RawURLEncoding.EncodeToString(sum[:]),
		"code_challenge_method": "S256",
		"state":                 randomString(16),
		"resource":              origin + "/mcp",
	}
	var decision struct {
		RedirectTo string `json:"redirectTo"`
	}
	if err := c.postJSON(ctx, origin+"/oauth/mcp/decision", map[string]any{
		"authorization": authorization,
		"decision":      map[string]string{"_tag": "pairing-code", "access": c.opts.Access, "code": code},
	}, &decision); err != nil {
		return cachedToken{}, fmt.Errorf("approve: %w", err)
	}
	u, err := url.Parse(decision.RedirectTo)
	if err != nil {
		return cachedToken{}, fmt.Errorf("approve: %w", err)
	}
	if e := u.Query().Get("error"); e != "" {
		return cachedToken{}, fmt.Errorf("approve: %s", e)
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {u.Query().Get("code")},
		"redirect_uri":  {redirectURI},
		"client_id":     {client.ClientID},
		"code_verifier": {verifier},
		"resource":      {origin + "/mcp"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, origin+"/oauth/mcp/token", strings.NewReader(form.Encode()))
	if err != nil {
		return cachedToken{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := c.do(req, &tok); err != nil {
		return cachedToken{}, fmt.Errorf("token: %w", err)
	}
	if tok.AccessToken == "" {
		return cachedToken{}, errors.New("token: empty access token")
	}
	return cachedToken{
		Token:     tok.AccessToken,
		ExpiresAt: time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second),
		Access:    c.opts.Access,
		Home:      c.opts.Home,
	}, nil
}

// pairingCode issues a short-lived one-time pairing code with the t3 CLI.
func (c *Client) pairingCode(ctx context.Context) (string, error) {
	if c.opts.Command == "" {
		return "", errors.New("t3 コマンドが見つかりません")
	}
	args := []string{"auth", "pairing", "create", "--ttl", "5m", "--label", "reviewer", "--json"}
	for _, s := range scopes(c.opts.Access) {
		args = append(args, "--scope", s)
	}
	cmd := exec.CommandContext(ctx, c.opts.Command, args...)
	cmd.Env = append(os.Environ(), "T3CODE_HOME="+c.opts.Home)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("t3 auth pairing create: %v: %s", err, lastLine(stderr.String()))
	}
	var res struct {
		Credential string `json:"credential"`
	}
	if err := decodeJSONOutput(stdout.Bytes(), &res); err != nil || res.Credential == "" {
		return "", fmt.Errorf("t3 auth pairing create: unexpected output: %s", lastLine(stdout.String()))
	}
	return res.Credential, nil
}

// decodeJSONOutput decodes the first JSON object of CLI output that may
// start with log lines.
func decodeJSONOutput(out []byte, v any) error {
	if json.Unmarshal(out, v) == nil {
		return nil
	}
	for i := 0; i < len(out); i++ {
		if out[i] == '{' && (i == 0 || out[i-1] == '\n') {
			if json.NewDecoder(bytes.NewReader(out[i:])).Decode(v) == nil {
				return nil
			}
		}
	}
	return errors.New("no JSON object")
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

func randomString(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (c *Client) postJSON(ctx context.Context, u string, body, out any) error {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode/100 != 2 {
		var e struct {
			Message     string `json:"message"`
			Description string `json:"error_description"`
		}
		json.Unmarshal(b, &e)
		msg := e.Message
		if msg == "" {
			msg = e.Description
		}
		if msg == "" {
			msg = strings.TrimSpace(string(b))
		}
		return fmt.Errorf("%s: %s", res.Status, msg)
	}
	return json.Unmarshal(b, out)
}
