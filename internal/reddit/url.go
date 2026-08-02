package reddit

import (
	"net/url"
	"strings"
)

type URLReference struct {
	Kind         string `json:"kind"`
	Subreddit    string `json:"subreddit,omitempty"`
	PostID       string `json:"post_id,omitempty"`
	CommentID    string `json:"comment_id,omitempty"`
	Username     string `json:"username,omitempty"`
	CanonicalURL string `json:"canonical_url,omitempty"`
}

func ExtractURL(raw string) URLReference {
	value := strings.TrimSpace(raw)
	if len(value) >= 3 && strings.EqualFold(value[:3], "t1_") && isPostID(value[3:]) {
		return URLReference{
			Kind:      "comment",
			CommentID: value[3:],
		}
	}

	postID := value
	if len(postID) >= 3 && strings.EqualFold(postID[:3], "t3_") {
		postID = postID[3:]
	}
	if isPostID(postID) {
		return URLReference{
			Kind:         "post",
			PostID:       postID,
			CanonicalURL: "https://www.reddit.com/comments/" + postID + "/",
		}
	}

	parseValue := value
	if strings.HasPrefix(parseValue, "/") {
		parseValue = "https://www.reddit.com" + parseValue
	}
	parsed, err := url.Parse(parseValue)
	if err != nil {
		return URLReference{Kind: "unknown"}
	}

	if !isRedditHost(parsed.Hostname()) {
		if parsed.Scheme == "" && parsed.Host == "" {
			return URLReference{Kind: "unknown"}
		}
		return URLReference{Kind: "unknown", CanonicalURL: parsed.String()}
	}

	parts := strings.FieldsFunc(parsed.Path, func(char rune) bool { return char == '/' })
	if len(parts) == 2 && strings.EqualFold(parts[0], "r") && parts[1] != "" {
		subreddit := parts[1]
		return URLReference{
			Kind:         "subreddit",
			Subreddit:    subreddit,
			CanonicalURL: "https://www.reddit.com/r/" + subreddit + "/",
		}
	}

	if len(parts) == 2 && (strings.EqualFold(parts[0], "u") || strings.EqualFold(parts[0], "user")) && parts[1] != "" {
		username := parts[1]
		return URLReference{
			Kind:         "user",
			Username:     username,
			CanonicalURL: "https://www.reddit.com/user/" + username + "/",
		}
	}

	if len(parts) >= 2 && strings.EqualFold(parts[0], "comments") && isPostID(parts[1]) {
		postID := parts[1]
		if len(parts) >= 4 && isPostID(parts[3]) {
			commentID := parts[3]
			return URLReference{
				Kind:         "comment",
				PostID:       postID,
				CommentID:    commentID,
				CanonicalURL: "https://www.reddit.com/comments/" + postID + "/_/" + commentID + "/",
			}
		}
		return URLReference{
			Kind:         "post",
			PostID:       postID,
			CanonicalURL: "https://www.reddit.com/comments/" + postID + "/",
		}
	}

	if len(parts) < 4 || !strings.EqualFold(parts[0], "r") || !strings.EqualFold(parts[2], "comments") {
		return URLReference{Kind: "unknown"}
	}

	subreddit, postID := parts[1], parts[3]
	if !isPostID(postID) {
		return URLReference{Kind: "unknown"}
	}
	if len(parts) >= 6 && isPostID(parts[5]) {
		commentID := parts[5]
		return URLReference{
			Kind:         "comment",
			Subreddit:    subreddit,
			PostID:       postID,
			CommentID:    commentID,
			CanonicalURL: "https://www.reddit.com/r/" + subreddit + "/comments/" + postID + "/_/" + commentID + "/",
		}
	}

	return URLReference{
		Kind:         "post",
		Subreddit:    subreddit,
		PostID:       postID,
		CanonicalURL: "https://www.reddit.com/r/" + subreddit + "/comments/" + postID + "/",
	}
}

func isPostID(value string) bool {
	if len(value) < 5 || len(value) > 12 {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') {
			return false
		}
	}
	return true
}

func isRedditHost(host string) bool {
	host = strings.ToLower(host)
	return host == "reddit.com" || host == "www.reddit.com" || host == "old.reddit.com" || host == "new.reddit.com"
}
