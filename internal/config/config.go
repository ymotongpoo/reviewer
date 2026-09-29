// Package config loads reviewer settings from global and project TOML files.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Label is a comment category such as "must" or "nit".
type Label struct {
	Name        string `toml:"name" json:"name"`
	Description string `toml:"description" json:"description"`
}

// Config is the merged configuration.
type Config struct {
	// DataDir is where review data is stored. Relative paths are resolved
	// against the project root. The special value "xdg" selects
	// $XDG_DATA_HOME/reviewer/<project>-<hash>.
	DataDir        string   `toml:"data_dir"`
	Port           int      `toml:"port"`
	Bind           string   `toml:"bind"`
	Exclude        []string `toml:"exclude"`
	Labels         []Label  `toml:"labels"`
	PromptTemplate string   `toml:"prompt_template"`
	Anchor         struct {
		FuzzyThreshold float64 `toml:"fuzzy_threshold"`
	} `toml:"anchor"`
	Agent AgentConfig `toml:"agent"`
}

// AgentConfig configures sending feedback directly to a coding agent.
type AgentConfig struct {
	// Kind is "auto" (use Hermes when its API server is configured),
	// "hermes", or "none".
	Kind string `toml:"kind"`
	// AutoSend is the initial state of "send to the agent" on submit.
	AutoSend bool `toml:"auto_send"`
	// Notify selects where start/finish notices go: "hermes" (the session's
	// own conversation, e.g. its Discord thread), "discord-webhook" or "none".
	Notify string `toml:"notify"`
	Hermes struct {
		URL       string `toml:"url"`
		APIKeyEnv string `toml:"api_key_env"`
		Profile   string `toml:"profile"`
		Command   string `toml:"command"`
		Home      string `toml:"home"`
	} `toml:"hermes"`
	DiscordWebhook struct {
		URL string `toml:"url"`
	} `toml:"discord_webhook"`
}

// DefaultPromptTemplate is the instruction copied for the agent.
const DefaultPromptTemplate = "`{{.FeedbackPath}}` のレビューコメント（ラウンド{{.Round}}）に対応してください。" +
	"各コメントIDへの返答を `{{.ResponsePath}}` に、feedback.md の末尾に記載された JSON 形式で書いてください。"

// Default returns the built-in defaults.
func Default() Config {
	c := Config{
		DataDir:        ".reviewer",
		Port:           7777,
		Bind:           "all",
		PromptTemplate: DefaultPromptTemplate,
		Labels: []Label{
			{Name: "must", Description: "必須"},
			{Name: "suggestion", Description: "提案"},
			{Name: "question", Description: "質問"},
			{Name: "nit", Description: "細かい指摘"},
		},
	}
	c.Anchor.FuzzyThreshold = 0.7
	c.Agent.Kind = "auto"
	c.Agent.AutoSend = true
	c.Agent.Notify = "hermes"
	c.Agent.Hermes.APIKeyEnv = "HERMES_API_KEY"
	return c
}

// GlobalPath returns the path of the global configuration file.
func GlobalPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "reviewer", "config.toml")
}

// ProjectPath returns the path of the project configuration file.
func ProjectPath(root string) string {
	return filepath.Join(root, ".reviewer", "config.toml")
}

// Load reads the defaults, then the global file, then the project file.
// projectPath overrides the default project location when non-empty.
// Missing files are ignored.
func Load(root, projectPath string) (Config, error) {
	c := Default()
	if projectPath == "" {
		projectPath = ProjectPath(root)
	}
	for _, p := range []string{GlobalPath(), projectPath} {
		if p == "" {
			continue
		}
		if _, err := toml.DecodeFile(p, &c); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return c, fmt.Errorf("config %s: %w", p, err)
		}
	}
	if len(c.Labels) == 0 {
		c.Labels = Default().Labels
	}
	if c.PromptTemplate == "" {
		c.PromptTemplate = DefaultPromptTemplate
	}
	return c, nil
}

// ResolveDataDir returns the absolute data directory for root.
func (c Config) ResolveDataDir(root string) (string, error) {
	switch {
	case c.DataDir == "xdg":
		base := os.Getenv("XDG_DATA_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, ".local", "share")
		}
		sum := sha256.Sum256([]byte(root))
		name := filepath.Base(root) + "-" + hex.EncodeToString(sum[:4])
		return filepath.Join(base, "reviewer", name), nil
	case c.DataDir == "":
		return filepath.Join(root, ".reviewer"), nil
	case filepath.IsAbs(c.DataDir):
		return filepath.Clean(c.DataDir), nil
	default:
		return filepath.Join(root, c.DataDir), nil
	}
}
