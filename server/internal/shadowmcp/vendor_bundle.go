package shadowmcp

import (
	"path"
	"regexp"
	"strings"
)

// vendorBundleRoots are the path prefixes of coding agents' own application
// bundles and managed directories. An MCP server whose launch command runs an
// executable under one of them ships with the agent rather than being
// installed by the user. Home-relative roots start with "~/"; commands are
// normalized to that form before they are compared.
var vendorBundleRoots = []string{
	"/Applications/ChatGPT.app/",
	"/Applications/Codex.app/",
	"/Applications/Claude.app/",
	"/Applications/Cursor.app/",

	// The Claude desktop app's managed directory, which holds the Claude Code
	// runtime and helpers the app installs and updates itself.
	"~/Library/Application Support/Claude/",

	// Codex starts its bundled computer use helper relative to the bundled
	// plugin directory it ships.
	"./Codex Computer Use.app/",
}

// codexBundledPluginCacheSegment is the directory Codex installs its own
// bundled plugins into, under whichever Codex home the user runs.
const codexBundledPluginCacheSegment = "/.codex/plugins/cache/openai-bundled/"

// shellNames are the shells whose "-c <script>" form wraps a launch command.
var shellNames = map[string]struct{}{
	"sh":   {},
	"bash": {},
	"zsh":  {},
	"dash": {},
}

// shellOptionPattern matches a bundled single-letter shell option group that
// includes -c (-c, -lc, -ec), after which the shell runs a script argument.
var shellOptionPattern = regexp.MustCompile(`^-[a-z]*c[a-z]*$`)

// envAssignmentPattern matches a NAME=VALUE environment assignment whose value
// carries no command substitution, so assigning it runs nothing.
var envAssignmentPattern = regexp.MustCompile("^[A-Za-z_][A-Za-z0-9_]*=[^`$()|&<>]*$")

// shellControlPattern matches shell syntax that runs or redirects another
// command. A script holding it could run something besides the executable.
var shellControlPattern = regexp.MustCompile("[;&|`<>\n]|\\$\\(")

// IsVendorBundleCommand reports whether an MCP launch command runs an
// executable shipped inside a coding agent's own bundle or managed directory.
//
// Commands arrive as the hook relay reports them: quotes removed, secret
// values masked, paths possibly home-relative, and possibly wrapped in a shell
// ("/bin/sh -c export NAME=value; exec /Applications/...") or an env prefix.
// The executable is unwrapped from those shapes; any other shape, a script
// that could run a second command, or a path that climbs out with ".." is not
// a vendor bundle.
func IsVendorBundleCommand(command string) bool {
	executable, ok := commandExecutable(command)
	if !ok {
		return false
	}
	return isVendorBundlePath(executable)
}

// commandToken is one whitespace-separated token of a command and the byte
// offset it starts at, so the rest of the command can be sliced with its
// original spacing (paths such as "Application Support" contain spaces).
type commandToken struct {
	text  string
	start int
}

func tokenizeCommand(command string) []commandToken {
	var tokens []commandToken
	start := -1
	for i, r := range command {
		isSpace := r == ' ' || r == '\t' || r == '\n' || r == '\r'
		if isSpace && start >= 0 {
			tokens = append(tokens, commandToken{text: command[start:i], start: start})
			start = -1
			continue
		}
		if !isSpace && start < 0 {
			start = i
		}
	}
	if start >= 0 {
		tokens = append(tokens, commandToken{text: command[start:], start: start})
	}
	return tokens
}

// commandExecutable returns the command text from the executable onward,
// unwrapping an env prefix and a shell -c script.
func commandExecutable(command string) (string, bool) {
	tokens := tokenizeCommand(command)
	i := 0
	if i < len(tokens) && path.Base(tokens[i].text) == "env" {
		i++
		for i < len(tokens) && envAssignmentPattern.MatchString(tokens[i].text) {
			i++
		}
	}
	if i >= len(tokens) {
		return "", false
	}

	if _, isShell := shellNames[path.Base(tokens[i].text)]; !isShell {
		return command[tokens[i].start:], true
	}
	if i+1 >= len(tokens) || !shellOptionPattern.MatchString(tokens[i+1].text) {
		return "", false
	}
	return scriptExecutable(command, tokens[i+2:])
}

// scriptExecutable returns the command a shell script runs, after any export
// and assignment statements and an optional exec. The rest of the script must
// hold no shell control syntax, so the executable is the only command it runs.
func scriptExecutable(command string, tokens []commandToken) (string, bool) {
	for i, token := range tokens {
		text := strings.TrimSuffix(token.text, ";")
		switch {
		case text == "" || text == "export":
			continue
		case envAssignmentPattern.MatchString(text):
			continue
		case text == "exec":
			if i+1 >= len(tokens) {
				return "", false
			}
			return plainScriptCommand(command[tokens[i+1].start:])
		default:
			return plainScriptCommand(command[token.start:])
		}
	}
	return "", false
}

func plainScriptCommand(script string) (string, bool) {
	if shellControlPattern.MatchString(script) {
		return "", false
	}
	return script, true
}

// homePrefixPattern matches the ways a launch command spells the user's home
// directory, so home-relative vendor roots compare equal.
var homePrefixPattern = regexp.MustCompile(`^(~|\$HOME|\$\{HOME\}|/Users/[^/]+|/home/[^/]+)/`)

func isVendorBundlePath(executable string) bool {
	executable = strings.TrimSpace(executable)
	for _, token := range tokenizeCommand(executable) {
		if climbsOut(token.text) {
			return false
		}
	}
	normalized := homePrefixPattern.ReplaceAllString(executable, "~/")
	for _, root := range vendorBundleRoots {
		if strings.HasPrefix(normalized, root) {
			return true
		}
	}

	program, _, _ := strings.Cut(normalized, " ")
	return strings.Contains(program, codexBundledPluginCacheSegment)
}

func climbsOut(token string) bool {
	return token == ".." || strings.HasPrefix(token, "../") || strings.Contains(token, "/../") || strings.HasSuffix(token, "/..")
}
