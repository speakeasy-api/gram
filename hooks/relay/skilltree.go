package relay

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/speakeasy-api/agenthooks"
)

// The caps mirror the server's skilltree rules, so a tree the relay builds is
// one the server accepts. Files over a cap are skipped rather than failing the
// capture: the uploaded tree is the deterministic subset that fits.
const (
	// maxSkillTreeFiles bounds the supporting files sent for one skill.
	maxSkillTreeFiles = 100

	// maxSkillTreeFileBytes bounds one supporting file.
	maxSkillTreeFileBytes = 5 * 1024 * 1024 // 5 MiB

	// maxSkillTreeTotalBytes bounds all supporting files of one skill.
	maxSkillTreeTotalBytes = 10 * 1024 * 1024 // 10 MiB

	// maxSkillTreePathBytes bounds a supporting-file path.
	maxSkillTreePathBytes = 512

	// maxSkillTreeWalkEntries bounds the directory entries visited while
	// collecting a tree. A skill directory holds a handful of files; one that
	// holds thousands is a build output or a checkout, and is captured as
	// SKILL.md only rather than stalling the hook on a long walk.
	maxSkillTreeWalkEntries = 10_000

	// skillTreeDigestVersion prefixes the tree digest preimage. It must match
	// the server's skilltree digest version.
	skillTreeDigestVersion = "skill-manifest-v1"

	// skillManifestName is the manifest file every skill directory holds.
	skillManifestName = "SKILL.md"
)

var errSkillTreeTooLarge = errors.New("skill directory has too many entries")

// skillTreeFile is one supporting file of a located skill directory.
type skillTreeFile struct {
	// Path is the slash-separated path relative to the skill directory.
	Path string

	// Content is the exact file bytes.
	Content []byte

	// SHA256 is the lowercase hex SHA-256 of Content.
	SHA256 string
}

// skillTree is a skill directory whose SKILL.md matched the activation.
type skillTree struct {
	// digest is the tree digest over the raw SKILL.md hash and files.
	digest string

	// files are the supporting files, sorted by path.
	files []skillTreeFile
}

// locateSkillTree finds the on-disk directory of an activated skill. A
// candidate directory only counts when its SKILL.md bytes hash to rawSHA256,
// the hash of the model-visible manifest: the directory is then the one the
// activation loaded, not a same-named skill elsewhere. Activations whose
// content differs from the file bytes (partial reads, decorated tool output)
// locate nothing and keep the manifest-only capture.
func locateSkillTree(typed any, name, rawSHA256 string) *skillTree {
	for _, dir := range skillDirCandidates(typed, name) {
		if tree, ok := readSkillTree(dir, rawSHA256); ok {
			return tree
		}
	}
	return nil
}

// skillDirCandidates lists directories that may hold the activated skill, in
// the order the relay checks them: the directory of a SKILL.md path the tool
// call read, then each provider's skill roots. The hash check in
// readSkillTree picks the right one, so the list may over-approximate.
func skillDirCandidates(typed any, name string) []string {
	base := agenthooks.EventOf(typed)
	if base == nil {
		return nil
	}
	cwd := base.Session.CWD
	var candidates []string
	if manifest := toolManifestPath(typed, cwd); manifest != "" {
		candidates = append(candidates, filepath.Dir(manifest))
	}

	home, _ := os.UserHomeDir()
	claudeConfig := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR"))
	if claudeConfig == "" && home != "" {
		claudeConfig = filepath.Join(home, ".claude")
	}

	if plugin, skill, ok := strings.Cut(name, ":"); ok {
		if skillTokenRE.MatchString(plugin) && skillTokenRE.MatchString(skill) {
			candidates = append(candidates, claudePluginSkillDirs(claudeConfig, plugin, skill)...)
		}
		return uniquePaths(candidates)
	}
	if !skillTokenRE.MatchString(name) {
		return uniquePaths(candidates)
	}

	var roots []string
	if managed := claudeManagedSkillsRoot(); managed != "" {
		roots = append(roots, managed)
	}
	if claudeConfig != "" {
		roots = append(roots, filepath.Join(claudeConfig, "skills"))
	}
	if filepath.IsAbs(cwd) {
		for dir := cwd; ; dir = filepath.Dir(dir) {
			for _, provider := range []string{".claude", ".agents", ".codex", ".cursor"} {
				roots = append(roots, filepath.Join(dir, provider, "skills"))
			}
			if pathExists(filepath.Join(dir, ".git")) || filepath.Dir(dir) == dir {
				break
			}
		}
	}
	if home != "" {
		roots = append(roots, filepath.Join(home, ".agents", "skills"), filepath.Join(home, ".cursor", "skills"))
	}
	codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if codexHome == "" && home != "" {
		codexHome = filepath.Join(home, ".codex")
	}
	if codexHome != "" {
		roots = append(roots, filepath.Join(codexHome, "skills"))
	}
	roots = append(roots, "/etc/codex/skills", "/opt/codex/skills")
	for _, root := range roots {
		candidates = append(candidates, filepath.Join(root, name), filepath.Join(root, ".system", name))
	}
	return uniquePaths(candidates)
}

// toolManifestPath returns the SKILL.md path a file-read or shell tool call
// read, resolved against the session directory, or "" when the call read no
// SKILL.md.
func toolManifestPath(typed any, cwd string) string {
	event, ok := typed.(*agenthooks.ToolPostEvent)
	if !ok {
		return ""
	}
	var input struct {
		FilePath string `json:"file_path"`
		Path     string `json:"path"`
		Command  string `json:"command"`
	}
	if json.Unmarshal(event.Tool.Input, &input) != nil {
		return ""
	}
	candidates := []string{input.FilePath, input.Path}
	if event.Tool.Canonical == agenthooks.ToolShell {
		// `cat <path>` and `sed -n 1,200p <path>` read the manifest as the
		// last argument; the hash check rejects anything else.
		fields := strings.Fields(input.Command)
		if len(fields) > 0 {
			candidates = append(candidates, strings.Trim(fields[len(fields)-1], `'"`))
		}
	}
	for _, candidate := range candidates {
		if filepath.Base(candidate) != skillManifestName {
			continue
		}
		if !filepath.IsAbs(candidate) {
			if !filepath.IsAbs(cwd) {
				continue
			}
			candidate = filepath.Join(cwd, candidate)
		}
		return filepath.Clean(candidate)
	}
	return ""
}

// claudePluginSkillDirs lists the directories a Claude Code plugin skill can
// live in, for every installed copy of the plugin.
func claudePluginSkillDirs(claudeConfig, plugin, skill string) []string {
	if claudeConfig == "" {
		return nil
	}
	file, err := os.Open(filepath.Join(claudeConfig, "plugins", "installed_plugins.json"))
	if err != nil {
		return nil
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxClaudePluginRegistryBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(data) > maxClaudePluginRegistryBytes {
		return nil
	}
	var registry struct {
		Plugins map[string][]struct {
			InstallPath string `json:"installPath"`
		} `json:"plugins"`
	}
	if json.Unmarshal(data, &registry) != nil {
		return nil
	}
	keys := make([]string, 0, len(registry.Plugins))
	for key := range registry.Plugins {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var dirs []string
	for _, key := range keys {
		if prefix, _, _ := strings.Cut(key, "@"); prefix != plugin {
			continue
		}
		for _, record := range registry.Plugins[key] {
			if filepath.IsAbs(record.InstallPath) {
				dirs = append(dirs, filepath.Join(record.InstallPath, "skills", skill), filepath.Join(record.InstallPath, skill))
			}
		}
	}
	return dirs
}

// maxClaudePluginRegistryBytes bounds the Claude Code plugin registry read.
// The registry lists installed plugins; 1 MiB is far above any real install.
const maxClaudePluginRegistryBytes = 1 << 20 // 1 MiB

// claudeManagedSkillsRoot is the managed-policy skills directory Claude Code
// reads before personal and project skills. A var so tests can redirect it.
var claudeManagedSkillsRoot = func() string {
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

func uniquePaths(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	out := paths[:0]
	for _, p := range paths {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// readSkillTree reads dir as a skill directory when its SKILL.md hashes to
// rawSHA256. All reads go through an os.Root, so symlinks that resolve outside
// the directory are never followed.
func readSkillTree(dir, rawSHA256 string) (*skillTree, bool) {
	if !filepath.IsAbs(dir) {
		return nil, false
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, false
	}
	defer func() { _ = root.Close() }()

	manifest, ok := readRootFile(root, skillManifestName, maxSkillContentBytes)
	if !ok {
		return nil, false
	}
	manifestDigest := sha256.Sum256(manifest)
	if hex.EncodeToString(manifestDigest[:]) != rawSHA256 {
		return nil, false
	}

	files, err := collectSkillTreeFiles(root)
	if err != nil {
		return nil, false
	}
	return &skillTree{digest: skillTreeDigest(manifestDigest, files), files: files}, true
}

// collectSkillTreeFiles walks a skill directory and returns its supporting
// files sorted by path. It skips hidden files and directories (.git, .env and
// the like), dependency and cache directories, symlinked directories,
// non-regular files, symlinks escaping the root, files over the size caps,
// and paths the server would reject.
func collectSkillTreeFiles(root *os.Root) ([]skillTreeFile, error) {
	files := []skillTreeFile{}
	seen := map[string]bool{}
	var total int64
	visited := 0
	err := fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable subdirectory is left out; the rest still counts.
			if d != nil && d.IsDir() && p != "." {
				return fs.SkipDir
			}
			return nil
		}
		if p == "." {
			return nil
		}
		visited++
		if visited > maxSkillTreeWalkEntries {
			return errSkillTreeTooLarge
		}
		name := d.Name()
		if d.IsDir() {
			if skipSkillTreeDir(name) {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") || strings.EqualFold(p, skillManifestName) || !validSkillTreePath(p) {
			return nil
		}
		if len(files) >= maxSkillTreeFiles {
			return fs.SkipAll
		}
		folded := strings.ToLower(p)
		if seen[folded] {
			return nil
		}
		content, ok := readRootFile(root, filepath.FromSlash(p), maxSkillTreeFileBytes)
		if !ok || total+int64(len(content)) > maxSkillTreeTotalBytes {
			return nil
		}
		seen[folded] = true
		total += int64(len(content))
		digest := sha256.Sum256(content)
		files = append(files, skillTreeFile{Path: p, Content: content, SHA256: hex.EncodeToString(digest[:])})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// skipSkillTreeDir reports directories that never belong to a skill's
// content: hidden directories (VCS metadata, editor state) and dependency or
// bytecode caches that are rebuilt from the skill's own files.
func skipSkillTreeDir(name string) bool {
	switch name {
	case "node_modules", "__pycache__", "venv":
		return true
	default:
		return strings.HasPrefix(name, ".")
	}
}

// validSkillTreePath mirrors the server's supporting-file path rules for
// paths produced by a directory walk, which are already clean and relative.
func validSkillTreePath(p string) bool {
	return p != "" && len(p) <= maxSkillTreePathBytes && utf8.ValidString(p) &&
		!strings.Contains(p, `\`) && strings.IndexFunc(p, unicode.IsControl) < 0 && path.Clean(p) == p
}

// readRootFile reads a regular file inside root of at most limit bytes. It
// follows symlinks only while they stay inside root, and refuses anything that
// is not a regular file so a FIFO cannot block the hook.
func readRootFile(root *os.Root, name string, limit int64) ([]byte, bool) {
	info, err := root.Stat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, false
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, false
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		_ = file.Close()
		return nil, false
	}
	content, readErr := io.ReadAll(io.LimitReader(file, limit+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || int64(len(content)) > limit {
		return nil, false
	}
	return content, true
}

// skillTreeDigest is the server's skill-manifest-v1 tree digest computed over
// the raw SKILL.md hash:
//
//	"skill-manifest-v1" NUL ( path NUL raw-sha256-bytes NUL )*
//
// with one record per file, SKILL.md included, sorted by path.
func skillTreeDigest(manifestDigest [sha256.Size]byte, files []skillTreeFile) string {
	type record struct {
		path   string
		digest []byte
	}
	records := []record{{path: skillManifestName, digest: manifestDigest[:]}}
	for _, file := range files {
		digest, err := hex.DecodeString(file.SHA256)
		if err != nil {
			return ""
		}
		records = append(records, record{path: file.Path, digest: digest})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].path < records[j].path })

	preimage := append([]byte(skillTreeDigestVersion), 0)
	for _, r := range records {
		preimage = append(preimage, r.path...)
		preimage = append(preimage, 0)
		preimage = append(preimage, r.digest...)
		preimage = append(preimage, 0)
	}
	sum := sha256.Sum256(preimage)
	return hex.EncodeToString(sum[:])
}
