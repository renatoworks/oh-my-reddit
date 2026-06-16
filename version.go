package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
)

// updateProxyURL is the Go module proxy endpoint reporting the version that
// `go install <repoModule>@latest` resolves to. We use the proxy (not the GitHub
// API) for the update check: it's Go-native, built for scale, and has no
// per-app rate limit — unauthenticated GitHub is only 60/hour per IP.
const updateProxyURL = "https://proxy.golang.org/" + repoModule + "/@latest"

// buildVersion returns this binary's module version (e.g. "v0.1.1"), or "" for a
// local/dev build ("(devel)"), where checking for an update doesn't apply.
func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	switch v := info.Main.Version; v {
	case "", "(devel)":
		return ""
	default:
		return v
	}
}

// latestVersion asks the Go module proxy for the latest released version.
func latestVersion() (string, error) {
	resp, err := httpClient.Get(updateProxyURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("proxy returned %s", resp.Status)
	}
	var out struct{ Version string }
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Version, nil
}

// newerVersion reports whether b is a strictly higher vMAJOR.MINOR.PATCH than a.
// Pre-release/build suffixes are ignored.
func newerVersion(a, b string) bool {
	pa, pb := parseSemver(a), parseSemver(b)
	for i := 0; i < 3; i++ {
		if pb[i] != pa[i] {
			return pb[i] > pa[i]
		}
	}
	return false
}

func parseSemver(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i] // drop pre-release/build metadata
	}
	var out [3]int
	parts := strings.SplitN(v, ".", 3)
	for i := 0; i < len(parts) && i < 3; i++ {
		out[i], _ = strconv.Atoi(parts[i])
	}
	return out
}
