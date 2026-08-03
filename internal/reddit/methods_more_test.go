package reddit_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jhartum/redditrs/internal/config"
	"github.com/jhartum/redditrs/internal/reddit"
)

func testSettings(serverURL string) config.Settings {
	return config.Settings{
		Cookie:         "reddit_session=test; token_v2=test",
		CachePath:      filepath.Join("/tmp", "redditrs-test-"+strings.ReplaceAll(serverURL, "://", "-")+".db"),
		DelayMS:        0,
		CacheTTLMS:     3_600_000,
		ThreadTTLMS:    21_600_000,
		SubredditTTLMS: 604_800_000,
		TopicTTLMS:     2_592_000_000,
		BaseURL:        serverURL,
		UserAgent:      "test-agent",
	}
}

func newTestClient(serverURL string) *reddit.Client {
	client := reddit.NewClient()
	settings := testSettings(serverURL)
	client.LoadConfig = func() config.Settings { return settings }
	client.HTTP = http.DefaultClient
	return client
}

func TestSearchSubredditAndListingMethodsParseKinds(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search.json", "/r/Go/search.json":
			if r.URL.Query().Get("raw_json") != "1" {
				t.Fatalf("raw_json missing from %s", r.URL.String())
			}
			_, _ = fmt.Fprint(w, `{"data":{"dist":2,"children":[{"kind":"t5","data":{}},{"kind":"t3","data":{"id":"abcde","subreddit":"Go","title":"Post","score":3,"created_utc":1704067200}}]}}`)
		case "/subreddits/search.json":
			_, _ = fmt.Fprint(w, `{"data":{"dist":2,"children":[{"kind":"t3","data":{}},{"kind":"t5","data":{"display_name":"Go","title":"Go","subscribers":1000}}]}}`)
		case "/r/Go/hot.json", "/r/Go/top.json":
			if r.URL.Path == "/r/Go/top.json" && r.URL.Query().Get("t") != "week" {
				t.Fatalf("top trend missing time: %s", r.URL.String())
			}
			if r.URL.Path == "/r/Go/hot.json" && r.URL.Query().Get("t") != "" {
				t.Fatalf("hot trend unexpectedly has time: %s", r.URL.String())
			}
			_, _ = fmt.Fprint(w, `{"data":{"dist":1,"children":[{"kind":"t3","data":{"id":"fghij","subreddit":"Go","title":"Trend","score":8,"created_utc":1704067200}},{"kind":"t1","data":{}}]}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := newTestClient(server.URL)

	result, err := client.Search(context.Background(), "query", reddit.SearchOptions{Limit: 1})
	if err != nil || len(result.Posts) != 1 || result.Posts[0].ID != "abcde" || result.Total != 2 {
		t.Fatalf("Search = %#v, err=%v", result, err)
	}
	scoped, err := client.Search(context.Background(), "query", reddit.SearchOptions{Subreddit: "Go", Limit: 2})
	if err != nil || len(scoped.Posts) != 1 {
		t.Fatalf("scoped Search = %#v, err=%v", scoped, err)
	}
	subreddits, total, err := client.Subreddits(context.Background(), "golang", 2)
	if err != nil || len(subreddits) != 1 || subreddits[0].Name != "Go" || total != 2 {
		t.Fatalf("Subreddits = %#v total=%d err=%v", subreddits, total, err)
	}
	hot, err := client.Trends(context.Background(), "Go", "hot", "week", 1)
	if err != nil || len(hot.Posts) != 1 || hot.Posts[0].ID != "fghij" {
		t.Fatalf("hot Trends = %#v err=%v", hot, err)
	}
	top, err := client.Trends(context.Background(), "Go", "top", "week", 2)
	if err != nil || len(top.Posts) != 1 {
		t.Fatalf("top Trends = %#v err=%v", top, err)
	}
	if atomic.LoadInt32(&requests) != 5 {
		t.Fatalf("request count = %d, want 5", requests)
	}
}

func TestThreadFlattensFiltersAndSortsComments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[{"data":{"children":[{"kind":"t2","data":{}},{"kind":"t3","data":{"id":"abcde","subreddit":"Go","title":"Thread","score":10,"num_comments":4,"created_utc":1704067200}}]}},{"data":{"children":[{"kind":"t1","data":{"id":"c1","author":"one","score":1,"body":" root ","created_utc":1704067201,"replies":{"data":{"children":[{"kind":"t1","data":{"id":"c2","author":"two","score":9,"body":"child","created_utc":1704067202,"depth":99,"replies":null}}]}}}},{"kind":"t1","data":{"id":"c3","body":"[deleted]"}},{"kind":"t1","data":{"id":"c4","body":"[removed]"}},{"kind":"t1","data":{"id":"c5","body":"   "}},{"kind":"t2","data":{}},{"kind":"t1","data":{"id":"c6","body":"empty replies","replies":""}},{"kind":"t1","data":{"id":"c7","body":"null replies","replies":null}},{"kind":"t1","data":{"id":"c8","body":"bad replies","replies":{"bad":true}}}]}}]`)
	}))
	defer server.Close()
	client := newTestClient(server.URL)

	result, err := client.Thread(context.Background(), "abcde", reddit.ThreadOptions{Sort: "top", CommentLimit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if result.Post.ID != "abcde" || len(result.Comments) != 5 {
		t.Fatalf("unexpected thread result: %#v", result)
	}
	if result.Comments[0].ID != "c2" || result.Comments[0].Depth != 1 || result.Comments[1].ID != "c1" {
		t.Fatalf("top sort/filter failed: %#v", result.Comments)
	}
	newResult, err := client.Thread(context.Background(), "abcde", reddit.ThreadOptions{Sort: "new", CommentLimit: 20})
	if err != nil || len(newResult.Comments) != 5 || newResult.Comments[0].ID != "c2" {
		t.Fatalf("new sort = %#v err=%v", newResult, err)
	}
	unchanged, err := client.Thread(context.Background(), "abcde", reddit.ThreadOptions{Sort: "controversial", CommentLimit: 20})
	if err != nil || len(unchanged.Comments) != 5 || unchanged.Comments[0].ID != "c1" {
		t.Fatalf("other sort = %#v err=%v", unchanged, err)
	}
}

func TestThreadHandlesEmptyAndMalformedPayloads(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/comments/abcde.json":
			_, _ = fmt.Fprint(w, `[]`)
		case "/comments/fghij.json":
			_, _ = fmt.Fprint(w, `[{"data":{"children":[{"kind":"t3","data":{"id":"fghij","title":"No comments","created_utc":1704067200}}]}}]`)
		case "/comments/klmno.json":
			_, _ = fmt.Fprint(w, `[{"data":{"children":[{"kind":"t3","data":{"id":"klmno","title":"Bad comments","created_utc":1704067200}}]}},"bad-json"]`)
		case "/comments/pqrst.json":
			_, _ = fmt.Fprint(w, `["bad-post"]`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := newTestClient(server.URL)
	if result, err := client.Thread(context.Background(), "abcde", reddit.ThreadOptions{}); err != nil || result.Post.ID != "" || result.Comments != nil {
		t.Fatalf("empty thread = %#v err=%v", result, err)
	}
	if result, err := client.Thread(context.Background(), "fghij", reddit.ThreadOptions{}); err != nil || result.Post.ID != "fghij" || result.Comments != nil {
		t.Fatalf("thread without comments = %#v err=%v", result, err)
	}
	if result, err := client.Thread(context.Background(), "klmno", reddit.ThreadOptions{}); err != nil || result.Post.ID != "klmno" || result.Comments != nil {
		t.Fatalf("thread with malformed comments = %#v err=%v", result, err)
	}
	if _, err := client.Thread(context.Background(), "pqrst", reddit.ThreadOptions{}); err == nil {
		t.Fatal("malformed post listing unexpectedly succeeded")
	}
}
