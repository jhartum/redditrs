package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jhartum/redditrs/internal/config"
	"github.com/jhartum/redditrs/internal/model"
	"github.com/jhartum/redditrs/internal/rank"
	"github.com/jhartum/redditrs/internal/reddit"
	"github.com/spf13/cobra"
)

func newSearchCommand() *cobra.Command {
	var options reddit.SearchOptions
	command := &cobra.Command{
		Use:     "search <query>",
		Short:   "Search Reddit posts",
		Example: "  redditrs search \"claude code vs opencode\" --limit 8",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			settings := config.Load()
			if !config.HasSessionCookie(settings.Cookie) {
				return authRequiredError(settings)
			}
			if options.Limit < 1 || options.Limit > 25 {
				return &cliError{Message: "--limit must be between 1 and 25", Code: "VALIDATION_ERROR"}
			}
			if !slices.Contains([]string{"relevance", "hot", "top", "new", "comments"}, options.Sort) {
				return &cliError{Message: "--sort must be relevance, hot, top, new, or comments", Code: "VALIDATION_ERROR"}
			}
			if !slices.Contains([]string{"hour", "day", "week", "month", "year", "all"}, options.Time) {
				return &cliError{Message: "--time must be hour, day, week, month, year, or all", Code: "VALIDATION_ERROR"}
			}
			if _, err := selectedSearchFields(); err != nil {
				return err
			}
			scopes := splitCSV(options.Subreddit)
			if options.Subreddit != "" && len(scopes) == 0 {
				return &cliError{Message: "--subreddits must contain at least one name", Code: "VALIDATION_ERROR"}
			}
			client := reddit.NewClient()
			var result reddit.SearchResult
			var err error
			if len(scopes) == 0 {
				options.Subreddit = ""
				result, err = client.Search(cmd.Context(), args[0], options)
				if err != nil {
					return mapRedditError(err)
				}
			} else {
				for _, scope := range scopes {
					scopedOptions := options
					scopedOptions.Subreddit = scope
					part, searchErr := client.Search(cmd.Context(), args[0], scopedOptions)
					if searchErr != nil {
						return mapRedditError(searchErr)
					}
					result.Posts = append(result.Posts, part.Posts...)
					result.Total += part.Total
				}
			}
			result.Posts = rank.Posts(uniquePosts(result.Posts), args[0], time.Now())
			if len(result.Posts) > options.Limit {
				result.Posts = result.Posts[:options.Limit]
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), renderSearch(args[0], result))
			return err
		},
	}
	command.Flags().StringVar(&options.Subreddit, "subreddits", "", "comma-separated subreddit scope")
	command.Flags().StringVar(&options.Sort, "sort", "relevance", "sort: relevance, hot, top, new, or comments")
	command.Flags().StringVar(&options.Time, "time", "all", "time range: hour, day, week, month, year, or all")
	command.Flags().IntVar(&options.Limit, "limit", 8, "number of posts (default 8, max 25)")
	return command
}

func authRequiredError(settings config.Settings) *cliError {
	return &cliError{
		Message: "no Reddit cookie configured — Reddit blocks unauthenticated .json access since June 2026",
		Code:    "AUTH_REQUIRED",
		Help: []string{
			"Put the cookie into " + displayPath(settings.ConfigPath) + " or set REDDITRS_COOKIE",
			"How to extract it: DevTools → Network → any reddit.com request → Request Headers → Cookie",
		},
	}
}

func renderSearch(query string, result reddit.SearchResult) string {
	if result.Posts == nil {
		result.Posts = []model.Post{}
	}
	if formatFlag == "json" {
		return marshalJSON(struct {
			Posts []model.Post `json:"posts"`
			Count int          `json:"count,omitempty"`
		}{Posts: result.Posts, Count: result.Total})
	}
	if len(result.Posts) == 0 {
		query = quoteTOON(query)
		return "posts: 0 posts found for " + query + "\nhelp[1]:\n  Run `redditrs search " + query + " --time year` to widen the time window"
	}

	fields, _ := selectedSearchFields()
	lines := []string{fmt.Sprintf("posts[%d]{%s}:", len(result.Posts), strings.Join(fields, ","))}
	for _, post := range result.Posts {
		values := make([]string, len(fields))
		for index, field := range fields {
			values[index] = searchFieldValue(post, field)
		}
		lines = append(lines, "  "+strings.Join(values, ","))
	}
	if result.Total > len(result.Posts) {
		lines = append(lines, fmt.Sprintf("count: %d of %d total", len(result.Posts), result.Total))
	}
	lines = append(lines,
		"help[2]:",
		"  "+safeHelpLine("If comments are needed, run `redditrs thread "+result.Posts[0].ID+" --top 10`"),
		"  "+safeHelpLine("If results are too broad, narrow with `redditrs search "+quoteTOON(query)+" --subreddits "+result.Posts[0].Subreddit+"`"),
	)
	return strings.Join(lines, "\n")
}

func selectedSearchFields() ([]string, error) {
	if fieldsFlag == "" {
		return []string{"id", "subreddit", "title", "score", "age"}, nil
	}
	fields := splitCSV(fieldsFlag)
	allowed := map[string]bool{"id": true, "subreddit": true, "title": true, "score": true, "age": true, "author": true, "url": true, "permalink": true, "num_comments": true, "flair": true, "created_utc": true, "selftext": true}
	for _, field := range fields {
		if !allowed[field] {
			return nil, &cliError{Message: "unknown field " + field, Code: "VALIDATION_ERROR"}
		}
	}
	if len(fields) == 0 {
		return nil, &cliError{Message: "--fields must not be empty", Code: "VALIDATION_ERROR"}
	}
	return fields, nil
}

func searchFieldValue(post model.Post, field string) string {
	switch field {
	case "id":
		return toonString(post.ID)
	case "subreddit":
		return toonString(post.Subreddit)
	case "title":
		return toonString(post.Title)
	case "score":
		return strconv.Itoa(post.Score)
	case "age":
		return post.Age
	case "author":
		author := post.Author
		if author != "" && !strings.HasPrefix(author, "u/") {
			author = "u/" + author
		}
		return toonString(author)
	case "url":
		return toonString(post.URL)
	case "permalink":
		return toonString(post.Permalink)
	case "num_comments":
		return strconv.Itoa(post.NumComments)
	case "flair":
		return toonString(post.Flair)
	case "created_utc":
		return strconv.FormatFloat(post.CreatedUTC, 'f', -1, 64)
	case "selftext":
		return toonString(truncateText(post.Selftext, "selftext", 500))
	default:
		return ""
	}
}

func marshalJSON(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

var toonNumberLike = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

func toonString(value string) string {
	if value == "" || strings.TrimSpace(value) != value || value == "true" || value == "false" || value == "null" || toonNumberLike.MatchString(value) || strings.HasPrefix(value, "-") || strings.HasPrefix(value, "#") || strings.ContainsAny(value, ",:\"\\[]{}\r\n\t") || !utf8.ValidString(value) {
		return quoteTOON(value)
	}
	for _, char := range value {
		if char < 0x20 {
			return quoteTOON(value)
		}
	}
	return value
}

func quoteTOON(value string) string {
	var builder strings.Builder
	builder.WriteByte('"')
	for _, char := range value {
		switch char {
		case '\\':
			builder.WriteString(`\\`)
		case '"':
			builder.WriteString(`\"`)
		case '\n':
			builder.WriteString(`\n`)
		case '\r':
			builder.WriteString(`\r`)
		case '\t':
			builder.WriteString(`\t`)
		default:
			if char < 0x20 {
				_, _ = fmt.Fprintf(&builder, `\u%04X`, char)
			} else {
				builder.WriteRune(char)
			}
		}
	}
	builder.WriteByte('"')
	return builder.String()
}

func safeHelpLine(value string) string {
	for _, char := range value {
		if char < 0x20 {
			return quoteTOON(value)
		}
	}
	return value
}
