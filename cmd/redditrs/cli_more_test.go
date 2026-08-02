package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jhartum/redditrs/internal/cache"
	"github.com/jhartum/redditrs/internal/config"
	"github.com/jhartum/redditrs/internal/model"
	"github.com/jhartum/redditrs/internal/rank"
	"github.com/jhartum/redditrs/internal/reddit"
)

func setInProcessEnv(t *testing.T, baseURL, cookie string) {
	t.Helper()
	cacheDir := t.TempDir()
	t.Setenv("REDDITRS_COOKIE", cookie)
	t.Setenv("REDDITRS_COOKIE_FILE", "")
	t.Setenv("REDDITRS_CONFIG_PATH", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("REDDITRS_CACHE_DIR", cacheDir)
	t.Setenv("REDDITRS_CACHE_PATH", filepath.Join(cacheDir, "reddit.db"))
	t.Setenv("REDDITRS_BASE_URL", baseURL)
	t.Setenv("REDDITRS_DELAY_MS", "250")
}

func executeInProcess(t *testing.T, baseURL, cookie string, args ...string) (string, error) {
	t.Helper()
	setInProcessEnv(t, baseURL, cookie)
	formatFlag = "toon"
	fieldsFlag = ""
	fullFlag = false
	root := newRootCommand()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs(args)
	err := root.Execute()
	return output.String(), err
}

func newCLIStub(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search.json", "/r/Go/search.json", "/r/Other/search.json":
			_, _ = fmt.Fprint(w, `{"data":{"dist":2,"children":[{"kind":"t3","data":{"id":"abcde","subreddit":"Go","title":"Test post","author":"alice","score":10,"num_comments":1,"created_utc":1704067200,"selftext":"settings guide"}}]}}`)
		case "/comments/abcde.json":
			_, _ = fmt.Fprint(w, `[{"data":{"children":[{"kind":"t3","data":{"id":"abcde","subreddit":"Go","title":"Test post","author":"alice","score":10,"num_comments":1,"created_utc":1704067200,"selftext":"settings guide"}}]}},{"data":{"children":[{"kind":"t1","data":{"id":"c1","author":"bob","score":4,"body":"use fp8 settings","created_utc":1704067200,"replies":""}}]}}]`)
		case "/subreddits/search.json":
			_, _ = fmt.Fprint(w, `{"data":{"dist":1,"children":[{"kind":"t5","data":{"display_name":"Go","title":"Go","public_description":"Go language","subscribers":1000}}]}}`)
		case "/r/Go/hot.json", "/r/Go/top.json", "/r/Go/new.json":
			_, _ = fmt.Fprint(w, `{"data":{"dist":1,"children":[{"kind":"t3","data":{"id":"abcde","subreddit":"Go","title":"Trend","score":10,"created_utc":1704067200}}]}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func testPost() model.Post {
	return model.Post{ID: "abcde", Subreddit: "Go", Title: "Title", Author: "alice", Score: 3, NumComments: 3, CreatedUTC: 1704067200, Age: "1d ago", URL: "https://example.com", Permalink: "/r/Go/comments/abcde/title/", Flair: "flair", Selftext: "body"}
}

func testComment() model.Comment {
	return model.Comment{ID: "c1", PostID: "abcde", Author: "alice", Score: 2, Body: "body", Depth: 1, CreatedUTC: 1704067200, URL: "/comments/c1"}
}

func TestRenderURLAndSearchVariants(t *testing.T) {
	formatFlag = "toon"
	ref := reddit.URLReference{Kind: "comment", Subreddit: "Go", PostID: "abcde", CommentID: "fghij", Username: "alice", CanonicalURL: "https://www.reddit.com"}
	got := renderURLReference(ref)
	if !strings.Contains(got, "comment_id: fghij") || !strings.Contains(got, "username: alice") {
		t.Fatalf("URL TOON = %q", got)
	}
	formatFlag = "json"
	got = renderURLReference(ref)
	if !json.Valid([]byte(got)) {
		t.Fatalf("URL JSON = %q", got)
	}

	result := reddit.SearchResult{Posts: []model.Post{testPost()}, Total: 3}
	formatFlag = "toon"
	fieldsFlag = "id,subreddit,title,score,age,author,url,permalink,num_comments,flair,created_utc,selftext"
	got = renderSearch("query", result)
	if !strings.Contains(got, "count: 1 of 3 total") || !strings.Contains(got, "u/alice") || !strings.Contains(got, "/r/Go/comments/abcde/title/") || !strings.Contains(got, "body") || !strings.Contains(got, "If comments are needed") {
		t.Fatalf("search TOON = %q", got)
	}
	formatFlag = "json"
	got = renderSearch("query", result)
	if !json.Valid([]byte(got)) {
		t.Fatalf("search JSON = %q", got)
	}
	if got := renderSearch("empty", reddit.SearchResult{}); !strings.Contains(got, `"posts":[]`) {
		t.Fatalf("empty search JSON = %q", got)
	}
	formatFlag = "toon"
	fieldsFlag = ""
	if got := renderSearch("empty", reddit.SearchResult{}); !strings.Contains(got, "0 posts found") {
		t.Fatalf("empty search = %q", got)
	}
	formatFlag = "toon"
	fieldsFlag = "unknown"
	if _, err := selectedSearchFields(); err == nil {
		t.Fatal("unknown search field unexpectedly accepted")
	}
	fieldsFlag = ","
	if _, err := selectedSearchFields(); err == nil {
		t.Fatal("empty search fields unexpectedly accepted")
	}
	fieldsFlag = ""
	for _, field := range []string{"id", "subreddit", "title", "score", "age", "author", "url", "permalink", "num_comments", "flair", "created_utc", "selftext", "unknown"} {
		_ = searchFieldValue(testPost(), field)
	}
	for value, want := range map[string]string{
		"plain":       "plain",
		"a,b":         `"a,b"`,
		"123":         `"123"`,
		"1e3":         `"1e3"`,
		"+1":          `"+1"`,
		"  padded  ":  `"  padded  "`,
		"\x01control": `"\u0001control"`,
		"\\\"\n\r\t":  `"\\\"\n\r\t"`,
	} {
		if got := toonString(value); got != want {
			t.Errorf("toonString(%q) = %q, want %q", value, got, want)
		}
	}
	if got := toonString(string([]byte{0xff})); !strings.HasPrefix(got, `"`) {
		t.Fatalf("invalid UTF-8 was not quoted: %q", got)
	}
	if safeHelpLine("plain") != "plain" || strings.ContainsRune(safeHelpLine("line\nbreak"), '\n') {
		t.Fatal("safeHelpLine did not escape control characters")
	}
	injected := renderSearch("x\"\ncode: INJECTED", reddit.SearchResult{})
	if strings.Contains(injected, "\ncode: INJECTED") || !strings.Contains(injected, `\ncode: INJECTED`) {
		t.Fatalf("query was not safely rendered: %q", injected)
	}
}

func TestRenderThreadPackAndResolvedVariants(t *testing.T) {
	long := strings.Repeat("x", 501)
	thread := reddit.ThreadResult{Post: model.Post{ID: "abcde", Subreddit: "Go", Title: "Title", Author: "alice", Score: 4, NumComments: 3, Age: "1d ago", URL: "url", Selftext: long}, Comments: []model.Comment{testComment(), {Author: "bob", Score: 1, Body: long, PostID: "abcde"}}}
	formatFlag = "toon"
	fullFlag = false
	fieldsFlag = "author,score,body,post_id,depth,created_utc,url"
	got := renderThread(thread)
	if !strings.Contains(got, "help[1]:") || !strings.Contains(got, "If the truncated text is needed") || !strings.Contains(got, "count: 2 of 3 total") {
		t.Fatalf("thread TOON = %q", got)
	}
	fullFlag = true
	got = renderThread(thread)
	if strings.Contains(got, "truncated") {
		t.Fatalf("full thread = %q", got)
	}
	formatFlag = "json"
	if got := renderThread(thread); !json.Valid([]byte(got)) {
		t.Fatalf("thread JSON = %q", got)
	}
	if got := renderThread(reddit.ThreadResult{}); !strings.Contains(got, `"comments":[]`) {
		t.Fatalf("empty thread JSON = %q", got)
	}
	formatFlag = "toon"
	fieldsFlag = "unknown"
	if _, err := selectedCommentFields(); err == nil {
		t.Fatal("unknown comment field unexpectedly accepted")
	}
	fieldsFlag = ","
	if fields, err := selectedCommentFields(); err != nil || len(fields) != 0 {
		t.Fatalf("empty comment fields = %#v err=%v", fields, err)
	}
	fieldsFlag = ""
	for _, field := range []string{"author", "score", "body", "post_id", "depth", "created_utc", "url", "unknown"} {
		_ = commentFieldValue(testComment(), field)
	}
	fullFlag = false
	if got := truncateText("short", "body", 10); got != "short" || !strings.Contains(truncateText(long, "body", 500), "truncated") {
		t.Fatal("truncateText variants failed")
	}

	data := packData{Topic: "topic", Intent: "compare", Posts: []model.Post{testPost()}, Comments: []model.Comment{testComment()}, Clusters: []rank.ClusterSummary{{Cluster: "general", Count: 1, Hint: "hint"}}}
	formatFlag = "toon"
	got = renderPack(data, "quick")
	if !strings.Contains(got, "evidence[1]") || !strings.Contains(got, "posts[1]") || !strings.Contains(got, "If comment quotes are needed") || !strings.Contains(got, `--intent compare --depth deep`) {
		t.Fatalf("quick pack = %q", got)
	}
	got = renderPack(data, "deep")
	if !strings.Contains(got, "comments[1]") || !strings.Contains(got, "clusters[1]") || !strings.Contains(got, "If one post needs more context") {
		t.Fatalf("deep pack = %q", got)
	}
	formatFlag = "json"
	if got := renderPack(data, "quick"); !json.Valid([]byte(got)) {
		t.Fatalf("pack JSON = %q", got)
	}
	formatFlag = "toon"
	if got := renderPack(packData{Topic: "empty", Intent: "general"}, "quick"); !strings.Contains(got, "subreddits: []") {
		t.Fatalf("empty pack = %q", got)
	}
	if got := renderPackPosts(nil); !strings.Contains(strings.Join(got, "\n"), "posts[0]") {
		t.Fatal("empty pack posts missing header")
	}
	if got := renderClusters(nil); len(got) != 1 || !strings.Contains(got[0], "clusters[0]") {
		t.Fatalf("empty clusters = %#v", got)
	}
	if got := splitCSV(" A, a, ,B,"); len(got) != 2 || got[0] != "A" || got[1] != "B" || splitCSV("") != nil {
		t.Fatalf("splitCSV = %#v", got)
	}
	if got := uniquePosts([]model.Post{{ID: "a"}, {ID: "a"}, {}, {}}); len(got) != 3 {
		t.Fatalf("uniquePosts = %#v", got)
	}
	evidence := make([]model.EvidenceItem, 0, 50)
	for index := 0; index < 7; index++ {
		evidence = append(evidence, model.EvidenceItem{Cluster: "same"})
	}
	for index := 0; index < 40; index++ {
		evidence = append(evidence, model.EvidenceItem{Cluster: fmt.Sprintf("cluster-%02d", index)})
	}
	if got := limitEvidence(evidence, 40, 6); len(got) != 40 || got[5].Cluster != "same" || got[6].Cluster == "same" {
		t.Fatalf("limitEvidence = %#v", got)
	}
	posts := []model.Post{{Subreddit: "A"}, {Subreddit: "A"}}
	for char := 'B'; char <= 'N'; char++ {
		posts = append(posts, model.Post{Subreddit: string(char)})
	}
	observed := observedSubreddits(posts)
	if len(observed) != 12 || observed[0] != "A (2)" || observed[1] != "B (1)" || observed[11] != "L (1)" {
		t.Fatalf("observed subreddits = %#v", observed)
	}
	caseTied := observedSubreddits([]model.Post{{Subreddit: "go"}, {Subreddit: "Go"}})
	if caseTied[0] != "Go (1)" {
		t.Fatalf("case-tied subreddits = %#v", caseTied)
	}

	items := []model.SubredditCandidate{{Name: "Go", Score: 3, Reasons: []string{"match"}, Subscribers: 100, Title: "Go help", PublicDescription: strings.Repeat("description ", 30)}}
	formatFlag = "toon"
	if got := renderResolvedSubreddits("local development", items, 2); !strings.Contains(got, "count: 1 of 2 total") || !strings.Contains(got, `redditrs search "local development" --subreddits Go`) || !strings.Contains(got, "If recent examples are needed") || !strings.Contains(got, "title,description") || !strings.Contains(got, "Go help") || !strings.Contains(got, "…") {
		t.Fatalf("resolved TOON = %q", got)
	}
	if got := renderResolvedSubreddits("empty", nil, 0); !strings.Contains(got, "0 candidates") {
		t.Fatalf("empty resolved = %q", got)
	}
	formatFlag = "json"
	if got := renderResolvedSubreddits("local development", items, 1); !json.Valid([]byte(got)) {
		t.Fatalf("resolved JSON = %q", got)
	}
	if got := renderResolvedSubreddits("empty", nil, 0); !strings.Contains(got, `"subreddits":[]`) {
		t.Fatalf("empty resolved JSON = %q", got)
	}
}

func TestSubredditDiscoveryQueryPlanning(t *testing.T) {
	got := subredditDiscoveryQueries(`NixOS AND Arch self:yes practical advice`)
	want := []string{`NixOS AND Arch self:yes practical advice`, "NixOS", "Arch"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("queries = %#v, want %#v", got, want)
	}
	got = subredditDiscoveryQueries("alpha beta gamma delta epsilon")
	want = []string{"alpha beta gamma delta epsilon", "alpha", "beta", "gamma"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("capped queries = %#v, want %#v", got, want)
	}
	if got := subredditDiscoveryQueries("  homelab  "); len(got) != 1 || got[0] != "homelab" {
		t.Fatalf("single-term queries = %#v", got)
	}
	if resolveCacheKey("  HomeLab   SELF-hosted ") != resolveCacheKey("homelab self-hosted") {
		t.Fatal("resolve cache key is not normalized")
	}
}

func TestRenderStatusHomeAndErrors(t *testing.T) {
	settings := config.Settings{Cookie: "reddit_session=x; token_v2=y", ConfigPath: "/home/test/.config/redditrs/config.json", CachePath: "/home/test/.cache/redditrs/reddit.db", DelayMS: 500}
	stats := cache.Stats{Requests: 2, SizeBytes: 1234, Cooldown: 1500 * time.Millisecond}
	formatFlag = "toon"
	if got := renderStatus(settings, stats); !strings.Contains(got, "cooldown: 2s") || !strings.Contains(got, "set (reddit_session, token_v2)") || !strings.Contains(got, "To verify live Reddit access") {
		t.Fatalf("status TOON = %q", got)
	}
	formatFlag = "json"
	if got := renderStatus(settings, stats); !json.Valid([]byte(got)) {
		t.Fatalf("status JSON = %q", got)
	}
	if formatCooldown(0) != "0s" || formatCooldown(-time.Second) != "0s" || formatCooldown(time.Second) != "1s" {
		t.Fatal("formatCooldown variants failed")
	}
	if cookieSummary("") != "not set" || cookieSummary("reddit_session=x") != "set (reddit_session)" {
		t.Fatal("cookieSummary variants failed")
	}
	home, _ := os.UserHomeDir()
	if displayPath(home+"/.config") != "~/.config" || displayPath("/tmp/other") == "~/.config" || displayPath(home) != "~" {
		t.Fatal("displayPath variants failed")
	}
	formatFlag = "toon"
	if got := renderHome(settings, stats); !strings.Contains(got, "help[4]") {
		t.Fatalf("home with cookie = %q", got)
	}
	settings.Cookie = ""
	if got := renderHome(settings, stats); !strings.Contains(got, "help[2]") {
		t.Fatalf("home without cookie = %q", got)
	}
	formatFlag = "json"
	if got := renderHome(settings, stats); !json.Valid([]byte(got)) {
		t.Fatalf("home JSON = %q", got)
	}

	cliErr := &cliError{Message: "bad", Code: "UNKNOWN", Help: []string{"fix"}}
	formatFlag = "toon"
	if cliErr.Error() != "bad" || !strings.Contains(renderCLIError(cliErr), "help[1]") || !strings.Contains(renderCLIError(&cliError{Message: "bad", Code: "UNKNOWN"}), "code: UNKNOWN") {
		t.Fatal("CLI error rendering failed")
	}
	formatFlag = "json"
	if got := renderCLIError(cliErr); !json.Valid([]byte(got)) || !strings.Contains(got, `"code":"UNKNOWN"`) {
		t.Fatalf("JSON CLI error = %q", got)
	}
	formatFlag = "toon"
	if got := renderCLIError(&cliError{Message: "oops\ncode: INJECTED", Code: "UNKNOWN"}); strings.Contains(got, "\ncode: INJECTED") || !strings.Contains(got, `\ncode: INJECTED`) {
		t.Fatalf("injected CLI error = %q", got)
	}
	for _, test := range []struct {
		err  error
		code string
	}{
		{&reddit.HTTPError{StatusCode: http.StatusForbidden}, "FORBIDDEN"},
		{&reddit.HTTPError{StatusCode: http.StatusTooManyRequests, RetryAfter: time.Second}, "RATE_LIMITED"},
		{&reddit.HTTPError{StatusCode: http.StatusNotFound}, "NOT_FOUND"},
		{&reddit.HTTPError{StatusCode: http.StatusInternalServerError}, "UNKNOWN"},
		{errors.New("network"), "UNKNOWN"},
	} {
		if got := mapRedditError(test.err); got.Code != test.code || got.exitCode() != 1 {
			t.Errorf("mapRedditError(%v) = %#v", test.err, got)
		}
	}
	if got := mapRedditError(&reddit.HTTPError{StatusCode: http.StatusTooManyRequests, RetryAfter: 500 * time.Millisecond}); !strings.Contains(got.Message, "for 1s") {
		t.Fatalf("subsecond rate limit = %#v", got)
	}
	if got := usageError(errors.New("unknown flag: --bad"), "search"); got.Code != "VALIDATION_ERROR" || !strings.Contains(got.Message, "unknown flag --bad") {
		t.Fatal("search usage error failed")
	}
	if got := usageError(errors.New("bad args"), "thread"); !strings.Contains(got.Message, "bad args") {
		t.Fatal("generic usage error failed")
	}
}

func TestRenderSubredditsAndRootFormatValidation(t *testing.T) {
	items := []model.Subreddit{{Name: "Go", Title: "Go language", Subscribers: 1800000}}
	formatFlag = "toon"
	if got := renderSubreddits(items, 2); !strings.Contains(got, "count: 1 of 2 total") || !strings.Contains(got, "1.8M") {
		t.Fatalf("subreddits TOON = %q", got)
	}
	if got := renderSubreddits(nil, 0); !strings.Contains(got, "0 subreddits") {
		t.Fatalf("empty subreddits = %q", got)
	}
	formatFlag = "json"
	if got := renderSubreddits(items, 1); !json.Valid([]byte(got)) {
		t.Fatalf("subreddits JSON = %q", got)
	}
	if got := renderSubreddits(nil, 0); !strings.Contains(got, `"subreddits":[]`) {
		t.Fatalf("empty subreddits JSON = %q", got)
	}
	for _, value := range []int64{0, 999, 1000, 1_000_000} {
		if formatSubscribers(value) == "" {
			t.Fatalf("empty subscriber format for %d", value)
		}
	}
	if compactNumber(1.26, "X") != "1.3X" {
		t.Fatalf("compactNumber rounding failed")
	}
	formatFlag = "bad"
	root := newRootCommand()
	root.SetArgs([]string{"--format", "bad"})
	if err := root.Execute(); err == nil {
		t.Fatal("invalid root format unexpectedly succeeded")
	}
}

func TestInProcessCommandsCoverSuccessPaths(t *testing.T) {
	server := newCLIStub(t)
	defer server.Close()
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "home", args: nil, want: "description:"},
		{name: "url", args: []string{"url-extract", "abcde"}, want: "kind: post"},
		{name: "status", args: []string{"status"}, want: "status:"},
		{name: "search", args: []string{"search", "test", "--limit", "1"}, want: "posts[1]"},
		{name: "thread", args: []string{"thread", "abcde", "--top", "1"}, want: "post:"},
		{name: "subreddits", args: []string{"subreddits", "Go", "--limit", "1"}, want: "subreddits[1]"},
		{name: "trends", args: []string{"trends", "Go", "--sort", "hot", "--limit", "1"}, want: "posts[1]"},
		{name: "resolve", args: []string{"resolve-subreddits", "Go", "--limit", "1", "--refresh"}, want: "subreddits[1]"},
		{name: "pack", args: []string{"pack", "settings", "--depth", "quick", "--limit", "1", "--comments-per-post", "1"}, want: "pack:"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := executeInProcess(t, server.URL, "reddit_session=x", test.args...)
			if err != nil || !strings.Contains(got, test.want) {
				t.Fatalf("output=%q err=%v", got, err)
			}
		})
	}
}

func TestAgentFacingFlagsAndPackContinuation(t *testing.T) {
	server := newCLIStub(t)
	defer server.Close()

	output, err := executeInProcess(t, server.URL, "reddit_session=x", "pack", "settings", "--intent", "settings", "--depth", "normal", "--time", "week", "--sort", "top", "--subreddits", "Go", "--limit", "1", "--comments-per-post", "1")
	if err != nil {
		t.Fatalf("canonical pack flags failed: %v\n%s", err, output)
	}
	want := `redditrs pack "settings" --intent settings --depth deep --time week --sort top --subreddits Go --limit 1 --comments-per-post 1`
	if !strings.Contains(output, want) {
		t.Fatalf("pack continuation lost options:\n%s\nwant substring:\n%s", output, want)
	}

	packHelp, err := executeInProcess(t, server.URL, "reddit_session=x", "pack", "--help")
	if err != nil || !strings.Contains(packHelp, "--limit") || strings.Contains(packHelp, "--max-posts") {
		t.Fatalf("pack help does not expose canonical limit: err=%v\n%s", err, packHelp)
	}
	trendsHelp, err := executeInProcess(t, server.URL, "reddit_session=x", "trends", "--help")
	if err != nil || !strings.Contains(trendsHelp, "--sort") || strings.Contains(trendsHelp, "--listing") {
		t.Fatalf("trends help does not expose canonical sort: err=%v\n%s", err, trendsHelp)
	}
}

func TestInProcessMultiScopeLimitsAndPackThreadBudget(t *testing.T) {
	var searchRequests int32
	var wantRequestLimit atomic.Value
	wantRequestLimit.Store("1")
	searchServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&searchRequests, 1)
		want := wantRequestLimit.Load().(string)
		if r.URL.Query().Get("limit") != want {
			t.Errorf("request limit = %q, want %s", r.URL.Query().Get("limit"), want)
		}
		subreddit := strings.Split(r.URL.Path, "/")[2]
		_, _ = fmt.Fprintf(w, `{"data":{"dist":1,"children":[{"kind":"t3","data":{"id":"id%s","subreddit":"%s","title":"post","score":1,"created_utc":1704067200}}]}}`, strings.ToLower(subreddit), subreddit)
	}))
	defer searchServer.Close()
	output, err := executeInProcess(t, searchServer.URL, "reddit_session=x", "search", "q", "--subreddits", "Go,go,Rust", "--limit", "1")
	if err != nil || !strings.Contains(output, "posts[1]") || !strings.Contains(output, "count: 1 of 2 total") {
		t.Fatalf("multi-scope search = %q err=%v", output, err)
	}
	if got := atomic.LoadInt32(&searchRequests); got != 2 {
		t.Fatalf("multi-scope requests = %d, want 2", got)
	}
	wantRequestLimit.Store("5")
	if output, err := executeInProcess(t, searchServer.URL, "reddit_session=x", "trends", "Go,Rust", "--limit", "10"); err != nil || !strings.Contains(output, "posts[2]") {
		t.Fatalf("multi-scope trends = %q err=%v", output, err)
	}
	if got := atomic.LoadInt32(&searchRequests); got != 4 {
		t.Fatalf("multi-scope search and trends requests = %d, want 4", got)
	}

	var threadRequests int32
	packServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/search.json" {
			children := make([]string, 6)
			for index := range children {
				children[index] = fmt.Sprintf(`{"kind":"t3","data":{"id":"id00%d","subreddit":"Edge","title":"q","score":%d,"created_utc":1704067200}}`, index, 10-index)
			}
			_, _ = fmt.Fprintf(w, `{"data":{"dist":6,"children":[%s]}}`, strings.Join(children, ","))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/comments/") {
			atomic.AddInt32(&threadRequests, 1)
			postID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/comments/"), ".json")
			_, _ = fmt.Fprintf(w, `[{"data":{"children":[{"kind":"t3","data":{"id":"%s","subreddit":"Edge","title":"q"}}]}},{"data":{"children":[]}}]`, postID)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer packServer.Close()
	if output, err := executeInProcess(t, packServer.URL, "reddit_session=x", "pack", "q", "--depth", "quick"); err != nil || !strings.Contains(output, "pack:") {
		t.Fatalf("quick pack = %q err=%v", output, err)
	}
	if got := atomic.LoadInt32(&threadRequests); got != 2 {
		t.Fatalf("quick pack fetched %d threads, want 2", got)
	}

	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `{"message":"failure"}`)
	}))
	defer errorServer.Close()
	if output, err := executeInProcess(t, errorServer.URL, "reddit_session=x", "pack", "q", "--subreddits", "Go", "--max-posts", "1"); err == nil {
		t.Fatalf("scoped pack swallowed search failure: %q", output)
	}
}

func TestInProcessCommandOptionAndErrorBranches(t *testing.T) {
	server := newCLIStub(t)
	defer server.Close()
	for _, test := range []struct {
		name string
		args []string
	}{
		{"search multiple scopes", []string{"search", "q", "--subreddits", "Go,Other", "--limit", "1"}},
		{"pack default depth and counts", []string{"pack", "q", "--max-posts", "0", "--comments-per-post", "0"}},
		{"pack scoped search", []string{"pack", "q", "--subreddits", "Go", "--max-posts", "1", "--comments-per-post", "1"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got, err := executeInProcess(t, server.URL, "reddit_session=x", test.args...); err != nil {
				t.Fatalf("output=%q err=%v", got, err)
			}
		})
	}

	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `{"message":"failure"}`)
	}))
	defer errorServer.Close()
	for _, args := range [][]string{
		{"search", "q"},
		{"subreddits", "q"},
		{"trends", "Go"},
		{"resolve-subreddits", "q", "--refresh"},
		{"pack", "q"},
	} {
		if _, err := executeInProcess(t, errorServer.URL, "reddit_session=x", args...); err == nil {
			t.Fatalf("%v unexpectedly succeeded", args)
		}
	}

	missingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/comments/abcde.json" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = fmt.Fprint(w, `{"data":{"dist":0,"children":[]}}`)
	}))
	defer missingServer.Close()
	if _, err := executeInProcess(t, missingServer.URL, "reddit_session=x", "thread", "unknown"); err == nil {
		t.Fatal("invalid thread reference unexpectedly succeeded")
	}
	if _, err := executeInProcess(t, missingServer.URL, "reddit_session=x", "thread", "abcde"); err == nil {
		t.Fatal("missing thread unexpectedly succeeded")
	}
}

func TestInProcessAdditionalCommandBranches(t *testing.T) {
	packServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search.json":
			_, _ = fmt.Fprint(w, `{"data":{"dist":2,"children":[{"kind":"t3","data":{"id":"abcde","subreddit":"Go","title":"One","score":10,"num_comments":2,"created_utc":1704067200}},{"kind":"t3","data":{"id":"fghij","subreddit":"Go","title":"Two","score":9,"num_comments":1,"created_utc":1704067200}}]}}`)
		case "/comments/abcde.json":
			comments := `{"data":{"children":[{"kind":"t1","data":{"id":"c1","author":"a","score":2,"body":"one","created_utc":1704067200,"replies":""}},{"kind":"t1","data":{"id":"c2","author":"b","score":1,"body":"two","created_utc":1704067200,"replies":""}}]}}`
			_, _ = fmt.Fprintf(w, `[{"data":{"children":[{"kind":"t3","data":{"id":"abcde","subreddit":"Go","title":"One","score":10,"num_comments":2,"created_utc":1704067200}}]}},%s]`, comments)
		case "/comments/fghij.json":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer packServer.Close()
	if _, err := executeInProcess(t, packServer.URL, "reddit_session=x", "pack", "q", "--depth", "quick", "--max-posts", "1", "--comments-per-post", "1"); err != nil {
		t.Fatalf("pack truncation branch failed: %v", err)
	}
	if _, err := executeInProcess(t, packServer.URL, "reddit_session=x", "pack", "q", "--depth", "quick", "--max-posts", "2", "--comments-per-post", "1"); err != nil {
		t.Fatalf("pack thread-error branch failed: %v", err)
	}

	evidenceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/search.json" {
			_, _ = fmt.Fprint(w, `{"data":{"dist":1,"children":[{"kind":"t3","data":{"id":"abcde","subreddit":"Go","title":"Evidence","created_utc":1704067200}}]}}`)
			return
		}
		if r.URL.Path == "/comments/abcde.json" {
			comments := make([]string, 41)
			for index := range comments {
				comments[index] = fmt.Sprintf(`{"kind":"t1","data":{"id":"c%04d","body":"comment","score":1,"created_utc":1704067200,"replies":""}}`, index)
			}
			_, _ = fmt.Fprintf(w, `[{"data":{"children":[{"kind":"t3","data":{"id":"abcde","subreddit":"Go","title":"Evidence","created_utc":1704067200}}]}},{"data":{"children":[%s]}}]`, strings.Join(comments, ","))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer evidenceServer.Close()
	if _, err := executeInProcess(t, evidenceServer.URL, "reddit_session=x", "pack", "q", "--depth", "deep", "--max-posts", "1", "--comments-per-post", "50"); err != nil {
		t.Fatalf("pack evidence cap branch failed: %v", err)
	}

	scopeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/r/Good/search.json" {
			_, _ = fmt.Fprint(w, `{"data":{"dist":1,"children":[{"kind":"t3","data":{"id":"abcde","subreddit":"Good","title":"Good","created_utc":1704067200}}]}}`)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer scopeServer.Close()
	if _, err := executeInProcess(t, scopeServer.URL, "reddit_session=x", "search", "q", "--subreddits", "Good,Bad", "--limit", "1"); err == nil {
		t.Fatal("scoped search error unexpectedly succeeded")
	}

	trendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"data":{"dist":2,"children":[{"kind":"t3","data":{"id":"abcde","subreddit":"A","title":"A","created_utc":1704067200}},{"kind":"t3","data":{"id":"fghij","subreddit":"A","title":"B","created_utc":1704067200}}]}}`)
	}))
	defer trendServer.Close()
	if _, err := executeInProcess(t, trendServer.URL, "reddit_session=x", "trends", " , A, ", "--limit", "1"); err != nil {
		t.Fatalf("trends empty-scope/truncate branch failed: %v", err)
	}
	formatFlag = "toon"
	fieldsFlag = "bad"
	setInProcessEnv(t, trendServer.URL, "reddit_session=x")
	trendCommand := newTrendsCommand()
	trendCommand.SetArgs([]string{"A"})
	if err := trendCommand.Execute(); err == nil {
		t.Fatal("trends invalid fields unexpectedly succeeded")
	}

	threadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[{"data":{"children":[{"kind":"t3","data":{"id":"abcde","subreddit":"Go","title":"Thread","num_comments":2,"created_utc":1704067200}}]}},{"data":{"children":[{"kind":"t1","data":{"id":"c1","score":1,"body":"one","created_utc":1704067200,"replies":""}},{"kind":"t1","data":{"id":"c2","score":2,"body":"two","created_utc":1704067201,"replies":""}}]}}]`)
	}))
	defer threadServer.Close()
	if _, err := executeInProcess(t, threadServer.URL, "reddit_session=x", "thread", "not a reference"); err == nil {
		t.Fatal("invalid thread reference unexpectedly succeeded")
	}
	if _, err := executeInProcess(t, threadServer.URL, "reddit_session=x", "thread", "abcde", "--top", "1"); err != nil {
		t.Fatalf("thread top truncation failed: %v", err)
	}
	noPostServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[{"data":{"children":[]}},{"data":{"children":[]}}]`)
	}))
	defer noPostServer.Close()
	if _, err := executeInProcess(t, noPostServer.URL, "reddit_session=x", "thread", "abcde"); err == nil {
		t.Fatal("thread without a post unexpectedly succeeded")
	}

	resolveServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/subreddits/search.json" {
			_, _ = fmt.Fprint(w, `{"data":{"dist":2,"children":[{"kind":"t5","data":{"display_name":"Go","title":"Go","subscribers":100}},{"kind":"t5","data":{"display_name":"Rust","title":"Rust","subscribers":200}}]}}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"data":{"dist":1,"children":[{"kind":"t3","data":{"id":"abcde","subreddit":"Other","title":"Other","created_utc":1704067200}}]}}`)
	}))
	defer resolveServer.Close()
	setInProcessEnv(t, resolveServer.URL, "reddit_session=x")
	formatFlag = "toon"
	fieldsFlag = ""
	first := newRootCommand()
	var firstOutput bytes.Buffer
	first.SetOut(&firstOutput)
	first.SetArgs([]string{"resolve-subreddits", "q", "--refresh", "--limit", "25"})
	if err := first.Execute(); err != nil {
		t.Fatal(err)
	}
	second := newRootCommand()
	var secondOutput bytes.Buffer
	second.SetOut(&secondOutput)
	second.SetArgs([]string{"resolve-subreddits", "q", "--limit", "1"})
	if err := second.Execute(); err != nil {
		t.Fatal(err)
	}
	formatFlag = "toon"
	third := newRootCommand()
	var thirdOutput bytes.Buffer
	third.SetOut(&thirdOutput)
	third.SetArgs([]string{"resolve-subreddits", "q", "--refresh", "--limit", "1"})
	if err := third.Execute(); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", "")
	if displayPath("relative") != "relative" {
		t.Fatal("displayPath fallback failed")
	}
}

func TestRunCLIReturnsStructuredExitCodes(t *testing.T) {
	setInProcessEnv(t, "", "")
	var output bytes.Buffer
	if code := runCLI([]string{"search", "q"}, &output); code != 1 || !strings.Contains(output.String(), "AUTH_REQUIRED") {
		t.Fatalf("runCLI auth = code %d output %q", code, output.String())
	}
	setInProcessEnv(t, "", "reddit_session=x")
	output.Reset()
	if code := runCLI([]string{"search", "q", "--limit", "99"}, &output); code != 2 || !strings.Contains(output.String(), "VALIDATION_ERROR") {
		t.Fatalf("runCLI validation = code %d output %q", code, output.String())
	}
	output.Reset()
	if code := runCLI([]string{"--bad"}, &output); code != 2 || !strings.Contains(output.String(), "VALIDATION_ERROR") {
		t.Fatalf("runCLI usage = code %d output %q", code, output.String())
	}
	output.Reset()
	if code := runCLI([]string{"url-extract", "abcde"}, &output); code != 0 || !strings.Contains(output.String(), "kind: post") {
		t.Fatalf("runCLI success = code %d output %q", code, output.String())
	}
}

func TestResolveExpandsTopicAndReturnsRelatedCandidates(t *testing.T) {
	var subredditQueries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/subreddits/search.json" {
			query := r.URL.Query().Get("q")
			subredditQueries = append(subredditQueries, query)
			switch query {
			case "homelab self-hosted":
				_, _ = fmt.Fprint(w, `{"data":{"dist":4,"children":[{"kind":"t5","data":{"display_name":"homelab","title":"Homelab","public_description":"Home lab discussion and help","subscribers":1000000}},{"kind":"t5","data":{"display_name":"selfhosted","title":"Self-hosted software","public_description":"Self-hosting discussion and assistance","subscribers":800000}},{"kind":"t5","data":{"display_name":"DataHoarder","title":"Data storage","public_description":"Storage and backup discussion","subscribers":900000}},{"kind":"t5","data":{"display_name":"homelabshitposting","title":"Homelab Shitposting","public_description":"Homelab memes","subscribers":500}}]}}`)
			case "homelab":
				_, _ = fmt.Fprint(w, `{"data":{"dist":3,"children":[{"kind":"t5","data":{"display_name":"HomeServer","title":"Home Server","public_description":"Home server hardware and software","subscribers":300000}},{"kind":"t5","data":{"display_name":"HomeNetworking","title":"Home networking help","public_description":"Community networking support","subscribers":570000}},{"kind":"t5","data":{"display_name":"homelabsales","title":"Homelab Sales","public_description":"Buy and sell hardware","subscribers":160000}}]}}`)
			case "self-hosted":
				_, _ = fmt.Fprint(w, `{"data":{"dist":1,"children":[{"kind":"t5","data":{"display_name":"HomeServer","title":"Home Server","public_description":"Practical home server operations","subscribers":300000}}]}}`)
			default:
				t.Errorf("unexpected subreddit query %q", query)
				_, _ = fmt.Fprint(w, `{"data":{"dist":0,"children":[]}}`)
			}
			return
		}
		if r.URL.Path == "/search.json" {
			_, _ = fmt.Fprint(w, `{"data":{"dist":2,"children":[{"kind":"t3","data":{"id":"id001","subreddit":"homelab","title":"homelab self-hosted advice","score":100,"num_comments":20,"created_utc":1704067200}},{"kind":"t3","data":{"id":"id002","subreddit":"selfhosted","title":"homelab self-hosted guide","score":90,"num_comments":10,"created_utc":1704067200}}]}}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	output, err := executeInProcess(t, server.URL, "reddit_session=x", "resolve-subreddits", "homelab self-hosted")
	if err != nil {
		t.Fatalf("resolve failed: %v\n%s", err, output)
	}
	for _, want := range []string{"HomeServer", "HomeNetworking", "DataHoarder", "Practical home server operations"} {
		if !strings.Contains(output, want) {
			t.Fatalf("resolve output omitted %q:\n%s", want, output)
		}
	}
	if strings.Count(output, "HomeServer") != 1 {
		t.Fatalf("duplicate candidates were not merged:\n%s", output)
	}
	wantQueries := []string{"homelab self-hosted", "homelab", "self-hosted"}
	if strings.Join(subredditQueries, "|") != strings.Join(wantQueries, "|") {
		t.Fatalf("subreddit queries = %#v, want %#v", subredditQueries, wantQueries)
	}
}

func TestResolveKeepsPrimaryResultsWhenExpansionFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/subreddits/search.json" && r.URL.Query().Get("q") == "alpha beta" {
			_, _ = fmt.Fprint(w, `{"data":{"dist":1,"children":[{"kind":"t5","data":{"display_name":"AlphaBeta","title":"Alpha Beta","subscribers":1000}}]}}`)
			return
		}
		if r.URL.Path == "/search.json" {
			_, _ = fmt.Fprint(w, `{"data":{"dist":0,"children":[]}}`)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `{"message":"optional expansion failed"}`)
	}))
	defer server.Close()

	output, err := executeInProcess(t, server.URL, "reddit_session=x", "resolve-subreddits", "alpha beta")
	if err != nil || !strings.Contains(output, "AlphaBeta") {
		t.Fatalf("optional expansion failure discarded primary result: err=%v\n%s", err, output)
	}
}

func TestInProcessResolveTopicCachePath(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/subreddits/search.json" {
			_, _ = fmt.Fprint(w, `{"data":{"dist":1,"children":[{"kind":"t5","data":{"display_name":"Alpha","title":"topic","subscribers":1}}]}}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"data":{"dist":2,"children":[{"kind":"t3","data":{"id":"id001","subreddit":"Beta","title":"topic","score":2,"created_utc":1704067200}},{"kind":"t3","data":{"id":"id002","subreddit":"Gamma","title":"topic","score":1,"created_utc":1704067200}}]}}`)
	}))
	defer server.Close()
	setInProcessEnv(t, server.URL, "reddit_session=x")
	formatFlag = "toon"
	fieldsFlag = ""
	fullFlag = false
	for index := 0; index < 3; index++ {
		root := newRootCommand()
		var output bytes.Buffer
		root.SetOut(&output)
		root.SetErr(&output)
		args := []string{"resolve-subreddits", "topic", "--limit", "1"}
		if index == 2 {
			args = append(args, "--refresh")
		}
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("resolve run %d: %v\n%s", index, err, output.String())
		}
		if !strings.Contains(output.String(), "count: 1 of 3 total") {
			t.Fatalf("resolve run %d lost total: %q", index, output.String())
		}
		wantRequests := []int32{2, 2, 4}[index]
		if got := atomic.LoadInt32(&requests); got != wantRequests {
			t.Fatalf("resolve run %d made %d requests, want %d", index, got, wantRequests)
		}
	}
}

func TestInProcessCommandsCoverValidationAndAuthPaths(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{"search auth", []string{"search", "q"}, "AUTH_REQUIRED"},
		{"search limit", []string{"search", "q", "--limit", "99"}, "VALIDATION_ERROR"},
		{"search zero limit", []string{"search", "q", "--limit", "0"}, "VALIDATION_ERROR"},
		{"search empty scopes", []string{"search", "q", "--subreddits", " , "}, "VALIDATION_ERROR"},
		{"search sort", []string{"search", "q", "--sort", "bad"}, "VALIDATION_ERROR"},
		{"search time", []string{"search", "q", "--time", "bad"}, "VALIDATION_ERROR"},
		{"search field", []string{"search", "q", "--fields", "bad"}, "VALIDATION_ERROR"},
		{"thread auth", []string{"thread", "abcde"}, "AUTH_REQUIRED"},
		{"thread limit", []string{"thread", "abcde", "--top", "99"}, "VALIDATION_ERROR"},
		{"thread zero top", []string{"thread", "abcde", "--top", "0"}, "VALIDATION_ERROR"},
		{"thread zero comment limit", []string{"thread", "abcde", "--comment-limit", "0"}, "VALIDATION_ERROR"},
		{"thread sort", []string{"thread", "abcde", "--sort", "bad"}, "VALIDATION_ERROR"},
		{"thread field", []string{"thread", "abcde", "--fields", "bad"}, "VALIDATION_ERROR"},
		{"subreddit auth", []string{"subreddits", "Go"}, "AUTH_REQUIRED"},
		{"subreddit limit", []string{"subreddits", "Go", "--limit", "99"}, "VALIDATION_ERROR"},
		{"subreddit zero limit", []string{"subreddits", "Go", "--limit", "0"}, "VALIDATION_ERROR"},
		{"trends auth", []string{"trends", "Go"}, "AUTH_REQUIRED"},
		{"trends limit", []string{"trends", "Go", "--limit", "99"}, "VALIDATION_ERROR"},
		{"trends zero limit", []string{"trends", "Go", "--limit", "0"}, "VALIDATION_ERROR"},
		{"trends empty scopes", []string{"trends", " , "}, "VALIDATION_ERROR"},
		{"trends listing", []string{"trends", "Go", "--listing", "bad"}, "VALIDATION_ERROR"},
		{"resolve auth", []string{"resolve-subreddits", "Go"}, "AUTH_REQUIRED"},
		{"resolve limit", []string{"resolve-subreddits", "Go", "--limit", "99"}, "VALIDATION_ERROR"},
		{"resolve zero limit", []string{"resolve-subreddits", "Go", "--limit", "0"}, "VALIDATION_ERROR"},
		{"pack auth", []string{"pack", "topic"}, "AUTH_REQUIRED"},
		{"pack intent", []string{"pack", "topic", "--intent", "bad"}, "VALIDATION_ERROR"},
		{"pack depth", []string{"pack", "topic", "--depth", "bad"}, "VALIDATION_ERROR"},
		{"pack sort", []string{"pack", "topic", "--sort", "bad"}, "VALIDATION_ERROR"},
		{"pack time", []string{"pack", "topic", "--time", "bad"}, "VALIDATION_ERROR"},
		{"pack empty scopes", []string{"pack", "topic", "--subreddits", " , "}, "VALIDATION_ERROR"},
		{"pack counts", []string{"pack", "topic", "--max-posts", "0", "--comments-per-post", "-1"}, "VALIDATION_ERROR"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cookie := ""
			if test.want == "VALIDATION_ERROR" {
				cookie = "reddit_session=x"
			}
			got, err := executeInProcess(t, "", cookie, test.args...)
			if err == nil {
				t.Fatalf("expected %s, output=%q", test.want, got)
			}
			var structured *cliError
			if !errors.As(err, &structured) || structured.Code != test.want {
				t.Fatalf("error=%T %#v, want code %s", err, err, test.want)
			}
		})
	}
}
