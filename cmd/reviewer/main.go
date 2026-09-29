// Command reviewer serves a web UI for reviewing documents written by AI
// agents and hands the feedback back to them as files.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ymotongpoo/reviewer/internal/app"
	"github.com/ymotongpoo/reviewer/internal/config"
	"github.com/ymotongpoo/reviewer/internal/server"
	"github.com/ymotongpoo/reviewer/internal/store"
)

var version = "dev"

const usage = `reviewer — AI エージェントが書いた文書にコメントし、フィードバックをファイルで返すツール

使い方:
  reviewer serve  [DIR] [flags]   Web UI を起動する（DIR を省略すると画面からディレクトリを選ぶ）
  reviewer service install|uninstall|status|restart
                                  systemd のユーザーサービスとして常駐させる
  reviewer url                    アクセス用の URL を表示する
  reviewer status [DIR] [flags]   現在のラウンドと未解決コメント数を表示する
  reviewer export [DIR] [flags]   フィードバックを標準出力に出す
  reviewer version

各サブコマンドの flags は reviewer <command> -h で確認できます。
`

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve(os.Args[2:])
	case "status":
		err = status(os.Args[2:])
	case "export":
		err = export(os.Args[2:])
	case "service":
		err = service(os.Args[2:])
	case "url":
		err = urlCmd(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println(version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		log.Fatalf("reviewer: %v", err)
	}
}

type common struct {
	dataDir    string
	configPath string
}

func (c *common) register(fs *flag.FlagSet) {
	fs.StringVar(&c.dataDir, "data-dir", "", "レビューデータの保存先（既定: DIR/.reviewer、\"xdg\" で ~/.local/share/reviewer/...）")
	fs.StringVar(&c.configPath, "config", "", "プロジェクト設定ファイル（既定: DIR/.reviewer/config.toml）")
}

// parse parses flags that may appear before or after the DIR argument.
func parse(fs *flag.FlagSet, args []string) (string, error) {
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	dir := "."
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
		if err := fs.Parse(fs.Args()[1:]); err != nil {
			return "", err
		}
		if fs.NArg() > 0 {
			return "", fmt.Errorf("unexpected arguments: %v", fs.Args())
		}
	}
	return dir, nil
}

func (c *common) load(dir string) (config.Config, error) {
	cfg, err := config.Load(dir, c.configPath)
	if err != nil {
		return cfg, err
	}
	if c.dataDir != "" {
		cfg.DataDir = c.dataDir
	}
	return cfg, nil
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", "", "サーバー設定ファイル（既定: ~/.config/reviewer/config.toml）")
	port := fs.Int("port", 0, "待ち受けポート（既定: 7777。使用中なら次の空きポート）")
	bind := fs.String("bind", "", "待ち受けアドレス（既定: all = 全インターフェースの IPv4/IPv6。127.0.0.1 でこのマシンからのみ）")
	token := fs.String("token", os.Getenv("REVIEWER_TOKEN"), "アクセストークン（既定: $REVIEWER_TOKEN、なければ ~/.local/state/reviewer/token）")
	dir, err := parseOptional(fs, args)
	if err != nil {
		return err
	}
	cfg, err := config.LoadGlobal(*configPath)
	if err != nil {
		return err
	}
	portSet := *port != 0
	if !portSet {
		*port = cfg.Port
	}
	if *bind == "" {
		*bind = cfg.Bind
	}
	if *token == "" {
		if *token, err = persistentToken(); err != nil {
			return err
		}
	}

	host := listenHost(*bind)
	ln, err := listen(host, *port, !portSet)
	if err != nil {
		return err
	}
	actualPort := ln.Addr().(*net.TCPAddr).Port
	urls := accessURLs(host, actualPort, *token, hostname(), interfaceAddrs())
	base := strings.TrimSuffix(strings.SplitN(urls[0], "?", 2)[0], "/")

	conn := connectAgent(cfg)
	reg, err := server.NewRegistry(cfg.Roots, config.StateDir(), base, conn.setup)
	if err != nil {
		return err
	}
	defer reg.CloseAll()
	server.Version = version
	srv := server.New(reg, *token, actualPort)
	httpSrv := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}

	path := "/"
	if dir != "" {
		p, err := reg.Open(dir)
		if err != nil {
			return err
		}
		path = "/p/" + p.ID + "/"
		fmt.Printf("Serving %s\n", p.Root)
		fmt.Printf("  data: %s\n", p.App.DataDir)
		for _, w := range p.App.Warnings() {
			fmt.Printf("  warning: %s\n", w)
		}
	} else {
		fmt.Printf("reviewer %s\n", version)
		fmt.Printf("  roots: %s\n", strings.Join(cfg.Roots, ", "))
	}
	fmt.Printf("  Open: %s\n", withPath(urls[0], path))
	for _, u := range urls[1:] {
		fmt.Printf("        %s\n", withPath(u, path))
	}
	if conn.ag != nil {
		fmt.Printf("  agent: %s（通知: %s）\n", conn.ag.Name(), conn.notifyName)
	} else if conn.reason != "" {
		fmt.Printf("  agent: 未接続（%s）\n", conn.reason)
	}
	if isLoopback(host) {
		fmt.Printf("  (このマシンからのみ開けます。他のマシンから開くには --bind all で起動してください)\n")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- httpSrv.Serve(ln) }()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		httpSrv.Shutdown(shutdown)
	}
	return nil
}

// withPath inserts path before the query of an access URL.
func withPath(u, path string) string {
	base, q, _ := strings.Cut(u, "?")
	return strings.TrimSuffix(base, "/") + path + "?" + q
}

// persistentToken returns the token stored in the state directory,
// creating it on first use so that URLs survive restarts.
func persistentToken() (string, error) {
	dir := config.StateDir()
	p := filepath.Join(dir, "token")
	if b, err := os.ReadFile(p); err == nil {
		if t := strings.TrimSpace(string(b)); t != "" {
			return t, nil
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	t := server.NewToken()
	if err := os.WriteFile(p, []byte(t+"\n"), 0o600); err != nil {
		return "", err
	}
	return t, nil
}

// parseOptional parses flags around an optional DIR argument ("" if absent).
func parseOptional(fs *flag.FlagSet, args []string) (string, error) {
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	if fs.NArg() == 0 {
		return "", nil
	}
	dir := fs.Arg(0)
	if err := fs.Parse(fs.Args()[1:]); err != nil {
		return "", err
	}
	if fs.NArg() > 0 {
		return "", fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return dir, nil
}

func listen(bind string, port int, probe bool) (net.Listener, error) {
	var lastErr error
	for i := 0; i < 20; i++ {
		ln, err := net.Listen("tcp", net.JoinHostPort(bind, strconv.Itoa(port+i)))
		if err == nil {
			return ln, nil
		}
		lastErr = err
		if !probe {
			break
		}
	}
	return nil, lastErr
}

// openApp opens an existing review without creating one.
func openApp(c *common, dir string) (*app.App, error) {
	cfg, err := c.load(dir)
	if err != nil {
		return nil, err
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	dataDir, err := cfg.ResolveDataDir(root)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(dataDir, "state.json")); err != nil {
		return nil, fmt.Errorf("%s: %w（先に reviewer serve を実行してください）", dataDir, store.ErrNotInitialized)
	}
	a, err := app.New(dir, cfg, nil)
	if err != nil {
		return nil, err
	}
	if err := a.Init(); err != nil {
		return nil, err
	}
	return a, nil
}

func status(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	var c common
	c.register(fs)
	asJSON := fs.Bool("json", false, "JSON で出力する")
	dir, err := parse(fs, args)
	if err != nil {
		return err
	}
	a, err := openApp(&c, dir)
	if err != nil {
		return err
	}
	s := a.StatusSummary()
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(s)
	}
	fmt.Printf("Round %d (%s)\n", s.Round, s.RoundStatus)
	fmt.Printf("未解決コメント: %d\n", s.Unresolved)
	for _, k := range []string{store.StatusDraft, store.StatusOpen, store.StatusAddressed, store.StatusWontfix, store.StatusQuestion, store.StatusResolved} {
		if n := s.ByStatus[k]; n > 0 {
			fmt.Printf("  %-9s %d\n", k, n)
		}
	}
	if s.Latest != nil {
		fmt.Printf("最新の提出: ラウンド%d\n  feedback: %s\n  response: %s\n", s.Latest.Round, s.Latest.FeedbackPath, s.Latest.ResponsePath)
	}
	return nil
}

func export(args []string) error {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	var c common
	c.register(fs)
	round := fs.Int("round", 0, "ラウンド番号（既定: 最新の提出済みラウンド）")
	format := fs.String("format", "md", "md または json")
	dir, err := parse(fs, args)
	if err != nil {
		return err
	}
	if *format != "md" && *format != "json" {
		return fmt.Errorf("unknown format %q", *format)
	}
	a, err := openApp(&c, dir)
	if err != nil {
		return err
	}
	b, err := a.Export(*round, *format)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(b)
	return err
}
