package rank_test

import (
	"testing"
	"time"

	"github.com/jhartum/redditrs/internal/model"
	"github.com/jhartum/redditrs/internal/rank"
)

func TestRankPostsPutsExactRecentMatchFirst(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	posts := []model.Post{
		{ID: "other", Subreddit: "AskReddit", Title: "A discussion about coding tools", Score: 800, NumComments: 200, CreatedUTC: float64(now.Add(-90 * 24 * time.Hour).Unix())},
		{ID: "match", Subreddit: "ClaudeCode", Title: "Claude Code", Selftext: "My daily workflow", Score: 20, NumComments: 4, CreatedUTC: float64(now.Add(-24 * time.Hour).Unix())},
	}

	ranked := rank.Posts(posts, "Claude Code", now)
	if len(ranked) != 2 {
		t.Fatalf("got %d posts, want 2", len(ranked))
	}
	if ranked[0].ID != "match" {
		t.Fatalf("first post is %q, want match", ranked[0].ID)
	}
	if ranked[0].RankScore <= ranked[1].RankScore {
		t.Fatalf("rank scores do not descend: %v, %v", ranked[0].RankScore, ranked[1].RankScore)
	}
	if len(ranked[0].RankReasons) == 0 {
		t.Fatal("exact match has no rank reasons")
	}
}
