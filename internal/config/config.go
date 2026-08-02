package config

import (
	"cmp"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const defaultDelayMS = 1200

type Settings struct {
	Cookie         string
	ConfigPath     string
	CachePath      string
	DelayMS        int
	CacheTTLMS     int
	ThreadTTLMS    int
	SubredditTTLMS int
	TopicTTLMS     int
	BaseURL        string
	UserAgent      string
}

func envInt(name string, fallback, minimum int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil {
		return fallback
	}
	return max(value, minimum)
}

func readCookieFile(path string) string {
	if path == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func Load() Settings {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "~"
	}

	configPath := cmp.Or(os.Getenv("REDDITRS_CONFIG_PATH"), filepath.Join(home, ".config", "redditrs", "config.json"))
	cacheDir := cmp.Or(os.Getenv("REDDITRS_CACHE_DIR"), filepath.Join(home, ".cache", "redditrs"))
	cachePath := cmp.Or(os.Getenv("REDDITRS_CACHE_PATH"), filepath.Join(cacheDir, "reddit.db"))

	delayMS := envInt("REDDITRS_DELAY_MS", defaultDelayMS, 250)
	cacheTTLMS := envInt("REDDITRS_CACHE_TTL_MS", 3_600_000, 30_000)
	threadTTLMS := envInt("REDDITRS_THREAD_TTL_MS", 21_600_000, 30_000)
	subredditTTLMS := envInt("REDDITRS_SUBREDDIT_TTL_MS", 604_800_000, 30_000)
	topicTTLMS := envInt("REDDITRS_TOPIC_TTL_MS", 2_592_000_000, 30_000)

	cookie := os.Getenv("REDDITRS_COOKIE")
	if cookie == "" {
		cookie = readCookieFile(os.Getenv("REDDITRS_COOKIE_FILE"))
	}
	if cookie == "" {
		if raw, err := os.ReadFile(configPath); err == nil {
			var file struct {
				Cookie     string `json:"cookie"`
				CookieFile string `json:"cookieFile"`
			}
			if json.Unmarshal(raw, &file) == nil {
				cookie = file.Cookie
				if cookie == "" {
					cookie = readCookieFile(file.CookieFile)
				}
			}
		}
	}

	baseURL := cmp.Or(os.Getenv("REDDITRS_BASE_URL"), "https://www.reddit.com")
	userAgent := cmp.Or(os.Getenv("REDDITRS_USER_AGENT"), "redditrs/0.1 personal-use (https://www.reddit.com/.json)")

	return Settings{
		Cookie:         cookie,
		ConfigPath:     configPath,
		CachePath:      cachePath,
		DelayMS:        delayMS,
		CacheTTLMS:     cacheTTLMS,
		ThreadTTLMS:    threadTTLMS,
		SubredditTTLMS: subredditTTLMS,
		TopicTTLMS:     topicTTLMS,
		BaseURL:        baseURL,
		UserAgent:      userAgent,
	}
}
