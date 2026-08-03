---
name: redditrs
version: 1.0.0
description: >-
  Use when the user asks for Reddit research — what people on Reddit think or
  say about a topic, product, or tool; finding Reddit discussions, threads,
  opinions, complaints, guides, or evidence; when the request mentions
  'reddit' or 'subreddit'; to find subreddits for a topic; to check what's
  hot on Reddit.
---

# redditrs

Reddit research via the `redditrs` CLI (cookie auth, no API keys). The CLI is
self-documenting — `--help` on every command, `help[N]:` hints at the end of
every output. Run the shortest matching command and use a hint only when its
condition applies.

## Loop

1. Run the target command directly. **Do not run `redditrs status` as a
   preflight.** Use it only after a configuration/cooldown problem; every
   research command already returns structured auth errors.
2. Pick the shortest complete route:

   | Want… | Run |
   |---|---|
   | opinions or comparison with comment evidence | `pack "<topic>" --intent <intent> --depth deep` |
   | matching posts | `search "<query>"` |
   | which subreddits fit a topic | `resolve-subreddits "<topic>" --limit 15` |
   | current/latest subreddit candidates | `resolve-subreddits "<topic>" --limit 15 --refresh` |
   | current hot posts | `trends <sub> --sort hot` |
   | notable themes in a period | `trends <sub> --sort top --time <period> --limit 25 --fields id,subreddit,title,score,num_comments,age` |
   | one known thread | `thread <id_or_url> --top 10 --full` |
   | find one substantive text discussion | `search "<query> self:yes" --limit 15 --fields id,subreddit,title,score,num_comments,age,selftext`, then one selected `thread <id> --top 20 --full` |

   `pack` intents: opinions, bugs, fixes, compare, settings, alternatives,
   trends, guides, hardware, general — pick the closest to the question.
   For a comparison discussion, keep only the key entities and join them with
   Reddit's `AND`, for example `"NixOS AND Arch self:yes"`; omit filler such as
   `vs`, `comparison`, and generic category words.

3. Read the output deliberately:
   - On success, `help[N]:` lines are optional suggestions. Follow one only
     when the current output lacks evidence needed for the question.
   - A truncation marker is not a failure. Use `--full` only when the omitted
     tail is needed for a claim; prefer one `thread <id> --full` over
     `pack --full`.
   - On `0 posts found`, make one relevant wider retry (query, time, sort, or
     scope), then report the gap instead of searching indefinitely.

4. Stop when the requested evidence is covered:
   - A deep `pack` is normally complete. Open at most one or two threads only
     for a relevant cluster that still lacks context.
   - A subreddit ranking already expands the full topic into significant terms
     and returns titles/descriptions for related candidates. Do not issue a
     second synonym/adjacent `resolve-subreddits`. When recent examples are
     requested, validate only the top two candidates and then stop; their trend
     listings are sufficient evidence.
   - A 25-post trends listing is enough to identify themes. Open at most three
     threads only when comment reactions matter.
   - For one substantive thread, use one search plus at most one rewritten
     search, inspect `num_comments` and `selftext`, then open no more than two
     candidates.
   - Do not continue merely because a success hint exists or to fill an
     incidental cluster unrelated to the user's question.

5. Synthesize from the evidence: cite `r/sub` and post ids, distinguish user
   opinions from verified facts, and avoid claiming consensus from a small
   sample.

## Execution constraints

Run only one `redditrs` process at a time. Do not launch commands in parallel;
the CLI also shares its request pacing through bbolt. Outputs are already
bounded: run commands directly without `head` or `sed`, using `--limit` and
`--fields` so truncation markers and `help[N]:` remain visible.

Errors arrive structured in stdout (`error:` + `code:` + `help[N]:`). On
`AUTH_REQUIRED`/`FORBIDDEN`, tell the user to refresh the cookie and stop. On
`RATE_LIMITED`, wait out the cooldown and retry once.
