# Security Policy

## Overview

`redditrs` is a CLI that interacts with Reddit using a **web-session cookie**
from a logged-in account. A session cookie grants full access to that Reddit
account, so the project treats anything that could leak or abuse credentials as
security-sensitive.

We take security seriously and will acknowledge and triage reports as quickly
as we reasonably can. There is **no bug bounty** program.

## Reporting a vulnerability

**Do not open a public issue** for a security vulnerability. Please report
privately instead.

Use GitHub's private **Security Advisory** flow:

1. Go to the repository's **Security** tab.
2. Click **Report a vulnerability**.
3. Describe the issue, the affected version, and if possible a minimal
   reproduction.

We aim to respond in a reasonable time frame — typically within a few business
days. Once a fix is ready we will reach a disclosure timeline with you in mind,
defaulting to coordinated disclosure so users can upgrade before details go
public.

## What is sensitive

- **Cookie files** — any file holding a Reddit session cookie (for example
  `cookie.txt` referenced from `config.json`). Keep cookie files with
  `0600` permissions and never commit them.
- **`config.json`** — the configuration file (default
  `~/.config/redditrs/config.json`) can reference cookie paths. Keep it with
  `0600` permissions and never commit it.
- **Tokens in the environment** — `REDDITRS_COOKIE` and any other environment
  variable carrying credentials. Treat these as secrets: don't log, echo, or
  paste them, and don't include them in issues or PRs.

## Supported versions

Security fixes are applied to the `main` branch and to the latest release. Only
the latest release is eligible for backports.

## Non-security issues

For bugs, feature requests, or questions that are **not** security issues, use
the standard issue templates or GitHub Discussions instead.