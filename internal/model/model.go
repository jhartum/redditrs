package model

import (
	"fmt"
	"time"
)

type Post struct {
	ID          string   `json:"id"`
	Fullname    string   `json:"name,omitempty"`
	Subreddit   string   `json:"subreddit"`
	Title       string   `json:"title"`
	Author      string   `json:"author,omitempty"`
	Score       int      `json:"score"`
	NumComments int      `json:"num_comments"`
	CreatedUTC  float64  `json:"created_utc"`
	Age         string   `json:"age,omitempty"`
	Permalink   string   `json:"permalink,omitempty"`
	URL         string   `json:"url,omitempty"`
	Domain      string   `json:"domain,omitempty"`
	Flair       string   `json:"link_flair_text,omitempty"`
	Selftext    string   `json:"selftext,omitempty"`
	Over18      bool     `json:"over_18"`
	RankScore   float64  `json:"rank_score,omitempty"`
	RankReasons []string `json:"rank_reasons,omitempty"`
}

type Comment struct {
	ID         string  `json:"id"`
	Author     string  `json:"author,omitempty"`
	Score      int     `json:"score"`
	Body       string  `json:"body,omitempty"`
	Depth      int     `json:"depth"`
	CreatedUTC float64 `json:"created_utc"`
	URL        string  `json:"permalink,omitempty"`
	PostID     string  `json:"post_id,omitempty"`
}

type Subreddit struct {
	Name              string `json:"display_name"`
	Title             string `json:"title,omitempty"`
	PublicDescription string `json:"public_description,omitempty"`
	Subscribers       int64  `json:"subscribers,omitempty"`
	DiscoveryMatches  int    `json:"-"`
}

type SubredditCandidate struct {
	Name              string   `json:"name"`
	Score             float64  `json:"score"`
	Reasons           []string `json:"reasons"`
	Subscribers       int64    `json:"subscribers,omitempty"`
	Title             string   `json:"title,omitempty"`
	PublicDescription string   `json:"public_description,omitempty"`
}

type EvidenceItem struct {
	Kind      string `json:"kind"`
	Cluster   string `json:"cluster"`
	PostID    string `json:"post_id"`
	PostIndex int    `json:"post_index"`
	Subreddit string `json:"subreddit"`
	Score     int    `json:"score"`
	URL       string `json:"url,omitempty"`
	Text      string `json:"text"`
	Reason    string `json:"reason"`
}

func RelativeAge(createdUTC float64, now time.Time) string {
	if createdUTC <= 0 {
		return "unknown"
	}
	seconds := max(int64(0), now.Unix()-int64(createdUTC))
	switch {
	case seconds < 60*60:
		return fmt.Sprintf("%dm ago", seconds/60)
	case seconds < 24*60*60:
		return fmt.Sprintf("%dh ago", seconds/(60*60))
	case seconds < 7*24*60*60:
		return fmt.Sprintf("%dd ago", seconds/(24*60*60))
	case seconds < 35*24*60*60:
		return fmt.Sprintf("%dw ago", seconds/(7*24*60*60))
	case seconds < 360*24*60*60:
		return fmt.Sprintf("%dmo ago", seconds/(30*24*60*60))
	default:
		return fmt.Sprintf("%dy ago", max(int64(1), seconds/(365*24*60*60)))
	}
}
