# CLAUDE.md

Project context for Claude Code.

## Overview

oh-my-reddit is a terminal UI that streams a Reddit thread's comments live, built with [Bubble Tea](https://github.com/charmbracelet/bubbletea). It is a single Go binary and one flat `package main` (no subpackages).

Reddit gates anonymous access hard (429/403), so the app reuses a logged-in session from the browser's cookie store rather than asking for credentials.

## Commands

```bash
go run .                 # run it (interactive)
go run . demo            # offline demo feed (no network, no login)
go build -o oh-my-reddit .
go test ./...            # tests
gofmt -l .               # formatting check (CI fails if this prints anything)
go vet ./...
```

## Architecture

- Standard Bubble Tea Model/Update/View. All state is one `model` struct (`model.go`), grouped by screen with section comments.
- Four screens routed by the `screen` enum: connect (auth), input, thread list, feed.
- Comments come from a thread's JSON endpoint (`sort=new`), with an RSS fallback when JSON is blocked. The feed polls every 10s with conditional requests and backoff (`reddit.go`).
- New comments are queued and released one at a time so a busy thread streams smoothly instead of dumping all at once (`feed.go`).
- Background work (polling, fetching, AI, narration) runs as `tea.Cmd`s (`commands.go`, `narrate.go`). Bubble Tea ticks cannot be cancelled, so commands carry a generation counter (`loopGen`, `narrateGen`, `animGen`) and stale ticks are tagged and dropped.

## File map

- `main.go`: entry point, `.env` loading, small shared helpers
- `model.go`: model struct, message types, construction, navigation
- `update.go`: the update loop: key handling and message dispatch
- `commands.go`: async `tea.Cmd`s (poll, fetch, AI requests)
- `narrate.go`: voice narration and the macOS `say` engine
- `feed.go`: comment-stream state, AI context, activity metrics
- `anim.go`: entrance and fade animations
- `view.go`: top-level view dispatch and shared layout chrome
- `view_auth.go` / `view_input.go` / `view_list.go` / `view_feed.go`: per-screen rendering
- `reddit.go`: fetch and parse threads and comments (JSON and RSS), backoff, caching, control-byte stripping
- `image.go`: fetch and render an OP link's image as terminal half-blocks
- `cookies.go`: find and decrypt the Reddit session from the browser
- `ai.go`: OpenAI client, sentiment and ask prompts
- `recents.go` / `settings.go`: state persisted under the user config dir
- `version.go`: build version and the Go-proxy update check
- `styles.go`: palette, Lip Gloss styles, wordmark gradient
- `sample.go`: demo comment generator

## Conventions

- Keep `gofmt` clean (CI enforces it) and `go vet` clean.
- Comments explain why, not what.
- Mutually-exclusive UI state is a typed enum (`screen`, `authMode`, `feedMode`, `listSort`), not a set of parallel bools.
- Errors the user can act on surface via `m.err` or the connect screen; persistence and browser-detection failures degrade silently by design.
- Prose and docs use plain English with no em dashes, and are not hard-wrapped (one line per paragraph, let it soft-wrap).
- When you change behavior, keys, flags, or the file layout, update `README.md` and this `CLAUDE.md` in the same change before committing, so the docs don't drift.

## Important notes

- Never commit `.env` (the OpenAI key) or the session cookie. `.env` is gitignored; the cookie is cached `0600` in the user config dir.
- On macOS, Chromium browsers (Chrome, Brave, Edge, Vivaldi) are decrypted natively, because the usual library can't read them. See `cookies.go`.
- Voice and the `say` engine are macOS only, gated on `runtime.GOOS == "darwin"`.
- AI features (`s` summary, `a` ask) require `OPENAI_API_KEY`. Without it they are hidden and the keys do nothing.
