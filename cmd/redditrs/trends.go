package main

import (
	"fmt"
	"slices"
	"time"

	"github.com/jhartum/redditrs/internal/config"
	"github.com/jhartum/redditrs/internal/model"
	"github.com/jhartum/redditrs/internal/rank"
	"github.com/jhartum/redditrs/internal/reddit"
	"github.com/spf13/cobra"
)

func newTrendsCommand() *cobra.Command {
	var sortValue string
	var timeRange string
	var limit int
	command := &cobra.Command{
		Use:     "trends <subs>",
		Short:   "Show hot, top, or new posts in subreddits",
		Example: "  redditrs trends LocalLLaMA --sort hot",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			settings := config.Load()
			if !config.HasSessionCookie(settings.Cookie) {
				return authRequiredError(settings)
			}
			if limit < 1 || limit > 30 {
				return &cliError{Message: "--limit must be between 1 and 30", Code: "VALIDATION_ERROR"}
			}
			if !slices.Contains([]string{"hot", "top", "new"}, sortValue) {
				return &cliError{Message: "--sort must be hot, top, or new", Code: "VALIDATION_ERROR"}
			}
			if _, err := selectedSearchFields(); err != nil {
				return err
			}
			subreddits := splitCSV(args[0])
			if len(subreddits) == 0 {
				return &cliError{Message: "<subs> must contain at least one subreddit", Code: "VALIDATION_ERROR"}
			}
			perSubredditLimit := (limit + len(subreddits) - 1) / len(subreddits)
			var result reddit.SearchResult
			client := reddit.NewClient()
			for _, subreddit := range subreddits {
				part, err := client.Trends(cmd.Context(), subreddit, sortValue, timeRange, perSubredditLimit)
				if err != nil {
					return mapRedditError(err)
				}
				result.Posts = append(result.Posts, part.Posts...)
				result.Total += part.Total
			}
			for index := range result.Posts {
				result.Posts[index].Age = model.RelativeAge(result.Posts[index].CreatedUTC, time.Now())
			}
			result.Posts = rank.Posts(uniquePosts(result.Posts), args[0], time.Now())
			if len(result.Posts) > limit {
				result.Posts = result.Posts[:limit]
			}
			_, err := fmt.Fprint(cmd.OutOrStdout(), renderSearch(args[0], result))
			return err
		},
	}
	command.Flags().StringVar(&sortValue, "sort", "hot", "sort: hot, top, or new")
	command.Flags().StringVar(&timeRange, "time", "week", "time range for top")
	command.Flags().IntVar(&limit, "limit", 10, "number of posts (default 10, max 30)")
	return command
}
