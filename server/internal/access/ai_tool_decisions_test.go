package access

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestService_SetAIToolDecision_RecordsDecisionAndSurfacesItOnTheInventory(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx, orgID, _ := withUniqueDetectionOrg(t, ctx, ti)
	seedAIDetection(t, ctx, ti, orgID, "claude-code", "serial-1", "alex@example.com", "installed", "harness", "", time.Now().UTC())

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAIToolDecisionSet)
	require.NoError(t, err)

	saved, err := ti.service.SetAIToolDecision(ctx, &gen.SetAIToolDecisionPayload{
		TargetID:     "claude-code",
		Decision:     "blocked",
		Rationale:    new("Not on the approved list."),
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "claude-code", saved.TargetID)
	require.Equal(t, "blocked", saved.Access.State)
	require.Equal(t, "blocked", saved.Access.Decision)
	require.True(t, saved.Access.Enforceable)
	require.Equal(t, "Not on the approved list.", conv.PtrValOr(saved.Access.Rationale, ""))

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAIToolDecisionSet)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	listed, err := ti.service.ListAIDetections(ctx, &gen.ListAIDetectionsPayload{Category: nil, DirectoryGroupID: nil, SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, listed.Detections, 1)
	require.Equal(t, "blocked", listed.Detections[0].Access.State)
}

func TestService_SetAIToolDecision_EnforceableForAVerifiedMatcher(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx, orgID, _ := withUniqueDetectionOrg(t, ctx, ti)
	seedAIDetection(t, ctx, ti, orgID, "claude-code", "serial-1", "alex@example.com", "installed", "harness", "", time.Now().UTC())

	saved, err := ti.service.SetAIToolDecision(ctx, &gen.SetAIToolDecisionPayload{
		TargetID:     "claude-code",
		Decision:     "blocked",
		Rationale:    nil,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.True(t, saved.Access.Enforceable, "a CIMD vendor key is a credential the server verifies")
}

// TestService_ListAIDetections_UnenforceableToolReadsUnreviewed: a tool that
// publishes no client ID metadata document cannot be recognized at the
// gateway, so no decision about it can mean anything. The inventory reads
// unreviewed rather than reporting a verdict enforcement cannot deliver, and
// the decision is refused outright rather than stored and then ignored.
func TestService_ListAIDetections_UnenforceableToolReadsUnreviewed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx, orgID, _ := withUniqueDetectionOrg(t, ctx, ti)
	seedAIDetection(t, ctx, ti, orgID, "cursor", "serial-1", "alex@example.com", "installed", "harness", "", time.Now().UTC())

	_, err := ti.service.SetAIToolDecision(ctx, &gen.SetAIToolDecisionPayload{
		TargetID:     "cursor",
		Decision:     "blocked",
		Rationale:    nil,
		SessionToken: nil,
	})
	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, oops.CodeBadRequest, shareableErr.Code)

	listed, err := ti.service.ListAIDetections(ctx, &gen.ListAIDetectionsPayload{Category: nil, DirectoryGroupID: nil, SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, listed.Detections, 1)
	require.Equal(t, "unreviewed", listed.Detections[0].Access.State)
	require.False(t, listed.Detections[0].Access.Enforceable)
}

func TestService_SetAIToolDecision_RejectsUnknownTarget(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx, _, _ = withUniqueDetectionOrg(t, ctx, ti)

	_, err := ti.service.SetAIToolDecision(ctx, &gen.SetAIToolDecisionPayload{
		TargetID:     "not-a-catalog-target",
		Decision:     "blocked",
		Rationale:    nil,
		SessionToken: nil,
	})
	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, oops.CodeNotFound, shareableErr.Code)
}

func TestService_SetAIToolDecision_RequiresOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	// Project read is enough to see the inventory and deliberately not enough
	// to change it: a block reaches every project's gateway.
	ctx = withRBACGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeProjectRead,
		Selector: authz.NewSelector(authz.ScopeProjectRead, authCtx.ProjectID.String()),
	})

	_, err := ti.service.SetAIToolDecision(ctx, &gen.SetAIToolDecisionPayload{
		TargetID:     "claude-code",
		Decision:     "blocked",
		Rationale:    nil,
		SessionToken: nil,
	})
	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, oops.CodeForbidden, shareableErr.Code)
}

func TestService_ListAIDetections_ProjectReaderGetsNoAttribution(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	clone := *authCtx
	clone.ActiveOrganizationID = "detections-test-org-" + uuid.NewString()
	seedOrganization(t, ctx, ti.conn, clone.ActiveOrganizationID)
	// The project the section is reached through has to belong to the org
	// whose detections are being read; the per-test org keeps this package's
	// parallel ClickHouse writes from bleeding into each other.
	project := createShadowMCPProject(t, ctx, ti, clone.ActiveOrganizationID)
	clone.ProjectID = &project.ID
	ctx = contextvalues.SetAuthContext(ctx, &clone)
	ctx = withRBACGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeProjectRead,
		Selector: authz.NewSelector(authz.ScopeProjectRead, project.ID.String()),
	})

	seedAIDetection(t, ctx, ti, clone.ActiveOrganizationID, "cursor", "serial-1", "alex@example.com", "installed", "harness", "", time.Now().UTC())

	result, err := ti.service.ListAIDetections(ctx, &gen.ListAIDetectionsPayload{Category: nil, DirectoryGroupID: nil, SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, result.Detections, 1)

	detection := result.Detections[0]
	// The tool-level answer a project viewer is here for.
	require.Equal(t, "cursor", detection.TargetID)
	require.Equal(t, "Cursor", detection.DisplayName)
	require.NotNil(t, detection.Access)
	require.Equal(t, "unreviewed", detection.Access.State)
	// Nothing that reaches a person.
	require.Nil(t, detection.UserCount)
	require.Nil(t, detection.DeviceCount)
	require.Nil(t, detection.Access.DecidedBy)
	require.Nil(t, detection.Access.DecidedAt)
	require.Nil(t, detection.Access.Rationale)
}

func TestService_ListAIDetections_ProjectReaderCannotFilterByTeam(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	clone := *authCtx
	clone.ActiveOrganizationID = "detections-test-org-" + uuid.NewString()
	groupID := seedAIDetectionDirectoryGroup(t, ctx, ti.conn, clone.ActiveOrganizationID, []string{"alex@example.com"})
	project := createShadowMCPProject(t, ctx, ti, clone.ActiveOrganizationID)
	clone.ProjectID = &project.ID
	ctx = contextvalues.SetAuthContext(ctx, &clone)
	ctx = withRBACGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeProjectRead,
		Selector: authz.NewSelector(authz.ScopeProjectRead, project.ID.String()),
	})

	// Narrowing an organization-wide inventory to one team and reading the
	// result off is attribution by another route.
	_, err := ti.service.ListAIDetections(ctx, &gen.ListAIDetectionsPayload{
		Category:         nil,
		DirectoryGroupID: new(groupID.String()),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, oops.CodeForbidden, shareableErr.Code)
}
