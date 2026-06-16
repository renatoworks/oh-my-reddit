package main

import (
	"os/exec"
	"runtime"
)

// openBrowser opens url in the user's default browser, returning any error from
// launching the opener. No-op (nil) on empty url.
func openBrowser(url string) error {
	if url == "" {
		return nil
	}
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{"--", url} // -- so a "-"-leading URL isn't read as a flag
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		name, args = "xdg-open", []string{"--", url}
	}
	return exec.Command(name, args...).Start()
}
