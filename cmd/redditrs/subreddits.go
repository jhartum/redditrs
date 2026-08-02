package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jhartum/redditrs/internal/config"
	"github.com/jhartum/redditrs/internal/model"
	"github.com/jhartum/redditrs/internal/reddit"
	"github.com/spf13/cobra"
)

func newSubredditsCommand() *cobra.Command {
	var limit int
	command := &cobra.Command{
		Use:     "subreddits <query>",
		Short:   "Search for subreddits",
		Example: "  redditrs subreddits \"Claude Code\" --limit 10",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			settings := config.Load()
			if !config.HasSessionCookie(settings.Cookie) {
				return authRequiredError(settings)
			}
			if limit < 1 || limit > 25 {
				return &cliError{Message: "--limit must be between 1 and 25", Code: "VALIDATION_ERROR"}
			}
			items, total, err := reddit.NewClient().Subreddits(cmd.Context(), args[0], limit)
			if err != nil {
				return mapRedditError(err)
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), renderSubreddits(items, total))
			return err
		},
	}
	command.Flags().IntVar(&limit, "limit", 10, "number of subreddits (default 10, max 25)")
	return command
}

func renderSubreddits(items []model.Subreddit, total int) string {
	if items == nil {
		items = []model.Subreddit{}
	}
	if formatFlag == "json" {
		return marshalJSON(struct {
			Subreddits []model.Subreddit `json:"subreddits"`
			Count      int               `json:"count,omitempty"`
		}{Subreddits: items, Count: total})
	}
	if len(items) == 0 {
		return "subreddits: 0 subreddits found"
	}
	lines := []string{fmt.Sprintf("subreddits[%d]{name,title,subscribers}:", len(items))}
	for _, item := range items {
		lines = append(lines, "  "+strings.Join([]string{toonString(item.Name), toonString(item.Title), formatSubscribers(item.Subscribers)}, ","))
	}
	if total > len(items) {
		lines = append(lines, fmt.Sprintf("count: %d of %d total", len(items), total))
	}
	return strings.Join(lines, "\n")
}

func formatSubscribers(value int64) string {
	if value < 1000 {
		return strconv.FormatInt(value, 10)
	}
	if value >= 1_000_000 {
		return compactNumber(float64(value)/1_000_000, "M")
	}
	return compactNumber(float64(value)/1_000, "K")
}

func compactNumber(value float64, suffix string) string {
	text := strconv.FormatFloat(value, 'f', 1, 64)
	text = strings.TrimSuffix(strings.TrimSuffix(text, "0"), ".")
	return text + suffix
}
