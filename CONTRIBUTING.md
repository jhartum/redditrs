# Contributing to redditrs

Thanks for your interest in contributing! This document covers the essentials for
building, testing, and getting changes merged.

## About the project

`redditrs` is an agent-optimized CLI for Reddit research. It authenticates with a
web-session cookie (no API keys), reads `.json` endpoints through a bbolt-backed
cache, and emits structured output designed for agents. It is written in Go and
ships a ready-made agent skill alongside the CLI.

## Requirements & building

`redditrs` requires **Go 1.25.12+**.

```sh
# run the full test suite
go test ./...

# static analysis
go vet ./...

# build the optimized release binary (stripped, trimmed paths)
go build -trimpath -ldflags="-s -w" -o redditrs ./cmd/redditrs
```

All three commands are also described in the project `README.md`.

## Code style

- Format all Go code with **gofmt** before committing (`gofmt -l -w .`).
- Keep `go vet ./...` clean.
- Follow the existing package layout — don't invent new top-level structures
  without a clear justification.

## Repository layout

The code is organized to mirror two concerns:

- **`cmd/redditrs/`** — the CLI surface. Each command (`search`, `thread`,
  `pack`, `resolve-subreddits`, `subreddits`, `trends`, `url-extract`,
  `status`) is a cobra command that wires wiring to the internal packages and
  formats structured output.
- **`internal/`** — the implementation, kept inward-looking:
  - `cache/` — bbolt-backed HTTP response cache and cross-process request pacing.
  - `config/` — settings, env-var precedence, and cookie loading.
  - `model/` — the data types shared by the CLI and the Reddit client.
  - `rank/` — ranking/clustering helpers used to resolve topics to subreddits.
  - `reddit/` — the Reddit `.json` client (request building, caching, retries,
    rate-limit handling).

When you change or add a command, keep the wiring and formatting in
`cmd/redditrs` and put all logic in `internal/` so it stays unit-testable.

## Commit conventions

Use **Conventional Commits**. Recent types: `feat:`, `fix:`, `chore:`,
`docs:`, `ci:`, `refactor:`. Keep each commit focused on a single change.

```sh
git commit -m "docs: add contribution guide"
git commit -m "feat: add --sort flag to trends"
```

## Pull requests

- Keep PRs **small and focused**. A PR should address one thing. If a change
  grows, split it into several PRs.
- Write a short **description of what you changed and why** — reviewers need to
  know the motivation, not just the diff.
- Link to the issue the PR fixes or relates to, when one exists (`Fixes #123`).
- Note if the change is user-visible so it can be reflected in `CHANGELOG.md`.

### Important: CLI & skill are version-locked

`redditrs` is self-documenting for agents. If your PR changes **CLI commands,
flags, or intents**, you must also update `skills/redditrs/SKILL.md` — the skill
**version equals the CLI version**, so the shipped agent skill always matches the
binary. Mention in the PR description that the skill was kept in sync.

## Tests and network access

Tests **must not** hit the network and **must not** use real Reddit cookies.

- Use local test HTTP servers. `REDDITRS_BASE_URL` is supported for this: point
  it at a local server (created with `httptest`) instead of the real Reddit host.
  See `internal/reddit/client_test.go` for examples of cookie-authentication,
  rate-limiting, and request-pacing tests that run entirely offline.
- Set `DelayMS`, `CachePath` (in a temp dir), `CacheTTLMS`, and the cookie to
  dummy values in test `Settings` so tests are deterministic and hermetic.

## Reporting issues

For feature ideas, questions, and user-facing bugs, open a GitHub issue and use
the provided templates. For **security vulnerabilities**, follow `SECURITY.md`
and report them privately — do not create a public issue.

Have fun hacking!