package relay

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/speakeasy-api/agenthooks"
)

// Codex has no structured skill signal: implicit activations surface as a
// reader tool opening a skills/<name>/SKILL.md path, and explicit $skill-name
// prompt mentions are expanded internally without any tool call. Both are
// inferred best-effort here and attached as data.skill while the event keeps
// its true type on the wire — reclassifying would skip the server's
// tool/prompt policy scan, so the server layers the skill.activated
// classification on top instead.

var (
	codexSkillPathRE  = regexp.MustCompile(`skills/(?:\.system/)?([A-Za-z0-9][A-Za-z0-9._-]*)/SKILL\.md`)
	codexSkillTokenRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

// codexToolSkillName infers an implicit activation from a reader tool opening
// a SKILL.md path. Callers gate on the pre-tool kind: completions must not
// re-report the activation and permission previews may still be denied.
func codexToolSkillName(tool *agenthooks.ToolCall) string {
	switch tool.Name {
	case "Bash", "shell", "Read":
	default:
		return ""
	}
	matches := codexSkillPathRE.FindAllStringSubmatch(string(tool.Input), -1)
	if len(matches) == 0 {
		return ""
	}
	// The bash senders' greedy sed match resolved the last occurrence; keep
	// that so both channels report the same skill for one command.
	return matches[len(matches)-1][1]
}

// codexPromptSkillName infers an explicit activation from a $skill-name
// mention in the submitted prompt. A candidate counts only when it resolves to
// a skill directory on disk, so dollar amounts and env-var mentions are
// ignored; candidates are tried in sorted order and the first that resolves
// wins, matching the bash senders.
func codexPromptSkillName(prompt, cwd string) string {
	if !strings.Contains(prompt, "$") {
		return ""
	}
	fields := strings.FieldsFunc(prompt, func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return false
		case r == '.', r == '_', r == '$', r == '-':
			return false
		default:
			return true
		}
	})
	seen := map[string]bool{}
	names := []string{}
	for _, f := range fields {
		name, ok := strings.CutPrefix(f, "$")
		if !ok || !codexSkillTokenRE.MatchString(name) {
			continue
		}
		// Sentence-final punctuation survives tokenization ("use $foo.").
		name = strings.TrimRight(name, ".")
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if codexSkillExists(name, cwd) {
			return name
		}
	}
	return ""
}

// codexSkillExists validates a candidate skill name against the directories
// Codex discovers skills from: the user root, the admin and Codex-home roots
// (whose bundled skills live under a .system subdirectory but are mentioned by
// bare name), and .agents/skills walking up from the session cwd.
func codexSkillExists(name, cwd string) bool {
	if name == "" || strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		return false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	if home != "" && skillManifestExists(filepath.Join(home, ".agents", "skills", name)) {
		return true
	}
	codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if codexHome == "" && home != "" {
		codexHome = filepath.Join(home, ".codex")
	}
	roots := []string{"/etc/codex/skills", "/opt/codex/skills"}
	if codexHome != "" {
		roots = append(roots, filepath.Join(codexHome, "skills"))
	}
	for _, root := range roots {
		if skillManifestExists(filepath.Join(root, name)) || skillManifestExists(filepath.Join(root, ".system", name)) {
			return true
		}
	}
	for dir := cwd; dir != "" && dir != "/" && dir != "."; {
		if skillManifestExists(filepath.Join(dir, ".agents", "skills", name)) {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return false
}

func skillManifestExists(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "SKILL.md"))
	return err == nil && !info.IsDir()
}
