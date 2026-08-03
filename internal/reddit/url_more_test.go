package reddit_test

import (
	"testing"

	"github.com/jhartum/redditrs/internal/reddit"
)

func TestExtractURLCoversRedditReferenceForms(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want reddit.URLReference
	}{
		{name: "comment fullname", raw: "t1_abcde", want: reddit.URLReference{Kind: "comment", CommentID: "abcde"}},
		{name: "post fullname", raw: "T3_abcde", want: reddit.URLReference{Kind: "post", PostID: "abcde", CanonicalURL: "https://www.reddit.com/comments/abcde/"}},
		{name: "bare post", raw: "abcde", want: reddit.URLReference{Kind: "post", PostID: "abcde", CanonicalURL: "https://www.reddit.com/comments/abcde/"}},
		{name: "subreddit", raw: "https://www.reddit.com/r/Go/", want: reddit.URLReference{Kind: "subreddit", Subreddit: "Go", CanonicalURL: "https://www.reddit.com/r/Go/"}},
		{name: "user", raw: "https://old.reddit.com/u/alice/", want: reddit.URLReference{Kind: "user", Username: "alice", CanonicalURL: "https://www.reddit.com/user/alice/"}},
		{name: "user alias", raw: "https://new.reddit.com/user/bob/", want: reddit.URLReference{Kind: "user", Username: "bob", CanonicalURL: "https://www.reddit.com/user/bob/"}},
		{name: "comments post", raw: "https://reddit.com/comments/abcde/slug/", want: reddit.URLReference{Kind: "post", PostID: "abcde", CanonicalURL: "https://www.reddit.com/comments/abcde/"}},
		{name: "comments comment", raw: "https://www.reddit.com/comments/abcde/slug/fghij/", want: reddit.URLReference{Kind: "comment", PostID: "abcde", CommentID: "fghij", CanonicalURL: "https://www.reddit.com/comments/abcde/_/fghij/"}},
		{name: "subreddit post", raw: "https://www.reddit.com/r/Go/comments/abcde/slug/", want: reddit.URLReference{Kind: "post", Subreddit: "Go", PostID: "abcde", CanonicalURL: "https://www.reddit.com/r/Go/comments/abcde/"}},
		{name: "subreddit comment", raw: "https://www.reddit.com/r/Go/comments/abcde/slug/fghij/", want: reddit.URLReference{Kind: "comment", Subreddit: "Go", PostID: "abcde", CommentID: "fghij", CanonicalURL: "https://www.reddit.com/r/Go/comments/abcde/_/fghij/"}},
		{name: "relative subreddit post", raw: " /r/Go/comments/abcde/slug/ ", want: reddit.URLReference{Kind: "post", Subreddit: "Go", PostID: "abcde", CanonicalURL: "https://www.reddit.com/r/Go/comments/abcde/"}},
		{name: "external URL", raw: "https://example.com/path?q=1", want: reddit.URLReference{Kind: "unknown", CanonicalURL: "https://example.com/path?q=1"}},
		{name: "plain unknown", raw: "not a reference", want: reddit.URLReference{Kind: "unknown"}},
		{name: "short id", raw: "abcd", want: reddit.URLReference{Kind: "unknown"}},
		{name: "invalid id character", raw: "abcde_", want: reddit.URLReference{Kind: "unknown"}},
		{name: "unknown reddit path", raw: "https://www.reddit.com/r/Go/about", want: reddit.URLReference{Kind: "unknown"}},
		{name: "empty subreddit", raw: "https://www.reddit.com/r//comments/abcde/slug", want: reddit.URLReference{Kind: "unknown"}},
		{name: "invalid comment id", raw: "https://www.reddit.com/r/Go/comments/abcde/slug/no", want: reddit.URLReference{Kind: "post", Subreddit: "Go", PostID: "abcde", CanonicalURL: "https://www.reddit.com/r/Go/comments/abcde/"}},
		{name: "invalid subreddit post id", raw: "https://www.reddit.com/r/Go/comments/!!!/", want: reddit.URLReference{Kind: "unknown"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := reddit.ExtractURL(test.raw)
			if got != test.want {
				t.Fatalf("ExtractURL(%q) = %#v, want %#v", test.raw, got, test.want)
			}
		})
	}
}

func TestExtractURLHandlesMalformedInput(t *testing.T) {
	got := reddit.ExtractURL("://")
	if got.Kind != "unknown" {
		t.Fatalf("malformed URL kind = %q", got.Kind)
	}
}
