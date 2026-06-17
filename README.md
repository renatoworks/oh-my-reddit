# oh-my-reddit

<img width="1600" height="1080" alt="oh-my-reddit" src="https://github.com/user-attachments/assets/21fac5ab-b64f-4fb1-8f29-5299a05f6eb8" />

Beautiful Reddit threads, live in your terminal.

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![CI](https://github.com/renatoworks/oh-my-reddit/actions/workflows/ci.yml/badge.svg)](https://github.com/renatoworks/oh-my-reddit/actions/workflows/ci.yml)

## Install

You need [Go](https://go.dev/dl/) (1.24 or newer). Install the command straight from the module:

```sh
go install github.com/renatoworks/oh-my-reddit@latest
```

That builds `oh-my-reddit` into your Go bin directory (`~/go/bin` by default). If that folder isn't on your PATH yet, add it (then open a new terminal):

```sh
echo 'export PATH="$PATH:$(go env GOPATH)/bin"' >> ~/.zshrc
```

Then start it by running `oh-my-reddit`. See [Usage](#usage) to jump straight to a subreddit or thread.

Prefer to work from a clone?

```sh
git clone https://github.com/renatoworks/oh-my-reddit
cd oh-my-reddit
go run .                       # run in place
go build -o oh-my-reddit .     # or build a one-off binary, then ./oh-my-reddit
```

## Usage

```sh
oh-my-reddit                 # interactive: type a subreddit, a thread URL, or 'demo'
oh-my-reddit demo            # offline demo feed (no network, no login)
oh-my-reddit r/soccer        # jump straight to a subreddit's thread list
oh-my-reddit https://www.reddit.com/r/soccer/comments/<id>/...   # open one thread
```

## Signing in

Reddit throttles anonymous access hard (lots of `429` and `403`), so you need to be logged in. The first time you run it (unless you pass `demo`) you get a connect screen. Press `enter` and it finds a logged-in reddit session in your browser (Chrome, Brave, Edge, Vivaldi, Safari, Firefox, across all profiles) and reuses it, no copy and paste. If auto-detect comes up empty, you can paste one by hand: on reddit.com (logged in), open DevTools, and from the Network tab copy any request's full `Cookie:` header.

The session is cached locally, so later launches skip this screen. If more than one session is found, whether different accounts or the same account across browsers, you choose which to use. If it expires, oh-my-reddit refreshes it from your browser when it can, or sends you back here to reconnect. Press `ctrl+l` to log out anytime.

### macOS notes

- **Chrome, Brave, Edge, Vivaldi**: must be logged in. The first read pops a one-time Keychain prompt per browser, so click *Allow*.
- **Safari**: your terminal needs Full Disk Access (System Settings, Privacy & Security, Full Disk Access).
- **Firefox**: works with no prompt.

The session cookie is treated like a password: it's stored in your config dir with owner-only permissions, never in `.env` or the repo.

## AI features (optional)

Set an `OPENAI_API_KEY` and the feed gets two AI reads. Export it in your shell so it works wherever you run the command, or put it in a `.env` file when running from a clone. Both run on `gpt-4o-mini`, show up as purple cards in the timeline, and are grounded only in the original post and the recent comments.

- **`s`, live sentiment**: a one-line read on the thread's mood right now. Press `s` again and it covers only what is new since the last card.
- **`a`, ask**: type a question and get a one-line answer from the thread. It won't invent facts, and it attributes opinions ("people are saying…"), since commenters can be wrong or joking.

```sh
export OPENAI_API_KEY=sk-...   # in your shell profile (~/.zshrc), works anywhere
# or, from a clone, put it in .env:  OPENAI_API_KEY=sk-...
```

## Voice (macOS)

Voice reads the thread to you out loud. It uses the macOS `say` command, so it is macOS only. It is off by default. Press `v` in the feed to toggle it, and your choice is remembered for next time.

- New comments are read aloud as they arrive. When a thread is busy it reads the newest and skips the backlog rather than falling behind.
- AI cards (`s` and `a`) are spoken too, and narration pauses while they play so the two never overlap.
- The line being read is highlighted so you can follow along.

### Better voices

The default macOS voices (Daniel, Samantha) sound robotic. The **Siri** voices sound far more natural, and oh-my-reddit uses whatever your System Voice is set to. To switch:

1. Open **System Settings** → **Accessibility** → **Spoken Content** *(or press `Cmd+Space` and search "Spoken Content")*.
2. Click the **ⓘ** info icon next to **System Voice**.
3. In the voice dropdown, search for **"Siri"** and download one you like.
4. Set it as your **System Voice**, so every `say` command uses it.

Test it from a terminal:

```sh
say "oh my reddit"
```

## How it works

- **No OAuth, no app registration.** Comments come from a thread's JSON endpoint (`?sort=new`), which carries scores and `parent_id` for replies. If JSON is blocked it falls back to the RSS feed (flat, no scores). Subreddit listings work the same way.
- **Polling.** The feed polls every 10 seconds. It uses conditional requests (`If-None-Match` and `If-Modified-Since`) so Reddit can answer `304 Not Modified` when nothing changed, which is cheap and less likely to get throttled. On `429` or `5xx` it honors `Retry-After` and backs off with jitter.
- **Graceful streaming.** New comments are queued and released one at a time, each fading in, instead of dumping all at once.
- **Live votes.** Each poll refreshes every visible comment's score.
- **Activity sparkline.** The status bar shows a 14-bar sparkline of comments per 30 seconds. The buckets are fixed wall-clock slices, so past bars stay put and the window steps left every 30 seconds.
- **Demo mode** plays a scripted match thread on a loop, so the chat clusters around what is "happening" (a goal sets off a rush, a bad call a pile-on), with no network or login.

## Files

It's a single `package main` (flat, no subpackages), split by concern:

| File | What's in it |
|------|--------------|
| `main.go` | entry point, `.env` loading, small shared helpers |
| `model.go` | the Bubble Tea model, message types, construction, navigation |
| `update.go` | the update loop: key handling and message dispatch |
| `commands.go` | async `tea.Cmd`s: polling, fetching, AI requests |
| `narrate.go` | voice narration and the macOS `say` engine |
| `feed.go` | comment-stream state, AI context, activity metrics |
| `anim.go` | entrance and fade animations |
| `view.go` | top-level view dispatch and shared layout chrome |
| `view_auth.go` | the connect / sign-in screen |
| `view_input.go` | the input screen (subreddit · URL · demo) |
| `view_list.go` | the thread list: sort tabs, rows, sections |
| `view_feed.go` | the comment feed, original-post modal, status bar |
| `reddit.go` | fetch and parse threads and comments (JSON and RSS), backoff, caching |
| `image.go` | fetch and render an OP link's image as terminal half-blocks |
| `cookies.go` | find and decrypt the reddit session from your browser |
| `ai.go` | OpenAI client, sentiment and ask prompts |
| `recents.go` | saved recent subreddits and threads |
| `settings.go` | persisted preferences (voice on or off) |
| `version.go` | build version and the Go-proxy update check |
| `browser.go` | open-in-browser helper |
| `styles.go` | dark palette, Lip Gloss styles, wordmark gradient |
| `sample.go` | demo comment generator |

## License

MIT. See [LICENSE](LICENSE).

---

Made in [Blueberry](https://meetblueberry.com) 🫐
