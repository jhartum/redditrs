package main

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jhartum/redditrs/internal/config"
	"github.com/jhartum/redditrs/internal/model"
	"github.com/jhartum/redditrs/internal/rank"
	"github.com/jhartum/redditrs/internal/reddit"
	"github.com/spf13/cobra"
)

type packData struct {
	Topic       string                `json:"topic"`
	Intent      string                `json:"intent"`
	Subreddits  []string              `json:"subreddits,omitempty"`
	Evidence    []model.EvidenceItem  `json:"evidence,omitempty"`
	Posts       []model.Post          `json:"posts,omitempty"`
	Comments    []model.Comment       `json:"comments,omitempty"`
	Clusters    []rank.ClusterSummary `json:"clusters,omitempty"`
	DeepCommand string                `json:"-"`
}

func newPackCommand() *cobra.Command {
	var intent, depth, timeRange, sortValue, subredditScope string
	var maxPosts, legacyMaxPosts, commentsPerPost int
	command := &cobra.Command{
		Use:     "pack <topic>",
		Short:   "Build a Reddit evidence pack",
		Example: "  redditrs pack \"comfyui settings for low VRAM\" --intent settings",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			settings := config.Load()
			if !config.HasSessionCookie(settings.Cookie) {
				return authRequiredError(settings)
			}
			if cmd.Flags().Changed("limit") && cmd.Flags().Changed("max-posts") {
				return &cliError{Message: "use only one of --limit and --max-posts", Code: "VALIDATION_ERROR"}
			}
			if cmd.Flags().Changed("max-posts") {
				maxPosts = legacyMaxPosts
			}
			if !rank.ValidIntent(intent) {
				return &cliError{Message: "--intent must be one of " + strings.Join(rank.Intents(), ", "), Code: "VALIDATION_ERROR"}
			}
			if depth != "quick" && depth != "normal" && depth != "deep" {
				return &cliError{Message: "--depth must be quick, normal, or deep", Code: "VALIDATION_ERROR"}
			}
			defaultPosts, defaultThreads, defaultComments := rank.DepthDefaults(depth)
			if maxPosts == 0 {
				maxPosts = defaultPosts
			}
			if commentsPerPost == 0 {
				commentsPerPost = defaultComments
			}
			if maxPosts < 1 || commentsPerPost < 1 {
				return &cliError{Message: "--limit and --comments-per-post must be positive", Code: "VALIDATION_ERROR"}
			}
			if !slices.Contains([]string{"relevance", "hot", "top", "new", "comments"}, sortValue) {
				return &cliError{Message: "--sort must be relevance, hot, top, new, or comments", Code: "VALIDATION_ERROR"}
			}
			if !slices.Contains([]string{"hour", "day", "week", "month", "year", "all"}, timeRange) {
				return &cliError{Message: "--time must be hour, day, week, month, year, or all", Code: "VALIDATION_ERROR"}
			}
			client := reddit.NewClient()
			var posts []model.Post
			scopes := splitCSV(subredditScope)
			if subredditScope != "" && len(scopes) == 0 {
				return &cliError{Message: "--subreddits must contain at least one name", Code: "VALIDATION_ERROR"}
			}
			if len(scopes) == 0 {
				result, err := client.Search(cmd.Context(), args[0], reddit.SearchOptions{Sort: sortValue, Time: timeRange, Limit: maxPosts})
				if err != nil {
					return mapRedditError(err)
				}
				posts = result.Posts
			} else {
				for _, scope := range scopes {
					result, err := client.Search(cmd.Context(), args[0], reddit.SearchOptions{Subreddit: scope, Sort: sortValue, Time: timeRange, Limit: maxPosts})
					if err != nil {
						return mapRedditError(err)
					}
					posts = append(posts, result.Posts...)
				}
			}
			posts = rank.Posts(uniquePosts(posts), args[0], time.Now())
			if len(posts) > maxPosts {
				posts = posts[:maxPosts]
			}
			data := packData{
				Topic:       args[0],
				Intent:      intent,
				Posts:       posts,
				DeepCommand: packDeepCommand(cmd, args[0], intent, timeRange, sortValue, subredditScope, maxPosts, commentsPerPost),
			}
			data.Subreddits = observedSubreddits(posts)
			threadCount := min(defaultThreads, len(posts))
			for index := range posts[:threadCount] {
				thread, err := client.Thread(cmd.Context(), posts[index].ID, reddit.ThreadOptions{Sort: "top", CommentLimit: commentsPerPost})
				if err != nil {
					continue
				}
				comments := thread.Comments
				if len(comments) > commentsPerPost {
					comments = comments[:commentsPerPost]
				}
				for _, comment := range comments {
					data.Comments = append(data.Comments, comment)
					data.Evidence = append(data.Evidence, model.EvidenceItem{Kind: "comment", Cluster: rank.Classify(comment.Body, intent), PostID: posts[index].ID, PostIndex: index + 1, Subreddit: posts[index].Subreddit, Score: comment.Score, URL: comment.URL, Text: comment.Body, Reason: rank.IntentHint(intent)})
				}
			}
			for index, post := range posts {
				data.Evidence = append(data.Evidence, model.EvidenceItem{Kind: "post", Cluster: rank.Classify(post.Title+" "+post.Selftext, intent), PostID: post.ID, PostIndex: index + 1, Subreddit: post.Subreddit, Score: post.Score, URL: post.URL, Text: post.Title, Reason: rank.IntentHint(intent)})
			}
			data.Evidence = limitEvidence(data.Evidence, 40, 6)
			data.Clusters = rank.SummarizeEvidence(data.Evidence, intent)
			_, err := fmt.Fprint(cmd.OutOrStdout(), renderPack(data, depth))
			return err
		},
	}
	command.Flags().StringVar(&intent, "intent", "general", "intent: opinions, bugs, fixes, compare, settings, alternatives, trends, guides, hardware, or general")
	command.Flags().StringVar(&depth, "depth", "normal", "depth: quick (6 posts, 2 threads x 3 comments), normal (10, 4 x 5), or deep (14, 6 x 8)")
	command.Flags().StringVar(&timeRange, "time", "all", "time range")
	command.Flags().StringVar(&sortValue, "sort", "relevance", "post sort")
	command.Flags().StringVar(&subredditScope, "subreddits", "", "comma-separated subreddit scope")
	command.Flags().IntVar(&maxPosts, "limit", 0, "maximum posts")
	command.Flags().IntVar(&legacyMaxPosts, "max-posts", 0, "maximum posts (legacy alias for --limit)")
	_ = command.Flags().MarkHidden("max-posts")
	command.Flags().IntVar(&commentsPerPost, "comments-per-post", 0, "comments per post")
	return command
}

func packDeepCommand(cmd *cobra.Command, topic, intent, timeRange, sortValue, subredditScope string, maxPosts, commentsPerPost int) string {
	parts := []string{"redditrs", "pack", quoteTOON(topic), "--intent", intent, "--depth", "deep"}
	if cmd.Flags().Changed("time") {
		parts = append(parts, "--time", timeRange)
	}
	if cmd.Flags().Changed("sort") {
		parts = append(parts, "--sort", sortValue)
	}
	if cmd.Flags().Changed("subreddits") {
		parts = append(parts, "--subreddits", subredditScope)
	}
	if cmd.Flags().Changed("limit") || cmd.Flags().Changed("max-posts") {
		parts = append(parts, "--limit", strconv.Itoa(maxPosts))
	}
	if cmd.Flags().Changed("comments-per-post") {
		parts = append(parts, "--comments-per-post", strconv.Itoa(commentsPerPost))
	}
	return strings.Join(parts, " ")
}

func renderPack(data packData, depth string) string {
	if formatFlag == "json" {
		return marshalJSON(data)
	}
	subreddits := strings.Join(data.Subreddits, ", ")
	if subreddits == "" {
		subreddits = "[]"
	}
	lines := []string{
		"pack:",
		"  topic: " + toonString(data.Topic),
		"  intent: " + data.Intent,
		"  subreddits: " + subreddits,
	}
	if depth == "deep" {
		lines = append(lines, renderPackPosts(data.Posts)...)
		lines = append(lines, fmt.Sprintf("comments[%d]{post_id,author,score,body}:", len(data.Comments)))
		for _, comment := range data.Comments {
			author := comment.Author
			if author != "" && !strings.HasPrefix(author, "u/") {
				author = "u/" + author
			}
			lines = append(lines, "  "+strings.Join([]string{toonString(comment.PostID), toonString(author), strconv.Itoa(comment.Score), toonString(truncateText(comment.Body, "body", 500))}, ","))
		}
		lines = append(lines, renderClusters(data.Clusters)...)
	} else {
		lines = append(lines, fmt.Sprintf("evidence[%d]{cluster,count,hint}:", len(data.Clusters)))
		for _, cluster := range data.Clusters {
			lines = append(lines, "  "+strings.Join([]string{cluster.Cluster, strconv.Itoa(cluster.Count), toonString(cluster.Hint)}, ","))
		}
		lines = append(lines, renderPackPosts(data.Posts)...)
	}
	if depth == "deep" {
		lines = append(lines, "help[1]:", "  If one post needs more context, run `redditrs thread <post_id> --top 20 --full`")
	} else {
		deepCommand := data.DeepCommand
		if deepCommand == "" {
			deepCommand = "redditrs pack " + quoteTOON(data.Topic) + " --intent " + data.Intent + " --depth deep"
		}
		lines = append(lines, "help[1]:", "  "+safeHelpLine("If comment quotes are needed, run `"+deepCommand+"`"))
	}
	return strings.Join(lines, "\n")
}

func renderPackPosts(posts []model.Post) []string {
	lines := []string{fmt.Sprintf("posts[%d]{id,subreddit,title,score,age}:", len(posts))}
	for _, post := range posts {
		lines = append(lines, "  "+strings.Join([]string{toonString(post.ID), toonString(post.Subreddit), toonString(post.Title), strconv.Itoa(post.Score), post.Age}, ","))
	}
	return lines
}

func renderClusters(clusters []rank.ClusterSummary) []string {
	lines := []string{fmt.Sprintf("clusters[%d]{cluster,count,hint}:", len(clusters))}
	for _, cluster := range clusters {
		lines = append(lines, "  "+strings.Join([]string{cluster.Cluster, strconv.Itoa(cluster.Count), toonString(cluster.Hint)}, ","))
	}
	return lines
}

func splitCSV(value string) []string {
	var result []string
	seen := make(map[string]bool)
	for _, part := range strings.Split(value, ",") {
		trimmed := strings.TrimSpace(part)
		key := strings.ToLower(trimmed)
		if trimmed != "" && !seen[key] {
			seen[key] = true
			result = append(result, trimmed)
		}
	}
	return result
}

func uniquePosts(posts []model.Post) []model.Post {
	result := make([]model.Post, 0, len(posts))
	seen := make(map[string]bool, len(posts))
	for _, post := range posts {
		if post.ID != "" && seen[post.ID] {
			continue
		}
		seen[post.ID] = true
		result = append(result, post)
	}
	return result
}

func limitEvidence(items []model.EvidenceItem, totalLimit, clusterLimit int) []model.EvidenceItem {
	result := make([]model.EvidenceItem, 0, min(len(items), totalLimit))
	counts := make(map[string]int)
	for _, item := range items {
		if len(result) == totalLimit {
			break
		}
		if counts[item.Cluster] == clusterLimit {
			continue
		}
		counts[item.Cluster]++
		result = append(result, item)
	}
	return result
}

func observedSubreddits(posts []model.Post) []string {
	counts := make(map[string]int)
	for _, post := range posts {
		counts[post.Subreddit]++
	}
	type pair struct {
		Name  string
		Count int
	}
	pairs := make([]pair, 0, len(counts))
	for name, count := range counts {
		pairs = append(pairs, pair{Name: name, Count: count})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].Count == pairs[j].Count {
			left, right := strings.ToLower(pairs[i].Name), strings.ToLower(pairs[j].Name)
			if left == right {
				return pairs[i].Name < pairs[j].Name
			}
			return left < right
		}
		return pairs[i].Count > pairs[j].Count
	})
	if len(pairs) > 12 {
		pairs = pairs[:12]
	}
	result := make([]string, len(pairs))
	for i, item := range pairs {
		result[i] = item.Name + " (" + strconv.Itoa(item.Count) + ")"
	}
	return result
}
