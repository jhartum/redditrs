package rank

import (
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jhartum/redditrs/internal/model"
)

func TestPostScoringCoversSignalsAndPenalties(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	post := model.Post{
		Subreddit:   "ClaudeCode",
		Title:       "Claude Code",
		Selftext:    "Claude Code works, but [removed]",
		Score:       100,
		NumComments: 50,
		CreatedUTC:  float64(now.Add(-24 * time.Hour).Unix()),
		Over18:      true,
	}
	score, reasons := scorePost(post, "claude code", []string{"claude", "code"}, now)
	if score == 0 {
		t.Fatalf("combined signals/penalties produced no score: %v", score)
	}
	if len(reasons) != 5 {
		t.Fatalf("reasons length = %d, want capped at 5: %#v", len(reasons), reasons)
	}
	if _, reasons := scorePost(model.Post{Over18: true, URL: "https://example.com"}, "", nil, now); !slices.Contains(reasons, "NSFW penalty") {
		t.Fatalf("NSFW penalty reason missing: %#v", reasons)
	}
	if _, reasons := scorePost(model.Post{Selftext: "[removed]", URL: "https://example.com"}, "", nil, now); !slices.Contains(reasons, "removed content penalty") {
		t.Fatalf("removed-content reason missing: %#v", reasons)
	}

	negative := model.Post{Score: -1, NumComments: -1, CreatedUTC: float64(now.Add(-365 * 24 * time.Hour).Unix())}
	if score, _ := scorePost(negative, "", nil, now); math.IsNaN(score) {
		t.Fatal("negative activity produced NaN")
	}
	if score, reasons := scorePost(model.Post{}, "", nil, now); score != -5 || len(reasons) != 1 || reasons[0] != "empty text/domain penalty" {
		t.Fatalf("empty post score = %v reasons=%#v", score, reasons)
	}
	if score, reasons := scorePost(model.Post{URL: "https://example.com"}, "missing", []string{"missing"}, now); score <= -5 || len(reasons) != 0 {
		t.Fatalf("non-empty URL post = %v reasons=%#v", score, reasons)
	}
}

func TestPostsTieBreaksAndCopiesInput(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	posts := []model.Post{
		{ID: "old", Title: "same", CreatedUTC: float64(now.Add(-2 * time.Hour).Unix())},
		{ID: "new", Title: "same", CreatedUTC: float64(now.Add(-time.Hour).Unix())},
	}
	ranked := Posts(posts, "same", now)
	if ranked[0].ID != "new" || posts[0].RankScore != 0 || posts[0].RankReasons != nil {
		t.Fatalf("unexpected stable ranking/copy: ranked=%#v original=%#v", ranked, posts)
	}
	tie := Posts([]model.Post{{ID: "a", Title: "same", CreatedUTC: 1}, {ID: "b", Title: "same", CreatedUTC: 1}}, "same", now)
	if len(tie) != 2 {
		t.Fatalf("tie ranking = %#v", tie)
	}
}

func TestTermsAndIntentHelpers(t *testing.T) {
	if got := terms("A ab C3 x_y"); len(got) != 3 || got[0] != "ab" || got[1] != "c3" || got[2] != "x_y" {
		t.Fatalf("terms = %#v", got)
	}
	if ValidIntent("general") != true || ValidIntent("invalid") {
		t.Fatal("ValidIntent returned wrong result")
	}
	intents := Intents()
	if len(intents) != 10 {
		t.Fatalf("Intents length = %d", len(intents))
	}
	intents[0] = "changed"
	if Intents()[0] == "changed" {
		t.Fatal("Intents leaked its backing array")
	}
	if IntentHint("general") == "" || IntentHint("invalid") != "" {
		t.Fatal("IntentHint returned wrong result")
	}
	if posts, threads, comments := DepthDefaults("quick"); posts != 6 || threads != 2 || comments != 3 {
		t.Fatalf("quick defaults = %d,%d,%d", posts, threads, comments)
	}
	if posts, threads, comments := DepthDefaults("deep"); posts != 14 || threads != 6 || comments != 8 {
		t.Fatalf("deep defaults = %d,%d,%d", posts, threads, comments)
	}
	if posts, threads, comments := DepthDefaults("normal"); posts != 10 || threads != 4 || comments != 5 {
		t.Fatalf("normal defaults = %d,%d,%d", posts, threads, comments)
	}
}

func TestClassifyPreferredAndFallbackClusters(t *testing.T) {
	preferred := []struct {
		intent  string
		text    string
		cluster string
	}{
		{"bugs", "a broken issue", "complaints"},
		{"fixes", "install command", "fixes"},
		{"settings", "fp8 setting", "settings"},
		{"hardware", "GPU issue", "hardware"},
		{"compare", "switch versus", "alternatives"},
		{"alternatives", "switch instead", "alternatives"},
		{"guides", "step tutorial", "guides"},
		{"opinions", "love it", "praise"},
	}
	for _, test := range preferred {
		if got := Classify(test.text, test.intent); got != test.cluster {
			t.Errorf("Classify(%q, %q) = %q, want %q", test.text, test.intent, got, test.cluster)
		}
	}
	for _, test := range []struct {
		text    string
		cluster string
	}{
		{"love this", "praise"},
		{"bad issue", "complaints"},
		{"update command", "fixes"},
		{"vae flag", "settings"},
		{"RTX hardware", "hardware"},
		{"replace tool", "alternatives"},
		{"how to guide", "guides"},
		{"privacy risk", "risks"},
		{"unclassified", "general"},
	} {
		if got := Classify(test.text, "general"); got != test.cluster {
			t.Errorf("fallback Classify(%q) = %q, want %q", test.text, got, test.cluster)
		}
	}
}

func TestSummarizeEvidenceAndClusterHints(t *testing.T) {
	for _, test := range []struct {
		cluster string
		intent  string
		want    string
	}{
		{"settings", "settings", "prioritize concrete configuration values and hardware limits"},
		{"settings", "general", "concrete configuration values and settings"},
		{"hardware", "general", "hardware specifications and constraints"},
		{"complaints", "general", "reported failures and negative experiences"},
		{"fixes", "general", "solutions, commands, and workarounds"},
		{"alternatives", "general", "competing tools and migration paths"},
		{"praise", "general", "evidence found in matching posts and comments"},
	} {
		if got := clusterHint(test.cluster, test.intent); got != test.want {
			t.Errorf("clusterHint(%q, %q) = %q, want %q", test.cluster, test.intent, got, test.want)
		}
	}
	if got := SummarizeEvidence(nil, "general"); len(got) != 0 {
		t.Fatalf("empty summary = %#v", got)
	}
	items := []model.EvidenceItem{{Cluster: "same"}, {Cluster: "other"}, {Cluster: "same"}}
	got := SummarizeEvidence(items, "general")
	if len(got) != 2 || got[0].Count != 2 {
		t.Fatalf("summary was not sorted by count: %#v", got)
	}
}

func TestSubredditRankingUsesDiscoveryAndQualitySignals(t *testing.T) {
	candidates := []model.Subreddit{
		{Name: "homelab", Title: "Homelab", PublicDescription: "Discussion and help", Subscribers: 1_000_000, DiscoveryMatches: 9},
		{Name: "HomeNetworking", PublicDescription: "Ask for home networking help", Subscribers: 500_000, DiscoveryMatches: 1},
		{Name: "homelabshitposting", Title: "Homelab Shitposting", Subscribers: 500, DiscoveryMatches: 1},
		{Name: "LocalLLaMA", PublicDescription: "Discuss local AI", Subscribers: 700_000, DiscoveryMatches: 1},
	}
	got := Subreddits("homelab", candidates, nil)
	if got[0].Name != "homelab" || !strings.Contains(strings.Join(got[0].Reasons, " "), "matched 3 discovery queries") {
		t.Fatalf("discovery score/cap missing: %#v", got)
	}
	byName := make(map[string]model.SubredditCandidate, len(got))
	for _, candidate := range got {
		byName[candidate.Name] = candidate
	}
	if byName["HomeNetworking"].Score <= byName["homelabshitposting"].Score || !strings.Contains(strings.Join(byName["homelabshitposting"].Reasons, " "), "low-signal") {
		t.Fatalf("quality penalties did not demote low-signal community: %#v", got)
	}
	if strings.Contains(strings.Join(byName["LocalLLaMA"].Reasons, " "), "priority subreddit") {
		t.Fatalf("unrelated static priority leaked into ranking: %#v", byName["LocalLLaMA"])
	}
	joined := Subreddits("self-hosted", []model.Subreddit{{Name: "selfhosted"}}, nil)
	if len(joined) != 1 || !strings.Contains(strings.Join(joined[0].Reasons, " "), "topic match") {
		t.Fatalf("punctuation-insensitive exact match failed: %#v", joined)
	}
}

func TestSubredditRankingCoversReasonsAndPosts(t *testing.T) {
	candidates := []model.Subreddit{
		{Name: "ClaudeCode", Title: "Claude Code", PublicDescription: "claude code tools", Subscribers: 1000},
		{Name: "Empty", Subscribers: 0},
		{Name: "Negative", Subscribers: -1},
		{Name: "SubscriberOnly", Subscribers: 100},
	}
	truncated := Subreddits("claude code tools extra words", []model.Subreddit{{Name: "ClaudeCode", Title: "claude code tools extra words", PublicDescription: "claude code tools extra words"}}, nil)
	if len(truncated) != 1 || len(truncated[0].Reasons) != 5 {
		t.Fatalf("reasons were not capped: %#v", truncated)
	}
	posts := []model.Post{
		{Subreddit: "claudecode", Score: 10, NumComments: 2},
		{Subreddit: "empty", Score: -5, NumComments: -2},
	}
	got := Subreddits("claude code tools priority extra terms", candidates, posts)
	if len(got) != len(candidates) || got[0].Name != "ClaudeCode" {
		t.Fatalf("unexpected subreddit ranking: %#v", got)
	}
	if len(got[0].Reasons) > 5 || len(got[0].Reasons) == 0 {
		t.Fatalf("ClaudeCode reasons = %#v", got[0].Reasons)
	}
	for _, candidate := range got {
		if candidate.Name == "Empty" && len(candidate.Reasons) != 1 {
			t.Fatalf("unmatched candidate reasons = %#v", candidate.Reasons)
		}
		if candidate.Name == "SubscriberOnly" && len(candidate.Reasons) != 1 {
			t.Fatalf("subscriber-only candidate reasons = %#v", candidate.Reasons)
		}
	}
	tied := Subreddits("", []model.Subreddit{{Name: "Zulu"}, {Name: "alpha"}, {Name: "go"}, {Name: "Go"}}, nil)
	if tied[0].Name != "alpha" || tied[1].Name != "Go" || tied[2].Name != "go" {
		t.Fatalf("equal-score candidates are not deterministic: %#v", tied)
	}
}
