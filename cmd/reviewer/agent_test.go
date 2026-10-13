package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ymotongpoo/reviewer/internal/config"
)

func TestConnectAgentPrefersT3(t *testing.T) {
	t3home, hermesHome := t.TempDir(), t.TempDir()
	t.Setenv("T3CODE_HOME", t3home)
	t.Setenv("HERMES_HOME", hermesHome)
	t.Setenv("T3_MCP_TOKEN", "")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	os.WriteFile(filepath.Join(hermesHome, ".env"), []byte("API_SERVER_ENABLED=true\nAPI_SERVER_KEY=0123456789abcdef0123\n"), 0o600)

	cfg := config.Default()
	if c := connectAgent(cfg); c.ag == nil || c.ag.Kind() != "hermes" || c.notifyName != "hermes" && c.notifyName != "none（hermes コマンドが見つかりません）" {
		t.Fatalf("without T3: %+v", c)
	}

	os.MkdirAll(filepath.Join(t3home, "userdata"), 0o755)
	cfg.Agent.T3.Command = "/bin/true"
	c := connectAgent(cfg)
	if c.ag == nil || c.ag.Kind() != "t3" || c.notifyName != "none" {
		t.Fatalf("with T3: %+v", c)
	}
	cfg.Agent.Kind = "hermes"
	if c := connectAgent(cfg); c.ag == nil || c.ag.Kind() != "hermes" {
		t.Fatalf("kind=hermes: %+v", c)
	}
	cfg.Agent.Kind = "t3"
	cfg.Agent.T3.Access = "root"
	if c := connectAgent(cfg); c.ag != nil || c.reason == "" {
		t.Fatalf("bad access: %+v", c)
	}
}
