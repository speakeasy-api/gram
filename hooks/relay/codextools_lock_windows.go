//go:build windows

package relay

import "os"

// Windows has no flock; the queue operates unlocked there, the bash sender's
// posture. Mis-correlation needs concurrent same-tool codex hooks on Windows,
// and the queue self-heals as entries drain.
func lockFile(*os.File) error { return nil }

func unlockFile(*os.File) {}
