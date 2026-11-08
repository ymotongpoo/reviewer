package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestRebuildRoundAPI(t *testing.T) {
	e := newEnv(t)
	id := e.open(t, e.root+"/a")
	settings := `{"buildCommand":"printf 'sample pdf' > output.pdf","buildDir":".","buildArtifact":"output.pdf"}`
	res, body := do(t, http.MethodPatch, e.ts.URL+"/p/"+id+"/api/session", settings, bearer)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("save build settings: %d %s", res.StatusCode, body)
	}
	res, body = do(t, http.MethodPost, e.ts.URL+"/p/"+id+"/api/rounds/1/build", `{}`, bearer)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("rebuild round: %d %s", res.StatusCode, body)
	}
	var out struct {
		Build struct {
			Status    string `json:"status"`
			Artifacts []struct {
				ID string `json:"id"`
			} `json:"artifacts"`
		} `json:"build"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if out.Build.Status != "completed" || len(out.Build.Artifacts) != 1 {
		t.Fatalf("build response: %s", body)
	}
	res, artifact := do(t, http.MethodGet, e.ts.URL+"/p/"+id+"/api/rounds/1/artifacts/"+out.Build.Artifacts[0].ID, "", bearer)
	if res.StatusCode != http.StatusOK || artifact != "sample pdf" || !strings.Contains(res.Header.Get("Content-Disposition"), "output.pdf") {
		t.Fatalf("read artifact: %d %q %q", res.StatusCode, artifact, res.Header.Get("Content-Disposition"))
	}
}
