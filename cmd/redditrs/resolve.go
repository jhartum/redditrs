package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jhartum/redditrs/internal/cache"
	"github.com/jhartum/redditrs/internal/config"
	"github.com/jhartum/redditrs/internal/model"
	"github.com/jhartum/redditrs/internal/rank"
	"github.com/jhartum/redditrs/internal/reddit"
	"github.com/spf13/cobra"
)

const resolveCacheVersion = "v2"

func newResolveSubredditsCommand() *cobra.Command {
	var limit int
	var refresh bool
	command := &cobra.Command{
		Use:     "resolve-subreddits <topic>",
		Short:   "Rank subreddits for a research topic",
		Example: "  redditrs resolve-subreddits \"local LLM\" --limit 15",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			settings := config.Load()
			if !config.HasSessionCookie(settings.Cookie) {
				return authRequiredError(settings)
			}
			if limit < 1 || limit > 25 {
				return &cliError{Message: "--limit must be between 1 and 25", Code: "VALIDATION_ERROR"}
			}
			cacheKey := "topic:" + resolveCacheKey(args[0])
			if !refresh {
				if store, storeErr := cache.Open(settings.CachePath); storeErr == nil {
					raw, fresh, _ := store.Fresh(cacheKey, time.Now())
					_ = store.Close()
					var cached []model.SubredditCandidate
					if fresh && json.Unmarshal(raw, &cached) == nil {
						total := len(cached)
						cached = cached[:min(len(cached), limit)]
						_, renderErr := fmt.Fprint(cmd.OutOrStdout(), renderResolvedSubreddits(args[0], cached, total))
						return renderErr
					}
				}
			}
			client := reddit.NewClient()
			client.ForceRefresh = refresh
			candidates, err := discoverSubreddits(cmd.Context(), client, args[0])
			if err != nil {
				return mapRedditError(err)
			}
			searchResult, _ := client.Search(cmd.Context(), args[0], reddit.SearchOptions{Limit: 25, Sort: "relevance", Time: "all"})
			known := make(map[string]bool, len(candidates))
			for _, candidate := range candidates {
				known[strings.ToLower(candidate.Name)] = true
			}
			for _, post := range searchResult.Posts {
				if !known[strings.ToLower(post.Subreddit)] {
					candidates = append(candidates, model.Subreddit{Name: post.Subreddit})
					known[strings.ToLower(post.Subreddit)] = true
				}
			}
			resolved := rank.Subreddits(args[0], candidates, searchResult.Posts)
			if store, storeErr := cache.Open(settings.CachePath); storeErr == nil {
				raw, _ := json.Marshal(resolved)
				_ = store.Save(cacheKey, 200, raw, "", time.Now().Add(time.Duration(settings.TopicTTLMS)*time.Millisecond))
				_ = store.Close()
			}
			total := len(resolved)
			if len(resolved) > limit {
				resolved = resolved[:limit]
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), renderResolvedSubreddits(args[0], resolved, total))
			return err
		},
	}
	command.Flags().IntVar(&limit, "limit", 15, "number of candidates (default 15, max 25)")
	command.Flags().BoolVar(&refresh, "refresh", false, "refresh the topic cache")
	return command
}

func resolveCacheKey(topic string) string {
	return resolveCacheVersion + ":" + strings.Join(strings.Fields(strings.ToLower(topic)), " ")
}

func subredditDiscoveryQueries(topic string) []string {
	normalized := strings.Join(strings.Fields(strings.TrimSpace(topic)), " ")
	queries := []string{normalized}
	seen := map[string]bool{strings.ToLower(normalized): true}
	stopwords := map[string]bool{
		"and": true, "or": true, "not": true, "the": true, "for": true,
		"with": true, "versus": true, "vs": true, "about": true,
		"advice": true, "help": true, "best": true, "practical": true,
		"current": true, "latest": true,
	}
	for _, field := range strings.Fields(normalized) {
		token := strings.Trim(field, "\"'`()[]{}.,;!?")
		lower := strings.ToLower(token)
		if lower == "" || strings.Contains(lower, ":") || stopwords[lower] || len([]rune(lower)) < 3 || seen[lower] {
			continue
		}
		queries = append(queries, token)
		seen[lower] = true
		if len(queries) == 4 {
			break
		}
	}
	return queries
}

func discoverSubreddits(ctx context.Context, client *reddit.Client, topic string) ([]model.Subreddit, error) {
	var candidates []model.Subreddit
	positions := make(map[string]int)
	for queryIndex, query := range subredditDiscoveryQueries(topic) {
		items, _, err := client.Subreddits(ctx, query, 25)
		if err != nil {
			if queryIndex == 0 {
				return nil, err
			}
			continue
		}
		seenInQuery := make(map[string]bool, len(items))
		for _, item := range items {
			key := strings.ToLower(strings.TrimSpace(item.Name))
			if key == "" {
				continue
			}
			if position, exists := positions[key]; exists {
				existing := &candidates[position]
				if item.Title != "" {
					existing.Title = item.Title
				}
				if item.PublicDescription != "" {
					existing.PublicDescription = item.PublicDescription
				}
				if item.Subscribers > existing.Subscribers {
					existing.Subscribers = item.Subscribers
				}
				if !seenInQuery[key] {
					existing.DiscoveryMatches++
				}
			} else {
				item.DiscoveryMatches = 1
				positions[key] = len(candidates)
				candidates = append(candidates, item)
			}
			seenInQuery[key] = true
		}
	}
	return candidates, nil
}

func resolvedDescription(value string) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= 240 {
		return value
	}
	return string(runes[:240]) + "…"
}

func renderResolvedSubreddits(topic string, items []model.SubredditCandidate, total int) string {
	if items == nil {
		items = []model.SubredditCandidate{}
	}
	if formatFlag == "json" {
		return marshalJSON(struct {
			Subreddits []model.SubredditCandidate `json:"subreddits"`
			Count      int                        `json:"count,omitempty"`
		}{Subreddits: items, Count: total})
	}
	if len(items) == 0 {
		return "subreddits: 0 candidates found"
	}
	lines := []string{fmt.Sprintf("subreddits[%d]{name,score,reasons,subscribers,title,description}:", len(items))}
	for _, item := range items {
		score := strconv.FormatFloat(item.Score, 'f', 0, 64)
		lines = append(lines, "  "+strings.Join([]string{toonString(item.Name), score, toonString(strings.Join(item.Reasons, "; ")), formatSubscribers(item.Subscribers), toonString(item.Title), toonString(resolvedDescription(item.PublicDescription))}, ","))
	}
	if total > len(items) {
		lines = append(lines, fmt.Sprintf("count: %d of %d total", len(items), total))
	}
	lines = append(lines, "help[1]:", "  "+safeHelpLine("If recent examples are needed, run `redditrs search "+quoteTOON(topic)+" --subreddits "+items[0].Name+" --sort new`"))
	return strings.Join(lines, "\n")
}
