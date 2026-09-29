package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DiscordWebhook posts notifications to a Discord channel webhook. It is the
// fallback when the agent itself cannot post into the session's thread.
type DiscordWebhook struct {
	URL    string
	Client *http.Client
}

// Notify implements Notifier.
func (d DiscordWebhook) Notify(ctx context.Context, _ string, text string) error {
	if d.URL == "" {
		return fmt.Errorf("discord webhook URL is not set")
	}
	if len([]rune(text)) > 1900 {
		text = string([]rune(text)[:1900]) + "…"
	}
	body, _ := json.Marshal(map[string]any{
		"content":          text,
		"username":         "reviewer",
		"allowed_mentions": map[string]any{"parse": []string{}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c := d.Client
	if c == nil {
		c = &http.Client{Timeout: 15 * time.Second}
	}
	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("discord webhook: %s: %s", res.Status, b)
	}
	return nil
}
