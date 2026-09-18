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
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

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

	// A block is the one decision whose cost lands on a person: the tool's
	// users get an OAuth error their client cannot explain. The entry is what
	// answers "who did this, to what, and what was it before", so every field
	// that answers one of those is pinned here — a count alone would pass with
	// the wrong admin named, the wrong tool blocked, or no snapshots at all.
	//
	// The actor and subject type strings are spelled out rather than taken
	// from the constants: they are the stored vocabulary the audit feed filters
	// on, so a rename has to be a deliberate migration, not a test that follows
	// the code wherever it goes.
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAIToolDecisionSet)
	require.NoError(t, err)
	require.Equal(t, orgID, record.OrganizationID)
	require.False(t, record.ProjectID.Valid, "the decision reaches every project's gateway, so it belongs to no single project")
	require.Equal(t, authCtx.UserID, record.ActorID, "the admin who decided, not the employee the detection is about")
	require.Equal(t, "user", record.ActorType)
	require.NotEmpty(t, record.ActorDisplay, "the entry names the admin without a second lookup")
	require.Equal(t, conv.PtrValOr(authCtx.Email, ""), record.ActorDisplay)
	require.Empty(t, record.ActorSlug, "a user actor carries no organization slug")
	require.Equal(t, "ai_tool_decision", record.SubjectType)
	require.Equal(t, orgID+"/claude-code", record.SubjectID, "the decision urn is org-scoped: the same tool is decided separately by each organization")
	require.Equal(t, "Claude Code", record.SubjectDisplay)

	// The pair of snapshots is the whole point of the entry: an admin reading
	// the feed has to see that this was the moment the tool left unreviewed.
	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, "unreviewed", beforeSnapshot["Decision"])
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "claude-code", afterSnapshot["TargetID"])
	require.Equal(t, "blocked", afterSnapshot["Decision"])
	require.Equal(t, "Not on the approved list.", afterSnapshot["Rationale"])

	listed, err := ti.service.ListAIDetections(ctx, &gen.ListAIDetectionsPayload{Category: nil, DirectoryGroupID: nil, SessionToken: nil})
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
// carries no matcher at all, neither a client ID metadata document nor a
// reported client name, cannot be recognized at either enforcement layer, so
// no decision about it can mean anything. A local model runner never calls the
// gateway and is the case in point. The inventory reads unreviewed rather than
// reporting a verdict enforcement cannot deliver, and the decision is refused
// outright rather than stored and then ignored.
func TestService_ListAIDetections_UnenforceableToolReadsUnreviewed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx, orgID, _ := withUniqueDetectionOrg(t, ctx, ti)
	seedAIDetection(t, ctx, ti, orgID, "ollama", "serial-1", "alex@example.com", "installed", "local_model", "", time.Now().UTC())

	_, err := ti.service.SetAIToolDecision(ctx, &gen.SetAIToolDecisionPayload{
		TargetID:     "ollama",
		Decision:     "blocked",
		Rationale:    nil,
		SessionToken: nil,
	})
	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, oops.CodeBadRequest, shareableErr.Code)

	listed, err := ti.service.ListAIDetections(ctx, &gen.ListAIDetectionsPayload{Category: nil, DirectoryGroupID: nil, SessionToken: nil})
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

// The organization-wide inventory is an admin surface: it names how many
// people run each tool. Project read does not open it.
func TestService_ListAIDetections_ProjectReaderIsRefused(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	clone := *authCtx
	clone.ActiveOrganizationID = "detections-test-org-" + uuid.NewString()
	seedOrganization(t, ctx, ti.conn, clone.ActiveOrganizationID)
	project := createShadowMCPProject(t, ctx, ti, clone.ActiveOrganizationID)
	clone.ProjectID = &project.ID
	ctx = contextvalues.SetAuthContext(ctx, &clone)
	ctx = withRBACGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeProjectRead,
		Selector: authz.NewSelector(authz.ScopeProjectRead, project.ID.String()),
	})

	seedAIDetection(t, ctx, ti, clone.ActiveOrganizationID, "cursor", "serial-1", "alex@example.com", "installed", "harness", "", time.Now().UTC())

	_, err := ti.service.ListAIDetections(ctx, &gen.ListAIDetectionsPayload{Category: nil, DirectoryGroupID: nil, SessionToken: nil})
	require.Error(t, err, "project read does not open the organization-wide inventory")
}

// listEmployeeAIDetections stays on project read, because the caller has
// already named the one employee it answers for. It carries the same model,
// though, and that model now holds an access decision — so the administrator
// who recorded it does not ride along.
//
// The decision has to be real for this to prove anything: with no row, the
// three fields are nil whatever the redaction does.
func TestService_ListEmployeeAIDetections_HidesWhoDecided(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx, orgID, _ := withUniqueDetectionOrg(t, ctx, ti)
	seedAIDetection(t, ctx, ti, orgID, "claude-code", "serial-1", "alex@example.com", "installed", "harness", "", time.Now().UTC())

	saved, err := ti.service.SetAIToolDecision(ctx, &gen.SetAIToolDecisionPayload{
		TargetID:     "claude-code",
		Decision:     "blocked",
		Rationale:    new("Not on the approved list."),
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, saved.Access.Rationale, "the reason is recorded, and is what must not travel")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	project := createShadowMCPProject(t, ctx, ti, orgID)
	clone := *authCtx
	clone.ProjectID = &project.ID
	ctx = contextvalues.SetAuthContext(ctx, &clone)
	ctx = withRBACGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeProjectRead,
		Selector: authz.NewSelector(authz.ScopeProjectRead, project.ID.String()),
	})

	result, err := ti.service.ListEmployeeAIDetections(ctx, &gen.ListEmployeeAIDetectionsPayload{
		UserEmail:    "alex@example.com",
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Detections, 1)

	detection := result.Detections[0]
	require.NotNil(t, detection.Access)
	require.Equal(t, "blocked", detection.Access.State, "the state itself reaches no person and stays")
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
	})
	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, oops.CodeForbidden, shareableErr.Code)
}
