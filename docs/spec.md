# `redditrs` Specification — v0.1 (draft)

> An agent-optimized CLI for Reddit research using a cookie, no API keys.
> Status: specification of the implemented CLI.
> Target output format version: **`toon-spec: 4.1`** (TOON Working Draft of 2026-07-26; watch the CHANGELOG until finalization).

---

## 1. Purpose

`redditrs` is a single-binary CLI for Reddit research: post search, thread reading, evidence-pack collection, subreddit search, trends. Data access is through Reddit's public `.json` endpoints, authenticated with a **web-session cookie** (browser), not an API key.

The output audience is **agents** (pi, other LLM agents, scripts), not only humans. Output is optimized for tokens and unambiguity for agents in **TOON** format.

The project is local (`~/code/redditrs`); publication on GitHub comes later, by the user.

## 2. Principles (what makes the tool "like pi-reddit-research")

1. **Cookie instead of API key.** The cookie is the Cookie-header string of a logged-in web session (`reddit_session=…; token_v2=…`). The config is re-read **before every request** — cookie updates without restart.
2. **Moderate pace.** At least 1200 ms between requests, strictly sequential across clients and CLI processes that share the bbolt cache.
3. **Block resistance.** 403/429 → cooldown (`Retry-After`, 300 s fallback) with stale-cache fallback; 403 → first re-read the cookie and retry if it changed.
4. **Cache.** bbolt (single file, ACID) with per-data-type TTL: search 1 h, threads 6 h, subreddits 7 d, topics 30 d.
5. **Agent-ergonomic output.** TOON v4.1 by default, agent output conventions (see §7): minimal schemas, pre-computed aggregates, explicit empty states, structured errors in stdout, exit 0/1/2, `help[N]:` hints, `--full`/`--fields`, content-first home.
6. **Researcher heuristics.** Relevance ranking, evidence clusters, intents, depths — a port from pi-reddit-research (§9).

## 3. Runtime and dependencies

- Go ≥ 1.23, a single `redditrs` binary.
- External dependencies: `cobra` (CLI), `go.etcd.io/bbolt` (cache, pure Go, no CGO). Nothing else.
- Module path: `github.com/<user>/redditrs` (fixed at publication; locally — any working name).
- The public output surface is runtime-independent: stdout carries only structured output (TOON or JSON), stderr only diagnostics/progress.

## 4. Configuration and cookies

### 4.1 Cookie sources (priority, highest → lowest)

1. `REDDITRS_COOKIE` env — the Cookie-header string.
2. `REDDITRS_COOKIE_FILE` env — path to a file whose contents are the Cookie-header string (`.trim()`).
3. `~/.config/redditrs/config.json`, field `cookie`.
4. `~/.config/redditrs/config.json`, field `cookieFile` — path to the cookie file.

The config path is overridden by `REDDITRS_CONFIG_PATH` (default `~/.config/redditrs/config.json`).

**Re-reading:** before every outgoing request the cookie is loaded anew (env or file/config read from disk). Updating the cookie in the config works without a restart.

**Config format:**
```json
{ "cookie": "reddit_session=…; token_v2=…" }
```
or
```json
{ "cookieFile": "/home/user/.config/redditrs/cookie.txt" }
```
`cookieFile` is preferred: the secret lives apart from the config.

### 4.2 Other environment variables

| Variable | Default | Description |
|---|---|---|
| `REDDITRS_CACHE_DIR` | `~/.cache/redditrs` | Cache directory |
| `REDDITRS_CACHE_PATH` | `$REDDITRS_CACHE_DIR/reddit.db` | Cache file path |
| `REDDITRS_USER_AGENT` | `redditrs/0.1 personal-use (https://www.reddit.com/.json)` | User-Agent (must be descriptive, honest — a Reddit requirement) |
| `REDDITRS_DELAY_MS` | `1200` (floor 250) | Minimum pause between requests |
| `REDDITRS_CACHE_TTL_MS` | `3_600_000` (floor 30_000) | TTL for search/other requests |
| `REDDITRS_THREAD_TTL_MS` | `21_600_000` (floor 30_000) | TTL for threads |
| `REDDITRS_SUBREDDIT_TTL_MS` | `604_800_000` (floor 30_000) | TTL for subreddits |
| `REDDITRS_TOPIC_TTL_MS` | `2_592_000_000` (floor 30_000) | TTL for "topic → subreddits" |
| `REDDITRS_CONFIG_PATH` | `~/.config/redditrs/config.json` | Path to the JSON config |

### 4.3 Obtaining the cookie

The step-by-step user guide (two methods: full Cookie header via the Network tab, or individual cookies via the Application tab; cleanup; config; verification) lives in [`docs/cookie.md`](docs/cookie.md). This section is the canonical requirements summary:

- The session must be logged in. `token_v2` is a JWT; anonymous `sub:"loid"` **does not work**, `sub:"user"` is required (or simply the presence of `reddit_session`). `loid`/`edgebucket`/`over18` are not required.
- Strip cookies with JSON values (`g_state`, `eu_cookie`, `seeker_session`, etc. — they break the JSON config); keep at least `reddit_session=…; token_v2=…`. A name without `=` and an empty value do not count as a configured session.
- Verification: `curl -s -H "cookie: …" -A "redditrs/0.1 (research)" 'https://www.reddit.com/r/popular/hot.json?limit=5&raw_json=1'` — must return JSON, not HTML; `redditrs status` shows `cookie: set`.
- Lifetime: unpublished; in practice "a few days to a week" (the reference refreshes with a 7-day TTL). The sign of expiry is a 403.

### 4.4 Security

- The cookie is full access to the account. Treat it like a password.
- `chmod 600` on the config and cookie file; don't commit them (`.gitignore`).
- Don't pass the cookie to third-party services.

### 4.5 Disclaimer (in the README and in `status` when no cookie is set)

Using a session cookie to read `.json` is a gray zone of Reddit's ToS (the Data API Terms require OAuth for "programmatic access", but the 2026 wiki and the r/modnews announcement of 28.05.2026 explicitly allow "logged-in and authenticated access"). Personal research use at a moderate pace is common practice (references: pi-reddit-research, rdt-cli, yt-dlp). There is a risk of account blocking; use an account you don't mind risking.

## 5. Network layer

### 5.1 Endpoints

Base URL: `https://www.reddit.com`. All requests include `raw_json=1` (the parameter is added to the URL; query-parameter order is normalized — this is the cache key).

| Purpose | Path |
|---|---|
| Search all subs | `/search.json?q=<q>&sort=<s>&t=<t>&limit=<n>` |
| Search within subs | `/r/<sub>/search.json?q=<q>&restrict_sr=1&sort=<s>&t=<t>&limit=<n>` |
| Thread | `/comments/<id>.json?limit=<n>&sort=<s>` (`relevance`/`comments` → `confidence`) |
| Trends | `/r/<sub>/{hot,top,new}.json?limit=<n>[&t=<t>]` |
| Subreddit search | `/subreddits/search.json?q=<q>&limit=<n>` |

Headers: `user-agent` (see §4.2), `accept: application/json,text/plain,*/*`, `accept-language: en-US,en;q=0.9`, `cookie` (if set). `redirect: follow`.

### 5.2 Pace and cooldown

- Request serialization (queue, one at a time); pause: `lastRequestAt + REDDITRS_DELAY_MS`.
- **403**: re-read the cookie; if it changed → "cookie refreshed — retry" error **without cooldown** (the agent retries immediately). If unchanged/no cookie → cooldown (below) + stale fallback + a meaningful error:
  - no cookie: `Reddit blocks unauthenticated .json access since June 2026 — set a cookie in ~/.config/redditrs/config.json or REDDITRS_COOKIE`;
  - cookie present: `the session cookie is likely expired — refresh it in the config and retry`.
- **429**: cooldown from `Retry-After` (seconds), 300 s fallback; stale fallback.
- During cooldown: stale cache first by URL; if absent — error `Reddit is cooling down for Ns after a rate-limit/block response`.
- Other HTTP errors: response body (truncated) goes into the error + stale fallback.
- Error records (status, message) are cached in bbolt — blocks are "remembered" between runs.

### 5.3 Parsing

- Listing: `data.children[]` with `kind: t3` (posts) / `t5` (subreddits); a thread is an array `[listing, listing]` (post + `t1` comments).
- Comments: recursive flattening of `data.replies` with depth; skip `[deleted]`/`[removed]`, empty ones.
- The full post/comment field list is as in the model §9.1.

## 6. Cache

bbolt (single-file, ACID), a single DB `$REDDITRS_CACHE_PATH` (default `reddit.db`). bbolt takes an exclusive file lock, so `internal/cache` is used open → read/write → close around every operation and is never held across network I/O or sleeps. Buckets:

- `requests(key → status, saved_at, expires_at, raw, error)` — compact binary request records keyed by normalized URL; topic resolution stores its sorted candidate slice as one JSON payload under a versioned `topic:` key, so replacement is atomic;
- `runtime_state(key → value)` — atomic cross-process request-slot reservation (read-modify-write inside one bbolt transaction; writers are serialized by the file lock).

TTL: `requests` — by request type (search 1 h / thread 6 h / subreddit 7 d), topic snapshots — 30 d. Stale URL reads are allowed only during cooldown/errors.

## 7. Output format (TOON, agent-optimized)

### 7.1 TOON v4.1 — specialized renderers

The CLI emits only fixed command schemas, so TOON serialization is implemented as narrow `render*` functions in `cmd/redditrs`, without a generic reflection encoder. The format is verified by CLI golden and integration tests.

Mandatory TOON renderer behavior (checklist of spec §13.1):
- UTF-8, LF; 2-space indent; no trailing spaces and no final `\n`; no comments;
- length declarations `[N]` always; schema headers `key[N]{fields}:`, keyed `key[N:]{fields}:`;
- 4 array forms: inline (primitives), tabular (homogeneous objects), list (the rest), keyed tabular;
- objects: `key: value` (one space); nested/empty objects — `key:` + contents at +1 indent; empty array — `key: []`;
- strings: unquoted, except §7.2 conditions (empty; leading/trailing spaces; `true`/`false`/`null`; number-like; contains `:` `"` `\` `[` `]` `{` `}`; control characters; active/document separator; leading `-`/`#`);
- only 6 escapes: `\\ \" \n \r \t \uXXXX`; keys per §7.3;
- numbers — canonical decimal form (§2); `NaN`/`±Infinity` → `null`;
- key order — as in the source.

### 7.2 Agent conventions (in the `cmd/redditrs` renderers)

- **`help[N]:` blocks** — a non-strict-TOON convention, emitted manually after the data: header `help[N]:` + lines at indent 2, without `- ` markers.
- **Truncation** — one quoted string with `\n` escape: `…\n... (truncated, N chars total - use --full to see complete <field>)`. Limits: posts/comments ~500 chars; `--full` lifts truncation.
- **Aggregates** — `count: N of M total`, only when the page was cut by the limit (honest total; no total — the line is omitted: "no total beats a wrong one").
- **Empty states** — an explicit line `posts: 0 posts found for "<query>" in the last <time>` (exit 0) + `help[1]:` with window/query expansion.
- **Errors** — on stdout and in the chosen format: for TOON — `error: <message>` + `code: <code>` + `help[N]:`; for `--format json` — an object `{"error":"…","code":"…","help":[…]}`.
- **Content-first home** — `redditrs` without arguments: `bin:` (path with `~` for home) + `description:` (one sentence) + state (cookie/cache) + `help[N]:`.
- **`--help`** — a consistent compact reference on every subcommand: flags with defaults, required arguments, 2–3 examples.

### 7.3 Global flags

| Flag | Action |
|---|---|
| `--format toon\|json` | default `toon`; `json` — full JSON output of the same data (for pipes) |
| `--fields a,b,c` | extend/narrow list fields (beyond the minimal schema) |
| `--full` | lift truncation of long fields |
| `--help` | compact reference (always allowed) |
| `--limit`, `--sort`, `--time`, etc. | per-command, see §8 |

### 7.4 Exit codes and error codes

| Exit | Meaning |
|---|---|
| 0 | success (including no-op and empty results) |
| 1 | runtime error (network, cookie, rate limit) |
| 2 | usage error (unknown flag/subcommand, invalid value) |

Error codes (field `code:`): `AUTH_REQUIRED`, `FORBIDDEN`, `RATE_LIMITED`, `NOT_FOUND`, `VALIDATION_ERROR`, `UNKNOWN`. Mapping: `VALIDATION_ERROR` → exit 2, the rest → exit 1.

Unknown flag — a "loud" refusal: `error: unknown flag --stat for 'search'` + `help[1]: valid flags for 'search': … (--help always allowed)`.

## 8. Commands

The schemas below are canonical for the CLI.

### 8.1 Root (home view) — `redditrs`

```
bin: ~/.local/bin/redditrs
description: Reddit research for agents — search, threads, evidence packs, trends (cookie auth, no API keys)
cookie: set (reddit_session, token_v2)
cache: 231 requests in ~/.cache/redditrs/reddit.db
help[4]:
  Run `redditrs search "claude code vs opencode"` to find posts
  Run `redditrs pack "comfyui settings for low VRAM" --intent settings` for an evidence pack
  Run `redditrs trends LocalLLaMA` to see what's hot
  Run `redditrs status` for config, cache and cooldown details
```
Fields: `bin:`, `description:`, `cookie: set|not set`, `cache: N requests in <path>`, `help[N]:`. Without a cookie — `cookie: not set` + `help[]` with instructions.

### 8.2 `redditrs status`

```
status:
  version: 0.1.0
  cookie: set (reddit_session, token_v2)
  config: ~/.config/redditrs/config.json
  cache: ~/.cache/redditrs/reddit.db (231 requests, 12.4 MB)
  cooldown: 0s
  delay_ms: 1200
help[1]:
  To verify live Reddit access, run `redditrs search "test" --limit 1`
```
Cookie check: presence of `reddit_session` and/or `token_v2`; `token_v2` — decode the JWT payload and show `sub` (`user` = ok, `loid` = anonymous, doesn't work).

### 8.3 `redditrs search "<query>" [--subreddits a,b] [--sort relevance|hot|top|new|comments] [--time hour|day|week|month|year|all] [--limit 8|1..25]`

Post search, no comments. Schema:

```
posts[5]{id,subreddit,title,score,age}:
  1abc234,ClaudeCode,Claude Code vs OpenCode — which do you use daily?,142,3d ago
  def5678,OpenCodeCLI,OpenCode is eating Claude Code's lunch,89,1w ago
count: 5 of 214 total          # only if the page was cut by the limit
help[2]:
  If comments are needed, run `redditrs thread 1abc234 --top 10`
  If results are too broad, narrow with `redditrs search "claude code vs opencode" --subreddits ClaudeCode`
```

- `id` is mandatory in the schema (follow-up `thread <id>`); `--fields` supports: `id,subreddit,title,score,age,author,url,permalink,num_comments,flair,created_utc,selftext`.
- `selftext` follows normal truncation rules; use it with `num_comments` to choose a substantive discussion before opening a thread.
- Reddit query operators can be passed through, e.g. `"NixOS AND Arch self:yes"` for text comparison posts.
- `age` is derived ("3d ago"), a pre-computed aggregate.
- Ranking: by `rankScore` (§9.2), default limit 8, maximum 25.

### 8.4 `redditrs thread <url_or_id> [--sort top|new|controversial|confidence] [--comment-limit 50|1..200] [--top 10|1..40]`

One thread + top comments. Accepts a URL, permalink, `t3_`-id, bare id. Schema:

```
post:
  id: 1abc234
  subreddit: ClaudeCode
  title: Claude Code vs OpenCode — which do you use daily?
  author: u/alex
  score: 142
  comments: 87
  age: 3d ago
  url: https://www.reddit.com/r/ClaudeCode/comments/1abc234/
  selftext: "I've been using Claude Code for 3 months, OpenCode for 2 weeks. Claude Code is better at\n... (truncated, 1843 chars total - use --full to see complete selftext)"
comments[10]{author,score,body}:
  u/dev42,34,I use OpenCode daily but keep Claude Code for big refactors — its agent mode is just faster
  u/agentfan,28,"Claude Code wins on long context, OpenCode loses track after ~50 edits"
count: 10 of 87 total
help[1]:
  If the truncated text is needed, run `redditrs thread 1abc234 --full`
```

- Thread not found → `error: thread <id> not found` + `code: NOT_FOUND` (exit 1).
- `--sort` defaults to `top`; `--comment-limit` — how many comments are requested from Reddit, `--top` — how many are shown.

### 8.5 `redditrs pack "<topic>" [--intent opinions|bugs|fixes|compare|settings|alternatives|trends|guides|hardware|general] [--depth quick|normal|deep] [--time …] [--sort …] [--subreddits a,b] [--limit N] [--comments-per-post N]`

The main research tool: search + top comments + evidence clusters.

**`--depth quick|normal` — variant A (flat):**

```
pack:
  topic: comfyui settings for low VRAM
  intent: settings
  subreddits: ComfyUI (4), StableDiffusion (2), LocalLLaMA (1)
evidence[3]{cluster,count,hint}:
  settings,5,VRAM-related configs in 4 posts
  hardware,3,GPU specs in top comments
  complaints,2,OOM errors reported by low-VRAM users
posts[4]{id,subreddit,title,score,age}:
  abc111,ComfyUI,Best settings for 8GB VRAM?,312,1w ago
  abc222,ComfyUI,Low VRAM guide — VAE tiling + fp8,204,2w ago
help[1]:
  If comment quotes are needed, run `redditrs pack "comfyui settings for low VRAM" --intent settings --depth deep`
```

**`--depth deep` — variant B (nested):**

```
pack:
  topic: comfyui settings for low VRAM
  intent: settings
  subreddits: ComfyUI (4), StableDiffusion (2), LocalLLaMA (1)
posts[2]{id,subreddit,title,score,age}:
  abc111,ComfyUI,Best settings for 8GB VRAM?,312,1w ago
  abc222,ComfyUI,Low VRAM guide — VAE tiling + fp8,204,2w ago
comments[4]{post_id,author,score,body}:
  abc111,u/vramninja,41,"--lowvram + VAE tiling let me run SDXL on 6GB"
  abc111,u/another,18,"fp8 weights: -2GB VRAM, barely any quality loss"
clusters[3]{cluster,count,hint}:
  settings,5,VRAM-related configs in 4 posts
  hardware,3,GPU specs in top comments
  complaints,2,OOM errors reported by low-VRAM users
help[1]:
  If one post needs more context, run `redditrs thread <post_id> --top 20 --full`
```

Depth defaults (posts, threads with comments, comments per post): quick `{6,2,3}`, normal `{10,4,5}`, deep `{14,6,8}`. `evidence`/`clusters` — from §9.3; `observed subreddits` — top-12 by frequency. A failure to load one thread doesn't drop the pack (skip it).

### 8.6 `redditrs resolve-subreddits "<topic>" [--limit 15|1..25] [--refresh]`

Ranked subreddit candidates for a topic. The command expands multi-term topics
internally, so callers should not issue separate synonym/adjacent resolves.
Schema:

```
subreddits[5]{name,score,reasons,subscribers,title,description}:
  ComfyUI,58,"priority subreddit; 4 matching posts; subscriber signal",1.8M,ComfyUI,"Community discussion and help for ComfyUI"
  StableDiffusion,31,"topic match; subscriber signal",2.4M,Stable Diffusion,"Tools, workflows, and support"
count: 5 of 12 total
help[1]:
  If recent examples are needed, run `redditrs search "comfyui settings" --subreddits ComfyUI --sort new`
```

`subscribers` — compact form (`1.8M`, `890K`); TOON descriptions are capped at 240 characters. `--refresh` bypasses the topic cache and fresh HTTP records, then atomically replaces the topic snapshot. Algorithm — §9.5.

### 8.7 `redditrs subreddits "<query>" [--limit 10|1..25]`

Raw subreddit search (no ranking):

```
subreddits[3]{name,title,subscribers}:
  ComfyUI,ComfyUI — the most powerful and modular diffusion GUI,1.8M
  …
```

### 8.8 `redditrs trends "<subs>" [--sort hot|top|new] [--time week] [--limit 10|1..30]`

hot/top/new for one or several subs (comma-separated). Merge, ranking as in search (query = the sub list), the limit is divided between subs. Schema — as in `search`; `--time` applies only to `--sort top`.

### 8.9 `redditrs url-extract "<url_or_id>"`

Parses a Reddit URL/ID into normalized fields: `kind: post|comment|subreddit|user|unknown`, `subreddit`, `post_id`, `comment_id`, `username`, `canonical_url`. Rules — a port of `extractRedditUrl` from the reference (bare id, `t1_`, `/r/<sub>/comments/<id>[/<slug>][/<cid>]`, `/comments/<id>`, `/r/<sub>`, `/u/<user>`). Output — a TOON object. There is no error — `unknown` on unrecognized input (exit 0).

## 9. Heuristics (port from pi-reddit-research)

### 9.1 Models

`RedditPost{id, fullname, subreddit, title, author, score, num_comments, created_utc, age, permalink, url, domain, flair, selftext, over18, rank_score?, rank_reasons?}`; `RedditComment{id, author, score, body, depth, created_utc, url?}`; `SubredditCandidate{subreddit, score, reasons[], title?, public_description?, subscribers?}`; `EvidenceItem{kind: post|comment, cluster, post_id, post_index, subreddit, score, url, text, reason}`.

### 9.2 Post ranking (`rankPost`)

Sum of weights + reasons (top-5 in `rank_reasons`):
- exact query match: title +40, body +18;
- term matches (≥2 chars, tokenized by `[^a-z0-9_+#.-]+`): title ×8, body ×2.5;
- activity: `log1p(score)×3` + `log1p(num_comments)×4` (reasons: "high score" ≥100, "active discussion" ≥50 comments);
- freshness: `max(0, 12 − 3×log1p(age_days))`, reason "recent" ≤30 days;
- priority subreddit (map below);
- penalties: NSFW −25, empty body/domain −5, `[removed]` in title/body −20.

Priority map: `localllama 14, localllm 12, claudecode 10, claudeai 8, opencodecli 8, ollama 7, mcpservers 7, vibecoding 6, comfyui 6, stablediffusion 5`.

### 9.3 Evidence clusters

Clusters: `praise, complaints, fixes, settings, hardware, alternatives, guides, risks, general`. Classification — keyword-regex (a port of the reference's pattern lists, `clusterKeywords`), with intent preferences: bugs → complaints/risks; fixes → fixes/settings; settings → settings/hardware; hardware → hardware/complaints; compare → alternatives/praise/complaints; alternatives → alternatives; guides → guides/fixes; opinions → praise/complaints. In `evidence`/`clusters` — at most 6 items per cluster, up to 40 total.

### 9.4 Intents and depths

Intents (for `--intent`): `opinions, bugs, fixes, compare, settings, alternatives, trends, guides, hardware, general`. Each carries a `reading hint` for the agent (a port of `intentHints` — e.g., fixes: "prioritize comments with commands, versions, outcomes"). Depths: quick/normal/deep (defaults see §8.5).

### 9.5 Subreddit resolution

A versioned normalized `topic_key` has a 30 d cache. Discovery queries the full topic plus up to three significant non-operator terms, merges candidates case-insensitively, and tolerates failures of optional expansion queries. Candidates also include subreddits from the full-topic post sample.

Candidate score: at most one weighted match per topic term (normalized exact name +20, name +12, title +8, description +5) + `log1p(subscribers)×2` + matching posts (×8 per post + `log1p(score+comments)`) + 6 per matching discovery query (max 3). Static priority applies only with topic/post evidence. Support-oriented communities get +8; unrelated meme/shitpost/circlejerk/porn/sales communities get −30; unvalidated communities below 10K/1K subscribers get −6/−12. Ranking is deterministic by score, then case-insensitive name.

## 10. Project structure

```
cmd/redditrs/              # cobra commands and specialized TOON renderers
internal/config/           # env → JSON-config priority, re-read per request
internal/reddit/           # HTTP client: .json endpoints, delay, 403/429 cooldown, stale fallback, parsing
internal/cache/            # bbolt (go.etcd.io/bbolt), per-type TTL, cross-process request gate
internal/model/            # RedditPost, RedditComment, SubredditCandidate, EvidenceItem
internal/rank/             # ranking, clusters, intents, depths — no I/O, pure logic
```

`internal/rank` has no network/cache dependencies and is tested in isolation.

## 11. Testing strategy

| Package | Tests |
|---|---|
| `internal/rank` | table-driven unit: ranking, clusters, intents, depths |
| `internal/config` | unit: priority chain (env > env-file > json > json-file), re-reading |
| `internal/reddit` | httptest server: status codes, retry-after, stale fallback, listing/thread parsing, 403 retry after a cookie change, shared request pacing |
| CLI layer | table-driven golden tests of schemas, errors, truncation and empty states; 2–3 integration smoke tests on httptest |

## 12. Definition of Done

1. `go build ./...`, `go vet ./...`, `go test ./...` — green.
2. Scenarios (manually or via smoke tests, with a real cookie):
   - `redditrs search "<query>"` returns a TOON list with the §8.3 schema;
   - `redditrs pack "<topic>" --intent settings` returns a pack (A) with clusters;
   - `redditrs thread <id> --top 10` returns the post + comments; `--full` lifts truncation;
   - without a cookie: `error: …AUTH_REQUIRED…` on stdout, exit 1, `help[]` with instructions;
   - `--format=json` returns valid JSON of the same data;
   - unknown flag: exit 2, `help[1]: valid flags…`;
   - empty search: explicit `0 posts found`, exit 0;
   - re-running a search within an hour makes no HTTP requests (cache).
3. TOON output matches the golden command schemas of §8.
4. README: installation, cookie configuration (per §4.3), disclaimer §4.5, examples.

## 13. Out of scope

- Write operations (posting, commenting, voting) — read-only.
- OAuth/API keys/Devvit.
- Anonymous access (yt-dlp loid flow) — fragile, not supported.
- pi extension on top of the CLI.
- Publication on GitHub (done by the user).
- Implementing the CLI itself — a separate effort under this specification.

## 14. Sources

- Reference: `github.com/SaintNerona/pi-reddit-research` (mechanics: config, queue, cooldown, cache, ranking, clusters, intents).
- TOON spec v4.1: `github.com/toon-format/spec/blob/main/SPEC.md`.
