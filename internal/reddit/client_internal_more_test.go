package reddit

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jhartum/redditrs/internal/cache"
	"github.com/jhartum/redditrs/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type errorReadCloser struct{}

func (errorReadCloser) Read([]byte) (int, error) { return 0, errors.New("body read failed") }
func (errorReadCloser) Close() error             { return nil }

func internalSettings(t *testing.T, baseURL string) config.Settings {
	t.Helper()
	return config.Settings{
		Cookie:         "cookie",
		CachePath:      filepath.Join(t.TempDir(), "reddit.db"),
		DelayMS:        0,
		CacheTTLMS:     30_000,
		ThreadTTLMS:    30_000,
		SubredditTTLMS: 30_000,
		TopicTTLMS:     30_000,
		BaseURL:        baseURL,
		UserAgent:      "test-agent",
	}
}

func internalClient(settings config.Settings) *Client {
	client := NewClient()
	client.LoadConfig = func() config.Settings { return settings }
	return client
}

func internalRequestURL(t *testing.T, settings config.Settings, path string, values url.Values) string {
	t.Helper()
	base, err := url.Parse(strings.TrimRight(settings.BaseURL, "/"))
	if err != nil {
		t.Fatal(err)
	}
	base.Path = strings.TrimRight(base.Path, "/") + path
	base.RawQuery = values.Encode()
	return base.String()
}

func TestHTTPErrorAndRetryAfterForms(t *testing.T) {
	if got := (&HTTPError{StatusCode: 403}).Error(); got != "Reddit returned HTTP 403" {
		t.Fatalf("empty HTTPError = %q", got)
	}
	if got := (&HTTPError{StatusCode: 500, Body: "boom"}).Error(); got != "Reddit returned HTTP 500: boom" {
		t.Fatalf("HTTPError with body = %q", got)
	}
	if got := retryAfter("10"); got != 10*time.Second {
		t.Fatalf("seconds Retry-After = %s", got)
	}
	future := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
	if got := retryAfter(future); got <= 0 || got > 2*time.Second {
		t.Fatalf("future date Retry-After = %s", got)
	}
	past := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
	if got := retryAfter(past); got != 0 {
		t.Fatalf("past date Retry-After = %s", got)
	}
	if got := retryAfter("-1"); got != 5*time.Minute {
		t.Fatalf("negative Retry-After = %s", got)
	}
	if got := retryAfter("invalid"); got != 5*time.Minute {
		t.Fatalf("invalid Retry-After = %s", got)
	}
}

func TestCooldownAndWaitStateTransitions(t *testing.T) {
	client := NewClient()
	if client.cooldownRemaining() > 0 {
		t.Fatal("new client is in cooldown")
	}
	client.startCooldown(0)
	if client.cooldownRemaining() <= 0 {
		t.Fatal("zero cooldown did not use fallback")
	}
	client.startCooldown(time.Nanosecond)
	client.startCooldown(time.Hour)
	if client.cooldownRemaining() <= 0 {
		t.Fatal("longer cooldown did not extend state")
	}
	client.wait(-1)
	client.lastSent = time.Now()
	client.wait(1)
}

func TestGetJSONCacheHitAndActiveErrorPaths(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("HTTP request should not be made for cache path: %s", r.URL)
	}))
	defer server.Close()
	settings := internalSettings(t, server.URL)
	values := url.Values{"q": []string{"test"}}
	requestURL := internalRequestURL(t, settings, "/search.json", values)
	store, err := cache.Open(settings.CachePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(requestURL, 200, []byte(`{"data":{"dist":1,"children":[]}}`), "", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	client := internalClient(settings)
	var got listingResponse
	if err := client.getJSON(context.Background(), settings, "/search.json", values, &got); err != nil || got.Data.Dist != 1 {
		t.Fatalf("fresh cache = %#v err=%v", got, err)
	}

	settings = internalSettings(t, server.URL)
	values = url.Values{"q": []string{"stale"}}
	requestURL = internalRequestURL(t, settings, "/search.json", values)
	store, err = cache.Open(settings.CachePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(requestURL, 429, []byte(`{"data":{"dist":2,"children":[]}}`), "rate limited", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	got = listingResponse{}
	if err := client.getJSON(context.Background(), settings, "/search.json", values, &got); err != nil || got.Data.Dist != 2 {
		t.Fatalf("stale cache = %#v err=%v", got, err)
	}

	settings = internalSettings(t, server.URL)
	values = url.Values{"q": []string{"blocked"}}
	requestURL = internalRequestURL(t, settings, "/search.json", values)
	store, err = cache.Open(settings.CachePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(requestURL, 429, nil, "rate limited", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	if err := client.getJSON(context.Background(), settings, "/search.json", values, &got); err == nil {
		t.Fatal("active error without stale data unexpectedly succeeded")
	} else {
		var httpErr *HTTPError
		if !errors.As(err, &httpErr) || httpErr.StatusCode != 429 {
			t.Fatalf("active error = %T %v", err, err)
		}
	}
}

func TestMalformedCacheIsRefetchedAndCooldownAppliesAcrossURLs(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requests, 1)
		_, _ = fmt.Fprint(w, `{"data":{"dist":9,"children":[]}}`)
	}))
	defer server.Close()
	settings := internalSettings(t, server.URL)
	values := url.Values{"q": []string{"malformed"}}
	requestURL := internalRequestURL(t, settings, "/search.json", values)
	store, err := cache.Open(settings.CachePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(requestURL, 200, []byte(`{broken`), "", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	client := internalClient(settings)
	var got listingResponse
	if err := client.getJSON(context.Background(), settings, "/search.json", values, &got); err != nil || got.Data.Dist != 9 {
		t.Fatalf("refetched malformed cache = %#v err=%v", got, err)
	}
	if err := client.getJSON(context.Background(), settings, "/search.json", values, &got); err != nil || atomic.LoadInt32(&requests) != 1 {
		t.Fatalf("replacement cache requests=%d err=%v", atomic.LoadInt32(&requests), err)
	}

	blockedServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("global cooldown unexpectedly allowed an HTTP request")
	}))
	defer blockedServer.Close()
	blockedSettings := internalSettings(t, blockedServer.URL)
	staleValues := url.Values{"q": []string{"stale"}}
	staleURL := internalRequestURL(t, blockedSettings, "/search.json", staleValues)
	store, err = cache.Open(blockedSettings.CachePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save("previous-url", 429, nil, "slow", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(staleURL, 200, []byte(`{"data":{"dist":8,"children":[]}}`), "", time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	got = listingResponse{}
	if err := internalClient(blockedSettings).getJSON(context.Background(), blockedSettings, "/search.json", staleValues, &got); err != nil || got.Data.Dist != 8 {
		t.Fatalf("global cooldown stale = %#v err=%v", got, err)
	}
	noStaleValues := url.Values{"q": []string{"new"}}
	err = internalClient(blockedSettings).getJSON(context.Background(), blockedSettings, "/search.json", noStaleValues, &got)
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusTooManyRequests || httpErr.RetryAfter <= 0 {
		t.Fatalf("global cooldown error = %#v (%v)", httpErr, err)
	}
}

func TestGetJSONTTLCacheAndURLFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"data":{"dist":3,"children":[]}}`)
	}))
	defer server.Close()
	settings := internalSettings(t, server.URL)
	values := url.Values{"q": []string{"ttl"}}
	requestURL := internalRequestURL(t, settings, "/ttl.json", values)
	store, err := cache.Open(settings.CachePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(requestURL, 200, []byte(`{"data":{"dist":4,"children":[]}}`), "", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	client := internalClient(settings)
	var got listingResponse
	if err := client.getJSONTTL(context.Background(), settings, "/ttl.json", values, &got, 123); err != nil || got.Data.Dist != 4 {
		t.Fatalf("TTL cache = %#v err=%v", got, err)
	}
	bad := settings
	bad.BaseURL = "://invalid"
	if err := client.getJSON(context.Background(), bad, "/x", nil, &got); err == nil {
		t.Fatal("invalid base URL unexpectedly succeeded")
	}
	if err := client.getJSONTTL(context.Background(), bad, "/x", nil, &got, 123); err == nil {
		t.Fatal("invalid TTL base URL unexpectedly succeeded")
	}
	activeSettings := internalSettings(t, server.URL)
	activeValues := url.Values{"q": []string{"active"}}
	activeURL := internalRequestURL(t, activeSettings, "/ttl.json", activeValues)
	activeStore, err := cache.Open(activeSettings.CachePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := activeStore.Save(activeURL, 429, []byte(`{"data":{"dist":5,"children":[]}}`), "rate limited", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_ = activeStore.Close()
	if err := client.getJSONTTL(context.Background(), activeSettings, "/ttl.json", activeValues, &got, 123); err != nil || got.Data.Dist != 5 {
		t.Fatalf("TTL stale cache = %#v err=%v", got, err)
	}
	noStaleSettings := internalSettings(t, server.URL)
	noStaleValues := url.Values{"q": []string{"blocked"}}
	noStaleURL := internalRequestURL(t, noStaleSettings, "/ttl.json", noStaleValues)
	noStaleStore, err := cache.Open(noStaleSettings.CachePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := noStaleStore.Save(noStaleURL, 429, nil, "rate limited", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_ = noStaleStore.Close()
	if err := client.getJSONTTL(context.Background(), noStaleSettings, "/ttl.json", noStaleValues, &got, 123); err == nil {
		t.Fatal("TTL active error without stale unexpectedly succeeded")
	}
	invalidCache := settings
	invalidCache.CachePath = filepath.Join(t.TempDir(), "parent-file")
	if err := osWriteFile(invalidCache.CachePath); err != nil {
		t.Fatal(err)
	}
	if result, err := internalClient(invalidCache).Search(context.Background(), "uncached", SearchOptions{Limit: 0}); err != nil || len(result.Posts) != 0 {
		t.Fatalf("search with unavailable cache = %#v err=%v", result, err)
	}
}

func osWriteFile(path string) error {
	return os.WriteFile(path, []byte("not a directory"), 0o600)
}

func TestGetJSONAttemptCooldownNetworkAndBodyErrors(t *testing.T) {
	settings := internalSettings(t, "http://example.invalid")
	client := internalClient(settings)
	var target listingResponse
	client.startCooldown(time.Hour)
	err := client.getJSONAttempt(context.Background(), settings, "http://example.invalid/cooldown", &target, 100, false)
	var cooldownErr *HTTPError
	if !errors.As(err, &cooldownErr) || cooldownErr.RetryAfter <= 0 {
		t.Fatalf("cooldown without stale = %#v (%v)", cooldownErr, err)
	}

	stalePath := filepath.Join(t.TempDir(), "stale.db")
	staleSettings := settings
	staleSettings.CachePath = stalePath
	store, err := cache.Open(stalePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save("http://example.invalid/stale", 200, []byte(`{"data":{"dist":7,"children":[]}}`), "", time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	staleClient := internalClient(staleSettings)
	staleClient.startCooldown(time.Hour)
	if err := staleClient.getJSONAttempt(context.Background(), staleSettings, "http://example.invalid/stale", &target, 100, false); err != nil || target.Data.Dist != 7 {
		t.Fatalf("cooldown stale = %#v err=%v", target, err)
	}

	networkClient := internalClient(settings)
	networkClient.HTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network failed")
	})}
	if err := networkClient.getJSONAttempt(context.Background(), settings, "http://example.invalid/network", &target, 100, false); err == nil || !strings.Contains(err.Error(), "network failed") {
		t.Fatalf("network error = %v", err)
	}

	staleNetworkSettings := internalSettings(t, "http://example.invalid")
	staleNetworkStore, err := cache.Open(staleNetworkSettings.CachePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := staleNetworkStore.Save("http://example.invalid/network", 200, []byte(`{"data":{"dist":8,"children":[]}}`), "", time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	_ = staleNetworkStore.Close()
	staleNetworkClient := internalClient(staleNetworkSettings)
	staleNetworkClient.HTTP = networkClient.HTTP
	if err := staleNetworkClient.getJSONAttempt(context.Background(), staleNetworkSettings, "http://example.invalid/network", &target, 100, false); err != nil || target.Data.Dist != 8 {
		t.Fatalf("network stale = %#v err=%v", target, err)
	}

	bodyClient := internalClient(settings)
	bodyClient.HTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: errorReadCloser{}, Header: make(http.Header)}, nil
	})}
	if err := bodyClient.getJSONAttempt(context.Background(), settings, "http://example.invalid/body", &target, 100, false); err == nil || err.Error() != "body read failed" {
		t.Fatalf("body error = %v", err)
	}

	requestClient := internalClient(settings)
	if err := requestClient.getJSONAttempt(context.Background(), settings, "://bad", &target, 100, false); err == nil {
		t.Fatal("invalid request URL unexpectedly succeeded")
	}
}

func TestGetJSONAttemptHTTPErrorVariants(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		headers    http.Header
		wantBody   string
		wantStatus int
		wantRetry  time.Duration
	}{
		{name: "json message", status: 500, body: `{"message":"server message"}`, wantBody: "server message", wantStatus: 500},
		{name: "plain truncated", status: 500, body: strings.Repeat("x", 600), wantBody: strings.Repeat("x", 500), wantStatus: 500},
		{name: "forbidden unchanged cookie", status: 403, body: "forbidden", wantBody: "forbidden", wantStatus: 403, wantRetry: defaultCooldown},
		{name: "rate limited", status: 429, body: "rate", headers: http.Header{"Retry-After": []string{"0"}}, wantBody: "rate", wantStatus: 429, wantRetry: defaultCooldown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for key, values := range test.headers {
					for _, value := range values {
						w.Header().Add(key, value)
					}
				}
				w.WriteHeader(test.status)
				_, _ = fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			settings := internalSettings(t, server.URL)
			client := internalClient(settings)
			var target listingResponse
			err := client.getJSONAttempt(context.Background(), settings, server.URL+"/error", &target, 100, false)
			var httpErr *HTTPError
			if !errors.As(err, &httpErr) || httpErr.StatusCode != test.wantStatus || httpErr.Body != test.wantBody || httpErr.RetryAfter != test.wantRetry {
				t.Fatalf("error = %#v (%v), want status=%d body=%q retry=%s", httpErr, err, test.wantStatus, test.wantBody, test.wantRetry)
			}
		})
	}
}

func TestPublicMethodsPropagateHTTPFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `{"message":"failure"}`)
	}))
	defer server.Close()
	settings := internalSettings(t, server.URL)
	client := internalClient(settings)
	if _, err := client.Search(context.Background(), "failure", SearchOptions{Limit: 1}); err == nil {
		t.Fatal("Search unexpectedly succeeded")
	}
	if _, _, err := client.Subreddits(context.Background(), "failure", 1); err == nil {
		t.Fatal("Subreddits unexpectedly succeeded")
	}
	if _, err := client.Trends(context.Background(), "Go", "hot", "week", 1); err == nil {
		t.Fatal("Trends unexpectedly succeeded")
	}
	if _, err := client.Thread(context.Background(), "abcde", ThreadOptions{}); err == nil {
		t.Fatal("Thread unexpectedly succeeded")
	}
}

func TestGetJSONAttemptSuccessfulAndMalformedJSONResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/bad" {
			_, _ = fmt.Fprint(w, "not json")
			return
		}
		_, _ = fmt.Fprint(w, `{"data":{"dist":9,"children":[]}}`)
	}))
	defer server.Close()
	settings := internalSettings(t, server.URL)
	client := internalClient(settings)
	var target listingResponse
	if err := client.getJSONAttempt(context.Background(), settings, server.URL+"/ok", &target, 100, false); err != nil || target.Data.Dist != 9 {
		t.Fatalf("success = %#v err=%v", target, err)
	}
	if err := client.getJSONAttempt(context.Background(), settings, server.URL+"/bad", &target, 100, false); err == nil {
		t.Fatal("malformed success body unexpectedly decoded")
	}
}
