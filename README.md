# redditrs

[![CI](https://github.com/jhartum/redditrs/actions/workflows/ci.yml/badge.svg)](https://github.com/jhartum/redditrs/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.23+-blue?logo=go)](https://go.dev/dl/)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

An agent-optimized CLI for Reddit research using a web-session cookie, no API keys.

## Installation

Requires Go 1.23+:

```sh
go install ./cmd/redditrs
```

From a published release:

```sh
go install github.com/jhartum/redditrs@latest
```

From a checkout (release build — strips debug info and source paths):

```sh
make build          # redditrs binary, ~7 MB
# or: go build -trimpath -ldflags="-s -w" -o redditrs ./cmd/redditrs
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
| `REDDITRS_USER_AGENT` | `redditrs/0.1 personal-use (https://www.reddit.com/.json)` |
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
make test   # or: go test ./...
make vet    # or: go vet ./...
make build  # optimized release binary (stripped, trimmed paths)
```

The full specification lives in [`docs/spec.md`](docs/spec.md).

## License

MIT License — see [LICENSE](LICENSE).

Copyright (c) 2026 Alexandr Brezgunov
