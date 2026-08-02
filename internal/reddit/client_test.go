package reddit_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jhartum/redditrs/internal/cache"
	"github.com/jhartum/redditrs/internal/config"
	"github.com/jhartum/redditrs/internal/reddit"
)

func TestSearchRetries403WhenCookieChanges(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.Header.Get("Cookie") == "old" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"data":{"dist":1,"children":[{"kind":"t3","data":{"id":"1abc234","subreddit":"ClaudeCode","title":"retried","score":1,"created_utc":1704067200}}]}}`)
	}))
	defer server.Close()

	settings := config.Settings{Cookie: "old", CachePath: filepath.Join(t.TempDir(), "reddit.db"), DelayMS: 250, CacheTTLMS: 3_600_000, BaseURL: server.URL, UserAgent: "test"}
	loads := 0
	client := reddit.NewClient()
	client.LoadConfig = func() config.Settings {
		loads++
		if loads > 1 {
			settings.Cookie = "new"
		}
		return settings
	}
	result, err := client.Search(context.Background(), "test", reddit.SearchOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Posts) != 1 || result.Posts[0].ID != "1abc234" {
		t.Fatalf("unexpected result: %#v", result.Posts)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("got %d requests, want 2", got)
	}
}

func TestClientsShareRequestPacingThroughCache(t *testing.T) {
	var mu sync.Mutex
	var received []time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		received = append(received, time.Now())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"data":{"dist":0,"children":[]}}`)
	}))
	defer server.Close()

	settings := config.Settings{Cookie: "cookie", CachePath: filepath.Join(t.TempDir(), "reddit.db"), DelayMS: 250, CacheTTLMS: 30_000, BaseURL: server.URL, UserAgent: "test"}
	store, err := cache.Open(settings.CachePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errors := make(chan error, 2)
	var workers sync.WaitGroup
	for _, query := range []string{"first", "second"} {
		workers.Add(1)
		go func(query string) {
			defer workers.Done()
			client := reddit.NewClient()
			client.LoadConfig = func() config.Settings { return settings }
			<-start
			_, err := client.Search(context.Background(), query, reddit.SearchOptions{Limit: 1})
			errors <- err
		}(query)
	}
	close(start)
	workers.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}

	mu.Lock()
	times := append([]time.Time(nil), received...)
	mu.Unlock()
	if len(times) != 2 {
		t.Fatalf("received %d requests, want 2", len(times))
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	if spacing := times[1].Sub(times[0]); spacing < 200*time.Millisecond {
		t.Fatalf("requests were only %s apart, want shared pacing near 250ms", spacing)
	}
}

func TestSearchServesStaleCacheAfterRateLimit(t *testing.T) {
	var calls int32
	body := `{"data":{"dist":1,"children":[{"kind":"t3","data":{"id":"1abc234","subreddit":"ClaudeCode","title":"stale result","score":1,"created_utc":1704067200}}]}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, body)
			return
		}
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "reddit.db")
	settings := config.Settings{Cookie: "cookie", CachePath: path, DelayMS: 250, CacheTTLMS: 30_000, BaseURL: server.URL, UserAgent: "test"}
	client := reddit.NewClient()
	client.LoadConfig = func() config.Settings { return settings }
	if _, err := client.Search(context.Background(), "test", reddit.SearchOptions{Limit: 1}); err != nil {
		t.Fatal(err)
	}
	values := url.Values{}
	values.Set("q", "test")
	values.Set("sort", "relevance")
	values.Set("t", "all")
	values.Set("limit", "1")
	values.Set("raw_json", "1")
	requestURL := server.URL + "/search.json?" + values.Encode()
	store, err := cache.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(requestURL, http.StatusOK, []byte(body), "", time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	client = reddit.NewClient()
	client.LoadConfig = func() config.Settings { return settings }
	result, err := client.Search(context.Background(), "test", reddit.SearchOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Posts) != 1 || result.Posts[0].Title != "stale result" {
		t.Fatalf("unexpected stale result: %#v", result.Posts)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("got %d requests, want 2", got)
	}
}
