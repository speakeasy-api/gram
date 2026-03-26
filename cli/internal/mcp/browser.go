package mcp

import (
	"fmt"
	"os/exec"
	"runtime"
)

// OpenURL opens a URL using the system's default handler
// Used for deep links like cursor://
func OpenURL(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url) //#nosec G204 -- url is passed to system browser opener
	case "linux":
		cmd = exec.Command("xdg-open", url) //#nosec G204 -- url is passed to system browser opener
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url) //#nosec G204 -- url is passed to system browser opener
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to open URL: %w", err)
	}

	return nil
}
