package config

import "strings"

func CookieNames(raw string) []string {
	present := map[string]bool{}
	for _, part := range strings.Split(raw, ";") {
		name, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if found && strings.TrimSpace(value) != "" && (name == "reddit_session" || name == "token_v2") {
			present[name] = true
		}
	}
	result := make([]string, 0, 2)
	for _, name := range []string{"reddit_session", "token_v2"} {
		if present[name] {
			result = append(result, name)
		}
	}
	return result
}

func HasSessionCookie(raw string) bool {
	return len(CookieNames(raw)) > 0
}
