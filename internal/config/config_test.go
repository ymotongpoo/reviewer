package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPublicURLLoading(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	global := filepath.Join(configHome, "reviewer", "config.toml")
	if err := os.MkdirAll(filepath.Dir(global), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, content, want string
	}{
		{"unset", `port = 8888`, ""},
		{"public", `public_url = "https://devbox.example.ts.net/"`, "https://devbox.example.ts.net/"},
		{"empty", `public_url = ""`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(global, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{"", global} {
				cfg, err := LoadGlobal(path)
				if err != nil {
					t.Fatal(err)
				}
				if cfg.PublicURL != tc.want {
					t.Errorf("LoadGlobal(%q).PublicURL = %q, want %q", path, cfg.PublicURL, tc.want)
				}
			}
		})
	}
}

func TestPresetLoading(t *testing.T) {
	root := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	global := filepath.Join(configHome, "reviewer", "config.toml")
	if err := os.MkdirAll(filepath.Dir(global), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(global, []byte(`[[presets]]
name = "全体確認"
prompt = "全体を確認してください"
scope = "all"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(root, ".reviewer", "config.toml")
	if err := os.MkdirAll(filepath.Dir(project), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(project, []byte(`[[presets]]
name = "ここには置かない"
prompt = "ignored"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(root, project)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Presets) != 3 {
		t.Fatalf("presets = %+v", cfg.Presets)
	}
	if cfg.Presets[0].Name != "技術的な誤りの検出" || cfg.Presets[1].Name != "誤訳の修正" || cfg.Presets[2].Name != "全体確認" {
		t.Errorf("presets = %+v", cfg.Presets)
	}
}
