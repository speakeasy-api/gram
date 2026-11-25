// Package skilltree models a skill as the directory the Agent Skills
// specification defines: a SKILL.md manifest plus optional supporting files
// such as scripts/, references/ and assets/.
//
// It owns the rules every write path shares: which supporting-file paths are
// allowed, the size caps, and the tree digest that identifies a skill
// version. The digest of a skill with no supporting files equals the
// single-file digest skills have always had, so existing versions keep their
// identity.
package skilltree

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ManifestPath is the path of the skill manifest relative to the skill root.
// The manifest is stored on the skill version itself, never as a supporting
// file.
const ManifestPath = "SKILL.md"

const (
	// MaxFiles bounds the supporting files in one skill version. Published
	// skills carry a handful of files; 100 leaves room for asset-heavy skills
	// without letting one version fan out into thousands of rows.
	MaxFiles = 100

	// MaxFileBytes bounds one supporting file. It matches the
	// skill_version_files content size CHECK in the database schema.
	MaxFileBytes = 5 * 1024 * 1024 // 5 MiB

	// MaxTotalBytes bounds the combined size of all supporting files in one
	// skill version, so one version cannot hold more than a few large assets.
	MaxTotalBytes = 10 * 1024 * 1024 // 10 MiB

	// MaxPathBytes bounds a supporting-file path. It matches the
	// skill_version_files path length CHECK in the database schema.
	MaxPathBytes = 512

	// digestVersion prefixes the tree digest preimage. Changing it changes
	// the identity of every stored skill version.
	digestVersion = "skill-manifest-v1"
)

var (
	// ErrInvalidPath reports a supporting-file path that is not a clean,
	// relative, slash-separated path inside the skill root.
	ErrInvalidPath = errors.New("invalid skill file path")

	// ErrDuplicatePath reports two supporting files whose paths are equal or
	// differ only by letter case. Case-only differences collide when a skill
	// is checked out on a case-insensitive file system.
	ErrDuplicatePath = errors.New("duplicate skill file path")

	// ErrTooManyFiles reports more than MaxFiles supporting files.
	ErrTooManyFiles = errors.New("too many skill files")

	// ErrFileTooLarge reports a supporting file larger than MaxFileBytes.
	ErrFileTooLarge = errors.New("skill file too large")

	// ErrTreeTooLarge reports supporting files whose combined size exceeds
	// MaxTotalBytes.
	ErrTreeTooLarge = errors.New("skill files too large")
)

// File is a supporting file as supplied by a writer: an API upload, a device
// capture, or an approved suggestion.
type File struct {
	// Path is the slash-separated path relative to the skill root, for
	// example references/REFERENCE.md.
	Path string

	// Content is the exact file bytes.
	Content []byte

	// ContentType is the media type the writer declared. When empty,
	// Validate infers it from the path extension and the content.
	ContentType string
}

// Entry describes a validated supporting file.
type Entry struct {
	// Path is the validated path relative to the skill root.
	Path string

	// Content is the exact file bytes.
	Content []byte

	// SHA256 is the lowercase hex SHA-256 of Content.
	SHA256 string

	// Size is the length of Content in bytes.
	Size int64

	// ContentType is the declared or inferred media type.
	ContentType string
}

// ValidatePath reports whether p is an acceptable supporting-file path.
//
// A path must be a clean, relative, slash-separated path of at most
// MaxPathBytes bytes. It may not contain empty, "." or ".." segments,
// backslashes, control characters, or invisible format characters such as
// bidirectional overrides, which can disguise a file name when it is shown.
// It may not name the skill root itself or the manifest.
func ValidatePath(p string) error {
	switch {
	case p == "":
		return fmt.Errorf("%w: path is empty", ErrInvalidPath)
	case p == ".":
		return fmt.Errorf("%w: %q names the skill root, not a file", ErrInvalidPath, p)
	case len(p) > MaxPathBytes:
		return fmt.Errorf("%w: %q is longer than %d bytes", ErrInvalidPath, p, MaxPathBytes)
	case !utf8.ValidString(p):
		return fmt.Errorf("%w: %q is not valid UTF-8", ErrInvalidPath, p)
	case strings.HasPrefix(p, "/"):
		return fmt.Errorf("%w: %q must be relative to the skill root", ErrInvalidPath, p)
	case strings.Contains(p, `\`):
		return fmt.Errorf("%w: %q must use forward slashes", ErrInvalidPath, p)
	case strings.IndexFunc(p, unicode.IsControl) >= 0:
		return fmt.Errorf("%w: %q contains a control character", ErrInvalidPath, p)
	case strings.IndexFunc(p, isFormatRune) >= 0:
		return fmt.Errorf("%w: %q contains an invisible format character", ErrInvalidPath, p)
	case path.Clean(p) != p:
		return fmt.Errorf("%w: %q must not contain empty, \".\" or \"..\" segments", ErrInvalidPath, p)
	case p == ".." || strings.HasPrefix(p, "../"):
		return fmt.Errorf("%w: %q escapes the skill root", ErrInvalidPath, p)
	case strings.EqualFold(p, ManifestPath):
		return fmt.Errorf("%w: %q is the skill manifest, not a supporting file", ErrInvalidPath, p)
	}
	return nil
}

// isFormatRune reports Unicode format characters (category Cf), such as
// bidirectional overrides and zero-width joiners.
func isFormatRune(r rune) bool {
	return unicode.Is(unicode.Cf, r)
}

// Validate checks supporting files against the path rules and size caps and
// returns them as entries sorted by path. It does not modify files.
func Validate(files []File) ([]Entry, error) {
	if len(files) > MaxFiles {
		return nil, fmt.Errorf("%w: %d files, at most %d allowed", ErrTooManyFiles, len(files), MaxFiles)
	}

	entries := make([]Entry, 0, len(files))
	seen := make(map[string]string, len(files))
	var total int64
	for _, file := range files {
		if err := ValidatePath(file.Path); err != nil {
			return nil, err
		}

		folded := strings.ToLower(file.Path)
		if previous, ok := seen[folded]; ok {
			return nil, fmt.Errorf("%w: %q and %q", ErrDuplicatePath, previous, file.Path)
		}
		seen[folded] = file.Path

		size := int64(len(file.Content))
		if size > MaxFileBytes {
			return nil, fmt.Errorf("%w: %q is %d bytes, at most %d allowed", ErrFileTooLarge, file.Path, size, MaxFileBytes)
		}
		total += size
		if total > MaxTotalBytes {
			return nil, fmt.Errorf("%w: more than %d bytes in total", ErrTreeTooLarge, MaxTotalBytes)
		}

		digest := sha256.Sum256(file.Content)
		entries = append(entries, Entry{
			Path:        file.Path,
			Content:     file.Content,
			SHA256:      hex.EncodeToString(digest[:]),
			Size:        size,
			ContentType: contentType(file),
		})
	}

	slices.SortFunc(entries, func(a, b Entry) int { return strings.Compare(a.Path, b.Path) })
	return entries, nil
}

// Digest returns the lowercase hex tree digest of a skill version.
//
// manifestDigest is the SHA-256 of the canonicalized SKILL.md. entries are
// the validated supporting files, in any order. The preimage is the digest
// version, then one "path NUL sha256 NUL" record per file sorted by path,
// with SKILL.md included as a file:
//
//	"skill-manifest-v1" NUL ( path NUL raw-sha256-bytes NUL )*
//
// With no supporting files this is exactly the single-file digest stored for
// every skill version created before supporting files existed.
func Digest(manifestDigest [sha256.Size]byte, entries []Entry) (string, error) {
	type record struct {
		path   string
		digest [sha256.Size]byte
	}

	records := make([]record, 0, len(entries)+1)
	records = append(records, record{path: ManifestPath, digest: manifestDigest})
	for _, entry := range entries {
		raw, err := hex.DecodeString(entry.SHA256)
		if err != nil || len(raw) != sha256.Size {
			return "", fmt.Errorf("decode digest of %q: invalid sha256 %q", entry.Path, entry.SHA256)
		}
		records = append(records, record{path: entry.Path, digest: [sha256.Size]byte(raw)})
	}
	slices.SortFunc(records, func(a, b record) int { return strings.Compare(a.path, b.path) })

	size := len(digestVersion) + 1
	for _, r := range records {
		size += len(r.path) + 1 + sha256.Size + 1
	}

	preimage := make([]byte, 0, size)
	preimage = append(preimage, digestVersion...)
	preimage = append(preimage, 0)
	for _, r := range records {
		preimage = append(preimage, r.path...)
		preimage = append(preimage, 0)
		preimage = append(preimage, r.digest[:]...)
		preimage = append(preimage, 0)
	}

	sum := sha256.Sum256(preimage)
	return hex.EncodeToString(sum[:]), nil
}

// ValidateRootName reports whether a skill's manifest name matches the name of
// the directory it was uploaded from, as the Agent Skills specification
// requires. Callers only check it when the upload carried a root directory
// name, such as a zip or folder upload.
func ValidateRootName(manifestName, rootDir string) error {
	if rootDir == "" || rootDir == manifestName {
		return nil
	}
	return fmt.Errorf("skill name %q must match its directory name %q", manifestName, rootDir)
}

// skillFileTypes maps extensions common in skills that the standard library
// table does not cover, so their media type does not depend on the host's
// mime.types file.
var skillFileTypes = map[string]string{
	".md":   "text/markdown; charset=utf-8",
	".py":   "text/x-python; charset=utf-8",
	".sh":   "text/x-shellscript; charset=utf-8",
	".txt":  "text/plain; charset=utf-8",
	".yaml": "application/yaml",
	".yml":  "application/yaml",
	".ts":   "text/typescript; charset=utf-8",
}

func contentType(file File) string {
	if file.ContentType != "" {
		return file.ContentType
	}
	if known, ok := skillFileTypes[strings.ToLower(path.Ext(file.Path))]; ok {
		return known
	}
	if byExtension := mime.TypeByExtension(path.Ext(file.Path)); byExtension != "" {
		return byExtension
	}
	return http.DetectContentType(file.Content)
}
