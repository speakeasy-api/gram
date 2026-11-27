package efficacy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	domainskills "github.com/speakeasy-api/gram/server/internal/skills"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/skills/skilltree"
)

func TestPublishReadsSkillVersionFilesOncePerPass(t *testing.T) {
	t.Parallel()
	h := newPublishHarness(t, "skill_efficacy_publish_files_cache")
	ctx := t.Context()

	// Capturing the same content again resolves the fixture's skill.
	captured, err := domainskills.CaptureSkillContent(ctx, h.fixture.db, h.fixture.projectID, skillContent)
	require.NoError(t, err)
	require.Equal(t, h.fixture.skillVersionID, captured.SkillVersionID)

	content := []byte("# Guide\n")
	sum := sha256.Sum256(content)
	require.NoError(t, repo.New(h.fixture.db).CreateSkillVersionFile(ctx, repo.CreateSkillVersionFileParams{
		Path:           "references/guide.md",
		Content:        content,
		Sha256:         hex.EncodeToString(sum[:]),
		SizeBytes:      int64(len(content)),
		ContentType:    "text/markdown",
		ProjectID:      h.fixture.projectID,
		SkillID:        captured.SkillID,
		SkillVersionID: captured.SkillVersionID,
	}))

	publisher := h.publisher(t, h.scores)
	cache := map[uuid.UUID][]skilltree.File{}

	files, err := publisher.loadSkillFiles(ctx, h.fixture.projectID, captured.SkillID, captured.SkillVersionID, cache)
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, "references/guide.md", files[0].Path)
	require.Equal(t, files, cache[captured.SkillVersionID], "a successful read is kept for the rest of the pass")

	// A later evaluation of the same version is served from the cache: the
	// sentinel is what comes back, not the stored row.
	sentinel := []skilltree.File{{Path: "references/cached.md", Content: []byte("cached\n"), ContentType: ""}}
	cache[captured.SkillVersionID] = sentinel
	again, err := publisher.loadSkillFiles(ctx, h.fixture.projectID, captured.SkillID, captured.SkillVersionID, cache)
	require.NoError(t, err)
	require.Equal(t, sentinel, again, "evaluations of the same version share one read")
}

func TestPublishSkillVersionFilesReadFailureIsRetryableAndNotCached(t *testing.T) {
	t.Parallel()
	h := newPublishHarness(t, "skill_efficacy_publish_files_failure")

	publisher := h.publisher(t, h.scores)
	cache := map[uuid.UUID][]skilltree.File{}

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := publisher.loadSkillFiles(cancelled, h.fixture.projectID, uuid.New(), h.fixture.skillVersionID, cache)
	require.ErrorIs(t, err, ErrRetryable, "a failed read is infrastructure, so the same rows are retried")
	require.Empty(t, cache, "a failed read is not handed to later evaluations")
}
