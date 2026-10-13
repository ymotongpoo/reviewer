package main

import (
	"os"
	"path/filepath"

	"github.com/ymotongpoo/reviewer/internal/agent"
	"github.com/ymotongpoo/reviewer/internal/agent/hermes"
	"github.com/ymotongpoo/reviewer/internal/agent/t3"
	"github.com/ymotongpoo/reviewer/internal/app"
	"github.com/ymotongpoo/reviewer/internal/config"
)

// agentConn is the agent connection shared by all projects.
type agentConn struct {
	ag         agent.Agent
	reason     string
	notifier   agent.Notifier
	notifyName string
}

// setup connects a project to the agent; url is the project page.
func (c agentConn) setup(a *app.App, url string) {
	a.ConfigureAgent(c.ag, c.reason, c.notifier, c.notifyName, url)
}

// connectAgent builds the configured agent connection. A missing agent is
// not an error: the UI shows the reason and the copy-the-prompt flow keeps
// working.
func connectAgent(cfg config.Config) agentConn {
	ac := cfg.Agent
	switch ac.Kind {
	case "none", "":
		return agentConn{notifyName: "none"}
	case "t3":
		return connectT3(cfg, false)
	case "hermes":
		return connectHermes(cfg, false)
	case "auto":
		if c := connectT3(cfg, true); c.ag != nil || c.reason != "" {
			return c
		}
		return connectHermes(cfg, true)
	default:
		return agentConn{reason: "未対応のエージェントです: " + ac.Kind, notifyName: "none"}
	}
}

// connectT3 connects to T3 Code. With auto, a machine without T3 Code
// yields an empty connection so that Hermes is tried next.
func connectT3(cfg config.Config, auto bool) agentConn {
	tc := cfg.Agent.T3
	opts := t3.Options{URL: tc.URL, Command: tc.Command, Home: tc.Home, Access: tc.Access,
		TokenFile: filepath.Join(config.StateDir(), "t3-token.json")}
	if tc.TokenEnv != "" {
		opts.Token = os.Getenv(tc.TokenEnv)
	}
	opts, err := t3.Resolve(opts)
	if err != nil {
		if auto && err == t3.ErrNotConfigured {
			return agentConn{}
		}
		return agentConn{reason: err.Error(), notifyName: "none"}
	}
	c := agentConn{ag: t3.New(opts), notifyName: "none"}
	switch cfg.Agent.Notify {
	case "discord-webhook":
		c.notifier, c.notifyName = agent.DiscordWebhook{URL: cfg.Agent.DiscordWebhook.URL}, "discord-webhook"
	case "hermes":
		c.notifyName = "none（T3 Code では hermes に通知できません）"
	}
	return c
}

func connectHermes(cfg config.Config, auto bool) agentConn {
	ac := cfg.Agent
	opts := hermes.Options{URL: ac.Hermes.URL, Profile: ac.Hermes.Profile, Command: ac.Hermes.Command, Home: ac.Hermes.Home}
	if ac.Hermes.APIKeyEnv != "" {
		opts.APIKey = os.Getenv(ac.Hermes.APIKeyEnv)
	}
	opts, err := hermes.Resolve(opts)
	if err != nil {
		reason := err.Error()
		if auto && err == hermes.ErrNotConfigured {
			if _, statErr := os.Stat(hermes.HomeDir()); statErr != nil {
				reason = "" // Hermes is not installed here: stay quiet.
			}
		}
		return agentConn{reason: reason, notifyName: "none"}
	}
	var n agent.Notifier
	notify := ac.Notify
	switch notify {
	case "hermes", "auto":
		notify = "hermes"
		if opts.Command == "" {
			notify = "none（hermes コマンドが見つかりません）"
		} else {
			n = hermes.NewNotifier(opts.Command, opts.Home)
		}
	case "discord-webhook":
		n = agent.DiscordWebhook{URL: ac.DiscordWebhook.URL}
	default:
		notify = "none"
	}
	return agentConn{ag: hermes.New(opts), notifier: n, notifyName: notify}
}
