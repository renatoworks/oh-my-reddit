package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestFuzzyFilter(t *testing.T) {
	threads := []thread{
		{title: "random other thread"},
		{title: "Match Thread: France vs Senegal"},
		{title: "match preview"},
	}
	got := fuzzyFilter(threads, "match")
	for _, th := range got {
		if th.title == "random other thread" {
			t.Error("non-matching thread should be filtered out")
		}
	}
	// A contiguous substring match outranks a scattered subsequence match.
	ranked := fuzzyFilter([]thread{
		{title: "m-a-t-c-h scattered"},
		{title: "match contiguous"},
	}, "match")
	if len(ranked) != 2 || ranked[0].title != "match contiguous" {
		t.Errorf("contiguous match should rank first, got %v", ranked)
	}
}

func TestParseThreadsJSONStickyStability(t *testing.T) {
	body := []byte(`{"data":{"children":[
		{"data":{"id":"1","title":"sticky A","permalink":"/a","stickied":true}},
		{"data":{"id":"2","title":"hot X","permalink":"/x","stickied":false}},
		{"data":{"id":"3","title":"sticky B","permalink":"/b","stickied":true}},
		{"data":{"id":"4","title":"hot Y","permalink":"/y","stickied":false}}
	]}}`)
	out, err := parseThreadsJSON(body)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{out[0].id, out[1].id, out[2].id, out[3].id}
	want := []string{"1", "3", "2", "4"} // stickies first, original relative order preserved
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestRetryAfter(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"5", 5 * time.Second},
		{"100", 30 * time.Second}, // capped
		{"", 0},
		{"garbage", 0},
	}
	for _, c := range cases {
		if got := retryAfter(c.in); got != c.want {
			t.Errorf("retryAfter(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestSparkline(t *testing.T) {
	if got := sparkline([]int{0, 0, 0}, 0); got != "▁▁▁" {
		t.Errorf("sparkline(zeros) = %q, want ▁▁▁", got)
	}
	got := []rune(sparkline([]int{0, 10}, 10))
	if got[0] != '▁' || got[1] != '█' {
		t.Errorf("sparkline = %q, want ▁ then █", string(got))
	}
}

func TestPushSub(t *testing.T) {
	subs := pushSub([]string{"a", "b"}, "b") // move b to front, dedup
	if len(subs) != 2 || subs[0] != "b" {
		t.Errorf("pushSub dedup/front = %v", subs)
	}
	var many []string
	for i := 0; i < maxRecents+3; i++ {
		many = pushSub(many, fmt.Sprintf("s%d", i))
	}
	if len(many) != maxRecents {
		t.Errorf("pushSub len = %d, want cap %d", len(many), maxRecents)
	}
	if many[0] != fmt.Sprintf("s%d", maxRecents+2) {
		t.Errorf("newest should be at front, got %q", many[0])
	}
}

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v0.1.1", "v0.1.2", true},
		{"v0.1.1", "v0.1.1", false},
		{"v0.1.2", "v0.1.1", false},
		{"v0.1.1", "v0.2.0", true},
		{"v0.1.1", "v1.0.0", true},
		{"v0.9.9", "v0.10.0", true},     // numeric compare, not lexical
		{"v1.2.3", "v1.2.3-rc1", false}, // pre-release suffix ignored → equal
	}
	for _, c := range cases {
		if got := newerVersion(c.a, c.b); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestPushThread(t *testing.T) {
	var ts []recentThread
	ts = pushThread(ts, recentThread{URL: "u1", Title: "one"})
	ts = pushThread(ts, recentThread{URL: "u2", Title: "two"})
	ts = pushThread(ts, recentThread{URL: "u1", Title: "one again"}) // dedup by URL, move to front
	if len(ts) != 2 || ts[0].URL != "u1" {
		t.Errorf("pushThread dedup/front = %v", ts)
	}
}

func TestFuzzyScore(t *testing.T) {
	if _, ok := fuzzyScore("hello world", "hlo"); !ok {
		t.Error("hlo should match hello world as a subsequence")
	}
	if _, ok := fuzzyScore("hello", "xyz"); ok {
		t.Error("xyz should not match hello")
	}
	// A contiguous run should score higher than the same chars scattered.
	cont, _ := fuzzyScore("abcdef", "abc")
	scat, _ := fuzzyScore("axbxcx", "abc")
	if cont <= scat {
		t.Errorf("contiguous score %d should beat scattered %d", cont, scat)
	}
	// Regression: non-ASCII titles must match (the old byte-vs-rune comparison
	// silently dropped any thread with accented or non-Latin characters).
	if _, ok := fuzzyScore("café", "é"); !ok {
		t.Error("é should match café (unicode)")
	}
	if _, ok := fuzzyScore("münchen", "mü"); !ok {
		t.Error("mü should match münchen (unicode)")
	}
}

func TestURLBuilders(t *testing.T) {
	if got := cleanSubreddit("  /r/Soccer/ "); got != "Soccer" {
		t.Errorf("cleanSubreddit = %q, want Soccer", got)
	}
	if got := cleanSubreddit("r/golang"); got != "golang" {
		t.Errorf("cleanSubreddit = %q, want golang", got)
	}

	if got := threadsJSONURL("soccer", sortNew); got != "https://www.reddit.com/r/soccer/new.json?limit=100&raw_json=1" {
		t.Errorf("threadsJSONURL new = %q", got)
	}
	if got := threadsJSONURL("soccer", sortTop); !strings.Contains(got, "/top.json") || !strings.Contains(got, "t=day") {
		t.Errorf("threadsJSONURL top = %q, want /top.json and t=day", got)
	}
	if got := threadsRSSURL("soccer", sortHot); got != "https://www.reddit.com/r/soccer/.rss" {
		t.Errorf("threadsRSSURL hot = %q", got)
	}
	if got := threadsRSSURL("soccer", sortNew); got != "https://www.reddit.com/r/soccer/new/.rss" {
		t.Errorf("threadsRSSURL new = %q", got)
	}

	// jsonURL accepts a full thread URL or a bare path and normalizes suffixes.
	want := "https://www.reddit.com/r/soccer/comments/abc/title.json?sort=new&limit=200&raw_json=1"
	if got := jsonURL("https://www.reddit.com/r/soccer/comments/abc/title/"); got != want {
		t.Errorf("jsonURL = %q, want %q", got, want)
	}
	if got := rssURL("/r/soccer/comments/abc/title/"); got != "https://www.reddit.com/r/soccer/comments/abc/title/.rss" {
		t.Errorf("rssURL = %q", got)
	}
}

func TestCleanContent(t *testing.T) {
	if got := cleanContent("<b>hi</b>  &amp; bye"); got != "hi & bye" {
		t.Errorf("cleanContent = %q, want %q", got, "hi & bye")
	}
}

func TestCleanSelftext(t *testing.T) {
	// Decodes entities and collapses 3+ blank lines to one, but keeps a paragraph break.
	got := cleanSelftext("para1\n\n\n\npara2 &amp; more")
	want := "para1\n\npara2 & more"
	if got != want {
		t.Errorf("cleanSelftext = %q, want %q", got, want)
	}
}

func TestEmojiReserveRestore(t *testing.T) {
	in := "goal 🎉 and ⚽ but text arrows ↑ ↓ → stay"
	reserved, picked := reserveEmoji(in)
	if len(picked) != 2 {
		t.Errorf("picked %d emoji, want 2 (🎉 ⚽); arrows must not count: %v", len(picked), picked)
	}
	if got := restoreEmoji(reserved, picked); got != in {
		t.Errorf("emoji round-trip = %q, want %q", got, in)
	}
}

func TestIsEmojiCluster(t *testing.T) {
	cases := []struct {
		r    rune
		want bool
	}{
		{'🎉', true},  // pictographic supplement
		{'⚽', true},  // misc symbols
		{'★', true},  // black star (in 0x2600–0x27BF)
		{'↑', false}, // text arrow, must stay
		{'→', false}, // text arrow, must stay
		{'a', false},
	}
	for _, c := range cases {
		if got := isEmojiCluster([]rune{c.r}); got != c.want {
			t.Errorf("isEmojiCluster(%q) = %v, want %v", c.r, got, c.want)
		}
	}
}

func TestDemoPoolShuffleBag(t *testing.T) {
	lines := []string{"a", "b", "c", "d", "e"}
	p := newPool(lines...)
	n := len(lines)

	var prev string
	for pass := 0; pass < 2; pass++ {
		seen := map[string]int{}
		for i := 0; i < n; i++ {
			got := p.next()
			if pass == 1 && i == 0 && got == prev {
				t.Errorf("line %q repeated across the reshuffle boundary", got)
			}
			seen[got]++
			prev = got
		}
		for _, l := range lines {
			if seen[l] != 1 {
				t.Errorf("pass %d: %q appeared %d times, want exactly 1 (not a permutation)", pass, l, seen[l])
			}
		}
	}
}

func TestEnqueueDedupAndCap(t *testing.T) {
	newM := func() *model {
		return &model{seen: map[string]bool{}, byName: map[string]comment{}, scores: map[string]int{}}
	}

	m := newM()
	m.enqueue([]comment{{id: "a"}, {id: "b"}})
	m.enqueue([]comment{{id: "b"}, {id: "c"}}) // b is a duplicate
	if len(m.pending) != 3 {
		t.Errorf("pending = %d, want 3 (b deduped)", len(m.pending))
	}

	// The first batch on an empty feed is capped to initialCap, keeping the newest.
	m2 := newM()
	big := make([]comment, initialCap+10)
	for i := range big {
		big[i] = comment{id: fmt.Sprintf("c%d", i)}
	}
	m2.enqueue(big)
	if len(m2.pending) != initialCap {
		t.Errorf("first batch pending = %d, want cap %d", len(m2.pending), initialCap)
	}
	if m2.pending[0].id != "c10" {
		t.Errorf("cap kept the wrong slice: first id %q, want c10 (newest %d)", m2.pending[0].id, initialCap)
	}
}
