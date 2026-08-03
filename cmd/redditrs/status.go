package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jhartum/redditrs/internal/cache"
	"github.com/jhartum/redditrs/internal/config"
	"github.com/spf13/cobra"
)

type statusData struct {
	Version   string `json:"version"`
	Cookie    string `json:"cookie"`
	Config    string `json:"config"`
	Cache     string `json:"cache"`
	Requests  int64  `json:"requests"`
	CacheSize string `json:"cache_size"`
	Cooldown  string `json:"cooldown"`
	DelayMS   int    `json:"delay_ms"`
}

func newStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Short:   "Show configuration, cache, and cooldown status",
		Example: "  redditrs status",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			settings := config.Load()
			stats := cache.ReadStats(settings.CachePath)
			_, err := fmt.Fprint(cmd.OutOrStdout(), renderStatus(settings, stats))
			return err
		},
	}
}

func renderStatus(settings config.Settings, stats cache.Stats) string {
	cookie := cookieSummary(settings.Cookie)
	cooldown := formatCooldown(stats.Cooldown)
	resolvedVersion := currentVersion()
	if formatFlag == "json" {
		return marshalJSON(struct {
			Status statusData `json:"status"`
			Help   []string   `json:"help"`
		}{
			Status: statusData{
				Version:   resolvedVersion,
				Cookie:    cookie,
				Config:    displayPath(settings.ConfigPath),
				Cache:     displayPath(settings.CachePath),
				Requests:  stats.Requests,
				CacheSize: cache.FormatBytes(stats.SizeBytes),
				Cooldown:  cooldown,
				DelayMS:   settings.DelayMS,
			},
			Help: []string{"To verify live Reddit access, run `redditrs search \"test\" --limit 1`"},
		})
	}

	return strings.Join([]string{
		"status:",
		"  version: " + resolvedVersion,
		"  cookie: " + cookie,
		"  config: " + toonString(displayPath(settings.ConfigPath)),
		"  cache: " + toonString(fmt.Sprintf("%s (%d requests, %s)", displayPath(settings.CachePath), stats.Requests, cache.FormatBytes(stats.SizeBytes))),
		"  cooldown: " + cooldown,
		fmt.Sprintf("  delay_ms: %d", settings.DelayMS),
		"help[1]:",
		"  To verify live Reddit access, run `redditrs search \"test\" --limit 1`",
	}, "\n")
}

func formatCooldown(value time.Duration) string {
	if value <= 0 {
		return "0s"
	}
	return strconv.Itoa(int((value+time.Second-1)/time.Second)) + "s"
}

func cookieSummary(raw string) string {
	names := config.CookieNames(raw)
	if len(names) == 0 {
		return "not set"
	}
	return "set (" + strings.Join(names, ", ") + ")"
}
