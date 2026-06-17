package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/browserutils/kooky"
	_ "github.com/browserutils/kooky/browser/all" // register Chrome/Safari/Firefox/Edge/... stores
)

// Reddit gates anonymous traffic hard; a logged-in browser session has a far
// higher rate budget. Rather than make the user copy a Cookie header from
// DevTools, we read the browser's own cookie store (the one place the httpOnly
// reddit_session lives) and reuse it — the same trick yt-dlp uses.
//
// The session is cached in a 0600 file under the user config dir (not .env,
// which is OPENAI_API_KEY only) so relaunch skips re-auth. We only re-read the
// browser when the cache is empty or the session goes stale, which is when the
// macOS Keychain prompt can appear.

var (
	cookieMu     sync.RWMutex
	redditCookie string

	// renew is single-flighted and rate-limited: a burst of 403s (or startup)
	// must trigger at most one browser read per cooldown, since reading Chrome's
	// store can pop a macOS Keychain prompt.
	renewMu   sync.Mutex
	renewing  bool
	lastRenew time.Time
)

const renewCooldown = 45 * time.Second

func currentCookie() string {
	cookieMu.RLock()
	defer cookieMu.RUnlock()
	return redditCookie
}

func setCookie(v string) {
	cookieMu.Lock()
	redditCookie = strings.TrimSpace(v)
	cookieMu.Unlock()
}

// sessionFile is the on-disk shape of the cached session.
type sessionFile struct {
	Cookie string `json:"cookie"`
}

// sessionPath is the cached-session file, in the user config dir alongside
// recents.json — deliberately not .env.
func sessionPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "oh-my-reddit", "session.json")
}

// initCookie seeds the in-memory cookie from the cached session, if any.
func initCookie() {
	p := sessionPath()
	if p == "" {
		return
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var s sessionFile
	if json.Unmarshal(data, &s) == nil {
		setCookie(s.Cookie)
	}
}

// saveCookie caches the session (0600, dir 0700) so relaunch skips re-auth.
func saveCookie(v string) {
	p := sessionPath()
	if p == "" {
		return
	}
	if os.MkdirAll(filepath.Dir(p), 0o700) != nil {
		return
	}
	data, err := json.Marshal(sessionFile{Cookie: strings.TrimSpace(v)})
	if err != nil {
		return
	}
	_ = os.WriteFile(p, data, 0o600)
}

// clearSavedCookie deletes the cached session (used on logout).
func clearSavedCookie() {
	if p := sessionPath(); p != "" {
		_ = os.Remove(p)
	}
}

// renewCookie tries to (re)acquire a reddit session from the browser. It's
// single-flighted and rate-limited. Returns whether the cookie changed and which
// browser it came from. Safe to call concurrently and from any goroutine.
func renewCookie() (changed bool, source string) {
	renewMu.Lock()
	if renewing || (!lastRenew.IsZero() && time.Since(lastRenew) < renewCooldown) {
		renewMu.Unlock()
		return false, ""
	}
	renewing = true
	renewMu.Unlock()
	defer func() {
		renewMu.Lock()
		renewing = false
		lastRenew = time.Now()
		renewMu.Unlock()
	}()

	header, src := detectRedditCookie()
	if header == "" || header == currentCookie() {
		return false, "" // nothing found, or the same (already-stale) cookie
	}
	setCookie(header)
	saveCookie(header)
	return true, src
}

// account is one logged-in reddit session found in a browser.
type account struct {
	header   string // the Cookie header to send
	username string // resolved via /api/me.json
	source   string // e.g. "Chrome · Renato"
}

// detectRedditAccounts finds every logged-in reddit session across browsers,
// resolves each one's username, and returns them, the list of browsers examined
// (for diagnostics on failure), and whether reddit was reachable. reached is
// false only when there were candidate cookies but every one failed to verify on
// a transport error, so callers can tell "nobody's logged in" from "couldn't
// reach reddit to check."
//
// The same reddit account logged in across several browsers shows up once per
// browser (Chrome and Brave hold separate cookie jars / session tokens), so the
// user can pick which session to use. Only exact browser+account repeats are
// collapsed.
func detectRedditAccounts() (accounts []account, checked []string, reached bool) {
	checked = checkedBrowsers()
	raw := append(chromiumAccounts(), kookyAccounts()...)
	reached = len(raw) == 0 // no candidates to verify is not a network problem
	seen := map[string]bool{}
	for _, a := range raw {
		name, ok := fetchUsernameFor(a.header)
		if ok {
			reached = true // reddit answered (a name, or a definitive 401/403)
		}
		if name == "" { // expired or not actually logged in
			continue
		}
		a.username = name
		key := a.source + "\x00" + name
		if seen[key] { // an exact same-browser/profile + account repeat
			continue
		}
		seen[key] = true
		accounts = append(accounts, a)
	}
	// Group by username, then browser source, so one user across browsers lists
	// together and the order stays stable.
	sort.Slice(accounts, func(i, j int) bool {
		if accounts[i].username != accounts[j].username {
			return accounts[i].username < accounts[j].username
		}
		return accounts[i].source < accounts[j].source
	})
	return accounts, checked, reached
}

// detectRedditCookie returns a single best session — used by the automatic
// stale-cookie renewal (the 403 path), where we just need any valid session.
func detectRedditCookie() (header, source string) {
	accounts, _, _ := detectRedditAccounts()
	if len(accounts) == 0 {
		return "", ""
	}
	return accounts[0].header, accounts[0].source
}

// checkedBrowsers lists the real (on-disk) browser stores examined, counting
// distinct profiles, so the failure screen can show what was looked at.
func checkedBrowsers() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	profiles := map[string]map[string]bool{}

	// Chromium browsers we read natively on macOS won't (reliably) show up via
	// kooky, so count their installed profiles directly — otherwise the failure
	// screen would claim we never looked at Brave/Edge/Vivaldi when we did.
	if runtime.GOOS == "darwin" {
		support := filepath.Join(os.Getenv("HOME"), "Library/Application Support")
		for _, b := range macChromium {
			profs := chromiumProfileDirs(filepath.Join(support, b.dir))
			if len(profs) == 0 {
				continue // not installed
			}
			profiles[b.name] = map[string]bool{}
			for _, p := range profs {
				profiles[b.name][p] = true
			}
		}
	}

	for _, s := range kooky.FindAllCookieStores(ctx) {
		if runtime.GOOS == "darwin" && nativeMacChromium(s.Browser()) {
			s.Close()
			continue // already counted natively above
		}
		if fp := s.FilePath(); fp != "" {
			if _, err := os.Stat(fp); err == nil {
				if profiles[s.Browser()] == nil {
					profiles[s.Browser()] = map[string]bool{}
				}
				profiles[s.Browser()][s.Profile()] = true
			}
		}
		s.Close()
	}
	var checked []string
	for b, profs := range profiles {
		if len(profs) > 1 {
			checked = append(checked, fmt.Sprintf("%s (%d profiles)", b, len(profs)))
		} else {
			checked = append(checked, b)
		}
	}
	sort.Strings(checked)
	return checked
}

// --- macOS Chromium browsers: native cookie decryption (mirrors yt-dlp) ------
//
// kooky can't read recent macOS Chromium browsers: the unsigned `go run` binary
// is denied the Keychain "<Browser> Safe Storage" key, and newer v10 cookies
// carry a 32-byte SHA256(host) prefix it doesn't strip. So we read the SQLite
// store via the sqlite3 CLI, fetch the AES key via the signed `security` CLI
// (which the user can authorize once), and decrypt v10 values ourselves. Every
// Chromium browser shares this scheme, differing only in store path and key.

// macChromium lists the Chromium-family browsers we read natively on macOS. They
// share the v10 decryption, the profile layout (Default, Profile N), and a
// "<name> Safe Storage" Keychain key — only the store dir and key name differ.
var macChromium = []struct {
	name     string // display name shown as the account source
	dir      string // store dir under ~/Library/Application Support
	keychain string // Keychain service holding the AES password
}{
	{"Chrome", "Google/Chrome", "Chrome Safe Storage"},
	{"Brave", "BraveSoftware/Brave-Browser", "Brave Safe Storage"},
	{"Edge", "Microsoft Edge", "Microsoft Edge Safe Storage"},
	{"Vivaldi", "Vivaldi", "Vivaldi Safe Storage"},
}

func chromiumAccounts() []account {
	if runtime.GOOS != "darwin" {
		return nil
	}
	if _, err := exec.LookPath("sqlite3"); err != nil {
		return nil
	}
	support := filepath.Join(os.Getenv("HOME"), "Library/Application Support")

	var out []account
	for _, b := range macChromium {
		base := filepath.Join(support, b.dir)
		if _, err := os.Stat(base); err != nil {
			continue // browser not installed — no further work, no Keychain prompt
		}
		// Pass 1: gather each profile's encrypted reddit cookies. Cookie names are
		// plaintext, so we can spot a logged-in profile here without the Keychain
		// key — a browser with no reddit session never triggers a Keychain prompt.
		type profile struct {
			name string
			rows map[string][]byte
		}
		var loggedIn []profile
		for _, prof := range chromiumProfileDirs(base) {
			rows := map[string][]byte{}
			for _, sub := range []string{"Network/Cookies", "Cookies"} {
				for k, v := range redditCookieRows(filepath.Join(base, prof, sub)) {
					rows[k] = v
				}
			}
			if hasSessionName(rows) {
				loggedIn = append(loggedIn, profile{prof, rows})
			}
		}
		if len(loggedIn) == 0 {
			continue // no reddit session in any profile — don't touch the Keychain
		}

		// Pass 2: a session exists, so it's worth the one Keychain prompt to decrypt.
		key, err := chromiumSafeStorageKey(b.keychain)
		if err != nil {
			continue // no key (e.g. the user denied the prompt) — skip this browser
		}
		for _, p := range loggedIn {
			pairs := map[string]string{}
			for name, enc := range p.rows {
				if v, ok := decryptChromiumV10(enc, key); ok && v != "" {
					pairs[name] = v
				}
			}
			if !hasSession(pairs) {
				continue // names looked logged-in but nothing decrypted to a session
			}
			out = append(out, account{header: joinCookies(pairs), source: b.name + " · " + chromiumProfileName(base, p.name)})
		}
		zero(key) // wipe the AES key now this browser's cookies are decrypted
	}
	return out
}

// chromiumSafeStorageKey returns the AES-128 key a Chromium browser uses for
// cookie values, derived from the Keychain password (named by service) via
// PBKDF2 (salt "saltysalt", 1003 iters).
func chromiumSafeStorageKey(service string) ([]byte, error) {
	out, err := exec.Command("security", "find-generic-password", "-w", "-s", service).Output()
	if err != nil {
		return nil, err
	}
	defer zero(out) // wipe the raw Keychain-password buffer once the key is derived
	pw := strings.TrimRight(string(out), "\n")
	return pbkdf2.Key(sha1.New, pw, []byte("saltysalt"), 1003, 16)
}

// chromiumProfileDirs lists a Chromium store's profile directories ("Default",
// "Profile 1", …). Shared by the cookie read and the "checked browsers"
// diagnostic so the two can't disagree on what a browser's profiles are.
func chromiumProfileDirs(base string) []string {
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var profs []string
	for _, e := range entries {
		if name := e.Name(); e.IsDir() && (name == "Default" || strings.HasPrefix(name, "Profile ")) {
			profs = append(profs, name)
		}
	}
	return profs
}

// zero overwrites b in place so secret material (a derived AES key, a Keychain
// password buffer) does not linger in memory longer than needed. Best-effort:
// Go strings are immutable and can't be wiped, so secrets are kept in []byte
// where this is possible.
func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// redditCookieRows copies the (possibly locked) SQLite store and returns the raw
// encrypted reddit cookie values by name. It does no decryption, so it needs no
// Keychain key — letting chromiumAccounts spot a logged-in profile from the
// plaintext cookie names before prompting for the key. Empty when the store is
// unreadable or holds no reddit cookies.
func redditCookieRows(dbPath string) map[string][]byte {
	data, err := os.ReadFile(dbPath)
	if err != nil {
		return nil
	}
	tmp, err := os.CreateTemp("", "omr-ck-*.sqlite")
	if err != nil {
		return nil
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return nil // a short/failed write would make sqlite3 read a truncated DB
	}
	if err := tmp.Close(); err != nil {
		return nil
	}

	out, err := exec.Command("sqlite3", tmp.Name(),
		`SELECT name || char(9) || hex(encrypted_value) FROM cookies WHERE host_key LIKE '%reddit%';`).Output()
	if err != nil {
		return nil
	}
	rows := map[string][]byte{}
	for _, line := range strings.Split(string(out), "\n") {
		i := strings.IndexByte(line, '\t')
		if i < 1 {
			continue
		}
		enc, err := hex.DecodeString(line[i+1:])
		if err != nil {
			continue
		}
		rows[line[:i]] = enc
	}
	return rows
}

// decryptChromiumV10 decrypts a "v10" AES-128-CBC cookie value (IV = 16 spaces),
// stripping PKCS7 padding and the 32-byte SHA256(host) prefix recent Chromium adds.
func decryptChromiumV10(enc, key []byte) (string, bool) {
	if len(enc) < 3+aes.BlockSize || string(enc[:3]) != "v10" {
		return "", false
	}
	ct := enc[3:]
	if len(ct)%aes.BlockSize != 0 {
		return "", false
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", false
	}
	pt := make([]byte, len(ct))
	cipher.NewCBCDecrypter(block, bytes.Repeat([]byte{' '}, aes.BlockSize)).CryptBlocks(pt, ct)
	if n := int(pt[len(pt)-1]); n >= 1 && n <= aes.BlockSize && n <= len(pt) {
		pt = pt[:len(pt)-n] // PKCS7
	}
	if !utf8.Valid(pt) && len(pt) >= 32 && utf8.Valid(pt[32:]) {
		pt = pt[32:] // strip the SHA256(host) hash newer Chrome prepends
	}
	if !utf8.Valid(pt) {
		// Still not text: a wrong key or an unknown format produced garbage.
		// Report failure so callers can tell "couldn't decrypt" from "no cookie".
		return "", false
	}
	return string(pt), true
}

// chromiumProfileName resolves a profile dir ("Default", "Profile 1") to its
// friendly name from the browser's Local State, falling back to the dir name.
func chromiumProfileName(base, dir string) string {
	data, err := os.ReadFile(filepath.Join(base, "Local State"))
	if err != nil {
		return dir
	}
	var ls struct {
		Profile struct {
			InfoCache map[string]struct {
				Name string `json:"name"`
			} `json:"info_cache"`
		} `json:"profile"`
	}
	if json.Unmarshal(data, &ls) == nil {
		if p, ok := ls.Profile.InfoCache[dir]; ok && p.Name != "" {
			return p.Name
		}
	}
	return dir
}

// nativeMacChromium reports whether a kooky-reported browser name is one we read
// natively on macOS (see macChromium). kooky can't decrypt these here, so its
// entries for them are skipped and chromiumAccounts is relied on instead.
func nativeMacChromium(browser string) bool {
	switch strings.ToLower(browser) {
	case "chrome", "chromium", "brave", "edge", "microsoft edge", "vivaldi":
		return true
	}
	return false
}

// kookyAccounts reads the browsers' own cookie stores and returns every
// browser/profile that holds a logged-in reddit session. On macOS, Chromium
// browsers are skipped here — they're read natively (kooky can't decrypt them).
func kookyAccounts() []account {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cookies, err := kooky.ReadCookies(ctx, kooky.Valid, kooky.DomainHasSuffix("reddit.com"))
	if err != nil || len(cookies) == 0 {
		return nil
	}

	// Group by browser+profile so we never mix cookies from two sessions.
	type group struct {
		pairs   map[string]string // name -> value (last write wins)
		browser string
		profile string
	}
	groups := map[string]*group{}
	for _, c := range cookies {
		if c == nil || strings.TrimSpace(c.Value) == "" {
			continue
		}
		browser, profile := "browser", ""
		if c.Browser != nil {
			browser, profile = c.Browser.Browser(), c.Browser.Profile()
		}
		if runtime.GOOS == "darwin" && nativeMacChromium(browser) {
			continue // read natively by chromiumAccounts; kooky can't decrypt these here
		}
		key := browser + "|" + profile
		g := groups[key]
		if g == nil {
			g = &group{pairs: map[string]string{}, browser: browser, profile: profile}
			groups[key] = g
		}
		g.pairs[c.Name] = c.Value
	}

	var out []account
	for _, g := range groups {
		if !hasSession(g.pairs) {
			continue
		}
		src := g.browser
		if g.profile != "" {
			src += " · " + g.profile
		}
		out = append(out, account{header: joinCookies(g.pairs), source: src})
	}
	return out
}

// sessionCookieNames are the cookies whose presence marks a logged-in reddit
// session. Shared by the name-only and value checks below.
var sessionCookieNames = []string{"reddit_session", "token_v2", "session"}

// hasSession reports whether a decrypted cookie set looks logged-in.
func hasSession(pairs map[string]string) bool {
	for _, name := range sessionCookieNames {
		if strings.TrimSpace(pairs[name]) != "" {
			return true
		}
	}
	return false
}

// hasSessionName reports whether a set of still-encrypted cookies includes a
// reddit session cookie. Cookie names are plaintext, so this works before any
// decryption — i.e. before we need the Keychain key.
func hasSessionName(rows map[string][]byte) bool {
	for _, name := range sessionCookieNames {
		if _, ok := rows[name]; ok {
			return true
		}
	}
	return false
}

// joinCookies renders a deterministic "name=value; …" Cookie header.
func joinCookies(pairs map[string]string) string {
	names := make([]string, 0, len(pairs))
	for n := range pairs {
		names = append(names, n)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, n+"="+pairs[n])
	}
	return strings.Join(parts, "; ")
}
