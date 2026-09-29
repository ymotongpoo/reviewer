package feedback

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Response statuses an agent may use.
var ResponseStatuses = map[string]bool{"addressed": true, "wontfix": true, "question": true}

// ResponseEntry is the agent's answer to one comment.
type ResponseEntry struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// Response is the content of response.json.
type Response struct {
	Round     int             `json:"round"`
	Responses []ResponseEntry `json:"responses"`
	Summary   string          `json:"summary"`
}

// ParseResponse parses response.json. Entries with an unknown status or a
// duplicated ID are dropped and reported as warnings. Code fences around the
// JSON, which agents sometimes add, are tolerated.
func ParseResponse(b []byte, round int, known map[string]bool) (*Response, []string, error) {
	text := strings.TrimSpace(string(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))))
	if strings.HasPrefix(text, "```") {
		if i := strings.Index(text, "\n"); i >= 0 {
			text = text[i+1:]
		}
		text = strings.TrimSuffix(strings.TrimSpace(text), "```")
	}
	var r Response
	dec := json.NewDecoder(bytes.NewReader([]byte(text)))
	if err := dec.Decode(&r); err != nil {
		return nil, nil, fmt.Errorf("response.json を解析できません: %w", err)
	}
	var warnings []string
	if r.Round != 0 && r.Round != round {
		warnings = append(warnings, fmt.Sprintf("round が %d になっています（ラウンド%dのファイルとして扱います）", r.Round, round))
	}
	seen := map[string]bool{}
	var valid []ResponseEntry
	for i, e := range r.Responses {
		e.ID = strings.TrimSpace(e.ID)
		e.Status = strings.ToLower(strings.TrimSpace(e.Status))
		switch {
		case e.ID == "":
			warnings = append(warnings, fmt.Sprintf("responses[%d]: id がありません", i))
		case !ResponseStatuses[e.Status]:
			warnings = append(warnings, fmt.Sprintf("%s: 不明な status %q", e.ID, e.Status))
		case known != nil && !known[e.ID]:
			warnings = append(warnings, fmt.Sprintf("%s: このラウンドのフィードバックに含まれない ID です", e.ID))
		case seen[e.ID]:
			warnings = append(warnings, fmt.Sprintf("%s: 返答が重複しています（最初のものを使います）", e.ID))
		default:
			seen[e.ID] = true
			valid = append(valid, e)
		}
	}
	for id := range known {
		if !seen[id] {
			warnings = append(warnings, fmt.Sprintf("%s: 返答がありません", id))
		}
	}
	r.Responses = valid
	return &r, sortStrings(warnings), nil
}

func sortStrings(s []string) []string {
	// Keep the parse order for per-entry warnings but make the "missing"
	// warnings, which come from map iteration, deterministic.
	var head, missing []string
	for _, w := range s {
		if strings.HasSuffix(w, "返答がありません") {
			missing = append(missing, w)
		} else {
			head = append(head, w)
		}
	}
	for i := 1; i < len(missing); i++ {
		for j := i; j > 0 && missing[j] < missing[j-1]; j-- {
			missing[j], missing[j-1] = missing[j-1], missing[j]
		}
	}
	return append(head, missing...)
}
