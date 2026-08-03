package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/jhartum/redditrs/internal/config"
	"github.com/jhartum/redditrs/internal/model"
	"github.com/jhartum/redditrs/internal/reddit"
	"github.com/spf13/cobra"
)

func newThreadCommand() *cobra.Command {
	var sort string
	var commentLimit int
	var top int
	command := &cobra.Command{
		Use:     "thread <url_or_id>",
		Short:   "Read a Reddit thread and top comments",
		Example: "  redditrs thread 1abc234 --top 10",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			settings := config.Load()
			if !config.HasSessionCookie(settings.Cookie) {
				return authRequiredError(settings)
			}
			if commentLimit < 1 || commentLimit > 200 || top < 1 || top > 40 {
				return &cliError{Message: "--comment-limit must be 1..200 and --top must be 1..40", Code: "VALIDATION_ERROR"}
			}
			if !slices.Contains([]string{"top", "new", "controversial", "confidence"}, sort) {
				return &cliError{Message: "--sort must be top, new, controversial, or confidence", Code: "VALIDATION_ERROR"}
			}
			if _, err := selectedCommentFields(); err != nil {
				return err
			}
			ref := reddit.ExtractURL(args[0])
			if ref.PostID == "" {
				return &cliError{Message: "thread " + args[0] + " not found", Code: "NOT_FOUND"}
			}
			result, err := reddit.NewClient().Thread(cmd.Context(), ref.PostID, reddit.ThreadOptions{Sort: sort, CommentLimit: commentLimit})
			if err != nil {
				if httpErr, ok := err.(*reddit.HTTPError); ok && httpErr.StatusCode == 404 {
					return &cliError{Message: "thread " + ref.PostID + " not found", Code: "NOT_FOUND"}
				}
				return mapRedditError(err)
			}
			if result.Post.ID == "" {
				return &cliError{Message: "thread " + ref.PostID + " not found", Code: "NOT_FOUND"}
			}
			if len(result.Comments) > top {
				result.Comments = result.Comments[:top]
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), renderThread(result))
			return err
		},
	}
	command.Flags().StringVar(&sort, "sort", "top", "comment sort: top, new, controversial, or confidence")
	command.Flags().IntVar(&commentLimit, "comment-limit", 50, "number of comments requested (default 50, max 200)")
	command.Flags().IntVar(&top, "top", 10, "number of comments shown (default 10, max 40)")
	return command
}

func renderThread(result reddit.ThreadResult) string {
	if result.Comments == nil {
		result.Comments = []model.Comment{}
	}
	if formatFlag == "json" {
		return marshalJSON(struct {
			Post     model.Post      `json:"post"`
			Comments []model.Comment `json:"comments"`
		}{Post: result.Post, Comments: result.Comments})
	}
	author := result.Post.Author
	if author != "" && !strings.HasPrefix(author, "u/") {
		author = "u/" + author
	}
	truncated := false
	if !fullFlag && len([]rune(result.Post.Selftext)) > 500 {
		truncated = true
	}
	for _, comment := range result.Comments {
		if !fullFlag && len([]rune(comment.Body)) > 500 {
			truncated = true
			break
		}
	}
	lines := []string{
		"post:",
		"  id: " + toonString(result.Post.ID),
		"  subreddit: " + toonString(result.Post.Subreddit),
		"  title: " + toonString(result.Post.Title),
		"  author: " + toonString(author),
		"  score: " + strconv.Itoa(result.Post.Score),
		"  comments: " + strconv.Itoa(result.Post.NumComments),
		"  age: " + result.Post.Age,
		"  url: " + toonString(result.Post.URL),
	}
	if result.Post.Selftext != "" {
		lines = append(lines, "  selftext: "+toonString(truncateText(result.Post.Selftext, "selftext", 500)))
	}
	commentFields, _ := selectedCommentFields()
	lines = append(lines, fmt.Sprintf("comments[%d]{%s}:", len(result.Comments), strings.Join(commentFields, ",")))
	for _, comment := range result.Comments {
		values := make([]string, len(commentFields))
		for index, field := range commentFields {
			values[index] = commentFieldValue(comment, field)
		}
		lines = append(lines, "  "+strings.Join(values, ","))
	}
	if result.Post.NumComments > len(result.Comments) {
		lines = append(lines, fmt.Sprintf("count: %d of %d total", len(result.Comments), result.Post.NumComments))
	}
	if truncated {
		lines = append(lines, "help[1]:", "  "+safeHelpLine("If the truncated text is needed, run `redditrs thread "+result.Post.ID+" --full`"))
	}
	return strings.Join(lines, "\n")
}

func selectedCommentFields() ([]string, error) {
	if fieldsFlag == "" {
		return []string{"author", "score", "body"}, nil
	}
	fields := splitCSV(fieldsFlag)
	allowed := map[string]bool{"author": true, "score": true, "body": true, "post_id": true, "depth": true, "created_utc": true, "url": true}
	for _, field := range fields {
		if !allowed[field] {
			return nil, &cliError{Message: "unknown field " + field, Code: "VALIDATION_ERROR"}
		}
	}
	return fields, nil
}

func commentFieldValue(comment model.Comment, field string) string {
	author := comment.Author
	if author != "" && !strings.HasPrefix(author, "u/") {
		author = "u/" + author
	}
	switch field {
	case "author":
		return toonString(author)
	case "score":
		return strconv.Itoa(comment.Score)
	case "body":
		return toonString(truncateText(comment.Body, "body", 500))
	case "post_id":
		return toonString(comment.PostID)
	case "depth":
		return strconv.Itoa(comment.Depth)
	case "created_utc":
		return strconv.FormatFloat(comment.CreatedUTC, 'f', -1, 64)
	case "url":
		return toonString(comment.URL)
	default:
		return ""
	}
}

func truncateText(value, field string, limit int) string {
	if fullFlag || len([]rune(value)) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit]) + fmt.Sprintf("\n... (truncated, %d chars total - use --full to see complete %s)", len(runes), field)
}
