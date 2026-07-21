package relay

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/speakeasy-api/agenthooks"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

const (
	maxSkillContentBytes        = 65_536
	maxClaudePluginRegistrySize = 1 << 20
)

var skillTokenRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

var claudeManagedSkillsRoot = platformClaudeManagedSkillsRoot

type resolvedSkill struct {
	name         string
	sourceLevel  string
	sourcePath   string
	rawSHA256    string
	content      string
	captureReady bool
	root         string
}

type skillLocation struct {
	path  string
	level string
	root  string
}

// resolveActivatedSkill resolves and captures the manifest for a skill already
// detected in the ingest payload. Resolution failures deliberately preserve the
// activation name without guessing at a different source.
func resolveActivatedSkill(typed any, payload *components.IngestRequestBody) *resolvedSkill {
	if payload == nil || payload.Data == nil || payload.Data.Skill == nil {
		return nil
	}

	result := &resolvedSkill{name: payload.Data.Skill.Name}
	base := agenthooks.EventOf(typed)
	if base == nil {
		return result
	}

	var location skillLocation
	switch base.Provider {
	case agenthooks.ProviderClaudeCode:
		location = resolveClaudeSkill(result.name, base.Session.CWD)
	case agenthooks.ProviderCodex:
		switch event := typed.(type) {
		case *agenthooks.ToolPreEvent:
			location = resolveCodexToolSkill(&event.Tool, base.Session.CWD)
		case *agenthooks.PromptEvent:
			_, location = codexPromptSkill(event.Prompt, base.Session.CWD)
		}
	case agenthooks.ProviderCursor:
		if event, ok := typed.(*agenthooks.ToolPreEvent); ok {
			location = resolveCursorToolSkill(&event.Tool, base.Session.CWD, base.Session.WorkspaceRoots)
		}
	}
	if location.path == "" {
		return result
	}
	return captureResolvedSkill(result, location)
}

func captureResolvedSkill(result *resolvedSkill, location skillLocation) *resolvedSkill {
	file, ok := openValidatedSkill(location.path, location.root)
	if !ok {
		return result
	}
	hasher := sha256.New()
	content, readErr := io.ReadAll(io.LimitReader(io.TeeReader(file, hasher), maxSkillContentBytes+1))
	if readErr == nil {
		// Capture stays bounded, but metadata requires the exact full-file hash.
		_, readErr = io.Copy(io.Discard, io.TeeReader(file, hasher))
	}
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return result
	}

	result.sourceLevel = location.level
	result.sourcePath = filepath.Clean(location.path)
	result.rawSHA256 = hex.EncodeToString(hasher.Sum(nil))
	result.root = filepath.Clean(location.root)
	if len(content) > maxSkillContentBytes || !utf8.Valid(content) {
		return result
	}

	result.content = string(content)
	result.captureReady = true
	return result
}

// Codex has no structured skill signal: implicit activations surface as a
// reader tool opening a skills/<name>/SKILL.md path, and explicit $skill-name
// prompt mentions are expanded internally without any tool call.

// codexToolSkillName infers an implicit activation from a reader tool opening
// a SKILL.md path. Callers gate on the pre-tool kind.
func codexToolSkillName(tool *agenthooks.ToolCall) string {
	_, name := codexToolSkillPath(tool, "")
	return name
}

func codexToolSkillPath(tool *agenthooks.ToolCall, cwd string) (string, string) {
	switch tool.Name {
	case "Read":
		path := readToolPath(tool.Input)
		name := skillNameFromManifestPath(path, true)
		if name == "" {
			return "", ""
		}
		return absoluteSessionPath(path, cwd), name
	case "Bash", "shell":
		var input struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(tool.Input, &input) != nil {
			return "", ""
		}
		var path, name string
		for _, token := range shellTokens(input.Command) {
			if candidate := skillNameFromManifestPath(token, true); candidate != "" {
				path, name = token, candidate
			}
		}
		return absoluteSessionPath(path, cwd), name
	default:
		return "", ""
	}
}

func resolveCodexToolSkill(tool *agenthooks.ToolCall, cwd string) skillLocation {
	path, _ := codexToolSkillPath(tool, cwd)
	if path == "" {
		return skillLocation{}
	}
	level, root := classifyCodexSkill(path, cwd)
	if level == "" {
		return skillLocation{}
	}
	return skillLocation{path: path, level: level, root: root}
}

func cursorToolSkillName(tool *agenthooks.ToolCall, cwd string, workspaceRoots []string) string {
	location := resolveCursorToolSkill(tool, cwd, workspaceRoots)
	return skillNameFromManifestPath(location.path, false)
}

func cursorToolSkillPath(tool *agenthooks.ToolCall, cwd string) (string, string) {
	if tool.Name != "Read" {
		return "", ""
	}
	path := readToolPath(tool.Input)
	name := skillNameFromManifestPath(path, false)
	if name == "" {
		return "", ""
	}
	return absoluteSessionPath(path, cwd), name
}

func readToolPath(input json.RawMessage) string {
	var paths struct {
		FilePath string `json:"file_path"`
		Path     string `json:"path"`
	}
	if json.Unmarshal(input, &paths) != nil {
		return ""
	}
	if paths.FilePath != "" {
		return paths.FilePath
	}
	return paths.Path
}

func resolveCursorToolSkill(tool *agenthooks.ToolCall, cwd string, workspaceRoots []string) skillLocation {
	path, _ := cursorToolSkillPath(tool, cwd)
	if path == "" {
		return skillLocation{}
	}
	level, root := classifyCursorSkill(path, workspaceRoots)
	if level == "" {
		return skillLocation{}
	}
	return skillLocation{path: path, level: level, root: root}
}

func skillNameFromManifestPath(path string, allowSystem bool) string {
	slashed := strings.ReplaceAll(path, `\`, "/")
	parts := strings.Split(slashed, "/")
	if len(parts) < 3 || parts[len(parts)-1] != "SKILL.md" {
		return ""
	}
	nameIndex := len(parts) - 2
	skillsIndex := nameIndex - 1
	if allowSystem && parts[skillsIndex] == ".system" {
		skillsIndex--
	}
	if skillsIndex < 0 || parts[skillsIndex] != "skills" || !skillTokenRE.MatchString(parts[nameIndex]) {
		return ""
	}
	return parts[nameIndex]
}

// codexPromptSkillName preserves the existing sorted-name inference behavior.
func codexPromptSkillName(prompt, cwd string) string {
	name, _ := codexPromptSkill(prompt, cwd)
	return name
}

func codexPromptSkill(prompt, cwd string) (string, skillLocation) {
	if !strings.Contains(prompt, "$") {
		return "", skillLocation{}
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
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		locations := codexSkillLocations(name, cwd)
		if len(locations) > 0 {
			if len(locations) == 1 {
				return name, locations[0]
			}
			return name, skillLocation{}
		}
	}
	return "", skillLocation{}
}

func codexSkillExists(name, cwd string) bool {
	return len(codexSkillLocations(name, cwd)) > 0
}

func codexSkillLocations(name, cwd string) []skillLocation {
	if name == "" || strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		return nil
	}
	var locations []skillLocation
	seen := map[string]bool{}
	add := func(path, level, root string) {
		if path == "" {
			return
		}
		clean := filepath.Clean(path)
		if seen[clean] || !readableRegularFile(clean) {
			return
		}
		seen[clean] = true
		locations = append(locations, skillLocation{path: clean, level: level, root: root})
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		root := filepath.Join(home, ".agents", "skills")
		add(existingSkillManifest(filepath.Join(root, name)), "personal", root)
	}
	codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if codexHome == "" && home != "" {
		codexHome = filepath.Join(home, ".codex")
	}
	roots := []skillLocation{
		{path: "/etc/codex/skills", level: "admin"},
		{path: "/opt/codex/skills", level: "system"},
	}
	if codexHome != "" {
		roots = append(roots, skillLocation{path: filepath.Join(codexHome, "skills"), level: "personal"})
	}
	for _, root := range roots {
		add(existingSkillManifest(filepath.Join(root.path, name)), root.level, root.path)
		level := root.level
		if codexHome != "" && root.path == filepath.Join(codexHome, "skills") {
			level = "system"
		}
		add(existingSkillManifest(filepath.Join(root.path, ".system", name)), level, root.path)
	}
	if filepath.IsAbs(cwd) {
		for dir := cwd; ; dir = filepath.Dir(dir) {
			root := filepath.Join(dir, ".agents", "skills")
			add(existingSkillManifest(filepath.Join(root, name)), "project", root)
			if pathExists(filepath.Join(dir, ".git")) {
				break
			}
			if parent := filepath.Dir(dir); parent == dir {
				break
			}
		}
	}
	return locations
}

func resolveClaudeSkill(name, cwd string) skillLocation {
	if plugin, skill, ok := strings.Cut(name, ":"); ok {
		if !skillTokenRE.MatchString(plugin) || !skillTokenRE.MatchString(skill) || strings.Contains(skill, ":") {
			return skillLocation{}
		}
		return resolveClaudePluginSkill(plugin, skill, cwd)
	}
	if !skillTokenRE.MatchString(name) {
		return skillLocation{}
	}
	if root := claudeManagedSkillsRoot(); root != "" {
		path := filepath.Join(root, name, "SKILL.md")
		info, err := os.Stat(path)
		switch {
		case err == nil && info.Mode().IsRegular():
			if readableRegularFile(path) {
				return skillLocation{path: path, level: "admin", root: root}
			}
			return skillLocation{}
		case err != nil && !os.IsNotExist(err):
			return skillLocation{}
		}
	}

	home, _ := os.UserHomeDir()
	configRoot := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR"))
	if configRoot == "" && home != "" {
		configRoot = filepath.Join(home, ".claude")
	}
	if configRoot != "" {
		root := filepath.Join(configRoot, "skills")
		if path := existingSkillManifest(filepath.Join(root, name)); path != "" {
			return skillLocation{path: path, level: "personal", root: root}
		}
	}
	if filepath.IsAbs(cwd) {
		for dir := cwd; ; dir = filepath.Dir(dir) {
			root := filepath.Join(dir, ".claude", "skills")
			if path := existingSkillManifest(filepath.Join(root, name)); path != "" {
				return skillLocation{path: path, level: "project", root: root}
			}
			if pathExists(filepath.Join(dir, ".git")) {
				break
			}
			if parent := filepath.Dir(dir); parent == dir {
				break
			}
		}
	}
	return skillLocation{}
}

func platformClaudeManagedSkillsRoot() string {
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/ClaudeCode/.claude/skills"
	case "linux":
		return "/etc/claude-code/.claude/skills"
	case "windows":
		return `C:\Program Files\ClaudeCode\.claude\skills`
	default:
		return ""
	}
}

func resolveClaudePluginSkill(plugin, skill, cwd string) skillLocation {
	home, _ := os.UserHomeDir()
	configRoot := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR"))
	if configRoot == "" && home != "" {
		configRoot = filepath.Join(home, ".claude")
	}
	if configRoot == "" {
		return skillLocation{}
	}
	registryFile, err := os.Open(filepath.Join(configRoot, "plugins", "installed_plugins.json"))
	if err != nil {
		return skillLocation{}
	}
	data, readErr := io.ReadAll(io.LimitReader(registryFile, maxClaudePluginRegistrySize+1))
	closeErr := registryFile.Close()
	if readErr != nil || closeErr != nil || len(data) > maxClaudePluginRegistrySize {
		return skillLocation{}
	}
	var registry struct {
		Version int `json:"version"`
		Plugins map[string][]struct {
			Scope       string `json:"scope"`
			ProjectPath string `json:"projectPath"`
			InstallPath string `json:"installPath"`
		} `json:"plugins"`
	}
	if json.Unmarshal(data, &registry) != nil || registry.Version != 2 {
		return skillLocation{}
	}

	type candidate struct {
		installPath string
		projectPath string
	}
	var projectCandidates, userCandidates []candidate
	for key, records := range registry.Plugins {
		prefix, _, _ := strings.Cut(key, "@")
		if prefix != plugin {
			continue
		}
		for _, record := range records {
			switch record.Scope {
			case "project", "local":
				if record.InstallPath != "" && pathWithin(cwd, record.ProjectPath) {
					projectCandidates = append(projectCandidates, candidate{installPath: record.InstallPath, projectPath: record.ProjectPath})
				}
			case "user":
				if record.InstallPath != "" {
					userCandidates = append(userCandidates, candidate{installPath: record.InstallPath})
				}
			}
		}
	}

	candidates := userCandidates
	if len(projectCandidates) > 0 {
		longest := 0
		for _, candidate := range projectCandidates {
			if len(filepath.Clean(candidate.projectPath)) > longest {
				longest = len(filepath.Clean(candidate.projectPath))
			}
		}
		candidates = nil
		for _, candidate := range projectCandidates {
			if len(filepath.Clean(candidate.projectPath)) == longest {
				candidates = append(candidates, candidate)
			}
		}
	}
	if len(candidates) != 1 {
		return skillLocation{}
	}
	for _, dir := range []string{filepath.Join(candidates[0].installPath, "skills", skill), candidates[0].installPath} {
		if path := existingSkillManifest(dir); path != "" {
			return skillLocation{path: path, level: "plugin", root: dir}
		}
	}
	return skillLocation{}
}

func classifyCursorSkill(path string, workspaceRoots []string) (string, string) {
	home, _ := os.UserHomeDir()
	for _, dir := range []string{".cursor", ".agents", ".claude", ".codex"} {
		root := filepath.Join(home, dir, "skills")
		if home != "" && pathWithin(path, root) {
			return "personal", root
		}
	}
	if root := pluginManifestAncestor(path, ".cursor-plugin", containingWorkspaceRoot(path, workspaceRoots)); root != "" {
		return "plugin", root
	}
	for _, root := range workspaceRoots {
		if skillRoot := providerSkillRoot(path, root); skillRoot != "" {
			return "project", skillRoot
		}
	}
	return "", ""
}

func classifyCodexSkill(path, cwd string) (string, string) {
	home, _ := os.UserHomeDir()
	root := filepath.Join(home, ".agents", "skills")
	if home != "" && pathWithin(path, root) {
		return "personal", root
	}
	if root = "/etc/codex/skills"; pathWithin(path, root) {
		return "admin", root
	}
	if root = "/opt/codex/skills"; pathWithin(path, root) {
		return "system", root
	}
	codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if codexHome == "" && home != "" {
		codexHome = filepath.Join(home, ".codex")
	}
	root = filepath.Join(codexHome, "skills")
	if codexHome != "" && pathWithin(path, filepath.Join(root, ".system")) {
		return "system", root
	}
	if filepath.IsAbs(cwd) {
		for dir := cwd; ; dir = filepath.Dir(dir) {
			root = filepath.Join(dir, ".agents", "skills")
			if pathWithin(path, root) {
				return "project", root
			}
			if pathExists(filepath.Join(dir, ".git")) {
				break
			}
			if parent := filepath.Dir(dir); parent == dir {
				break
			}
		}
	}
	root = filepath.Join(codexHome, "skills")
	if codexHome != "" && pathWithin(path, root) {
		return "personal", root
	}
	if root = pluginManifestAncestor(path, ".codex-plugin", ""); root != "" {
		return "plugin", root
	}
	return "", ""
}

func existingSkillManifest(dir string) string {
	path := filepath.Join(dir, "SKILL.md")
	info, err := os.Stat(path)
	if err == nil && info.Mode().IsRegular() {
		return path
	}
	return ""
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

func openValidatedSkill(path, root string) (*os.File, bool) {
	if !filepath.IsAbs(path) || !filepath.IsAbs(root) {
		return nil, false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsLocal(rel) {
		return nil, false
	}
	rootDir, err := os.OpenRoot(root)
	if err != nil {
		return nil, false
	}
	info, err := rootDir.Stat(rel)
	if err != nil || !info.Mode().IsRegular() {
		_ = rootDir.Close()
		return nil, false
	}
	file, err := rootDir.Open(rel)
	closeRootErr := rootDir.Close()
	if err != nil || closeRootErr != nil {
		if file != nil {
			_ = file.Close()
		}
		return nil, false
	}
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() {
		_ = file.Close()
		return nil, false
	}
	return file, true
}

func absoluteSessionPath(path, cwd string) string {
	if path == "" || filepath.IsAbs(path) || cwd == "" {
		return path
	}
	return filepath.Join(cwd, path)
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

func containingWorkspaceRoot(path string, workspaceRoots []string) string {
	var result string
	for _, root := range workspaceRoots {
		rootAbs, err := filepath.Abs(root)
		if err == nil && pathWithin(path, rootAbs) && (result == "" || len(rootAbs) > len(result)) {
			result = rootAbs
		}
	}
	return result
}

func providerSkillRoot(path, workspaceRoot string) string {
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	rootAbs, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	parts := strings.Split(rel, string(filepath.Separator))
	for i := 0; i+2 < len(parts); i++ {
		switch parts[i] {
		case ".cursor", ".agents", ".claude", ".codex":
			if parts[i+1] == "skills" {
				return filepath.Join(append([]string{rootAbs}, parts[:i+2]...)...)
			}
		}
	}
	return ""
}

func pluginManifestAncestor(path, markerDir, stop string) string {
	stop = filepath.Clean(stop)
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		if readableRegularFile(filepath.Join(dir, markerDir, "plugin.json")) {
			return dir
		}
		if stop != "." && filepath.Clean(dir) == stop {
			return ""
		}
		if parent := filepath.Dir(dir); parent == dir {
			return ""
		}
	}
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func shellTokens(command string) []string {
	var tokens []string
	var token strings.Builder
	var quote rune
	escaped := false
	flush := func() {
		if token.Len() > 0 {
			tokens = append(tokens, token.String())
			token.Reset()
		}
	}
	for _, char := range command {
		if escaped {
			token.WriteRune(char)
			escaped = false
			continue
		}
		if char == '\\' && quote != '\'' && runtime.GOOS != "windows" {
			escaped = true
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
			} else {
				token.WriteRune(char)
			}
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
			continue
		}
		if strings.ContainsRune(" \t\r\n;|&()<>", char) {
			flush()
			continue
		}
		token.WriteRune(char)
	}
	if escaped {
		token.WriteRune('\\')
	}
	flush()
	return tokens
}
