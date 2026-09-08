package platformaiscantargets

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/platform_ai_scan_targets"
	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, code, shareable.Code)
}

func classicPayload() *gen.UpsertPayload {
	return &gen.UpsertPayload{
		SessionToken: nil,
		ID:           "chatgpt-classic",
		DisplayName:  "ChatGPT Classic",
		Category:     "harness",
		Signatures: &gen.AiScanTargetSignatures{
			BundleIds:    []string{"com.openai.chat"},
			Binaries:     []string{},
			ConfigDirs:   []string{},
			ProcessNames: []string{},
		},
		VersionPlistKey: nil,
		Enabled:         nil,
		Reason:          new("customer asked for ChatGPT Classic visibility"),
	}
}

func TestListRequiresPlatformAdmin(t *testing.T) {
	t.Parallel()
	ti := newTestService(t)

	_, err := ti.service.List(memberContext(t), &gen.ListPayload{SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestListSeedsAndReturnsTheCatalog(t *testing.T) {
	t.Parallel()
	ti := newTestService(t)

	result, err := ti.service.List(readOnlyAdminContext(t), &gen.ListPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, len(aitargets.Defaults()), result.ListVersion)
	require.NotEmpty(t, result.Etag)
	require.Len(t, result.Targets, len(aitargets.Defaults()))
	require.Equal(t, "aider", result.Targets[0].ID)
	require.Equal(t, "claude-code", result.Targets[1].ID)
	require.Equal(t, "windsurf", result.Targets[len(result.Targets)-1].ID)
	require.True(t, result.Targets[0].Enabled)
	require.NotEmpty(t, result.Targets[0].CreatedAt)
}

func TestUpsertRequiresFreshPlatformAdminSession(t *testing.T) {
	t.Parallel()
	ti := newTestService(t)

	_, err := ti.service.Upsert(readOnlyAdminContext(t), classicPayload())
	requireOopsCode(t, err, oops.CodeUnauthorized)

	ti.admins.admin = false
	_, err = ti.service.Upsert(freshAdminContext(t), classicPayload())
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestUpsertCreatesATargetAndBumpsTheListVersion(t *testing.T) {
	t.Parallel()
	ti := newTestService(t)
	ctx := freshAdminContext(t)

	before, err := ti.service.List(ctx, &gen.ListPayload{SessionToken: nil})
	require.NoError(t, err)

	created, err := ti.service.Upsert(ctx, classicPayload())
	require.NoError(t, err)
	require.Equal(t, before.ListVersion+1, created.ListVersion)
	require.Equal(t, "chatgpt-classic", created.Target.ID)
	require.True(t, created.Target.Enabled, "enabled defaults to true")
	require.Equal(t, []string{"com.openai.chat"}, created.Target.Signatures.BundleIds)
	require.Nil(t, created.Target.VersionPlistKey)

	snapshot, err := ti.catalog.Load(ctx)
	require.NoError(t, err)
	require.EqualValues(t, created.ListVersion, snapshot.ListVersion)
	served, ok := snapshot.ByID("chatgpt-classic")
	require.True(t, ok)
	require.Equal(t, "ChatGPT Classic", served.DisplayName)

	revisions, err := ti.service.ListRevisions(ctx, &gen.ListRevisionsPayload{SessionToken: nil, Limit: 1})
	require.NoError(t, err)
	require.Len(t, revisions.Revisions, 1)
	latest := revisions.Revisions[0]
	require.Equal(t, created.ListVersion, latest.Revision)
	require.Equal(t, aitargets.ActionUpsert, latest.Action)
	require.Equal(t, "chatgpt-classic", latest.TargetID)
	require.Equal(t, "user-1", *latest.ActorUserID)
	require.Equal(t, "admin@example.com", *latest.ActorEmail)
	require.Equal(t, "customer asked for ChatGPT Classic visibility", *latest.Reason)
	require.Nil(t, latest.TargetBefore, "a creation has no before state")
	require.NotNil(t, latest.TargetAfter)
}

func TestUpsertReplacesAnExistingTargetAndRecordsBefore(t *testing.T) {
	t.Parallel()
	ti := newTestService(t)
	ctx := freshAdminContext(t)

	_, err := ti.service.Upsert(ctx, classicPayload())
	require.NoError(t, err)

	renamed := classicPayload()
	renamed.DisplayName = "ChatGPT (Classic)"
	renamed.VersionPlistKey = new("CFBundleVersion")
	renamed.Signatures.ProcessNames = []string{"ChatGPT"}
	updated, err := ti.service.Upsert(ctx, renamed)
	require.NoError(t, err)
	require.Equal(t, "ChatGPT (Classic)", updated.Target.DisplayName)
	require.Equal(t, "CFBundleVersion", *updated.Target.VersionPlistKey)
	require.Equal(t, []string{"ChatGPT"}, updated.Target.Signatures.ProcessNames)

	revisions, err := ti.service.ListRevisions(ctx, &gen.ListRevisionsPayload{SessionToken: nil, Limit: 1})
	require.NoError(t, err)
	latest := revisions.Revisions[0]
	require.NotNil(t, latest.TargetBefore)
	beforeState, ok := latest.TargetBefore.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "ChatGPT Classic", beforeState["display_name"])
}

func TestUpsertRejectsATargetTheAgentWouldRefuse(t *testing.T) {
	t.Parallel()
	ti := newTestService(t)
	ctx := freshAdminContext(t)

	escaping := classicPayload()
	escaping.Signatures.Binaries = []string{"../../etc/passwd"}
	_, err := ti.service.Upsert(ctx, escaping)
	requireOopsCode(t, err, oops.CodeBadRequest)

	homeOnly := classicPayload()
	homeOnly.Signatures.ConfigDirs = []string{"~/"}
	_, err = ti.service.Upsert(ctx, homeOnly)
	requireOopsCode(t, err, oops.CodeBadRequest)

	padded := classicPayload()
	padded.Signatures.Binaries = []string{" ", ""}
	created, err := ti.service.Upsert(ctx, padded)
	require.NoError(t, err)
	require.Empty(t, created.Target.Signatures.Binaries)
}

func TestSetEnabledTogglesServingAndRecordsTheAction(t *testing.T) {
	t.Parallel()
	ti := newTestService(t)
	ctx := freshAdminContext(t)

	disabled, err := ti.service.SetEnabled(ctx, &gen.SetEnabledPayload{SessionToken: nil, ID: "aider", Enabled: false, Reason: nil})
	require.NoError(t, err)
	require.False(t, disabled.Target.Enabled)

	snapshot, err := ti.catalog.Load(ctx)
	require.NoError(t, err)
	_, served := snapshot.ByID("aider")
	require.False(t, served, "a disabled target is not served to agents")

	listed, err := ti.service.List(ctx, &gen.ListPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, disabled.ListVersion, listed.ListVersion)
	require.Len(t, listed.Targets, len(aitargets.Defaults()), "the management listing keeps disabled targets")

	enabled, err := ti.service.SetEnabled(ctx, &gen.SetEnabledPayload{SessionToken: nil, ID: "aider", Enabled: true, Reason: nil})
	require.NoError(t, err)
	require.True(t, enabled.Target.Enabled)
	require.Equal(t, disabled.ListVersion+1, enabled.ListVersion)

	revisions, err := ti.service.ListRevisions(ctx, &gen.ListRevisionsPayload{SessionToken: nil, Limit: 2})
	require.NoError(t, err)
	require.Equal(t, aitargets.ActionEnable, revisions.Revisions[0].Action)
	require.Equal(t, aitargets.ActionDisable, revisions.Revisions[1].Action)
}

func TestSetEnabledAndDeleteReportUnknownTargets(t *testing.T) {
	t.Parallel()
	ti := newTestService(t)
	ctx := freshAdminContext(t)

	_, err := ti.service.SetEnabled(ctx, &gen.SetEnabledPayload{SessionToken: nil, ID: "never-added", Enabled: false, Reason: nil})
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = ti.service.Delete(ctx, &gen.DeletePayload{SessionToken: nil, ID: "never-added", Reason: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDeleteTombstonesAndUpsertRevives(t *testing.T) {
	t.Parallel()
	ti := newTestService(t)
	ctx := freshAdminContext(t)

	deleted, err := ti.service.Delete(ctx, &gen.DeletePayload{SessionToken: nil, ID: "aider", Reason: new("retired")})
	require.NoError(t, err)

	listed, err := ti.service.List(ctx, &gen.ListPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, deleted.ListVersion, listed.ListVersion)
	require.Len(t, listed.Targets, len(aitargets.Defaults())-1)

	snapshot, err := ti.catalog.Load(ctx)
	require.NoError(t, err)
	_, served := snapshot.ByID("aider")
	require.False(t, served)

	_, err = ti.service.Delete(ctx, &gen.DeletePayload{SessionToken: nil, ID: "aider", Reason: nil})
	requireOopsCode(t, err, oops.CodeNotFound)

	revived, err := ti.service.Upsert(ctx, &gen.UpsertPayload{
		SessionToken:    nil,
		ID:              "aider",
		DisplayName:     "Aider",
		Category:        "harness",
		Signatures:      &gen.AiScanTargetSignatures{BundleIds: []string{}, Binaries: []string{"aider"}, ConfigDirs: []string{"~/.aider"}, ProcessNames: []string{"aider"}},
		VersionPlistKey: nil,
		Enabled:         nil,
		Reason:          nil,
	})
	require.NoError(t, err)
	require.Equal(t, deleted.ListVersion+1, revived.ListVersion)

	revisions, err := ti.service.ListRevisions(ctx, &gen.ListRevisionsPayload{SessionToken: nil, Limit: 1})
	require.NoError(t, err)
	require.Nil(t, revisions.Revisions[0].TargetBefore, "a revived tombstone has no live before state")

	listed, err = ti.service.List(ctx, &gen.ListPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, listed.Targets, len(aitargets.Defaults()))
}
