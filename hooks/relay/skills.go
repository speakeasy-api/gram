package relay

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/speakeasy-api/agenthooks"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

const maxSkillContentBytes = 65_536

var skillTokenRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type resolvedSkill struct {
	name         string
	rawSHA256    string
	content      string
	captureReady bool
}

func resolveActivatedSkill(typed any, payload *components.IngestRequestBody) *resolvedSkill {
	if payload == nil || payload.Data == nil || payload.Data.Skill == nil {
		return nil
	}

	result := &resolvedSkill{name: payload.Data.Skill.Name}
	activation := agenthooks.SkillActivationOf(typed)
	if activation == nil || !activation.ContentAvailable || len(activation.Content) > maxSkillContentBytes || !utf8.ValidString(activation.Content) {
		return result
	}
	digest := sha256.Sum256([]byte(activation.Content))
	result.rawSHA256 = hex.EncodeToString(digest[:])
	result.content = activation.Content
	result.captureReady = true
	return result
}

// Codex has no structured signal for explicit $skill-name prompt activations.
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
	for _, field := range fields {
		name, ok := strings.CutPrefix(field, "$")
		if !ok || !skillTokenRE.MatchString(name) {
			continue
		}
		name = strings.TrimRight(name, ".")
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if codexSkillExists(name, cwd) {
			return name
		}
	}
	return ""
}

func codexSkillExists(name, cwd string) bool {
	if name == "" || strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		return false
	}
	home, _ := os.UserHomeDir()
	roots := []string{
		"/etc/codex/skills", filepath.Join("/etc/codex/skills", ".system"),
		"/opt/codex/skills", filepath.Join("/opt/codex/skills", ".system"),
	}
	if home != "" {
		personal := filepath.Join(home, ".agents", "skills")
		roots = append(roots, personal, filepath.Join(personal, ".system"))
	}
	codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if codexHome == "" && home != "" {
		codexHome = filepath.Join(home, ".codex")
	}
	if codexHome != "" {
		roots = append(roots, filepath.Join(codexHome, "skills"), filepath.Join(codexHome, "skills", ".system"))
	}
	for _, root := range roots {
		if readableRegularFile(filepath.Join(root, name, "SKILL.md")) {
			return true
		}
	}
	if filepath.IsAbs(cwd) {
		for dir := cwd; ; dir = filepath.Dir(dir) {
			if readableRegularFile(filepath.Join(dir, ".agents", "skills", name, "SKILL.md")) {
				return true
			}
			if pathExists(filepath.Join(dir, ".git")) || filepath.Dir(dir) == dir {
				break
			}
		}
	}
	return false
}

func readableRegularFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	openedInfo, statErr := file.Stat()
	closeErr := file.Close()
	return statErr == nil && openedInfo.Mode().IsRegular() && closeErr == nil
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func pathWithin(path, root string) bool {
	if path == "" || root == "" {
		return false
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
