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
// resolves each one's username, and returns them (deduped by username), the list
// of browsers examined (for diagnostics on failure), and whether reddit was
// reachable. reached is false only when there were candidate cookies but every
// one failed to verify on a transport error, so callers can tell "nobody's
// logged in" from "couldn't reach reddit to check."
func detectRedditAccounts() (accounts []account, checked []string, reached bool) {
	checked = checkedBrowsers()
	raw := append(chromeAccounts(), kookyAccounts()...)
	reached = len(raw) == 0 // no candidates to verify is not a network problem
	seen := map[string]bool{}
	for _, a := range raw {
		name, ok := fetchUsernameFor(a.header)
		if ok {
			reached = true // reddit answered (a name, or a definitive 401/403)
		}
		if name == "" || seen[name] { // expired/not-logged-in, or a dupe of the same user
			continue
		}
		seen[name] = true
		a.username = name
		accounts = append(accounts, a)
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].username < accounts[j].username })
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
	for _, s := range kooky.FindAllCookieStores(ctx) {
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

// --- macOS Chrome: native cookie decryption (mirrors yt-dlp) -----------------
//
// kooky can't read recent macOS Chrome: the unsigned `go run` binary is denied
// the Keychain "Chrome Safe Storage" key, and newer v10 cookies carry a 32-byte
// SHA256(host) prefix it doesn't strip. So we read the SQLite store via the
// sqlite3 CLI, fetch the AES key via the signed `security` CLI (which the user
// can authorize once), and decrypt v10 values ourselves.

func chromeAccounts() []account {
	if runtime.GOOS != "darwin" {
		return nil
	}
	if _, err := exec.LookPath("sqlite3"); err != nil {
		return nil
	}
	key, err := chromeSafeStorageKey()
	if err != nil {
		return nil
	}
	base := filepath.Join(os.Getenv("HOME"), "Library/Application Support/Google/Chrome")
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}

	var out []account
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || (name != "Default" && !strings.HasPrefix(name, "Profile ")) {
			continue
		}
		pairs := map[string]string{}
		for _, sub := range []string{"Network/Cookies", "Cookies"} {
			readChromeCookieDB(filepath.Join(base, name, sub), key, pairs)
		}
		if !hasSession(pairs) {
			continue // not a logged-in profile
		}
		out = append(out, account{header: joinCookies(pairs), source: "Chrome · " + chromeProfileName(base, name)})
	}
	return out
}

// chromeSafeStorageKey returns the AES-128 key Chrome uses for cookie values,
// derived from the Keychain password via PBKDF2 (salt "saltysalt", 1003 iters).
func chromeSafeStorageKey() ([]byte, error) {
	out, err := exec.Command("security", "find-generic-password", "-w", "-s", "Chrome Safe Storage").Output()
	if err != nil {
		return nil, err
	}
	pw := strings.TrimRight(string(out), "\n")
	return pbkdf2.Key(sha1.New, pw, []byte("saltysalt"), 1003, 16)
}

// readChromeCookieDB copies the (possibly locked) SQLite store, pulls reddit
// cookies via sqlite3, and decrypts each into `into`.
func readChromeCookieDB(dbPath string, key []byte, into map[string]string) {
	data, err := os.ReadFile(dbPath)
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp("", "omr-ck-*.sqlite")
	if err != nil {
		return
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return // a short/failed write would make sqlite3 read a truncated DB
	}
	if err := tmp.Close(); err != nil {
		return
	}

	out, err := exec.Command("sqlite3", tmp.Name(),
		`SELECT name || char(9) || hex(encrypted_value) FROM cookies WHERE host_key LIKE '%reddit%';`).Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		i := strings.IndexByte(line, '\t')
		if i < 1 {
			continue
		}
		enc, err := hex.DecodeString(line[i+1:])
		if err != nil {
			continue
		}
		if v, ok := decryptChromeV10(enc, key); ok && v != "" {
			into[line[:i]] = v
		}
	}
}

// decryptChromeV10 decrypts a "v10" AES-128-CBC cookie value (IV = 16 spaces),
// stripping PKCS7 padding and the 32-byte SHA256(host) prefix recent Chrome adds.
func decryptChromeV10(enc, key []byte) (string, bool) {
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

// chromeProfileName resolves a profile dir ("Default", "Profile 1") to its
// friendly name from Chrome's Local State, falling back to the dir name.
func chromeProfileName(base, dir string) string {
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

// kookyAccounts reads the browsers' own cookie stores and returns every
// browser/profile that holds a logged-in reddit session. On macOS, Chrome is
// skipped here — it's read natively (kooky can't decrypt it).
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
		if runtime.GOOS == "darwin" && strings.EqualFold(browser, "chrome") {
			continue // handled by chromeAccounts
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

// hasSession reports whether a cookie set looks logged-in.
func hasSession(pairs map[string]string) bool {
	for _, name := range []string{"reddit_session", "token_v2", "session"} {
		if strings.TrimSpace(pairs[name]) != "" {
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
