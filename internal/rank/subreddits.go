package rank

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/jhartum/redditrs/internal/model"
)

func Subreddits(topic string, candidates []model.Subreddit, posts []model.Post) []model.SubredditCandidate {
	terms := terms(topic)
	topicLower := strings.ToLower(topic)
	result := make([]model.SubredditCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		lowerName := strings.ToLower(candidate.Name)
		lowerTitle := strings.ToLower(candidate.Title)
		lowerDescription := strings.ToLower(candidate.PublicDescription)
		score := math.Log1p(math.Max(0, float64(candidate.Subscribers))) * 2
		reasons := make([]string, 0, 5)
		topicMatched := false
		comparableName := comparableSubredditTerm(lowerName)
		comparableTitle := comparableSubredditTerm(lowerTitle)
		comparableDescription := comparableSubredditTerm(lowerDescription)
		for _, term := range terms {
			comparableTerm := comparableSubredditTerm(term)
			if comparableTerm == "" {
				continue
			}
			matchScore := 0.0
			switch {
			case comparableName == comparableTerm:
				matchScore = 20
			case strings.Contains(lowerName, term) || strings.Contains(comparableName, comparableTerm):
				matchScore = 12
			case strings.Contains(lowerTitle, term) || strings.Contains(comparableTitle, comparableTerm):
				matchScore = 8
			case strings.Contains(lowerDescription, term) || strings.Contains(comparableDescription, comparableTerm):
				matchScore = 5
			}
			if matchScore > 0 {
				score += matchScore
				topicMatched = true
				if len(reasons) < 5 {
					reasons = append(reasons, term+" topic match")
				}
			}
		}
		matchingPosts := 0
		for _, post := range posts {
			if strings.EqualFold(post.Subreddit, candidate.Name) {
				score += 8 + math.Log1p(math.Max(0, float64(post.Score+post.NumComments)))
				matchingPosts++
			}
		}
		if matchingPosts > 0 && len(reasons) < 5 {
			reasons = append(reasons, fmt.Sprintf("%d matching posts", matchingPosts))
		}
		if priority := subredditPriorities[lowerName]; priority > 0 && (topicMatched || matchingPosts > 0) {
			score += priority
			if len(reasons) < 5 {
				reasons = append(reasons, fmt.Sprintf("priority subreddit (+%g)", priority))
			}
		}
		discoveryMatches := candidate.DiscoveryMatches
		if discoveryMatches > 3 {
			discoveryMatches = 3
		}
		if discoveryMatches > 0 {
			score += float64(discoveryMatches) * 6
			if len(reasons) < 5 {
				label := "queries"
				if discoveryMatches == 1 {
					label = "query"
				}
				reasons = append(reasons, fmt.Sprintf("matched %d discovery %s", discoveryMatches, label))
			}
		}
		candidateText := lowerName + " " + lowerTitle + " " + lowerDescription
		for _, marker := range []string{"meme", "shitpost", "circlejerk", "porn", "sales"} {
			if strings.Contains(candidateText, marker) && !strings.Contains(topicLower, marker) {
				score -= 30
				if len(reasons) < 5 {
					reasons = append(reasons, "low-signal community penalty")
				}
				break
			}
		}
		if candidate.DiscoveryMatches > 0 && matchingPosts == 0 {
			switch {
			case candidate.Subscribers < 1_000:
				score -= 12
				if len(reasons) < 5 {
					reasons = append(reasons, "small unvalidated community penalty")
				}
			case candidate.Subscribers < 10_000:
				score -= 6
				if len(reasons) < 5 {
					reasons = append(reasons, "small unvalidated community penalty")
				}
			}
		}
		for _, marker := range []string{"help", "advice", "support", "ask ", "discussion", "discuss", "beginner"} {
			if strings.Contains(candidateText, marker) {
				score += 8
				if len(reasons) < 5 {
					reasons = append(reasons, "support-oriented community")
				}
				break
			}
		}
		if candidate.Subscribers > 0 && len(reasons) < 5 {
			reasons = append(reasons, "subscriber signal")
		}
		if len(reasons) == 0 {
			reasons = append(reasons, "candidate subreddit")
		}
		result = append(result, model.SubredditCandidate{Name: candidate.Name, Score: score, Reasons: reasons, Subscribers: candidate.Subscribers, Title: candidate.Title, PublicDescription: candidate.PublicDescription})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Score == result[j].Score {
			left, right := strings.ToLower(result[i].Name), strings.ToLower(result[j].Name)
			if left == right {
				return result[i].Name < result[j].Name
			}
			return left < right
		}
		return result[i].Score > result[j].Score
	})
	return result
}

func comparableSubredditTerm(value string) string {
	replacer := strings.NewReplacer("-", "", ".", "", "_", "", " ", "")
	return replacer.Replace(value)
}
