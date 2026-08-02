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
	seconds := now.Unix() - int64(createdUTC)
	if seconds < 0 {
		seconds = 0
	}
	minutes := seconds / 60
	if minutes < 60 {
		return fmt.Sprintf("%dm ago", minutes)
	}
	hours := minutes / 60
	if hours < 24 {
		return fmt.Sprintf("%dh ago", hours)
	}
	days := hours / 24
	if days < 7 {
		return fmt.Sprintf("%dd ago", days)
	}
	weeks := days / 7
	if weeks < 5 {
		return fmt.Sprintf("%dw ago", weeks)
	}
	months := days / 30
	if months < 12 {
		return fmt.Sprintf("%dmo ago", months)
	}
	return fmt.Sprintf("%dy ago", max(1, days/365))
}
