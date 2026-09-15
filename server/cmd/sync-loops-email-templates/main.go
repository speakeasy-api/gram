// Command sync-loops-email-templates is a compatibility launcher for release
// automation that has not yet switched to the Bash reconciler directly.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

func main() {
	script, err := findScript(os.Args[1:])
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	args := append([]string{"bash", script}, os.Args[1:]...)
	if err := syscall.Exec(bash, args, os.Environ()); err != nil { // #nosec G204 G702 -- arguments come from the operator invoking this compatibility command
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func findScript(args []string) (string, error) {
	_, source, _, _ := runtime.Caller(0)
	candidates := []string{
		scriptBesideManifest(args),
		"server/internal/email/loops/sync.sh",
		"internal/email/loops/sync.sh",
		filepath.Join(filepath.Dir(source), "..", "..", "internal", "email", "loops", "sync.sh"),
	}
	for _, candidate := range candidates {
		path, err := filepath.Abs(candidate)
		if err == nil {
			if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
				return path, nil
			}
		}
	}
	return "", fmt.Errorf("find Loops sync script: run from the repository root or pass a manifest beside sync.sh")
}

func scriptBesideManifest(args []string) string {
	for i, arg := range args {
		if arg == "--manifest" && i+1 < len(args) {
			return filepath.Join(filepath.Dir(args[i+1]), "sync.sh")
		}
		if manifest, ok := strings.CutPrefix(arg, "--manifest="); ok {
			return filepath.Join(filepath.Dir(manifest), "sync.sh")
		}
	}
	return ""
}
