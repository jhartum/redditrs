package rank

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jhartum/redditrs/internal/model"
)

var termSeparator = regexp.MustCompile(`[^a-z0-9_+#.-]+`)

var subredditPriorities = map[string]float64{
	"localllama":      14,
	"localllm":        12,
	"claudecode":      10,
	"claudeai":        8,
	"opencodecli":     8,
	"ollama":          7,
	"mcpservers":      7,
	"vibecoding":      6,
	"comfyui":         6,
	"stablediffusion": 5,
}

func Posts(posts []model.Post, query string, now time.Time) []model.Post {
	terms := terms(query)
	queryLower := strings.ToLower(strings.TrimSpace(query))
	ranked := make([]model.Post, len(posts))
	copy(ranked, posts)
	for index := range ranked {
		ranked[index].RankScore, ranked[index].RankReasons = scorePost(ranked[index], queryLower, terms, now)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].RankScore == ranked[j].RankScore {
			return ranked[i].CreatedUTC > ranked[j].CreatedUTC
		}
		return ranked[i].RankScore > ranked[j].RankScore
	})
	return ranked
}

func scorePost(post model.Post, query string, terms []string, now time.Time) (float64, []string) {
	title := strings.ToLower(post.Title)
	text := strings.ToLower(post.Selftext)
	full := title + " " + text
	score := 0.0
	var reasons []string
	if query != "" && strings.Contains(title, query) {
		score += 40
		reasons = append(reasons, "exact title match")
	}
	if query != "" && strings.Contains(text, query) {
		score += 18
		reasons = append(reasons, "exact text match")
	}
	for _, term := range terms {
		if strings.Contains(title, term) {
			score += 8
		}
		if strings.Contains(text, term) {
			score += 2.5
		}
	}
	activity := math.Log1p(math.Max(0, float64(post.Score)))*3 + math.Log1p(math.Max(0, float64(post.NumComments)))*4
	score += activity
	if post.Score >= 100 {
		reasons = append(reasons, "high score")
	}
	if post.NumComments >= 50 {
		reasons = append(reasons, "active discussion")
	}
	ageDays := math.Max(0, float64(now.Unix()-int64(post.CreatedUTC))/86400)
	freshness := math.Max(0, 12-3*math.Log1p(ageDays))
	score += freshness
	if ageDays <= 30 {
		reasons = append(reasons, "recent")
	}
	if priority := subredditPriorities[strings.ToLower(post.Subreddit)]; priority > 0 {
		score += priority
		reasons = append(reasons, fmt.Sprintf("priority subreddit (+%g)", priority))
	}
	if post.Over18 {
		score -= 25
		reasons = append(reasons, "NSFW penalty")
	}
	if strings.TrimSpace(post.Selftext) == "" && strings.TrimSpace(post.URL) == "" && strings.TrimSpace(post.Domain) == "" {
		score -= 5
		reasons = append(reasons, "empty text/domain penalty")
	}
	if strings.Contains(strings.ToLower(full), "[removed]") {
		score -= 20
		reasons = append(reasons, "removed content penalty")
	}
	if len(reasons) > 5 {
		reasons = reasons[:5]
	}
	return score, reasons
}

func terms(query string) []string {
	parts := termSeparator.Split(strings.ToLower(query), -1)
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if len([]rune(part)) >= 2 {
			result = append(result, part)
		}
	}
	return result
}
