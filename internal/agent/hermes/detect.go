// Package hermes connects reviewer to Hermes Agent (Nous Research) through
// the API server built into its gateway.
package hermes

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Options configures the connection. Empty fields are detected from the
// Hermes home directory (~/.hermes, or $HERMES_HOME).
type Options struct {
	URL     string // e.g. http://127.0.0.1:8642
	APIKey  string
	Profile string // served under /p/<profile>/ when set
	Command string // path of the hermes CLI, used for notifications
	Home    string // Hermes home directory
}

// HomeDir returns the Hermes home directory: $HERMES_HOME, or the first of
// ~/.hermes and ~/hermes whose .env enables the API server (installers put
// the home in either place; the gateway's systemd unit sets HERMES_HOME), or
// ~/.hermes.
func HomeDir() string {
	if h := os.Getenv("HERMES_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	candidates := []string{filepath.Join(home, ".hermes"), filepath.Join(home, "hermes")}
	for _, c := range candidates {
		if readEnvFile(filepath.Join(c, ".env"))["API_SERVER_KEY"] != "" {
			return c
		}
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(c, "config.yaml")); err == nil {
			return c
		}
	}
	return candidates[0]
}

// ErrNotConfigured means the API server of Hermes is not enabled.
var ErrNotConfigured = errors.New("Hermes の API サーバーが有効になっていません（~/.hermes/.env に API_SERVER_ENABLED=true と API_SERVER_KEY を設定し、gateway を再起動してください）")

// Resolve fills empty options from the Hermes configuration.
func Resolve(o Options) (Options, error) {
	if o.Home == "" {
		o.Home = HomeDir()
	}
	env := readEnvFile(filepath.Join(o.Home, ".env"))
	get := func(k string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return env[k]
	}
	if o.APIKey == "" {
		o.APIKey = get("API_SERVER_KEY")
	}
	if o.URL == "" {
		if get("API_SERVER_KEY") == "" && o.APIKey == "" {
			return o, ErrNotConfigured
		}
		host := get("API_SERVER_HOST")
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "127.0.0.1"
		}
		port := get("API_SERVER_PORT")
		if port == "" {
			port = "8642"
		}
		o.URL = "http://" + net.JoinHostPort(host, port)
	}
	o.URL = strings.TrimRight(o.URL, "/")
	if o.APIKey == "" {
		return o, ErrNotConfigured
	}
	if o.Command == "" {
		o.Command = findCommand(o.Home)
	}
	if o.Home == "" {
		o.Home = HomeDir()
	}
	return o, nil
}

// findCommand locates the hermes CLI, which is often installed in a venv
// that is not on PATH.
func findCommand(home string) string {
	if p, err := exec.LookPath("hermes"); err == nil {
		return p
	}
	userHome, _ := os.UserHomeDir()
	for _, p := range []string{
		filepath.Join(userHome, "hermes", "hermes-agent", "venv", "bin", "hermes"),
		filepath.Join(home, "hermes-agent", "venv", "bin", "hermes"),
		filepath.Join(userHome, ".local", "bin", "hermes"),
	} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// readEnvFile parses KEY=VALUE lines, ignoring comments and "export ".
func readEnvFile(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		} else if i := strings.Index(v, " #"); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
		out[k] = v
	}
	return out
}

func (o Options) base() string {
	if o.Profile != "" {
		return fmt.Sprintf("%s/p/%s", o.URL, o.Profile)
	}
	return o.URL
}
