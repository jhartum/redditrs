package reddit

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jhartum/redditrs/internal/cache"
	"github.com/jhartum/redditrs/internal/config"
	"github.com/jhartum/redditrs/internal/model"
)

type Client struct {
	HTTP          *http.Client
	LoadConfig    func() config.Settings
	ForceRefresh  bool
	mu            sync.Mutex
	lastSent      time.Time
	cooldownUntil time.Time
}

type SearchOptions struct {
	Subreddit string
	Sort      string
	Time      string
	Limit     int
}

type SearchResult struct {
	Posts []model.Post
	Total int
}

const defaultCooldown = 5 * time.Minute

type HTTPError struct {
	StatusCode int
	RetryAfter time.Duration
	Body       string
}

func (e *HTTPError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("Reddit returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("Reddit returned HTTP %d: %s", e.StatusCode, e.Body)
}

func NewClient() *Client {
	return &Client{
		HTTP:       &http.Client{Timeout: 30 * time.Second},
		LoadConfig: config.Load,
	}
}

func (c *Client) Search(ctx context.Context, query string, options SearchOptions) (SearchResult, error) {
	settings := c.LoadConfig()
	path := "/search.json"
	if options.Subreddit != "" {
		path = "/r/" + url.PathEscape(options.Subreddit) + "/search.json"
	}
	values := url.Values{}
	values.Set("q", query)
	values.Set("sort", cmp.Or(options.Sort, "relevance"))
	values.Set("t", cmp.Or(options.Time, "all"))
	values.Set("limit", strconv.Itoa(options.Limit))
	values.Set("raw_json", "1")
	if options.Subreddit != "" {
		values.Set("restrict_sr", "1")
	}

	var listing listingResponse
	if err := c.getJSON(ctx, settings, path, values, &listing); err != nil {
		return SearchResult{}, err
	}

	posts := make([]model.Post, 0, len(listing.Data.Children))
	for _, child := range listing.Data.Children {
		if child.Kind != "t3" {
			continue
		}
		post := child.Data
		post.Age = model.RelativeAge(post.CreatedUTC, time.Now())
		posts = append(posts, post)
	}
	return SearchResult{Posts: posts, Total: listing.Data.Dist}, nil
}

func (c *Client) getJSON(ctx context.Context, settings config.Settings, path string, values url.Values, target any) error {
	return c.getJSONTTL(ctx, settings, path, values, target, settings.CacheTTLMS)
}

func (c *Client) getJSONTTL(ctx context.Context, settings config.Settings, path string, values url.Values, target any, ttlMS int) error {
	base, err := url.Parse(strings.TrimRight(settings.BaseURL, "/"))
	if err != nil {
		return err
	}
	base.Path = strings.TrimRight(base.Path, "/") + path
	base.RawQuery = values.Encode()
	requestURL := base.String()
	if !c.ForceRefresh {
		if served, err := c.cachedResponse(settings, requestURL, target); served {
			return err
		}
	}
	return c.getJSONAttempt(ctx, settings, requestURL, target, ttlMS, false)
}

// cachedResponse serves fresh or stale entries and remembers active errors
// and the global cooldown. It opens the store only for the duration of the
// read: bbolt holds an exclusive file lock, so the store must never stay
// open across network I/O or sleeps.
func (c *Client) cachedResponse(settings config.Settings, requestURL string, target any) (bool, error) {
	store, err := cache.Open(settings.CachePath)
	if err != nil {
		return false, nil
	}
	defer store.Close()
	now := time.Now()
	if raw, found, err := store.Fresh(requestURL, now); err == nil && found {
		if err := json.Unmarshal(raw, target); err == nil {
			return true, nil
		}
	}
	if status, message, retry, active, err := store.ActiveError(requestURL, now); err == nil && active {
		if raw, found, _ := store.Stale(requestURL); found {
			if err := json.Unmarshal(raw, target); err == nil {
				return true, nil
			}
		}
		return true, &HTTPError{StatusCode: status, RetryAfter: retry, Body: message}
	}
	if retry, active, err := store.ActiveCooldown(now); err == nil && active {
		if raw, found, _ := store.Stale(requestURL); found {
			if err := json.Unmarshal(raw, target); err == nil {
				return true, nil
			}
		}
		return true, &HTTPError{StatusCode: http.StatusTooManyRequests, RetryAfter: retry, Body: "Reddit is cooling down after a rate-limit or block response"}
	}
	return false, nil
}

func (c *Client) getJSONAttempt(ctx context.Context, settings config.Settings, requestURL string, target any, ttlMS int, retriedCookie bool) error {
	if retry := c.cooldownRemaining(); retry > 0 {
		if raw, found := c.staleBody(settings, requestURL); found {
			if err := json.Unmarshal(raw, target); err == nil {
				return nil
			}
		}
		return &HTTPError{StatusCode: http.StatusTooManyRequests, RetryAfter: retry, Body: "Reddit is cooling down after a rate-limit or block response"}
	}
	c.waitShared(settings.DelayMS, settings)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", settings.UserAgent)
	req.Header.Set("Accept", "application/json,text/plain,*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	if settings.Cookie != "" {
		req.Header.Set("Cookie", settings.Cookie)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if raw, found := c.staleBody(settings, requestURL); found {
			if decodeErr := json.Unmarshal(raw, target); decodeErr == nil {
				return nil
			}
		}
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusForbidden && !retriedCookie {
		refreshed := c.LoadConfig()
		if refreshed.Cookie != settings.Cookie {
			return c.getJSONAttempt(ctx, refreshed, requestURL, target, ttlMS, true)
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var message struct {
			Error string `json:"message"`
		}
		_ = json.Unmarshal(body, &message)
		if message.Error == "" {
			message.Error = strings.TrimSpace(string(body))
			if len(message.Error) > 500 {
				message.Error = message.Error[:500]
			}
		}
		expiresIn := time.Duration(ttlMS) * time.Millisecond
		var retry time.Duration
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
			retry = retryAfter(resp.Header.Get("Retry-After"))
			if retry <= 0 {
				retry = defaultCooldown
			}
			c.startCooldown(retry)
			expiresIn = retry
		}
		var stale []byte
		staleFound := false
		if raw, found := c.staleBody(settings, requestURL); found {
			stale, staleFound = raw, true
		}
		var rawToSave []byte
		if staleFound {
			rawToSave = stale
		}
		c.saveBody(settings, requestURL, resp.StatusCode, rawToSave, message.Error, expiresIn)
		if staleFound {
			if err := json.Unmarshal(stale, target); err == nil {
				return nil
			}
		}
		return &HTTPError{StatusCode: resp.StatusCode, RetryAfter: retry, Body: message.Error}
	}
	if err := json.Unmarshal(body, target); err != nil {
		return err
	}
	c.saveBody(settings, requestURL, resp.StatusCode, body, "", time.Duration(ttlMS)*time.Millisecond)
	return nil
}

// staleBody and saveBody open the store only for the duration of the
// operation; see cachedResponse for why.
func (c *Client) staleBody(settings config.Settings, requestURL string) ([]byte, bool) {
	store, err := cache.Open(settings.CachePath)
	if err != nil {
		return nil, false
	}
	defer store.Close()
	raw, found, err := store.Stale(requestURL)
	if err != nil {
		return nil, false
	}
	return raw, found
}

func (c *Client) saveBody(settings config.Settings, requestURL string, status int, raw []byte, errText string, ttl time.Duration) {
	store, err := cache.Open(settings.CachePath)
	if err != nil {
		return
	}
	defer store.Close()
	_ = store.Save(requestURL, status, raw, errText, time.Now().Add(ttl))
}

func (c *Client) cooldownRemaining() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	remaining := time.Until(c.cooldownUntil)
	if remaining <= 0 {
		return 0
	}
	return remaining
}

func (c *Client) startCooldown(duration time.Duration) {
	if duration <= 0 {
		duration = defaultCooldown
	}
	c.mu.Lock()
	until := time.Now().Add(duration)
	if until.After(c.cooldownUntil) {
		c.cooldownUntil = until
	}
	c.mu.Unlock()
}

func retryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	seconds, err := strconv.Atoi(value)
	if err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		if wait := time.Until(when); wait > 0 {
			return wait
		}
		return 0
	}
	return defaultCooldown
}

func (c *Client) waitShared(delayMS int, settings config.Settings) {
	if delayMS < 0 {
		delayMS = 0
	}
	if store, err := cache.Open(settings.CachePath); err == nil {
		scheduled, err := store.ReserveRequestSlot(time.Now(), time.Duration(delayMS)*time.Millisecond)
		_ = store.Close()
		if err == nil {
			if wait := time.Until(scheduled); wait > 0 {
				time.Sleep(wait)
			}
			c.mu.Lock()
			c.lastSent = time.Now()
			c.mu.Unlock()
			return
		}
	}
	c.wait(delayMS)
}

func (c *Client) wait(delayMS int) {
	if delayMS < 0 {
		delayMS = 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.lastSent.IsZero() {
		wait := c.lastSent.Add(time.Duration(delayMS) * time.Millisecond).Sub(time.Now())
		if wait > 0 {
			time.Sleep(wait)
		}
	}
	c.lastSent = time.Now()
}

type listingResponse struct {
	Data struct {
		Dist     int `json:"dist"`
		Children []struct {
			Kind string     `json:"kind"`
			Data model.Post `json:"data"`
		} `json:"children"`
	} `json:"data"`
}
