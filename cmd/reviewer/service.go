package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ymotongpoo/reviewer/internal/config"
)

const serviceName = "reviewer.service"

func unitPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "systemd", "user", serviceName), nil
}

// unitFile renders the systemd user unit that runs `reviewer serve`.
func unitFile(exe string, args []string) string {
	quoted := make([]string, 0, len(args)+1)
	for _, a := range append([]string{exe}, args...) {
		if strings.ContainsAny(a, " \t\"'\\") {
			a = strconv.Quote(a)
		}
		quoted = append(quoted, a)
	}
	return fmt.Sprintf(`[Unit]
Description=reviewer - review documents written by AI agents
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
`, strings.Join(quoted, " "))
}

func systemctl(args ...string) error {
	cmd := exec.Command("systemctl", append([]string{"--user"}, args...)...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

func service(args []string) error {
	if len(args) == 0 {
		return errors.New("使い方: reviewer service install|uninstall|status|restart")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return errors.New("systemd（systemctl）が見つかりません。このマシンでは reviewer serve を直接起動してください")
	}
	switch args[0] {
	case "install":
		fs := flag.NewFlagSet("service install", flag.ExitOnError)
		port := fs.Int("port", 0, "待ち受けポート（既定: 設定ファイルか 7777）")
		bind := fs.String("bind", "", "待ち受けアドレス（既定: 設定ファイルか all）")
		fs.Parse(args[1:])
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		serveArgs := []string{"serve"}
		if *port != 0 {
			serveArgs = append(serveArgs, "--port", strconv.Itoa(*port))
		}
		if *bind != "" {
			serveArgs = append(serveArgs, "--bind", *bind)
		}
		p, err := unitPath()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(unitFile(exe, serveArgs)), 0o644); err != nil {
			return err
		}
		fmt.Printf("wrote %s\n", p)
		if _, err := persistentToken(); err != nil {
			return err
		}
		if err := systemctl("daemon-reload"); err != nil {
			return err
		}
		if err := systemctl("enable", "--now", serviceName); err != nil {
			return err
		}
		if u, err := user.Current(); err == nil {
			out, _ := exec.Command("loginctl", "show-user", u.Username, "--property=Linger").Output()
			if !strings.Contains(string(out), "Linger=yes") {
				fmt.Printf("\nログアウト後やマシンの再起動後も動かし続けるには、次を実行してください:\n  sudo loginctl enable-linger %s\n", u.Username)
			}
		}
		fmt.Println()
		return printURLs(*port, *bind)
	case "uninstall":
		systemctl("disable", "--now", serviceName)
		p, err := unitPath()
		if err != nil {
			return err
		}
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		fmt.Printf("removed %s\n", p)
		return systemctl("daemon-reload")
	case "status":
		return systemctl("status", "--no-pager", serviceName)
	case "restart":
		return systemctl("restart", serviceName)
	default:
		return fmt.Errorf("unknown service command %q", args[0])
	}
}

// urlCmd prints the access URLs of the (persistent-token) server.
func urlCmd(args []string) error {
	fs := flag.NewFlagSet("url", flag.ExitOnError)
	port := fs.Int("port", 0, "ポート（既定: 設定ファイルか 7777）")
	bind := fs.String("bind", "", "待ち受けアドレス（既定: 設定ファイルか all）")
	fs.Parse(args)
	return printURLs(*port, *bind)
}

func printURLs(port int, bind string) error {
	cfg, err := config.LoadGlobal("")
	if err != nil {
		return err
	}
	if port == 0 {
		port = cfg.Port
	}
	if bind == "" {
		bind = cfg.Bind
	}
	token := os.Getenv("REVIEWER_TOKEN")
	if token == "" {
		if token, err = persistentToken(); err != nil {
			return err
		}
	}
	host := listenHost(bind)
	var addrs []net.Addr
	if isWildcard(host) {
		addrs = interfaceAddrs()
	}
	urls := accessURLs(host, port, token, hostname(), addrs)
	fmt.Printf("Open: %s\n", urls[0])
	for _, u := range urls[1:] {
		fmt.Printf("      %s\n", u)
	}
	return nil
}
