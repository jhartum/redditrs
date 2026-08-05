# redditrs

[![CI](https://github.com/jhartum/redditrs/actions/workflows/ci.yml/badge.svg)](https://github.com/jhartum/redditrs/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.25.12+-blue?logo=go)](https://go.dev/dl/)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

`redditrs` is a fast Go CLI for Reddit search and research: search results, threads,
subreddit listings and trends read directly from Reddit using only the cookie of a
logged-in web session — **no API keys, no OAuth, no rate-limit quota**. Its output is
optimized for AI agents and for humans in the terminal.

## Features

- **Reddit search from the terminal** — `redditrs search "claude code vs opencode"`, with limits, JSON output and caching
- **Thread & subreddit research** — read a thread with top comments, discover and rank subreddits for a topic
- **Trends without the Reddit API** — hot/rising/new listings per subreddit
- **Cookie-based auth** — any logged-in web session works; the cookie is re-read before every request
- **Polite & cached** — built-in rate limiting and a persistent bbolt cache shared across CLI processes
- **Agent-ready** — self-documenting help hints in every output, ready-made skill for pi / Claude Code

## Installation

Install the latest release (macOS / Linux):

```sh
curl -fsSLO https://raw.githubusercontent.com/jhartum/redditrs/main/install.sh && sh install.sh
```

The installer verifies the release checksum, never uses `sudo`, and prints an
`export PATH=...` command if needed. Remove the downloaded script afterwards
with `rm install.sh`.

To inspect the script before running it:

```sh
curl -fsSLO https://raw.githubusercontent.com/jhartum/redditrs/main/install.sh
less install.sh
sh install.sh
```

### With Go 1.25.12+

```sh
go install github.com/jhartum/redditrs/cmd/redditrs@latest
```

`go install` writes the binary to `$GOBIN`, or to `$GOPATH/bin` when
`GOBIN` is unset. Make sure that directory is on your `PATH`:

```sh
GO_BIN="$(go env GOBIN)"
[ -n "$GO_BIN" ] || GO_BIN="$(go env GOPATH)/bin"
export PATH="$GO_BIN:$PATH"
```

Persist the final `export PATH=...` line in your shell profile (for example,
`~/.zshrc` on macOS).

From a checkout (release build — strips debug info and source paths):

```sh
go build -trimpath -ldflags="-s -w" -o redditrs ./cmd/redditrs
```

## Cookie

Step-by-step walkthrough (browser → cookie → config → verify): [`docs/cookie.md`](docs/cookie.md).

Pass the cookie of a logged-in Reddit web session via `REDDITRS_COOKIE` or a file:

```sh
export REDDITRS_COOKIE='reddit_session=...; token_v2=...'
```

Or create `~/.config/redditrs/config.json`:

```json
{"cookieFile":"/home/user/.config/redditrs/cookie.txt"}
```

Priority: `REDDITRS_COOKIE` → `REDDITRS_COOKIE_FILE` → `cookie` in JSON → `cookieFile` in JSON. The cookie is re-read before every HTTP request. Keep the config and cookie file with `0600` permissions and don't commit them.

The cookie is full access to the account. Using a cookie for programmatic `.json` reads may conflict with Reddit's terms and carries a risk of account blocking; use a throwaway account at your own risk.

## Commands

```sh
redditrs                                      # agent home view
redditrs status
redditrs search "claude code vs opencode" --limit 8
redditrs thread 1abc234 --top 10
redditrs pack "comfyui settings for low VRAM" --intent settings
redditrs resolve-subreddits "local LLM" --limit 15
redditrs subreddits "Claude Code"
redditrs trends LocalLLaMA --sort hot
redditrs url-extract https://www.reddit.com/r/ClaudeCode/comments/1abc234/
```

By default stdout uses TOON output optimized for agents; `--format json` is available for pipes. Long fields can be expanded with `--full`, extra list fields with `--fields`. `resolve-subreddits` expands multi-term topics internally and returns ranked titles/descriptions in one call. Errors are also structured in stdout; exit codes: `0` success, `1` runtime error, `2` usage error.

## Settings

| Variable | Default |
|---|---|
| `REDDITRS_CONFIG_PATH` | `~/.config/redditrs/config.json` |
| `REDDITRS_CACHE_DIR` | `~/.cache/redditrs` |
| `REDDITRS_CACHE_PATH` | `$REDDITRS_CACHE_DIR/reddit.db` |
| `REDDITRS_USER_AGENT` | `redditrs/1.0 personal-use (https://www.reddit.com/.json)` |
| `REDDITRS_DELAY_MS` | `1200` (minimum `250`, shared across CLI processes through the bbolt cache) |
| `REDDITRS_CACHE_TTL_MS` | `3600000` |
| `REDDITRS_THREAD_TTL_MS` | `21600000` |
| `REDDITRS_SUBREDDIT_TTL_MS` | `604800000` |
| `REDDITRS_TOPIC_TTL_MS` | `2592000000` |

`REDDITRS_BASE_URL` is supported for local test HTTP servers and defaults to `https://www.reddit.com`.

## Agent skill

`redditrs` is self-documenting for agents (`help[N]:` hints at the end of every output, `--help` on every command). For agents with skill support (pi, Claude Code), the repository ships a ready-made skill — `skills/redditrs/SKILL.md`: direct command selection by intent, evidence-aware follow-ups, and explicit completion criteria.

Install it via a symlink so the skill always matches the CLI version:

```sh
ln -s "$(pwd)/skills/redditrs" ~/.pi/agent/skills/redditrs    # pi
ln -s "$(pwd)/skills/redditrs" ~/.claude/skills/redditrs      # Claude Code
```

When CLI commands or intents change, update `skills/redditrs/SKILL.md` — the skill version equals the CLI version.

## Development

```sh
go test ./...
go vet ./...
go build -trimpath -ldflags="-s -w" -o redditrs ./cmd/redditrs
```

The full specification lives in [`docs/spec.md`](docs/spec.md).

## License

MIT License — see [LICENSE](LICENSE).

Copyright (c) 2026 Alexandr Brezgunov
