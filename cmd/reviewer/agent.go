package main

import (
	"os"

	"github.com/ymotongpoo/reviewer/internal/agent"
	"github.com/ymotongpoo/reviewer/internal/agent/hermes"
	"github.com/ymotongpoo/reviewer/internal/app"
	"github.com/ymotongpoo/reviewer/internal/config"
)

// setupAgent connects the app to the configured agent. A missing agent is
// not an error: the UI shows the reason and the copy-the-prompt flow keeps
// working.
func setupAgent(a *app.App, cfg config.Config, baseURL string) {
	ac := cfg.Agent
	switch ac.Kind {
	case "none", "":
		a.ConfigureAgent(nil, "", nil, "none", baseURL)
		return
	case "auto", "hermes":
	default:
		a.ConfigureAgent(nil, "未対応のエージェントです: "+ac.Kind, nil, "none", baseURL)
		return
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
		a.ConfigureAgent(nil, reason, nil, "none", baseURL)
		return
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
	a.ConfigureAgent(hermes.New(opts), "", n, notify, baseURL)
}
