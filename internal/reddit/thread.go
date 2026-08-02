package reddit

import (
	"cmp"
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jhartum/redditrs/internal/model"
)

type ThreadOptions struct {
	Sort         string
	CommentLimit int
}

type ThreadResult struct {
	Post     model.Post
	Comments []model.Comment
}

func (c *Client) Thread(ctx context.Context, postID string, options ThreadOptions) (ThreadResult, error) {
	values := url.Values{}
	values.Set("limit", strconv.Itoa(options.CommentLimit))
	values.Set("sort", cmp.Or(options.Sort, "top"))
	values.Set("raw_json", "1")
	var payload []json.RawMessage
	settings := c.LoadConfig()
	if err := c.getJSONTTL(ctx, settings, "/comments/"+url.PathEscape(postID)+".json", values, &payload, settings.ThreadTTLMS); err != nil {
		return ThreadResult{}, err
	}
	if len(payload) == 0 {
		return ThreadResult{}, nil
	}
	var postListing listingResponse
	if err := json.Unmarshal(payload[0], &postListing); err != nil {
		return ThreadResult{}, err
	}
	var result ThreadResult
	for _, child := range postListing.Data.Children {
		if child.Kind == "t3" {
			result.Post = child.Data
			result.Post.Age = model.RelativeAge(result.Post.CreatedUTC, time.Now())
			break
		}
	}
	if len(payload) > 1 {
		var commentListing commentListingResponse
		if err := json.Unmarshal(payload[1], &commentListing); err == nil {
			flattenComments(commentListing.Data.Children, postID, 0, &result.Comments)
		}
	}
	if options.Sort == "top" || options.Sort == "" {
		sort.SliceStable(result.Comments, func(i, j int) bool { return result.Comments[i].Score > result.Comments[j].Score })
	} else if options.Sort == "new" {
		sort.SliceStable(result.Comments, func(i, j int) bool { return result.Comments[i].CreatedUTC > result.Comments[j].CreatedUTC })
	}
	return result, nil
}

type commentListingResponse struct {
	Data struct {
		Children []commentChild `json:"children"`
	} `json:"data"`
}

type commentChild struct {
	Kind string      `json:"kind"`
	Data commentData `json:"data"`
}

type commentData struct {
	ID         string          `json:"id"`
	Author     string          `json:"author"`
	Score      int             `json:"score"`
	Body       string          `json:"body"`
	CreatedUTC float64         `json:"created_utc"`
	Permalink  string          `json:"permalink"`
	Replies    json.RawMessage `json:"replies"`
}

func flattenComments(children []commentChild, postID string, depth int, output *[]model.Comment) {
	for _, child := range children {
		if child.Kind != "t1" {
			continue
		}
		body := strings.TrimSpace(child.Data.Body)
		if body == "" || body == "[deleted]" || body == "[removed]" {
			continue
		}
		comment := model.Comment{
			ID:         child.Data.ID,
			Author:     child.Data.Author,
			Score:      child.Data.Score,
			Body:       body,
			Depth:      depth,
			CreatedUTC: child.Data.CreatedUTC,
			URL:        child.Data.Permalink,
			PostID:     postID,
		}
		*output = append(*output, comment)
		if len(child.Data.Replies) > 0 && string(child.Data.Replies) != `""` && string(child.Data.Replies) != "null" {
			var replies commentListingResponse
			if json.Unmarshal(child.Data.Replies, &replies) == nil {
				flattenComments(replies.Data.Children, postID, depth+1, output)
			}
		}
	}
}
