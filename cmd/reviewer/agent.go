package main

import (
	"os"

	"github.com/ymotongpoo/reviewer/internal/agent"
	"github.com/ymotongpoo/reviewer/internal/agent/hermes"
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
	case "auto", "hermes":
	default:
		return agentConn{reason: "未対応のエージェントです: " + ac.Kind, notifyName: "none"}
	}
	opts := hermes.Options{URL: ac.Hermes.URL, Profile: ac.Hermes.Profile, Command: ac.Hermes.Command, Home: ac.Hermes.Home}
	if ac.Hermes.APIKeyEnv != "" {
		opts.APIKey = os.Getenv(ac.Hermes.APIKeyEnv)
	}
	opts, err := hermes.Resolve(opts)
	if err != nil {
		reason := err.Error()
		if ac.Kind == "auto" && err == hermes.ErrNotConfigured {
			if _, statErr := os.Stat(hermes.HomeDir()); statErr != nil {
				reason = "" // Hermes is not installed here: stay quiet.
			}
		}
		return agentConn{reason: reason, notifyName: "none"}
	}
	var n agent.Notifier
	notify := ac.Notify
	switch notify {
	case "hermes":
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
