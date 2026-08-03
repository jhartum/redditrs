package main

import (
	"fmt"
	"strings"

	"github.com/jhartum/redditrs/internal/cache"
	"github.com/jhartum/redditrs/internal/config"
	"github.com/spf13/cobra"
)

const homeDescription = "Reddit research for agents — search, threads, evidence packs, trends (cookie auth, no API keys)"

func runHome(cmd *cobra.Command, _ []string) error {
	settings := config.Load()
	stats := cache.ReadStats(settings.CachePath)
	_, err := fmt.Fprint(cmd.OutOrStdout(), renderHome(settings, stats))
	return err
}

func renderHome(settings config.Settings, stats cache.Stats) string {
	cookie := cookieSummary(settings.Cookie)
	if formatFlag == "json" {
		help := []string{"Run `redditrs status` for config, cache and cooldown details"}
		if !config.HasSessionCookie(settings.Cookie) {
			help = []string{"Put the cookie into " + displayPath(settings.ConfigPath) + " or set REDDITRS_COOKIE", help[0]}
		}
		return marshalJSON(struct {
			Bin           string   `json:"bin"`
			Description   string   `json:"description"`
			Cookie        string   `json:"cookie"`
			CachePath     string   `json:"cache"`
			CacheRequests int64    `json:"cache_requests"`
			Help          []string `json:"help"`
		}{"~/.local/bin/redditrs", homeDescription, cookie, displayPath(settings.CachePath), stats.Requests, help})
	}
	lines := []string{
		"bin: ~/.local/bin/redditrs",
		"description: " + homeDescription,
		"cookie: " + cookieSummary(settings.Cookie),
		"cache: " + toonString(fmt.Sprintf("%d requests in %s", stats.Requests, displayPath(settings.CachePath))),
	}
	if !config.HasSessionCookie(settings.Cookie) {
		lines = append(lines,
			"help[2]:",
			"  "+safeHelpLine("Put the cookie into "+displayPath(settings.ConfigPath)+" or set REDDITRS_COOKIE"),
			"  Run `redditrs status` for config, cache and cooldown details",
		)
	} else {
		lines = append(lines,
			"help[4]:",
			"  Run `redditrs search \"claude code vs opencode\"` to find posts",
			"  Run `redditrs pack \"comfyui settings for low VRAM\" --intent settings` for an evidence pack",
			"  Run `redditrs trends LocalLLaMA` to see what's hot",
			"  Run `redditrs status` for config, cache and cooldown details",
		)
	}
	return strings.Join(lines, "\n")
}
