package hooks

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/skills/skilltree"
)

const (
	// hookSkillContentSchemaV2 uploads a SKILL.md together with the
	// supporting files of its skill directory.
	hookSkillContentSchemaV2 = "hook.skill-content.v2"

	// maxSkillScanDocumentBytes bounds the text handed to the prompt-injection
	// judge for one captured skill version. The judge applies its own rune cap
	// on top; this bound only keeps a tree of large text files from being
	// copied into one oversized string.
	maxSkillScanDocumentBytes = 1024 * 1024 // 1 MiB
)

// deviceSkillTreeKnown reports whether a live skill version already holds the
// skill directory a device reported: SKILL.md bytes hashing to rawSHA256 and
// supporting files whose tree digest, computed over the raw SKILL.md hash, is
// treeSHA256. Candidates are the version the raw hash alias resolves to and
// the versions created from the same SKILL.md bytes.
func deviceSkillTreeKnown(ctx context.Context, queries *repo.Queries, projectID uuid.UUID, rawSHA256, treeSHA256 string) (bool, error) {
	manifest, err := hex.DecodeString(rawSHA256)
	if err != nil || len(manifest) != sha256.Size {
		return false, fmt.Errorf("decode raw skill hash: invalid sha256")
	}

	rows, err := queries.ListSkillTreeCandidateFiles(ctx, repo.ListSkillTreeCandidateFilesParams{
		ProjectID: projectID,
		RawSha256: rawSHA256,
	})
	if err != nil {
		return false, fmt.Errorf("list skill tree candidates: %w", err)
	}

	matches := func(entries []skilltree.Entry) bool {
		digest, err := skilltree.Digest([sha256.Size]byte(manifest), entries)
		return err == nil && digest == treeSHA256
	}
	var current uuid.UUID
	var entries []skilltree.Entry
	for i, row := range rows {
		if i > 0 && row.SkillVersionID != current {
			if matches(entries) {
				return true, nil
			}
			entries = entries[:0]
		}
		current = row.SkillVersionID
		if row.Path.Valid && row.Sha256.Valid {
			entries = append(entries, skilltree.Entry{Path: row.Path.String, Content: nil, SHA256: row.Sha256.String, Size: 0, ContentType: ""})
		}
	}
	return len(rows) > 0 && matches(entries), nil
}

// uploadedSkillFiles converts uploaded supporting files after checking each
// declared sha256 against its bytes. Path and size rules are enforced when the
// capture validates the tree.
func uploadedSkillFiles(files []*gen.SkillContentFile) ([]skilltree.File, error) {
	out := make([]skilltree.File, 0, len(files))
	for _, file := range files {
		if file == nil {
			return nil, fmt.Errorf("skill file is empty")
		}
		digest := sha256.Sum256(file.Content)
		if hex.EncodeToString(digest[:]) != file.Sha256 {
			return nil, fmt.Errorf("skill file %q does not match its sha256", file.Path)
		}
		out = append(out, skilltree.File{Path: file.Path, Content: file.Content, ContentType: ""})
	}
	return out, nil
}

// skillScanDocument joins a SKILL.md with its text supporting files, each
// introduced by its path, so the prompt-injection judge sees instructions an
// agent reads from references or scripts as well as from the manifest.
// Binary files and text beyond maxSkillScanDocumentBytes are left out.
func skillScanDocument(content string, files []skilltree.File) string {
	var doc strings.Builder
	doc.WriteString(content)
	for _, file := range files {
		if !utf8.Valid(file.Content) || bytes.IndexByte(file.Content, 0) >= 0 {
			continue
		}
		header := "\n\n--- " + file.Path + " ---\n"
		if doc.Len()+len(header)+len(file.Content) > maxSkillScanDocumentBytes {
			continue
		}
		doc.WriteString(header)
		doc.Write(file.Content)
	}
	return doc.String()
}
