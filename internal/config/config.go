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
	"strings"

	"github.com/BurntSushi/toml"
)

// Label is a comment category such as "must" or "nit".
type Label struct {
	Name        string `toml:"name" json:"name"`
	Description string `toml:"description" json:"description"`
}

// Preset is a reusable instruction for an annotation request.
type Preset struct {
	Name   string `toml:"name" json:"name"`
	Prompt string `toml:"prompt" json:"prompt"`
	Scope  string `toml:"scope" json:"scope"`
}

// DefaultAnnotationPrompt asks the agent to find factual and technical errors.
const DefaultAnnotationPrompt = `文書に含まれる事実上または技術上の誤りを確認してください。
仕様、API、コマンド、コード例、バージョン依存の説明、数値、固有名詞を対象にし、好みだけに基づく表現上の指摘は除外してください。
誤りだと判断した箇所ごとに、何が誤っているかと、読者や実装に生じる影響を具体的に説明してください。
事実に関する指摘には、確認に使った一次情報または信頼できる出典の URL と、判断を裏付ける該当箇所の引用を必ず付けてください。
根拠を確認できない推測は指摘として提出せず、確認が必要な事項として confidence を low にしてください。`

// TranslationReviewPrompt asks the agent to find mistranslations against
// the source text.
const TranslationReviewPrompt = `対象ファイルは翻訳文です。原文と突き合わせて、誤訳を確認してください。

原文は次のどちらかにあります。
- 同じリポジトリにある翻訳元のファイル。パスの対応から探してください（例: content/ja/docs/foo.md に対する content/en/docs/foo.md、README.ja.md に対する README.md、docs/ja/ に対する docs/en/ や docs/）。
- 文書の中に併記・引用されている原文。
原文を特定できない場合は、推測で指摘せず、summary にその旨と探した場所を書いてください。原文の候補が複数ある場合は、使った原文を summary に書いてください。

指摘するのは、原文と意味が食い違う箇所です。
- 意味の取り違え、主語・目的語・修飾関係の誤り
- 訳抜け（原文にある内容が無い）と、訳の追加（原文に無い内容がある）
- 否定、条件、数量、数値、単位、固有名詞、コード・コマンド・設定値の誤り
- 同じ原語に別の訳語を使っているなど、文書内の用語の不統一
意味が同じで、文体や語順、好みだけが違うものは指摘しないでください。

各指摘の body には、原文の該当箇所、何が誤っているか、正しい意味を書いてください。evidence には、url に原文のファイルパス（リポジトリ内なら対象ファイルと同じ形式のパス）か URL を、quote に原文の該当文を入れてください。

置換案について：翻訳文の1行には複数の文があることがよくあります。直すのが行の一部の文や語だけなら、必ず edits を使い、find に誤訳の部分を quote のとおりに、replace に修正した訳を書いてください。suggestion は、行全体を訳し直す場合だけに使い、その場合は直さない文も含めて行全体を書いてください。`

// BuiltinPresets returns a copy of the built-in annotation presets.
func BuiltinPresets() []Preset {
	return []Preset{
		{Name: "技術的な誤りの検出", Prompt: DefaultAnnotationPrompt, Scope: "all"},
		{Name: "誤訳の修正", Prompt: TranslationReviewPrompt, Scope: "current"},
	}
}

// Config is the merged configuration.
type Config struct {
	// DataDir is where review data is stored. Relative paths are resolved
	// against the project root. The special value "xdg" selects
	// $XDG_DATA_HOME/reviewer/<project>-<hash>.
	DataDir   string `toml:"data_dir"`
	Port      int    `toml:"port"`
	Bind      string `toml:"bind"`
	PublicURL string `toml:"public_url"`
	// Roots limits the directories the server may open ("~" = home).
	Roots          []string `toml:"roots"`
	Exclude        []string `toml:"exclude"`
	Labels         []Label  `toml:"labels"`
	Presets        []Preset `toml:"presets"`
	PromptTemplate string   `toml:"prompt_template"`
	Anchor         struct {
		FuzzyThreshold float64 `toml:"fuzzy_threshold"`
	} `toml:"anchor"`
	Agent AgentConfig `toml:"agent"`
	Git   GitConfig   `toml:"git"`
}

// GitConfig configures committing and pushing from the web UI. The project
// copy edited by the UI lives in <data>/git.toml and overrides these.
type GitConfig struct {
	// Language of generated commit messages: "ja" or "en".
	Language string `toml:"language,omitempty" json:"language"`
	// Remote and Branch are the default push target. Empty means the
	// upstream of the current branch (or "origin") and the current branch.
	Remote string `toml:"remote,omitempty" json:"remote"`
	Branch string `toml:"branch,omitempty" json:"branch"`
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
		Presets: BuiltinPresets(),
	}
	c.Roots = []string{"~"}
	c.Anchor.FuzzyThreshold = 0.7
	c.Agent.Kind = "auto"
	c.Agent.AutoSend = true
	c.Agent.Notify = "hermes"
	c.Agent.Hermes.APIKeyEnv = "HERMES_API_KEY"
	c.Git.Language = "ja"
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

// StateDir returns the directory for server state that does not belong to
// any project: the recent project list and the access token.
func StateDir() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "reviewer")
}

// ExpandHome replaces a leading "~" with the home directory.
func ExpandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~"))
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
	globalPath := GlobalPath()
	if globalPath != "" {
		if _, err := toml.DecodeFile(globalPath, &c); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return c, fmt.Errorf("config %s: %w", globalPath, err)
		}
		var global struct {
			Presets []Preset `toml:"presets"`
		}
		if _, err := toml.DecodeFile(globalPath, &global); err == nil {
			c.Presets = append(BuiltinPresets(), global.Presets...)
		}
	}
	globalPresets := append([]Preset(nil), c.Presets...)
	for _, p := range []string{projectPath} {
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
	// Project presets live in <data>/presets.toml, not config.toml.
	c.Presets = globalPresets
	if len(c.Labels) == 0 {
		c.Labels = Default().Labels
	}
	if c.PromptTemplate == "" {
		c.PromptTemplate = DefaultPromptTemplate
	}
	return c, nil
}

// LoadGlobal reads the defaults and the global file only; it holds the
// server-wide settings (port, bind, public URL, roots, agent).
func LoadGlobal(path string) (Config, error) {
	c := Default()
	if path == "" {
		path = GlobalPath()
	}
	if path != "" {
		if _, err := toml.DecodeFile(path, &c); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return c, fmt.Errorf("config %s: %w", path, err)
		}
		var global struct {
			Presets []Preset `toml:"presets"`
		}
		if _, err := toml.DecodeFile(path, &global); err == nil {
			c.Presets = append(BuiltinPresets(), global.Presets...)
		}
	}
	if len(c.Roots) == 0 {
		c.Roots = []string{"~"}
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
