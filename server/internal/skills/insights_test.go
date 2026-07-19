package skills_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/conv"
	hooksrepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/skills"
)

func insertSkillObservationWithHostname(t *testing.T, ti *testInstance, name, rawSHA256, hostname string, seenAt time.Time) {
	t.Helper()
	require.NoError(t, hooksrepo.New(ti.conn).InsertSkillObservation(t.Context(), hooksrepo.InsertSkillObservationParams{
		ProjectID: ti.projectID, IdempotencyKey: conv.ToPGText(uuid.NewString()), Provider: "test",
		UserID: conv.ToPGTextEmpty(""), UserEmail: conv.ToPGTextEmpty(""), Hostname: conv.ToPGTextEmpty(hostname),
		SessionID: conv.ToPGTextEmpty(""), SkillName: name, Source: conv.ToPGTextEmpty("workspace"),
		SourceLevel: conv.ToPGTextEmpty("project"), SourcePath: conv.ToPGTextEmpty(""),
		RawSha256: conv.ToPGTextEmpty(rawSHA256), SeenAt: conv.ToPGTimestamptz(seenAt),
	}))
}

func TestSkillInsightsReportsVersionAdoptionAndDrift(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	now := time.Now().UTC().Truncate(time.Second)
	oldContent := capturedManifest("adopted-skill", "Old.", "old body")
	oldVersion, err := skills.CaptureSkillContent(ctx, ti.conn, ti.projectID, oldContent)
	require.NoError(t, err)
	newContent := capturedManifest("adopted-skill", "New.", "new body")
	newVersion, err := skills.CaptureSkillContent(ctx, ti.conn, ti.projectID, newContent)
	require.NoError(t, err)
	plugin := createPlugin(t, ctx, ti, ti.projectID, "adoption-plugin")
	_, err = ti.service.Distribute(ctx, &gen.DistributePayload{
		ID: newVersion.SkillID.String(), PluginID: plugin.ID.String(), PinnedVersionID: conv.PtrEmpty(newVersion.SkillVersionID.String()),
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	insertSkillObservationWithHostname(t, ti, "adopted-skill", contentSHA256(oldContent), "machine-a", now.Add(-2*time.Hour))
	insertSkillObservationWithHostname(t, ti, "adopted-skill", contentSHA256(newContent), "machine-a", now.Add(-time.Hour))
	insertSkillObservationWithHostname(t, ti, "adopted-skill", contentSHA256(oldContent), "machine-b", now.Add(-time.Hour))
	insertSkillObservationWithHostname(t, ti, "adopted-skill", "", "machine-c", now.Add(-time.Hour))
	insertSkillObservationWithHostname(t, ti, "adopted-skill", contentSHA256(oldContent), "old-machine", now.Add(-31*24*time.Hour))
	insertSkillObservationWithHostname(t, ti, "adopted-skill", contentSHA256(oldContent), "future-machine", now.Add(time.Hour))
	result, err := skills.ReconcileSkillObservations(ctx, ti.conn, ti.projectID, 10)
	require.NoError(t, err)
	require.Equal(t, 6, result.Processed)

	details, err := ti.service.Get(ctx, &gen.GetPayload{
		ID: newVersion.SkillID.String(), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, int64(6), details.Skill.SeenCount)
	require.Equal(t, int64(3), details.Adoption.DistinctHostnames)
	require.Equal(t, int64(4), details.Adoption.ActivationsInWindow)
	require.Equal(t, "single", details.Drift.TargetState)
	require.Equal(t, []string{newVersion.SkillVersionID.String()}, details.Drift.TargetVersionIds)
	require.Equal(t, int64(3), details.Drift.ActiveMachines)
	require.Equal(t, int64(1), details.Drift.OnTargetMachines)
	require.Equal(t, int64(1), details.Drift.DriftedMachines)
	require.Equal(t, int64(1), details.Drift.IndeterminateMachines)
	require.Len(t, details.SightingTimeline, 1)
	require.Equal(t, int64(4), details.SightingTimeline[0].ActivationCount)

	versions, err := ti.service.ListVersions(ctx, &gen.ListVersionsPayload{
		ID: newVersion.SkillID.String(), Limit: 20, Cursor: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, versions.Versions, 2)
	require.Equal(t, int64(1), versions.Versions[0].SeenCount)
	require.Equal(t, int64(4), versions.Versions[1].SeenCount)
	require.Equal(t, oldVersion.SkillVersionID.String(), versions.Versions[1].ID)

	duplicate, err := ti.service.Create(ctx, &gen.CreatePayload{
		Content: newContent, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.False(t, duplicate.CreatedVersion)
	require.Equal(t, int64(1), duplicate.Version.SeenCount)
	require.NotNil(t, duplicate.Version.FirstSeenAt)
	require.NotNil(t, duplicate.Version.LastSeenAt)
}

func TestSkillInsightsExposeVersionlessObservedSkill(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	insertSkillObservationWithHostname(t, ti, "metadata-only", "", "machine", time.Now().UTC())
	_, err := skills.ReconcileSkillObservations(ctx, ti.conn, ti.projectID, 10)
	require.NoError(t, err)

	listed, err := ti.service.List(ctx, &gen.ListPayload{Limit: 20, Cursor: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, listed.Skills, 1)
	require.Nil(t, listed.Skills[0].LatestVersionID)
	require.Zero(t, listed.Skills[0].VersionCount)

	details, err := ti.service.Get(ctx, &gen.GetPayload{
		ID: listed.Skills[0].ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Nil(t, details.LatestVersion)
	require.Equal(t, int64(1), details.Skill.SeenCount)
}

func TestListUnknownSkillActivationsPaginatesTerminalFailures(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	insertSkillObservationWithHostname(t, ti, "bad/name", "", "machine", now.Add(-time.Minute))
	insertSkillObservationWithHostname(t, ti, "unknown-hash", contentSHA256("unknown"), "machine", now)
	_, err := skills.ReconcileSkillObservations(ctx, ti.conn, ti.projectID, 10)
	require.NoError(t, err)

	first, err := ti.service.ListUnknownActivations(ctx, &gen.ListUnknownActivationsPayload{
		Limit: 1, Cursor: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, first.Activations, 1)
	require.Equal(t, "unknown-hash", first.Activations[0].SkillName)
	require.Equal(t, "unresolved_hash", first.Activations[0].Reason)
	require.NotNil(t, first.NextCursor)

	second, err := ti.service.ListUnknownActivations(ctx, &gen.ListUnknownActivationsPayload{
		Limit: 1, Cursor: first.NextCursor, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, second.Activations, 1)
	require.Equal(t, "bad/name", second.Activations[0].SkillName)
	require.Equal(t, "invalid_name", second.Activations[0].Reason)
	require.Nil(t, second.NextCursor)
}
