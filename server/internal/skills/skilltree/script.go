package skilltree

import "strings"

// scriptsDir is the skill directory the Agent Skills specification reserves
// for executable code.
const scriptsDir = "scripts"

// IsScript reports whether a path relative to a skill directory is in its
// scripts/ directory. Every surface that writes a skill to a filesystem marks
// those files executable, whatever their extension.
func IsScript(p string) bool {
	return strings.HasPrefix(p, scriptsDir+"/")
}

// IsPackagedScript reports whether a path in a plugin package is in the
// scripts/ directory of a skill the package carries, that is, a path of the
// form .../skills/<name>/scripts/.... The plugin ZIP writer and the GitHub
// publisher both use it, so the two distribution surfaces mark the same files
// executable.
func IsPackagedScript(p string) bool {
	segments := strings.Split(p, "/")
	for i := 0; i+3 < len(segments); i++ {
		if segments[i] == "skills" && segments[i+2] == scriptsDir {
			return true
		}
	}
	return false
}
