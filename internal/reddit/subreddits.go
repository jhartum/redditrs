package reddit

import (
	"context"
	"net/url"
	"strconv"

	"github.com/jhartum/redditrs/internal/model"
)

func (c *Client) Subreddits(ctx context.Context, query string, limit int) ([]model.Subreddit, int, error) {
	values := url.Values{}
	values.Set("q", query)
	values.Set("limit", strconv.Itoa(limit))
	values.Set("raw_json", "1")
	var response subredditListingResponse
	settings := c.LoadConfig()
	if err := c.getJSONTTL(ctx, settings, "/subreddits/search.json", values, &response, settings.SubredditTTLMS); err != nil {
		return nil, 0, err
	}
	items := make([]model.Subreddit, 0, len(response.Data.Children))
	for _, child := range response.Data.Children {
		if child.Kind == "t5" {
			items = append(items, child.Data)
		}
	}
	return items, response.Data.Dist, nil
}

func (c *Client) Trends(ctx context.Context, subreddit, listing, timeRange string, limit int) (SearchResult, error) {
	values := url.Values{}
	values.Set("limit", strconv.Itoa(limit))
	values.Set("raw_json", "1")
	if listing == "top" {
		values.Set("t", timeRange)
	}
	var response listingResponse
	if err := c.getJSON(ctx, c.LoadConfig(), "/r/"+url.PathEscape(subreddit)+"/"+listing+".json", values, &response); err != nil {
		return SearchResult{Posts: nil}, err
	}
	posts := make([]model.Post, 0, len(response.Data.Children))
	for _, child := range response.Data.Children {
		if child.Kind == "t3" {
			post := child.Data
			posts = append(posts, post)
		}
	}
	return SearchResult{Posts: posts, Total: response.Data.Dist}, nil
}

type subredditListingResponse struct {
	Data struct {
		Dist     int `json:"dist"`
		Children []struct {
			Kind string          `json:"kind"`
			Data model.Subreddit `json:"data"`
		} `json:"children"`
	} `json:"data"`
}
