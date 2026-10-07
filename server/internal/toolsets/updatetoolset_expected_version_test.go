package toolsets_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
)

// toolListUpdate is the whole-array tool-list write the dashboard sends,
// optionally carrying the version_token of the read it was computed from.
func toolListUpdate(slug types.Slug, name string, toolURNs []string, expected *string) *gen.UpdateToolsetPayload {
	return &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		Slug:                   slug,
		Name:                   &name,
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		PromptTemplateNames:    nil,
		ToolUrns:               toolURNs,
		ResourceUrns:           nil,
		McpEnabled:             nil,
		McpSlug:                nil,
		McpIsPublic:            nil,
		NetworkAccessMode:      nil,
		CustomDomainID:         nil,
		ToolSelectionMode:      nil,
		ExpectedVersionToken:   expected,
		ProjectSlugInput:       nil,
	}
}

// concurrentToolEditFixture creates a toolset with one tool and returns it as
// the first editor read it, plus the petstore tool URNs to edit with.
func concurrentToolEditFixture(t *testing.T, ctx context.Context, ti *testInstance) (*types.Toolset, []string) {
	t.Helper()

	dep := createPetstoreDeployment(t, ctx, ti)
	tools, err := testrepo.New(ti.conn).ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(tools), 3)
	urns := []string{tools[0].ToolUrn.String(), tools[1].ToolUrn.String(), tools[2].ToolUrn.String()}

	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Concurrent " + uuid.NewString()[:8],
		Description:            nil,
		ToolUrns:               urns[:1],
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	read, err := ti.service.GetToolset(ctx, &gen.GetToolsetPayload{SessionToken: nil, ApikeyToken: nil, Slug: string(created.Slug), ProjectSlugInput: nil})
	require.NoError(t, err)
	require.NotEmpty(t, read.VersionToken, "a toolset read must report the token an update can be checked against")
	return read, urns
}

func TestToolsetsService_UpdateToolset_StaleExpectedVersionIsRefused(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	staleRead, urns := concurrentToolEditFixture(t, ctx, ti)

	// A second editor saves first, from the same read.
	_, err := ti.service.UpdateToolset(ctx, toolListUpdate(staleRead.Slug, staleRead.Name, []string{urns[0], urns[1]}, &staleRead.VersionToken))
	require.NoError(t, err)
	afterWinner, err := ti.service.GetToolset(ctx, &gen.GetToolsetPayload{SessionToken: nil, ApikeyToken: nil, Slug: string(staleRead.Slug), ProjectSlugInput: nil})
	require.NoError(t, err)
	require.NotEqual(t, staleRead.VersionToken, afterWinner.VersionToken, "a tool-list change must move the token")

	auditBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)

	// The first editor's save was computed from the older read: refused, not applied.
	_, err = ti.service.UpdateToolset(ctx, toolListUpdate(staleRead.Slug, "Renamed by the loser", []string{urns[0], urns[2]}, &staleRead.VersionToken))
	require.Error(t, err)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeConflict, shareable.Code)
	require.ErrorIs(t, err, toolsets.ErrToolsetVersionConflict)

	// Nothing about the toolset changed: not the tools, not the name, not the version.
	after, err := ti.service.GetToolset(ctx, &gen.GetToolsetPayload{SessionToken: nil, ApikeyToken: nil, Slug: string(staleRead.Slug), ProjectSlugInput: nil})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{urns[0], urns[1]}, after.ToolUrns)
	require.Equal(t, afterWinner.Name, after.Name)
	require.Equal(t, afterWinner.ToolsetVersion, after.ToolsetVersion)
	require.Equal(t, afterWinner.VersionToken, after.VersionToken)

	auditAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)
	require.Equal(t, auditBefore, auditAfter, "a refused update must not be audited as an update")
}

func TestToolsetsService_UpdateToolset_MatchingExpectedVersionSucceeds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	read, urns := concurrentToolEditFixture(t, ctx, ti)

	result, err := ti.service.UpdateToolset(ctx, toolListUpdate(read.Slug, read.Name, []string{urns[0], urns[1]}, &read.VersionToken))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{urns[0], urns[1]}, result.ToolUrns)
	require.Equal(t, read.ToolsetVersion+1, result.ToolsetVersion)
	require.NotEqual(t, read.VersionToken, result.VersionToken)

	// The token the update returned is itself a valid basis for the next save.
	chained, err := ti.service.UpdateToolset(ctx, toolListUpdate(read.Slug, read.Name, []string{urns[2]}, &result.VersionToken))
	require.NoError(t, err)
	require.Equal(t, []string{urns[2]}, chained.ToolUrns)
}

func TestToolsetsService_UpdateToolset_OmittedExpectedVersionKeepsLastWriteWins(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	staleRead, urns := concurrentToolEditFixture(t, ctx, ti)

	_, err := ti.service.UpdateToolset(ctx, toolListUpdate(staleRead.Slug, staleRead.Name, []string{urns[0], urns[1]}, nil))
	require.NoError(t, err)

	// An older caller that never sends a token still overwrites, as before.
	result, err := ti.service.UpdateToolset(ctx, toolListUpdate(staleRead.Slug, staleRead.Name, []string{urns[2]}, nil))
	require.NoError(t, err)
	require.Equal(t, []string{urns[2]}, result.ToolUrns)
}
