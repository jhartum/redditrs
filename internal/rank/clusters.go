package rank

import (
	"slices"
	"sort"
	"strings"

	"github.com/jhartum/redditrs/internal/model"
)

var validIntents = []string{"opinions", "bugs", "fixes", "compare", "settings", "alternatives", "trends", "guides", "hardware", "general"}

var intentHints = map[string]string{
	"opinions":     "prioritize praise and complaints from different users",
	"bugs":         "prioritize complaints, risks, and reproducible failures",
	"fixes":        "prioritize comments with commands, versions, and outcomes",
	"compare":      "prioritize alternatives, praise, and complaints",
	"settings":     "prioritize concrete configuration values and hardware limits",
	"alternatives": "prioritize named alternatives and migration experiences",
	"trends":       "prioritize recent posts and repeated themes",
	"guides":       "prioritize step-by-step guides and fixes",
	"hardware":     "prioritize GPU, VRAM, CPU, and device details",
	"general":      "balance evidence across clusters",
}

var clusterKeywords = map[string][]string{
	"praise":       {"love", "great", "best", "excellent", "works well", "impressive"},
	"complaints":   {"bad", "slow", "issue", "problem", "hate", "broken", "fails", "complaint"},
	"fixes":        {"fix", "fixed", "solution", "command", "install", "update", "workaround"},
	"settings":     {"setting", "config", "parameter", "flag", "vae", "fp8", "lowvram"},
	"hardware":     {"gpu", "vram", "ram", "cpu", "rtx", "gb", "hardware"},
	"alternatives": {"alternative", "instead", "switch", "versus", "vs", "replace"},
	"guides":       {"guide", "tutorial", "how to", "step", "walkthrough"},
	"risks":        {"risk", "warning", "danger", "privacy", "ban", "security"},
}

func ValidIntent(value string) bool {
	return slices.Contains(validIntents, value)
}

func Intents() []string {
	return append([]string(nil), validIntents...)
}

func IntentHint(intent string) string {
	return intentHints[intent]
}

func DepthDefaults(depth string) (posts, threads, comments int) {
	switch depth {
	case "quick":
		return 6, 2, 3
	case "deep":
		return 14, 6, 8
	default:
		return 10, 4, 5
	}
}

func Classify(text, intent string) string {
	lower := strings.ToLower(text)
	preferred := preferredClusters(intent)
	for _, cluster := range preferred {
		for _, keyword := range clusterKeywords[cluster] {
			if strings.Contains(lower, keyword) {
				return cluster
			}
		}
	}
	for _, cluster := range []string{"praise", "complaints", "fixes", "settings", "hardware", "alternatives", "guides", "risks"} {
		for _, keyword := range clusterKeywords[cluster] {
			if strings.Contains(lower, keyword) {
				return cluster
			}
		}
	}
	return "general"
}

func preferredClusters(intent string) []string {
	switch intent {
	case "bugs":
		return []string{"complaints", "risks"}
	case "fixes":
		return []string{"fixes", "settings"}
	case "settings":
		return []string{"settings", "hardware"}
	case "hardware":
		return []string{"hardware", "complaints"}
	case "compare":
		return []string{"alternatives", "praise", "complaints"}
	case "alternatives":
		return []string{"alternatives"}
	case "guides":
		return []string{"guides", "fixes"}
	case "opinions":
		return []string{"praise", "complaints"}
	default:
		return nil
	}
}

type ClusterSummary struct {
	Cluster string `json:"cluster"`
	Count   int    `json:"count"`
	Hint    string `json:"hint"`
}

func SummarizeEvidence(items []model.EvidenceItem, intent string) []ClusterSummary {
	counts := make(map[string]int)
	for _, item := range items {
		counts[item.Cluster]++
	}
	result := make([]ClusterSummary, 0, len(counts))
	for cluster, count := range counts {
		result = append(result, ClusterSummary{Cluster: cluster, Count: count, Hint: clusterHint(cluster, intent)})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count == result[j].Count {
			return result[i].Cluster < result[j].Cluster
		}
		return result[i].Count > result[j].Count
	})
	return result
}

func clusterHint(cluster, intent string) string {
	if hint := intentHints[intent]; hint != "" && len(preferredClusters(intent)) > 0 && preferredClusters(intent)[0] == cluster {
		return hint
	}
	switch cluster {
	case "settings":
		return "concrete configuration values and settings"
	case "hardware":
		return "hardware specifications and constraints"
	case "complaints":
		return "reported failures and negative experiences"
	case "fixes":
		return "solutions, commands, and workarounds"
	case "alternatives":
		return "competing tools and migration paths"
	default:
		return "evidence found in matching posts and comments"
	}
}
