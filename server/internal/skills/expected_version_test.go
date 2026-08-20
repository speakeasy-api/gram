package skills_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// An expected-version token is what lets two authors edit the same skill
// without one silently overwriting the other. It is checked inside the write's
// own transaction, so "the skill moved on" is a refusal rather than a race.
func TestAddSkillVersionRejectsAStaleExpectedVersion(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "contested", "First summary.")
	second, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: created.Skill.ID, Content: skillManifest("contested", "Second summary.", "second"),
		DerivedFromVersionID: nil, ExpectedLatestVersionID: nil,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	_, err = ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: created.Skill.ID, Content: skillManifest("contested", "Third summary.", "third"),
		DerivedFromVersionID: nil, ExpectedLatestVersionID: &created.Version.ID,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})

	requireOopsCode(t, err, oops.CodeConflict)

	current, err := ti.service.Get(ctx, &gen.GetPayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, second.Version.ID, current.LatestVersion.ID, "the refused write left the skill untouched")
	require.Equal(t, int64(2), current.Skill.VersionCount)
}

func TestAddSkillVersionAcceptsTheCurrentExpectedVersion(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "uncontested", "First summary.")

	next, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: created.Skill.ID, Content: skillManifest("uncontested", "Second summary.", "second"),
		DerivedFromVersionID: nil, ExpectedLatestVersionID: &created.Version.ID,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})

	require.NoError(t, err)
	require.True(t, next.CreatedVersion)
	require.NotEqual(t, created.Version.ID, next.Version.ID)
}

// A retry of a write that already landed carries a token that is stale by its
// own definition. Refusing it would turn a dropped response into a conflict the
// caller cannot resolve, so the content decides: identical canonical content
// that is already current is the same no-op an unconditional retry gets.
func TestAddSkillVersionTreatsARetriedIdenticalWriteAsANoOp(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "retried", "First summary.")
	content := skillManifest("retried", "Second summary.", "second")
	landed, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: created.Skill.ID, Content: content,
		DerivedFromVersionID: nil, ExpectedLatestVersionID: &created.Version.ID,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	retried, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: created.Skill.ID, Content: content,
		DerivedFromVersionID: nil, ExpectedLatestVersionID: &created.Version.ID,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})

	require.NoError(t, err)
	require.False(t, retried.CreatedVersion)
	require.Equal(t, landed.Version.ID, retried.Version.ID)
	require.Equal(t, int64(2), retried.Skill.VersionCount)
}

func TestUpdateSkillRejectsAStaleExpectedVersion(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "renamed", "First summary.")
	_, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: created.Skill.ID, Content: skillManifest("renamed", "Second summary.", "second"),
		DerivedFromVersionID: nil, ExpectedLatestVersionID: nil,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	_, err = ti.service.Update(ctx, &gen.UpdatePayload{
		ID: created.Skill.ID, Name: "renamed", DisplayName: "Renamed", Summary: nil, Tags: nil,
		ExpectedLatestVersionID: &created.Version.ID,
		SessionToken:            nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})

	requireOopsCode(t, err, oops.CodeConflict)

	current, err := ti.service.Get(ctx, &gen.GetPayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, created.Skill.DisplayName, current.Skill.DisplayName)
}

// Metadata edits never record a version, so the token a caller read before the
// edit is still valid after it — a retry does not have to re-read.
func TestUpdateSkillAcceptsTheCurrentExpectedVersionRepeatedly(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "stable", "First summary.")

	for _, displayName := range []string{"Stable One", "Stable Two"} {
		updated, err := ti.service.Update(ctx, &gen.UpdatePayload{
			ID: created.Skill.ID, Name: "stable", DisplayName: displayName, Summary: nil, Tags: nil,
			ExpectedLatestVersionID: &created.Version.ID,
			SessionToken:            nil, ApikeyToken: nil, ProjectSlugInput: nil,
		})
		require.NoError(t, err)
		require.Equal(t, displayName, updated.DisplayName)
	}
}

// A malformed token is a bad request, not a stale one. Recovering from it as a
// replay would accept a write whose concurrency claim was never valid.
func TestAddSkillVersionRejectsAMalformedExpectedVersionEvenWhenContentMatches(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "malformed", "First summary.")
	malformed := "not-a-uuid"

	_, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: created.Skill.ID, Content: created.Version.Content,
		DerivedFromVersionID: nil, ExpectedLatestVersionID: &malformed,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})

	requireOopsCode(t, err, oops.CodeBadRequest)
}

// A metadata retry whose values already match is the same no-op the equivalent
// version write gets, even once a concurrent version has made its token stale.
//
// The concurrent version repeats the manifest description on purpose: recording
// a version syncs the registry summary from it, so a version carrying different
// prose genuinely changes the metadata and is a conflict rather than a replay.
func TestUpdateSkillTreatsAReplayedEditAsANoOpAfterAConcurrentVersion(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "replayed", "First summary.")
	summary := "First summary."
	applied, err := ti.service.Update(ctx, &gen.UpdatePayload{
		ID: created.Skill.ID, Name: "replayed", DisplayName: "Replayed", Summary: &summary, Tags: nil,
		ExpectedLatestVersionID: &created.Version.ID,
		SessionToken:            nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	// A concurrent author records a version, so the caller's token goes stale
	// through no fault of its own.
	_, err = ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: created.Skill.ID, Content: skillManifest("replayed", "First summary.", "second"),
		DerivedFromVersionID: nil, ExpectedLatestVersionID: nil,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	replayed, err := ti.service.Update(ctx, &gen.UpdatePayload{
		ID: created.Skill.ID, Name: "replayed", DisplayName: "Replayed", Summary: &summary, Tags: nil,
		ExpectedLatestVersionID: &created.Version.ID,
		SessionToken:            nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})

	require.NoError(t, err)
	require.Equal(t, applied.DisplayName, replayed.DisplayName)
}

// A different edit under the same stale token is still a lost update.
func TestUpdateSkillRejectsADifferentEditUnderAStaleToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "diverged", "First summary.")
	_, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: created.Skill.ID, Content: skillManifest("diverged", "Second summary.", "second"),
		DerivedFromVersionID: nil, ExpectedLatestVersionID: nil,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	_, err = ti.service.Update(ctx, &gen.UpdatePayload{
		ID: created.Skill.ID, Name: "diverged", DisplayName: "Something Else", Summary: nil, Tags: nil,
		ExpectedLatestVersionID: &created.Version.ID,
		SessionToken:            nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})

	requireOopsCode(t, err, oops.CodeConflict)
}

// A replay writes nothing. Advancing updated_at or recording a second update
// event would make a dropped response look like a real edit in the audit trail.
func TestUpdateSkillReplayRecordsNoSecondUpdateEvent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "quiet-replay", "First summary.")
	summary := "First summary."
	applied, err := ti.service.Update(ctx, &gen.UpdatePayload{
		ID: created.Skill.ID, Name: "quiet-replay", DisplayName: "Quiet Replay", Summary: &summary, Tags: nil,
		ExpectedLatestVersionID: &created.Version.ID,
		SessionToken:            nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	_, err = ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: created.Skill.ID, Content: skillManifest("quiet-replay", "First summary.", "second"),
		DerivedFromVersionID: nil, ExpectedLatestVersionID: nil,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillUpdate)
	require.NoError(t, err)

	replayed, err := ti.service.Update(ctx, &gen.UpdatePayload{
		ID: created.Skill.ID, Name: "quiet-replay", DisplayName: "Quiet Replay", Summary: &summary, Tags: nil,
		ExpectedLatestVersionID: &created.Version.ID,
		SessionToken:            nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillUpdate)
	require.NoError(t, err)
	require.Equal(t, before, after, "a replay records no second update event")
	require.Equal(t, applied.UpdatedAt, replayed.UpdatedAt, "a replay does not advance updated_at")
}
