package ui

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

// openBrowser launches the OS default browser at url. Only https URLs are
// accepted to avoid passing arbitrary strings to exec.
func openBrowser(url string) error {
	if !strings.HasPrefix(url, "https://") {
		return errors.New("refusing to open non-https url")
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
