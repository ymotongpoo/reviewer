package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPostAgentBuildSavesRoundArtifact(t *testing.T) {
	a, _ := setup(t)
	command := "printf 'sample pdf' > build-output.pdf"
	dir := "."
	pattern := "build-output.pdf"
	if _, err := a.UpdateSession(context.Background(), nil, nil, nil, &command, &dir, &pattern); err != nil {
		t.Fatal(err)
	}
	var notified bool
	a.notify = func(ev Event) {
		if ev.Type == "round" && ev.Round == 1 {
			notified = true
		}
	}

	a.runPostAgentBuild(1)

	record, err := a.Store.BuildRecord(1)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != "completed" || record.ExitCode != 0 {
		t.Fatalf("build record = %+v", record)
	}
	if len(record.Artifacts) != 1 || record.Artifacts[0].Name != "build-output.pdf" {
		t.Fatalf("artifacts = %+v", record.Artifacts)
	}
	data, mediaType, name, err := a.RoundArtifact(1, record.Artifacts[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "sample pdf" || mediaType != "application/pdf" || name != "build-output.pdf" {
		t.Fatalf("artifact = %q, %q, %q", data, mediaType, name)
	}
	if _, err := os.Stat(filepath.Join(a.Store.RoundDir(1), "artifacts", record.Artifacts[0].ID)); err != nil {
		t.Fatalf("stored artifact: %v", err)
	}
	if !notified {
		t.Fatal("round update was not published")
	}

	info := a.Info()
	if len(info.Rounds) != 1 || info.Rounds[0].Build == nil || info.Rounds[0].Build.Status != "completed" {
		t.Fatalf("round build missing from info: %+v", info.Rounds)
	}
}

func TestReadBuildArtifactRequiresRoundReference(t *testing.T) {
	a, _ := setup(t)
	if _, _, _, err := a.RoundArtifact(1, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.pdf"); err == nil {
		t.Fatal("unreferenced artifact was served")
	}
}
