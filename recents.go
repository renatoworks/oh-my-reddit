package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const maxRecents = 6

// recentThread is a thread the user has opened before.
type recentThread struct {
	Sub   string `json:"sub"`
	URL   string `json:"url"`
	Title string `json:"title"`
}

// recentsFile is the on-disk shape, persisted under the user config dir.
type recentsFile struct {
	Subs    []string       `json:"subs"`
	Threads []recentThread `json:"threads"`
}

func recentsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "oh-my-reddit", "recents.json")
}

func loadRecents() ([]string, []recentThread) {
	p := recentsPath()
	if p == "" {
		return nil, nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, nil
	}
	var rf recentsFile
	if json.Unmarshal(data, &rf) != nil {
		return nil, nil
	}
	return rf.Subs, rf.Threads
}

func saveRecents(subs []string, threads []recentThread) {
	p := recentsPath()
	if p == "" {
		return
	}
	if os.MkdirAll(filepath.Dir(p), 0o700) != nil {
		return
	}
	data, err := json.MarshalIndent(recentsFile{Subs: subs, Threads: threads}, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(p, data, 0o600)
}

// pushSub moves sub to the front, dedupes, and caps the list.
func pushSub(subs []string, sub string) []string {
	out := []string{sub}
	for _, s := range subs {
		if s != sub {
			out = append(out, s)
		}
	}
	if len(out) > maxRecents {
		out = out[:maxRecents]
	}
	return out
}

// pushThread moves a thread (by URL) to the front, dedupes, and caps the list.
func pushThread(threads []recentThread, t recentThread) []recentThread {
	out := []recentThread{t}
	for _, x := range threads {
		if x.URL != t.URL {
			out = append(out, x)
		}
	}
	if len(out) > maxRecents {
		out = out[:maxRecents]
	}
	return out
}
