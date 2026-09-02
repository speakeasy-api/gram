package hostedmcpbackfill

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func requireOnlyOutcome(t *testing.T, report Report, outcome Outcome) {
	t.Helper()
	require.Len(t, report.Outcomes, 1)
	require.Equal(t, report.Scanned, report.Outcomes[outcome])
}

func rowFor(t *testing.T, report Report, toolsetID uuid.UUID) RowReport {
	t.Helper()
	for _, row := range report.Rows {
		if row.ToolsetID == toolsetID {
			return row
		}
	}
	t.Fatalf("no row for toolset %s", toolsetID)
	return RowReport{}
}

func TestRun_DryRunWritesNothing(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "org-dry", public: true, enabled: true})

	report := f.run(t, Options{})

	require.Equal(t, "dry-run", report.Mode)
	require.Equal(t, 1, report.Scanned)
	requireOnlyOutcome(t, report, OutcomeWouldCreate)
	require.Equal(t, toolsetID, report.Rows[0].ToolsetID)
	require.Len(t, report.Rows[0].Endpoints, 1)
	count, err := New(f.pool).CountWrappersFixture(t.Context(), uuid.NullUUID{UUID: toolsetID, Valid: true})
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestRun_ApplyCreatesWrapperAndEndpoint(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	issuerID := f.seedIssuer(t)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "org-created", public: true, enabled: true, issuerID: uuid.NullUUID{UUID: issuerID, Valid: true}})

	report := f.apply(t)

	requireOnlyOutcome(t, report, OutcomeCreated)
	w := f.wrapper(t, toolsetID)
	require.Equal(t, uuid.NewSHA1(idNamespace, []byte("mcp_server:"+toolsetID.String())), w.ID)
	require.Equal(t, "public", w.Visibility)
	require.Equal(t, uuid.NullUUID{UUID: issuerID, Valid: true}, w.UserSessionIssuerID)
	require.Equal(t, "hosted-org-created-"+w.ID.String()[len(w.ID.String())-4:], w.Slug.String)

	eps := f.endpoints(t, w.ID)
	require.Len(t, eps, 1)
	require.Equal(t, "org-created", eps[0].Slug)
	require.False(t, eps[0].CustomDomainID.Valid)
	require.False(t, eps[0].Deleted)
}

func TestRun_SecondApplyWritesNothing(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "org-twice", enabled: true})

	first := f.apply(t)
	second := f.apply(t)

	requireOnlyOutcome(t, first, OutcomeCreated)
	requireOnlyOutcome(t, second, OutcomeAlreadyComplete)
	require.False(t, second.Rows[0].Endpoints[0].Created)
	require.Equal(t, first.Rows[0].Endpoints[0].ID, second.Rows[0].Endpoints[0].ID)
	require.Len(t, f.endpoints(t, f.wrapper(t, toolsetID).ID), 1)
}

func TestRun_RerunAfterSlugRenameMovesEndpoint(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "org-before", enabled: true})
	f.apply(t)
	require.NoError(t, New(f.pool).UpdateToolsetSlugFixture(t.Context(), UpdateToolsetSlugFixtureParams{McpSlug: conv.ToPGText("org-after"), ID: toolsetID}))

	dry := f.run(t, Options{})
	requireOnlyOutcome(t, dry, OutcomeWouldAdopt)
	require.True(t, dry.Rows[0].Endpoints[0].Moved)

	report := f.apply(t)
	requireOnlyOutcome(t, report, OutcomeAdopted)
	eps := f.endpoints(t, f.wrapper(t, toolsetID).ID)
	require.Len(t, eps, 1)
	require.Equal(t, "org-after", eps[0].Slug)
	require.Equal(t, uuid.NewSHA1(idNamespace, []byte("mcp_endpoint:primary:"+toolsetID.String())), eps[0].ID)
	requireOnlyOutcome(t, f.apply(t), OutcomeAlreadyComplete)
}

func TestRun_SoftDeletedBackfillWrapperIsDrift(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "org-gone", enabled: true})
	f.apply(t)
	require.NoError(t, New(f.pool).SoftDeleteWrapperFixture(t.Context(), uuid.NewSHA1(idNamespace, []byte("mcp_server:"+toolsetID.String()))))

	for _, opts := range []Options{{}, {Apply: true}} {
		report := f.run(t, opts)
		requireOnlyOutcome(t, report, OutcomeBlockedDrift)
		require.Equal(t, "backfilled wrapper was deleted", report.Rows[0].Reason)
	}
}

func TestRun_AdoptsExistingWrapper(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	issuerID := f.seedIssuer(t)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "org-adopt", public: true, enabled: true, issuerID: uuid.NullUUID{UUID: issuerID, Valid: true}})
	foreignID := f.seedForeignWrapper(t, toolsetID)

	report := f.apply(t)

	requireOnlyOutcome(t, report, OutcomeAdopted)
	require.Empty(t, report.Rows[0].Reason)
	w := f.wrapper(t, toolsetID)
	require.Equal(t, foreignID, w.ID)
	require.Equal(t, "public", w.Visibility)
	require.Equal(t, uuid.NullUUID{UUID: issuerID, Valid: true}, w.UserSessionIssuerID)
	require.Equal(t, "manual wrapper", w.Name.String, "adoption keeps the wrapper's own name")
	eps := f.endpoints(t, foreignID)
	require.Len(t, eps, 1)
	require.Equal(t, "org-adopt", eps[0].Slug)
	requireOnlyOutcome(t, f.apply(t), OutcomeAlreadyComplete)
}

// A wrapper the mirror created on the toolset's first write: nothing to do.
func TestRun_MirrorCreatedWrapperIsComplete(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "org-mirrored", enabled: true})
	wrapperID := f.seedMirroredWrapper(t, toolsetID, "hosted org-mirrored")
	f.seedEndpoint(t, wrapperID, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, "org-mirrored", false)
	before := f.wrapper(t, toolsetID)

	for _, opts := range []Options{{}, {Apply: true}} {
		requireOnlyOutcome(t, f.run(t, opts), OutcomeAlreadyComplete)
	}
	require.Equal(t, before, f.wrapper(t, toolsetID))
	require.Len(t, f.endpoints(t, wrapperID), 1)
}

// A user's rename on the MCP server page must survive every backfill pass.
func TestRun_AdoptPreservesRenamedWrapper(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "org-renamed", enabled: true})
	wrapperID := f.seedMirroredWrapper(t, toolsetID, "Support (prod)")
	f.seedEndpoint(t, wrapperID, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, "org-renamed", false)
	require.NoError(t, New(f.pool).UpdateWrapperSlugFixture(t.Context(), UpdateWrapperSlugFixtureParams{Slug: conv.ToPGText("support-prod-ab12"), ID: wrapperID}))

	requireOnlyOutcome(t, f.apply(t), OutcomeAlreadyComplete)
	w := f.wrapper(t, toolsetID)
	require.Equal(t, "Support (prod)", w.Name.String)
	require.Equal(t, "support-prod-ab12", w.Slug.String)
}

func TestRun_DerivesRemoteSessionIssuer(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	issuerID := f.seedIssuer(t)
	f.seedRemoteSessionClientBinding(t, issuerID)
	created := f.seedToolset(t, toolsetSpec{mcpSlug: "org-remote-created", enabled: true, issuerID: uuid.NullUUID{UUID: issuerID, Valid: true}})
	adopted := f.seedToolset(t, toolsetSpec{mcpSlug: "org-remote-adopted", enabled: true, issuerID: uuid.NullUUID{UUID: issuerID, Valid: true}})
	f.seedForeignWrapper(t, adopted)

	f.apply(t)

	for _, toolsetID := range []uuid.UUID{created, adopted} {
		require.True(t, f.wrapper(t, toolsetID).RemoteSessionIssuerID.Valid, "wrapper derives remote_session_issuer_id like the mirror")
	}
}

func TestRun_ClearedIssuerClearsDerivedRemoteIssuer(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	issuerID := f.seedIssuer(t)
	f.seedRemoteSessionClientBinding(t, issuerID)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "org-issuer-cleared", enabled: true, issuerID: uuid.NullUUID{UUID: issuerID, Valid: true}})
	f.apply(t)
	require.True(t, f.wrapper(t, toolsetID).RemoteSessionIssuerID.Valid)

	require.NoError(t, New(f.pool).UpdateToolsetIssuerFixture(t.Context(), UpdateToolsetIssuerFixtureParams{UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, ID: toolsetID}))
	requireOnlyOutcome(t, f.apply(t), OutcomeAdopted)

	w := f.wrapper(t, toolsetID)
	require.False(t, w.UserSessionIssuerID.Valid)
	require.False(t, w.RemoteSessionIssuerID.Valid, "derived remote issuer must not outlive the user issuer")
}

func TestRun_MoveToPlatformScopeClearsDomainRoot(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	domainID := f.seedDomain(t, nil)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "org-move", enabled: true, domainID: uuid.NullUUID{UUID: domainID, Valid: true}})
	f.apply(t)
	q := New(f.pool)
	endpointID := f.endpoints(t, f.wrapper(t, toolsetID).ID)[0].ID
	require.NoError(t, q.SetEndpointRootFixture(t.Context(), endpointID))
	require.NoError(t, q.UpdateToolsetDomainFixture(t.Context(), UpdateToolsetDomainFixtureParams{CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, ID: toolsetID}))

	report := f.apply(t)

	requireOnlyOutcome(t, report, OutcomeAdopted)
	eps := f.endpoints(t, f.wrapper(t, toolsetID).ID)
	require.Len(t, eps, 1)
	require.False(t, eps[0].CustomDomainID.Valid)
	require.False(t, eps[0].IsDomainRoot.Valid, "root marker cleared on scope change")
}

func TestRun_AdoptToDisabledClearsDomainRoot(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	domainID := f.seedDomain(t, nil)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "org-root", enabled: false, domainID: uuid.NullUUID{UUID: domainID, Valid: true}})
	foreignID := f.seedForeignWrapper(t, toolsetID)
	f.seedEndpoint(t, foreignID, uuid.NullUUID{UUID: domainID, Valid: true}, "org-root", true)

	report := f.apply(t)

	requireOnlyOutcome(t, report, OutcomeAdopted)
	require.Equal(t, []uuid.UUID{domainID}, report.Rows[0].ClearedRootDomain)
	require.Equal(t, "disabled", f.wrapper(t, toolsetID).Visibility)
	for _, ep := range f.endpoints(t, foreignID) {
		require.False(t, ep.IsDomainRoot.Bool)
	}
}

func TestRun_DisabledToolsetProjectsDisabledVisibility(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "org-off", public: true})

	f.apply(t)

	require.Equal(t, "disabled", f.wrapper(t, toolsetID).Visibility)
}

func TestRun_AliasAllowlistCreatesPlatformTwin(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	domainID := f.seedDomain(t, nil)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "org-alias", public: true, enabled: true, domainID: uuid.NullUUID{UUID: domainID, Valid: true}})

	report := f.run(t, Options{Aliases: []AliasKey{{Slug: "org-alias", CustomDomainID: domainID}}, Apply: true})

	requireOnlyOutcome(t, report, OutcomeCreated)
	eps := f.endpoints(t, f.wrapper(t, toolsetID).ID)
	require.Len(t, eps, 2)
	var domains []uuid.NullUUID
	for _, ep := range eps {
		require.Equal(t, "org-alias", ep.Slug)
		domains = append(domains, ep.CustomDomainID)
	}
	require.ElementsMatch(t, []uuid.NullUUID{{UUID: domainID, Valid: true}, {}}, domains)
}

func TestRun_AliasBlockedWhenAnotherToolsetOwnsPlatformSlug(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	domainID := f.seedDomain(t, nil)
	aliased := f.seedToolset(t, toolsetSpec{mcpSlug: "org-shared", public: true, enabled: true, domainID: uuid.NullUUID{UUID: domainID, Valid: true}})
	owner := f.seedToolset(t, toolsetSpec{mcpSlug: "org-shared", public: true, enabled: true})

	report := f.run(t, Options{Aliases: []AliasKey{{Slug: "org-shared", CustomDomainID: domainID}}, Apply: true})

	blocked := rowFor(t, report, aliased)
	require.Equal(t, OutcomeBlockedCollision, blocked.Outcome)
	require.Equal(t, "platform slug owned by another toolset", blocked.Reason)
	require.Equal(t, OutcomeCreated, rowFor(t, report, owner).Outcome)
	count, err := New(f.pool).CountWrappersFixture(t.Context(), uuid.NullUUID{UUID: aliased, Valid: true})
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestRun_DeadDomainTombstonesEndpoint(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	deletedAt := time.Now().Add(-48 * time.Hour).UTC().Truncate(time.Microsecond)
	domainID := f.seedDomain(t, &deletedAt)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "org-dead", public: true, enabled: true, domainID: uuid.NullUUID{UUID: domainID, Valid: true}})

	report := f.run(t, Options{Aliases: []AliasKey{{Slug: "org-dead", CustomDomainID: domainID}}, Apply: true})

	requireOnlyOutcome(t, report, OutcomeCreated)
	eps := f.endpoints(t, f.wrapper(t, toolsetID).ID)
	require.Len(t, eps, 1, "no alias twin for a dead domain")
	require.True(t, eps[0].Deleted)
	require.Equal(t, deletedAt, eps[0].DeletedAt.Time.UTC())
	require.Equal(t, uuid.NullUUID{UUID: domainID, Valid: true}, eps[0].CustomDomainID)
	requireOnlyOutcome(t, f.apply(t), OutcomeAlreadyComplete)
}

func TestRun_BlockedByEndpointCollision(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "org-clash", public: true, enabled: true})
	otherToolset := f.seedToolset(t, toolsetSpec{mcpSlug: "org-other", public: true, enabled: true})
	f.seedEndpoint(t, f.seedForeignWrapper(t, otherToolset), uuid.NullUUID{}, "org-clash", false)

	report := f.apply(t)

	blocked := rowFor(t, report, toolsetID)
	require.Equal(t, OutcomeBlockedCollision, blocked.Outcome)
	require.Nil(t, blocked.WrapperID)
	count, err := New(f.pool).CountWrappersFixture(t.Context(), uuid.NullUUID{UUID: toolsetID, Valid: true})
	require.NoError(t, err)
	require.Zero(t, count, "a blocked row writes nothing")
}

func TestRun_BlockedDriftOnForeignDomain(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	otherOrg := "org_" + uuid.NewString()
	require.NoError(t, New(f.pool).SeedOrganizationFixture(t.Context(), SeedOrganizationFixtureParams{ID: otherOrg, Name: "other", Slug: "o-" + uuid.NewString()[:8]}))
	foreignDomain := uuid.New()
	require.NoError(t, New(f.pool).SeedCustomDomainFixture(t.Context(), SeedCustomDomainFixtureParams{
		ID: foreignDomain, OrganizationID: otherOrg, Domain: "foreign-" + uuid.NewString()[:8] + ".example.test",
	}))
	f.seedToolset(t, toolsetSpec{mcpSlug: "org-foreign", public: true, enabled: true, domainID: uuid.NullUUID{UUID: foreignDomain, Valid: true}})

	requireOnlyOutcome(t, f.apply(t), OutcomeBlockedDrift)
}

func TestRun_CopiesToolsetKeyedGrantsIncludingExclusions(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "org-grant", enabled: true})
	principal, err := urn.ParsePrincipal("user:user_" + uuid.NewString()[:8])
	require.NoError(t, err)
	f.seedGrant(t, principal, authz.ScopeMCPConnect, "", toolsetID.String())
	f.seedGrant(t, principal, authz.ScopeMCPBlockedConnect, "deny", toolsetID.String())
	f.seedGrant(t, principal, authz.ScopeMCPConnect, "", "*")

	report := f.apply(t)
	require.Equal(t, int64(2), report.Rows[0].GrantsCopied)

	wrapperID := f.wrapper(t, toolsetID).ID.String()
	var seen []string
	for _, g := range f.grants(t) {
		seen = append(seen, g.Scope+"/"+g.Effect.String+"/"+g.ResourceID)
	}
	require.ElementsMatch(t, []string{
		"mcp:connect//*",
		"mcp:connect//" + toolsetID.String(), "mcp:connect//" + wrapperID,
		"mcp:blocked_connect/deny/" + toolsetID.String(), "mcp:blocked_connect/deny/" + wrapperID,
	}, seen, "toolset-keyed rows survive; exclusions are enforced on both ids")

	requireOnlyOutcome(t, f.apply(t), OutcomeAlreadyComplete)
}

func TestRetireGrants_DeletesToolsetKeyedTwins(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "org-retire", enabled: true})
	principal, err := urn.ParsePrincipal("user:user_" + uuid.NewString()[:8])
	require.NoError(t, err)
	f.seedGrant(t, principal, authz.ScopeMCPConnect, "", toolsetID.String())
	f.seedGrant(t, principal, authz.ScopeMCPBlockedConnect, "deny", toolsetID.String())

	requireOnlyOutcome(t, f.run(t, Options{Phase: PhaseRetireGrants, Apply: true}), OutcomeBlockedNoWrapper)
	f.apply(t)

	dry := f.run(t, Options{Phase: PhaseRetireGrants})
	requireOnlyOutcome(t, dry, OutcomeWouldRetireGrants)
	require.Len(t, f.grants(t), 4)

	report := f.run(t, Options{Phase: PhaseRetireGrants, Apply: true})
	requireOnlyOutcome(t, report, OutcomeRetiredGrants)
	require.Equal(t, int64(2), report.Rows[0].GrantsRetired)
	wrapperID := f.wrapper(t, toolsetID).ID.String()
	for _, g := range f.grants(t) {
		require.Equal(t, wrapperID, g.ResourceID)
	}
	requireOnlyOutcome(t, f.run(t, Options{Phase: PhaseRetireGrants, Apply: true}), OutcomeAlreadyComplete)
}

func TestRun_ProjectFilterAndCursor(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	a := f.seedToolset(t, toolsetSpec{mcpSlug: "org-a", public: true, enabled: true})
	b := f.seedToolset(t, toolsetSpec{mcpSlug: "org-b", public: true, enabled: true})
	first, second := a, b
	if b.String() < a.String() {
		first, second = b, a
	}

	filtered := f.run(t, Options{ProjectID: uuid.NullUUID{UUID: uuid.New(), Valid: true}})
	require.Zero(t, filtered.Scanned)

	resumed := f.run(t, Options{ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true}, Cursor: first, PageSize: 1})
	require.Equal(t, 1, resumed.Scanned)
	require.Equal(t, second, resumed.Rows[0].ToolsetID)
	require.Equal(t, second, resumed.LastCursor)
}

func TestMoveDependents_RequiresWrapper(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.seedToolset(t, toolsetSpec{mcpSlug: "org-nowrap", public: true, enabled: true})

	requireOnlyOutcome(t, f.run(t, Options{Phase: PhaseDependents, Apply: true}), OutcomeBlockedNoWrapper)
}

func TestMoveDependents_MovesEveryDependentKind(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "org-deps", public: true, enabled: true})
	q := New(f.pool)
	tsID := uuid.NullUUID{UUID: toolsetID, Valid: true}

	metadataID := uuid.New()
	require.NoError(t, q.SeedMcpMetadataFixture(t.Context(), SeedMcpMetadataFixtureParams{ID: metadataID, ToolsetID: tsID, ProjectID: f.projectID, Instructions: conv.ToPGText("hi")}))
	collectionID := uuid.New()
	require.NoError(t, q.SeedCollectionFixture(t.Context(), SeedCollectionFixtureParams{ID: collectionID, OrganizationID: f.orgID, Name: "c", Slug: "c-" + uuid.NewString()[:8]}))
	liveAttachment, deadAttachment := uuid.New(), uuid.New()
	require.NoError(t, q.SeedCollectionAttachmentFixture(t.Context(), SeedCollectionAttachmentFixtureParams{ID: liveAttachment, CollectionID: collectionID, ToolsetID: tsID}))
	require.NoError(t, q.SeedCollectionAttachmentFixture(t.Context(), SeedCollectionAttachmentFixtureParams{ID: deadAttachment, CollectionID: collectionID, ToolsetID: tsID, DeletedAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true}}))
	pluginID := uuid.New()
	require.NoError(t, q.SeedPluginFixture(t.Context(), SeedPluginFixtureParams{ID: pluginID, OrganizationID: f.orgID, ProjectID: f.projectID, Name: "p", Slug: "p-" + uuid.NewString()[:8]}))
	pluginServerID := uuid.New()
	require.NoError(t, q.SeedPluginServerFixture(t.Context(), SeedPluginServerFixtureParams{ID: pluginServerID, PluginID: pluginID, ToolsetID: tsID, DisplayName: "hosted"}))
	assistantID := uuid.New()
	require.NoError(t, q.SeedAssistantFixture(t.Context(), SeedAssistantFixtureParams{ID: assistantID, ProjectID: f.projectID, OrganizationID: f.orgID, Name: "a"}))
	assistantToolsetID := uuid.New()
	require.NoError(t, q.SeedAssistantToolsetFixture(t.Context(), SeedAssistantToolsetFixtureParams{ID: assistantToolsetID, AssistantID: assistantID, ToolsetID: toolsetID, ProjectID: f.projectID}))

	before, err := q.GetCollectionAttachmentFixture(t.Context(), deadAttachment)
	require.NoError(t, err)

	f.apply(t)
	wrapperID := f.wrapper(t, toolsetID).ID
	report := f.run(t, Options{Phase: PhaseDependents, Apply: true})

	requireOnlyOutcome(t, report, OutcomeMovedDependents)
	require.Equal(t, &DependentsReport{McpMetadata: 1, CollectionAttachments: 2, PluginServers: 1, AssistantToolsets: 1}, report.Rows[0].Dependents)
	require.Nil(t, report.Rows[0].DependentsSkipped)

	serverID := uuid.NullUUID{UUID: wrapperID, Valid: true}
	metadata, err := q.GetMcpMetadataFixture(t.Context(), metadataID)
	require.NoError(t, err)
	require.Equal(t, GetMcpMetadataFixtureRow{ID: metadataID, McpServerID: serverID}, metadata)

	dead, err := q.GetCollectionAttachmentFixture(t.Context(), deadAttachment)
	require.NoError(t, err)
	require.Equal(t, serverID, dead.McpServerID)
	require.True(t, dead.Deleted, "soft-deletion state is preserved")
	require.Equal(t, before.CreatedAt, dead.CreatedAt, "timestamps are preserved")

	plugin, err := q.GetPluginServerFixture(t.Context(), pluginServerID)
	require.NoError(t, err)
	require.Equal(t, serverID, plugin.McpServerID)

	remaining, err := q.CountAssistantToolsetsFixture(t.Context(), toolsetID)
	require.NoError(t, err)
	require.Zero(t, remaining)
	moved, err := q.GetAssistantMcpServerFixture(t.Context(), GetAssistantMcpServerFixtureParams{AssistantID: assistantID, McpServerID: wrapperID})
	require.NoError(t, err)
	require.Equal(t, assistantToolsetID, moved.ID, "row id is preserved across the move")

	requireOnlyOutcome(t, f.run(t, Options{Phase: PhaseDependents, Apply: true}), OutcomeAlreadyComplete)
}

func TestMoveDependents_PreexistingTwinIsSkippedNotDeleted(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "org-twin", public: true, enabled: true})
	q := New(f.pool)
	assistantID := uuid.New()
	require.NoError(t, q.SeedAssistantFixture(t.Context(), SeedAssistantFixtureParams{ID: assistantID, ProjectID: f.projectID, OrganizationID: f.orgID, Name: "a"}))
	require.NoError(t, q.SeedAssistantToolsetFixture(t.Context(), SeedAssistantToolsetFixtureParams{ID: uuid.New(), AssistantID: assistantID, ToolsetID: toolsetID, ProjectID: f.projectID}))
	f.apply(t)
	wrapperID := f.wrapper(t, toolsetID).ID
	require.NoError(t, q.SeedAssistantMcpServerFixture(t.Context(), SeedAssistantMcpServerFixtureParams{ID: uuid.New(), AssistantID: assistantID, McpServerID: wrapperID, ProjectID: f.projectID}))

	for range 2 {
		report := f.run(t, Options{Phase: PhaseDependents, Apply: true})
		requireOnlyOutcome(t, report, OutcomeSkippedDependents)
		require.Equal(t, &DependentsReport{AssistantToolsets: 1}, report.Rows[0].DependentsSkipped)
		remaining, err := q.CountAssistantToolsetsFixture(t.Context(), toolsetID)
		require.NoError(t, err)
		require.Equal(t, int64(1), remaining, "an unmoved row is never deleted")
	}
}

func TestMoveDependents_DryRunLeavesRowsKeyedByToolset(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "org-depsdry", public: true, enabled: true})
	q := New(f.pool)
	metadataID := uuid.New()
	require.NoError(t, q.SeedMcpMetadataFixture(t.Context(), SeedMcpMetadataFixtureParams{ID: metadataID, ToolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true}, ProjectID: f.projectID, Instructions: conv.ToPGText("hi")}))
	f.apply(t)

	report := f.run(t, Options{Phase: PhaseDependents})

	requireOnlyOutcome(t, report, OutcomeWouldMoveDependent)
	metadata, err := q.GetMcpMetadataFixture(t.Context(), metadataID)
	require.NoError(t, err)
	require.Equal(t, uuid.NullUUID{UUID: toolsetID, Valid: true}, metadata.ToolsetID)
}
