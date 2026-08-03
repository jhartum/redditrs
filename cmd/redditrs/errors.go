package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jhartum/redditrs/internal/reddit"
)

type cliError struct {
	Message string
	Code    string
	Help    []string
}

func (e *cliError) Error() string {
	return e.Message
}

func (e *cliError) exitCode() int {
	if e.Code == "VALIDATION_ERROR" {
		return 2
	}
	return 1
}

func renderCLIError(err *cliError) string {
	if formatFlag == "json" {
		return marshalJSON(struct {
			Error string   `json:"error"`
			Code  string   `json:"code"`
			Help  []string `json:"help,omitempty"`
		}{Error: err.Message, Code: err.Code, Help: err.Help})
	}
	lines := []string{
		"error: " + toonString(err.Message),
		"code: " + err.Code,
	}
	if len(err.Help) > 0 {
		lines = append(lines, "help["+strconv.Itoa(len(err.Help))+"]:")
		for _, line := range err.Help {
			lines = append(lines, "  "+safeHelpLine(line))
		}
	}
	return strings.Join(lines, "\n")
}

func mapRedditError(err error) *cliError {
	if httpErr, ok := err.(*reddit.HTTPError); ok {
		switch httpErr.StatusCode {
		case 403:
			return &cliError{Message: "Reddit returned 403 — the session cookie is likely expired", Code: "FORBIDDEN", Help: []string{"Refresh the cookie in the config and retry"}}
		case 429:
			return &cliError{Message: fmt.Sprintf("Reddit is cooling down for %s after a rate-limit response", formatCooldown(httpErr.RetryAfter)), Code: "RATE_LIMITED", Help: []string{"Wait before retrying, or raise REDDITRS_DELAY_MS to slow down request rate"}}
		case 404:
			return &cliError{Message: "Reddit resource not found", Code: "NOT_FOUND"}
		}
	}
	return &cliError{Message: err.Error(), Code: "UNKNOWN"}
}

func usageError(err error, command string) *cliError {
	message := err.Error()
	help := []string{"Run `redditrs " + command + " --help` for valid flags and examples"}
	if strings.HasPrefix(message, "unknown flag:") {
		flag := strings.TrimSpace(strings.TrimPrefix(message, "unknown flag:"))
		message = fmt.Sprintf("unknown flag %s for '%s'", flag, command)
		if command == "search" {
			help = []string{"valid flags for 'search': --limit, --sort, --time, --subreddits, --format, --fields, --full (--help always allowed)"}
		}
	}
	return &cliError{Message: message, Code: "VALIDATION_ERROR", Help: help}
}
