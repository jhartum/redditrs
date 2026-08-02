package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"REDDITRS_COOKIE", "REDDITRS_COOKIE_FILE", "REDDITRS_CONFIG_PATH",
		"REDDITRS_CACHE_DIR", "REDDITRS_CACHE_PATH", "REDDITRS_DELAY_MS",
		"REDDITRS_CACHE_TTL_MS", "REDDITRS_THREAD_TTL_MS", "REDDITRS_SUBREDDIT_TTL_MS",
		"REDDITRS_TOPIC_TTL_MS", "REDDITRS_BASE_URL", "REDDITRS_USER_AGENT",
	} {
		t.Setenv(name, "")
	}
}

func TestLoadUsesDefaults(t *testing.T) {
	clearConfigEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	settings := Load()
	if settings.Cookie != "" {
		t.Fatalf("Cookie = %q, want empty", settings.Cookie)
	}
	if settings.ConfigPath != filepath.Join(home, ".config", "redditrs", "config.json") {
		t.Fatalf("ConfigPath = %q", settings.ConfigPath)
	}
	if settings.CachePath != filepath.Join(home, ".cache", "redditrs", "reddit.db") {
		t.Fatalf("CachePath = %q", settings.CachePath)
	}
	if settings.DelayMS != 1200 || settings.CacheTTLMS != 3_600_000 || settings.ThreadTTLMS != 21_600_000 || settings.SubredditTTLMS != 604_800_000 || settings.TopicTTLMS != 2_592_000_000 {
		t.Fatalf("unexpected defaults: %#v", settings)
	}
	if settings.BaseURL != "https://www.reddit.com" {
		t.Fatalf("BaseURL = %q", settings.BaseURL)
	}
	if settings.UserAgent != "redditrs/0.1 personal-use (https://www.reddit.com/.json)" {
		t.Fatalf("UserAgent = %q", settings.UserAgent)
	}
}

func TestLoadUsesTildeWhenHomeIsUnavailable(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("HOME", "")

	settings := Load()
	if settings.ConfigPath != filepath.Join("~", ".config", "redditrs", "config.json") {
		t.Fatalf("ConfigPath = %q, want tilde fallback", settings.ConfigPath)
	}
	if settings.CachePath != filepath.Join("~", ".cache", "redditrs", "reddit.db") {
		t.Fatalf("CachePath = %q, want tilde fallback", settings.CachePath)
	}
}

func TestLoadUsesEnvironmentOverridesAndDurationRules(t *testing.T) {
	clearConfigEnv(t)
	configPath := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("REDDITRS_CONFIG_PATH", configPath)
	t.Setenv("REDDITRS_CACHE_DIR", "/tmp/redditrs-test-cache")
	t.Setenv("REDDITRS_CACHE_PATH", "/tmp/redditrs-test.db")
	t.Setenv("REDDITRS_COOKIE", "env-cookie")
	t.Setenv("REDDITRS_COOKIE_FILE", filepath.Join(t.TempDir(), "ignored-cookie"))
	t.Setenv("REDDITRS_DELAY_MS", "500")
	t.Setenv("REDDITRS_CACHE_TTL_MS", "bad")
	t.Setenv("REDDITRS_THREAD_TTL_MS", "29999")
	t.Setenv("REDDITRS_SUBREDDIT_TTL_MS", "30000")
	t.Setenv("REDDITRS_TOPIC_TTL_MS", "40000")
	t.Setenv("REDDITRS_BASE_URL", "http://localhost:1234")
	t.Setenv("REDDITRS_USER_AGENT", "test-agent")

	settings := Load()
	if settings.Cookie != "env-cookie" || settings.ConfigPath != configPath || settings.CachePath != "/tmp/redditrs-test.db" || settings.DelayMS != 500 {
		t.Fatalf("environment overrides not applied: %#v", settings)
	}
	if settings.CacheTTLMS != 3_600_000 || settings.ThreadTTLMS != 30_000 || settings.SubredditTTLMS != 30_000 || settings.TopicTTLMS != 40_000 {
		t.Fatalf("duration rules not applied: %#v", settings)
	}
	if settings.BaseURL != "http://localhost:1234" || settings.UserAgent != "test-agent" {
		t.Fatalf("network overrides not applied: %#v", settings)
	}
}

func TestLoadClampsShortDelayAndIgnoresInvalidDelay(t *testing.T) {
	for _, test := range []struct {
		value string
		want  int
	}{
		{value: "249", want: 250},
		{value: "0", want: 250},
		{value: "-1", want: 250},
		{value: "invalid", want: 1200},
	} {
		t.Run(test.value, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv("REDDITRS_DELAY_MS", test.value)
			if got := Load().DelayMS; got != test.want {
				t.Fatalf("DelayMS for %q = %d, want %d", test.value, got, test.want)
			}
		})
	}
}

func TestCookieNamesAndSessionDetection(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
		has  bool
	}{
		{name: "empty", raw: "", want: nil, has: false},
		{name: "missing value", raw: "reddit_session", want: nil, has: false},
		{name: "empty values", raw: "reddit_session=; token_v2=  ", want: nil, has: false},
		{name: "session", raw: "reddit_session=value", want: []string{"reddit_session"}, has: true},
		{name: "token", raw: "token_v2=value", want: []string{"token_v2"}, has: true},
		{name: "both and unknown", raw: "unknown=x; token_v2=y; reddit_session=z", want: []string{"reddit_session", "token_v2"}, has: true},
		{name: "whitespace and malformed", raw: " ; reddit_session = value; =ignored", want: nil, has: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := CookieNames(test.raw)
			if len(got) != len(test.want) {
				t.Fatalf("CookieNames(%q) = %#v, want %#v", test.raw, got, test.want)
			}
			for index := range got {
				if got[index] != test.want[index] {
					t.Fatalf("CookieNames(%q) = %#v, want %#v", test.raw, got, test.want)
				}
			}
			if got := HasSessionCookie(test.raw); got != test.has {
				t.Fatalf("HasSessionCookie(%q) = %v, want %v", test.raw, got, test.has)
			}
		})
	}
}

func TestLoadCookieSourcePriority(t *testing.T) {
	t.Run("environment cookie wins", func(t *testing.T) {
		clearConfigEnv(t)
		dir := t.TempDir()
		envFile := filepath.Join(dir, "env-cookie")
		configPath := filepath.Join(dir, "config.json")
		if err := os.WriteFile(envFile, []byte("file-cookie\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(configPath, []byte(`{"cookie":"json-cookie"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("REDDITRS_CONFIG_PATH", configPath)
		t.Setenv("REDDITRS_COOKIE_FILE", envFile)
		t.Setenv("REDDITRS_COOKIE", "env-cookie")
		if got := Load().Cookie; got != "env-cookie" {
			t.Fatalf("Cookie = %q, want env-cookie", got)
		}
	})

	t.Run("environment file wins over json", func(t *testing.T) {
		clearConfigEnv(t)
		dir := t.TempDir()
		envFile := filepath.Join(dir, "env-cookie")
		configPath := filepath.Join(dir, "config.json")
		if err := os.WriteFile(envFile, []byte(" file-cookie \n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(configPath, []byte(`{"cookie":"json-cookie"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("REDDITRS_CONFIG_PATH", configPath)
		t.Setenv("REDDITRS_COOKIE_FILE", envFile)
		if got := Load().Cookie; got != "file-cookie" {
			t.Fatalf("Cookie = %q, want trimmed file-cookie", got)
		}
	})

	t.Run("json cookie wins over json file", func(t *testing.T) {
		clearConfigEnv(t)
		dir := t.TempDir()
		cookieFile := filepath.Join(dir, "cookie")
		configPath := filepath.Join(dir, "config.json")
		if err := os.WriteFile(cookieFile, []byte("cookie-file\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		config := map[string]string{"cookie": "json-cookie", "cookieFile": cookieFile}
		raw, _ := json.Marshal(config)
		if err := os.WriteFile(configPath, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("REDDITRS_CONFIG_PATH", configPath)
		if got := Load().Cookie; got != "json-cookie" {
			t.Fatalf("Cookie = %q, want json-cookie", got)
		}
	})

	t.Run("json file is trimmed", func(t *testing.T) {
		clearConfigEnv(t)
		dir := t.TempDir()
		cookieFile := filepath.Join(dir, "cookie")
		configPath := filepath.Join(dir, "config.json")
		if err := os.WriteFile(cookieFile, []byte(" cookie-file \n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(configPath, []byte(`{"cookieFile":"`+cookieFile+`"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("REDDITRS_CONFIG_PATH", configPath)
		if got := Load().Cookie; got != "cookie-file" {
			t.Fatalf("Cookie = %q, want trimmed cookie-file", got)
		}
	})

	t.Run("invalid json is ignored", func(t *testing.T) {
		clearConfigEnv(t)
		configPath := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(configPath, []byte("not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("REDDITRS_CONFIG_PATH", configPath)
		if got := Load().Cookie; got != "" {
			t.Fatalf("Cookie = %q, want empty", got)
		}
	})

	t.Run("missing files are ignored", func(t *testing.T) {
		clearConfigEnv(t)
		t.Setenv("REDDITRS_COOKIE_FILE", filepath.Join(t.TempDir(), "missing"))
		t.Setenv("REDDITRS_CONFIG_PATH", filepath.Join(t.TempDir(), "missing-config"))
		if got := Load().Cookie; got != "" {
			t.Fatalf("Cookie = %q, want empty", got)
		}
	})
}
