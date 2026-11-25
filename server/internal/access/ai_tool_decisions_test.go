package access

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	accessserver "github.com/speakeasy-api/gram/server/gen/http/access/server"
	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/aivendors/catalogseed"
	seedrepo "github.com/speakeasy-api/gram/server/internal/aivendors/catalogseed/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestService_SetAIToolDecision_RecordsDecisionAndSurfacesItOnTheInventory(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	require.NoError(t, catalogseed.EnsureCatalog(ctx, ti.conn))
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
	rows, err := agentrepo.New(ti.conn).ListAIScanTargets(ctx, orgID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, conv.ToPGText("claude-code"), rows[0].AiProductID)
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
	require.NoError(t, catalogseed.EnsureCatalog(ctx, ti.conn))
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
	require.NoError(t, catalogseed.EnsureCatalog(ctx, ti.conn))
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

func TestService_SetAIToolDecision_WaitsForCatalog(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	ctx, orgID, _ := withUniqueDetectionOrg(t, ctx, ti)
	_, err := ti.service.SetAIToolDecision(ctx, &gen.SetAIToolDecisionPayload{
		TargetID: "claude-code", Decision: "blocked",
	})
	require.Error(t, err)
	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, oops.CodeUnavailable, shareableErr.Code)
	response := httptest.NewRecorder()
	encode := accessserver.EncodeSetAIToolDecisionError(goahttp.ResponseEncoder, nil)
	require.NoError(t, encode(ctx, response, shareableErr.AsGoa(ctx)))
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	rows, err := agentrepo.New(ti.conn).ListAIScanTargets(ctx, orgID)
	require.NoError(t, err)
	require.Empty(t, rows)
}

var productRemovedAt = pgtype.Timestamptz{Time: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Valid: true, InfinityModifier: pgtype.Finite}

// removeProduct soft-deletes a seeded product and, when fresh, serves the catalog pods load afterwards.
func removeProduct(t *testing.T, ctx context.Context, ti *testInstance, id string, name string, fresh bool) {
	t.Helper()
	require.NoError(t, testrepo.New(ti.conn).EditProductFixture(ctx, testrepo.EditProductFixtureParams{ID: id, DisplayName: name, DeletedAt: productRemovedAt}))
	if fresh {
		snapshot, err := aivendors.Load(ctx, ti.conn, testenv.NewLogger(t), aitargets.ValidateCatalog)
		require.NoError(t, err)
		_, listed := aitargets.DefaultByID(snapshot, id)
		require.False(t, listed)
		ti.service.aiCatalog = aivendors.NewStore(snapshot)
	}
}

func TestService_SetAIToolDecision_RefusesNewDecisionForRemovedProduct(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		fresh bool
	}{
		{name: "catalog not yet refreshed", fresh: false},
		{name: "catalog refreshed", fresh: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestAccessService(t)
			ctx, orgID, _ := withUniqueDetectionOrg(t, ctx, ti)
			require.NoError(t, catalogseed.EnsureCatalog(ctx, ti.conn))
			removeProduct(t, ctx, ti, "claude-code", "Claude Code", tc.fresh)
			_, err := ti.service.SetAIToolDecision(ctx, &gen.SetAIToolDecisionPayload{
				TargetID: "claude-code", Decision: "blocked",
			})
			var shareableErr *oops.ShareableError
			require.ErrorAs(t, err, &shareableErr)
			require.Equal(t, oops.CodeFailedPrecondition, shareableErr.Code)
			require.Equal(t, removedAIToolMessage, shareableErr.Error())
			response := httptest.NewRecorder()
			encode := accessserver.EncodeSetAIToolDecisionError(goahttp.ResponseEncoder, nil)
			require.NoError(t, encode(ctx, response, shareableErr.AsGoa(ctx)))
			require.Less(t, response.Code, http.StatusInternalServerError, "the refusal is not retryable")
			rows, err := agentrepo.New(ti.conn).ListAIScanTargets(ctx, orgID)
			require.NoError(t, err)
			require.Empty(t, rows)
		})
	}
}

func TestService_SetAIToolDecision_EditsExistingDecisionForRemovedProduct(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		fresh bool
	}{
		{name: "catalog not yet refreshed", fresh: false},
		{name: "catalog refreshed", fresh: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestAccessService(t)
			ctx, orgID, _ := withUniqueDetectionOrg(t, ctx, ti)
			require.NoError(t, catalogseed.EnsureCatalog(ctx, ti.conn))
			_, err := ti.service.SetAIToolDecision(ctx, &gen.SetAIToolDecisionPayload{TargetID: "claude-code", Decision: "blocked"})
			require.NoError(t, err)
			removeProduct(t, ctx, ti, "claude-code", "Claude Code", tc.fresh)

			saved, err := ti.service.SetAIToolDecision(ctx, &gen.SetAIToolDecisionPayload{TargetID: "claude-code", Decision: "blocked", Rationale: new("Still not approved.")})
			require.NoError(t, err)
			require.Equal(t, "blocked", saved.Access.State, "the block still applies, so it reads blocked")
			require.True(t, saved.Access.Enforceable)
			require.Equal(t, "removed", conv.PtrValOr(saved.Access.CatalogStatus, ""))
			for _, decision := range []string{"approved", "unreviewed"} {
				saved, err := ti.service.SetAIToolDecision(ctx, &gen.SetAIToolDecisionPayload{TargetID: "claude-code", Decision: decision})
				require.NoError(t, err)
				require.Equal(t, "claude-code", saved.TargetID)
				rows, err := agentrepo.New(ti.conn).ListAIScanTargets(ctx, orgID)
				require.NoError(t, err)
				require.Len(t, rows, 1)
				require.Equal(t, decision, rows[0].Status)
				require.Equal(t, conv.ToPGText("claude-code"), rows[0].AiProductID, "the catalog link is kept")
			}
			record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAIToolDecisionSet)
			require.NoError(t, err)
			require.Equal(t, "Claude Code", record.SubjectDisplay)

			// Cleared, the leftover unreviewed row is not a decision, so a new one is refused.
			_, err = ti.service.SetAIToolDecision(ctx, &gen.SetAIToolDecisionPayload{TargetID: "claude-code", Decision: "blocked"})
			var shareableErr *oops.ShareableError
			require.ErrorAs(t, err, &shareableErr)
			require.Equal(t, oops.CodeFailedPrecondition, shareableErr.Code)
			require.Equal(t, removedAIToolMessage, shareableErr.Error())
		})
	}
}

func TestService_SetAIToolDecision_EditsExistingDecisionWhenEveryDocumentIsDisabled(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	ctx, orgID, _ := withUniqueDetectionOrg(t, ctx, ti)
	require.NoError(t, catalogseed.EnsureCatalog(ctx, ti.conn))
	seedAIDetection(t, ctx, ti, orgID, "zed", "serial-1", "alex@example.com", "installed", "harness", "", time.Now().UTC())
	_, err := ti.service.SetAIToolDecision(ctx, &gen.SetAIToolDecisionPayload{TargetID: "zed", Decision: "blocked"})
	require.NoError(t, err)
	docs, err := seedrepo.New(ti.conn).ListDocuments(ctx)
	require.NoError(t, err)
	for _, d := range docs {
		if d.AiProductID == "zed" || d.AiProductID == "goose" {
			require.NoError(t, testrepo.New(ti.conn).EditDocumentFixture(ctx, testrepo.EditDocumentFixtureParams{ID: d.ID, Url: d.Url, DisplayName: d.DisplayName, Enabled: false, DeletedAt: pgtype.Timestamptz{}}))
		}
	}
	snapshot, err := aivendors.Load(ctx, ti.conn, testenv.NewLogger(t), aitargets.ValidateCatalog)
	require.NoError(t, err)
	ti.service.aiCatalog = aivendors.NewStore(snapshot)

	listed, err := ti.service.ListAIDetections(ctx, &gen.ListAIDetectionsPayload{Category: nil, DirectoryGroupID: nil, SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, listed.Detections, 1)
	require.Equal(t, "blocked", listed.Detections[0].Access.State, "the inventory shows the block that still applies")
	require.Equal(t, "documents_disabled", conv.PtrValOr(listed.Detections[0].Access.CatalogStatus, ""))

	_, err = ti.service.SetAIToolDecision(ctx, &gen.SetAIToolDecisionPayload{TargetID: "zed", Decision: "unreviewed"})
	require.NoError(t, err, "a disabled document still enforces the block, so it stays clearable")
	rows, err := agentrepo.New(ti.conn).ListAIScanTargets(ctx, orgID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "unreviewed", rows[0].Status)

	for _, id := range []string{"zed", "goose"} {
		_, err = ti.service.SetAIToolDecision(ctx, &gen.SetAIToolDecisionPayload{TargetID: id, Decision: "blocked"})
		var shareableErr *oops.ShareableError
		require.ErrorAs(t, err, &shareableErr, id)
		require.Equal(t, oops.CodeBadRequest, shareableErr.Code, "%s: a new decision, or one after a leftover unreviewed row, still needs a document Speakeasy can admit", id)
	}
}

func TestService_SetAIToolDecision_LinksExistingDecision(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	ctx, orgID, _ := withUniqueDetectionOrg(t, ctx, ti)
	q := agentrepo.New(ti.conn)
	_, err := q.SetAIScanTargetStatus(ctx, agentrepo.SetAIScanTargetStatusParams{
		OrganizationID: orgID, ID: "claude-code", Status: "approved",
	})
	require.NoError(t, err)
	require.NoError(t, catalogseed.EnsureCatalog(ctx, ti.conn))
	_, err = ti.service.SetAIToolDecision(ctx, &gen.SetAIToolDecisionPayload{
		TargetID: "claude-code", Decision: "blocked",
	})
	require.NoError(t, err)
	rows, err := q.ListAIScanTargets(ctx, orgID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, conv.ToPGText("claude-code"), rows[0].AiProductID)
	require.Equal(t, "blocked", rows[0].Status)
}

func TestService_SetAIToolDecision_LeavesCustomTargetUnlinked(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		id     string
		seeded bool
	}{
		{name: "custom ID", id: "custom-tool", seeded: false},
		{name: "built-in ID before bootstrap", id: "codex", seeded: false},
		{name: "built-in ID after bootstrap", id: "codex", seeded: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestAccessService(t)
			ctx, orgID, _ := withUniqueDetectionOrg(t, ctx, ti)
			q := agentrepo.New(ti.conn)
			original, err := q.UpsertAIScanTarget(ctx, agentrepo.UpsertAIScanTargetParams{
				OrganizationID: orgID, ID: tc.id, DisplayName: conv.ToPGText("Custom tool"),
				Category: conv.ToPGText("harness"), BundleIds: []string{}, Binaries: []string{"custom-tool"},
				ConfigDirs: []string{}, ProcessNames: []string{}, CimdVendorKeys: []string{},
				OauthClientIds: []string{"https://example.com/client.json"}, ClientInfoNames: []string{},
			})
			require.NoError(t, err)
			if tc.seeded {
				require.NoError(t, catalogseed.Apply(ctx, testenv.NewLogger(t), ti.conn))
			}
			_, err = ti.service.SetAIToolDecision(ctx, &gen.SetAIToolDecisionPayload{
				TargetID: tc.id, Decision: "blocked",
			})
			require.NoError(t, err)
			rows, err := q.ListAIScanTargets(ctx, orgID)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.False(t, rows[0].AiProductID.Valid)
			require.Equal(t, "blocked", rows[0].Status)
			original.Status = "blocked"
			original.UpdatedAt = rows[0].UpdatedAt
			require.Equal(t, original, rows[0], "a decision update preserves the custom definition and reference")
		})
	}
}

func TestService_ListAIDetections_ShowsBlockOnRemovedProduct(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	ctx, orgID, _ := withUniqueDetectionOrg(t, ctx, ti)
	require.NoError(t, catalogseed.EnsureCatalog(ctx, ti.conn))
	seedAIDetection(t, ctx, ti, orgID, "claude-code", "serial-1", "alex@example.com", "installed", "harness", "", time.Now().UTC())
	_, err := ti.service.SetAIToolDecision(ctx, &gen.SetAIToolDecisionPayload{TargetID: "claude-code", Decision: "blocked"})
	require.NoError(t, err)
	removeProduct(t, ctx, ti, "claude-code", "Claude Code", true)

	listed, err := ti.service.ListAIDetections(ctx, &gen.ListAIDetectionsPayload{Category: nil, DirectoryGroupID: nil, SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, listed.Detections, 1)
	require.Equal(t, "Claude Code", listed.Detections[0].DisplayName)
	require.Equal(t, "blocked", listed.Detections[0].Access.State)
	require.True(t, listed.Detections[0].Access.Enforceable)
	require.Equal(t, "removed", conv.PtrValOr(listed.Detections[0].Access.CatalogStatus, ""))
}

func TestService_SetAIToolDecision_LiveProductWhoseLastDocumentWasRemoved(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	ctx, orgID, _ := withUniqueDetectionOrg(t, ctx, ti)
	require.NoError(t, catalogseed.EnsureCatalog(ctx, ti.conn))
	seedAIDetection(t, ctx, ti, orgID, "zed", "serial-1", "alex@example.com", "installed", "harness", "", time.Now().UTC())
	_, err := ti.service.SetAIToolDecision(ctx, &gen.SetAIToolDecisionPayload{TargetID: "zed", Decision: "blocked"})
	require.NoError(t, err)
	docs, err := seedrepo.New(ti.conn).ListDocuments(ctx)
	require.NoError(t, err)
	for _, d := range docs {
		if d.AiProductID == "zed" {
			require.NoError(t, testrepo.New(ti.conn).EditDocumentFixture(ctx, testrepo.EditDocumentFixtureParams{ID: d.ID, Url: d.Url, DisplayName: d.DisplayName, Enabled: true, DeletedAt: productRemovedAt}))
		}
	}
	snapshot, err := aivendors.Load(ctx, ti.conn, testenv.NewLogger(t), aitargets.ValidateCatalog)
	require.NoError(t, err)
	ti.service.aiCatalog = aivendors.NewStore(snapshot)

	listed, err := ti.service.ListAIDetections(ctx, &gen.ListAIDetectionsPayload{Category: nil, DirectoryGroupID: nil, SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, listed.Detections, 1)
	require.Equal(t, "blocked", listed.Detections[0].Access.State, "the gateway still enforces it through the removed document")
	require.Equal(t, "documents_disabled", conv.PtrValOr(listed.Detections[0].Access.CatalogStatus, ""))

	_, err = ti.service.SetAIToolDecision(ctx, &gen.SetAIToolDecisionPayload{TargetID: "zed", Decision: "unreviewed"})
	require.NoError(t, err)
	_, err = ti.service.SetAIToolDecision(ctx, &gen.SetAIToolDecisionPayload{TargetID: "zed", Decision: "blocked"})
	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, oops.CodeBadRequest, shareableErr.Code)
}
