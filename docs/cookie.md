# Getting your Reddit cookie

`redditrs` reads Reddit's `.json` endpoints using a **web-session cookie** from a logged-in browser session. Since mid-2026, Reddit blocks unauthenticated `.json` access, so a valid cookie is required.

## What you need

- A Reddit account you don't mind risking (see the disclaimer in the README).
- About 5 minutes. The cookie lasts a few days to a week, then needs refreshing.

## Step 1 — copy the cookie

Open a **private/incognito** window (avoids unrelated cookies from your main profile), go to https://www.reddit.com/login and sign in (2FA is fine).

### Method A — full Cookie header (Network tab)

1. Press F12 → **Network** → reload the page.
2. Find any request to `www.reddit.com`.
3. **Headers → Request Headers → Cookie** → copy the whole value.

### Method B — individual cookies (Application tab)

More robust: no page reload or request hunting needed.

1. Press F12 → **Application → Cookies → https://www.reddit.com**.
2. Click any cookie → Ctrl+A → Ctrl+C → choose "Copy as string" (Chrome), or paste the list into an editor and join the entries with `; `.

### Cleanup

Remove cookies with JSON values — they break the JSON config file: `g_state`, `eu_cookie`, `seeker_session`, etc. Keep at least:

```
reddit_session=…; token_v2=…
```

`reddit_session` alone is enough for authentication. A name without `=` or an empty value does not count as a configured session.

## Step 2 — configure `redditrs`

### Option 1: config file (recommended)

```sh
mkdir -p ~/.config/redditrs && chmod 700 ~/.config/redditrs
```

`~/.config/redditrs/config.json`:

```json
{ "cookie": "reddit_session=…; token_v2=…" }
```

Or keep the secret in a separate file (preferred — the secret lives apart from the config):

```sh
printf '%s\n' 'reddit_session=…; token_v2=…' > ~/.config/redditrs/cookie.txt
chmod 600 ~/.config/redditrs/cookie.txt
```

```json
{ "cookieFile": "/home/user/.config/redditrs/cookie.txt" }
```

### Option 2: environment variable

```sh
export REDDITRS_COOKIE='reddit_session=…; token_v2=…'
```

### Priority

`REDDITRS_COOKIE` → `REDDITRS_COOKIE_FILE` → `cookie` in JSON → `cookieFile` in JSON. The cookie is re-read from disk before every request — updates apply without a restart.

## Step 3 — verify

```sh
curl -s -H "cookie: reddit_session=…; token_v2=…" -A "redditrs/1.0 (research)" \
  'https://www.reddit.com/r/popular/hot.json?limit=5&raw_json=1'
```

Must return JSON, not HTML. Then:

```sh
redditrs status
```

Should show `cookie: set`.

## Validity

- `token_v2` is a JWT; its payload must have `sub: "user"`. `sub: "loid"` means an anonymous session — it does **not** work.
- `loid`, `edgebucket`, `over18` are not required.
- `reddit_session` alone (without `token_v2`) is sufficient.

## Expiry and refresh

- Lifetime is unpublished; in practice a few days to a week.
- The sign of expiry is a 403 / `FORBIDDEN` error.
- Update the cookie in the config or cookie file and retry — no restart needed.

## Security

- The cookie is full access to your account. Treat it like a password.
- `chmod 600` on the config and cookie file; don't commit them.
- Don't pass the cookie to third-party services.
- Using a cookie for programmatic `.json` reads is a gray zone of Reddit's ToS — use a throwaway account at your own risk.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `cookie: not set` in `status` | no valid cookie configured | configure per Step 2; a bare name without `=` or an empty value doesn't count |
| `AUTH_REQUIRED` error | no cookie at all | configure per Step 2 |
| `FORBIDDEN`, "session cookie is likely expired" | cookie expired | copy a fresh cookie (Step 1) and update the config |
| 403 right after setup | session not logged in, or only anonymous cookies copied | re-login in the private window, re-copy; check `sub` in `token_v2` |
| curl returns HTML instead of JSON | cookie invalid or session anonymous | re-copy; verify with `redditrs status` |
| `RATE_LIMITED`, cooldown | rate limit | wait out the cooldown (the error says how long); the CLI retries are safe to continue after |
