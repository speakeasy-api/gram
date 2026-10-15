package functions

import (
	"os"
	"path/filepath"
	"strings"
)

// Package managers that can install and run a functions project.
const (
	PackageManagerNPM  = "npm"
	PackageManagerPNPM = "pnpm"
	PackageManagerYarn = "yarn"
	PackageManagerBun  = "bun"
)

// lockfiles maps each lockfile to the package manager that writes it, in the
// order they are checked.
var lockfiles = []struct {
	name           string
	packageManager string
}{
	{"pnpm-lock.yaml", PackageManagerPNPM},
	{"yarn.lock", PackageManagerYarn},
	{"bun.lock", PackageManagerBun},
	{"bun.lockb", PackageManagerBun},
	{"package-lock.json", PackageManagerNPM},
}

// DetectPackageManager picks the package manager for the project in dir. A
// lockfile in dir wins, because it records what the project already uses.
// Without one, the package manager that launched this process (from
// npm_config_user_agent) is used, then npm.
func DetectPackageManager(dir string, userAgent string) string {
	for _, lf := range lockfiles {
		if _, err := os.Stat(filepath.Join(dir, lf.name)); err == nil {
			return lf.packageManager
		}
	}

	name, _, _ := strings.Cut(userAgent, "/")
	switch name {
	case PackageManagerNPM, PackageManagerPNPM, PackageManagerYarn, PackageManagerBun:
		return name
	default:
		return PackageManagerNPM
	}
}

// runScriptArgs returns the arguments that make pm run script with extra
// arguments. npm needs a "--" separator to forward them; the others forward
// them as-is.
func runScriptArgs(pm string, script string, extra []string) []string {
	args := []string{"run", script}
	if len(extra) == 0 {
		return args
	}
	if pm == PackageManagerNPM {
		args = append(args, "--")
	}
	return append(args, extra...)
}
