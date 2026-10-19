package hostedmcpbackfill

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestDryRunWritesNothing(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "dry-run-slug", public: true, enabled: true, domainID: uuid.NullUUID{}})
	drifted := f.seedToolset(t, toolsetSpec{mcpSlug: "dry-drifted", public: false, enabled: true, domainID: uuid.NullUUID{}})
	f.seedServer(t, drifted, drifted, "stale name", "dry-drifted", "public")
	f.seedEndpoint(t, drifted, uuid.NullUUID{}, "dry-drifted")
	before := f.state(t)

	report := f.run(t, Options{Apply: false})

	require.Equal(t, before, f.state(t))
	require.Equal(t, "dry-run", report.Mode)
	require.Equal(t, OutcomeWouldCreate, rowFor(t, report, toolsetID).Outcome)
	require.Equal(t, OutcomeWouldReconcile, rowFor(t, report, drifted).Outcome)
	require.Equal(t, 2, report.Writes)
}

func TestApplyCreatesCanonicalWrapperAndIsIdempotent(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	public := f.seedToolset(t, toolsetSpec{mcpSlug: "acme-public", public: true, enabled: true, domainID: uuid.NullUUID{}})
	private := f.seedToolset(t, toolsetSpec{mcpSlug: "acme-private", public: false, enabled: true, domainID: uuid.NullUUID{}})
	disabled := f.seedToolset(t, toolsetSpec{mcpSlug: "acme-disabled", public: true, enabled: false, domainID: uuid.NullUUID{}})
	domain := f.seedDomain(t, false)
	onDomain := f.seedToolset(t, toolsetSpec{mcpSlug: "on-domain", public: true, enabled: true, domainID: domain})

	report := f.run(t, Options{Apply: true})
	require.Len(t, report.Outcomes, 1)
	require.Equal(t, 4, report.Outcomes[OutcomeCreated])

	state := f.state(t)
	for id, want := range map[uuid.UUID]struct{ visibility, slug string }{
		public:   {"public", "acme-public"},
		private:  {"private", "acme-private"},
		disabled: {"disabled", "acme-disabled"},
		onDomain: {"public", "on-domain"},
	} {
		server := state.server(t, id)
		require.Equal(t, uuid.NullUUID{UUID: id, Valid: true}, server.ToolsetID)
		require.Equal(t, want.visibility, server.Visibility)
		require.Equal(t, want.slug, server.Slug.String)
		endpoints := state.liveEndpoints(id)
		require.Len(t, endpoints, 1)
		require.Equal(t, want.slug, endpoints[0].Slug)
	}
	require.Equal(t, domain, state.liveEndpoints(onDomain)[0].CustomDomainID)

	require.NotEmpty(t, state.audits)
	for _, entry := range state.audits {
		require.Equal(t, string(urn.PrincipalTypeSystem), entry.ActorType)
		require.Equal(t, ActorComponent, entry.ActorID)
	}

	rerun := f.run(t, Options{Apply: true})
	require.Len(t, rerun.Outcomes, 1)
	require.Equal(t, 4, rerun.Outcomes[OutcomeAlreadyComplete])
	require.Zero(t, rerun.Writes)
	require.Equal(t, state, f.state(t))
}

func TestApplyKeepsUnprefixedPlatformSlugVerbatim(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "legacy-unprefixed-slug", public: true, enabled: true, domainID: uuid.NullUUID{}})

	report := f.run(t, Options{Apply: true})

	require.Equal(t, OutcomeCreated, rowFor(t, report, toolsetID).Outcome)
	endpoints := f.state(t).liveEndpoints(toolsetID)
	require.Len(t, endpoints, 1)
	require.Equal(t, "legacy-unprefixed-slug", endpoints[0].Slug)
	require.False(t, endpoints[0].CustomDomainID.Valid)
}

func TestApplyReconcilesDriftedCanonical(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "current-slug", public: false, enabled: true, domainID: uuid.NullUUID{}})
	f.seedServer(t, toolsetID, toolsetID, "stale name", "old-slug", "public")
	endpointID := f.seedEndpoint(t, toolsetID, uuid.NullUUID{}, "old-slug")

	report := f.run(t, Options{Apply: true})
	require.Equal(t, OutcomeReconciled, rowFor(t, report, toolsetID).Outcome)

	state := f.state(t)
	server := state.server(t, toolsetID)
	require.Equal(t, "private", server.Visibility)
	require.Equal(t, "current-slug", server.Slug.String)
	require.Equal(t, "Hosted current-slug", server.Name.String)
	endpoints := state.liveEndpoints(toolsetID)
	require.Len(t, endpoints, 1)
	require.Equal(t, endpointID, endpoints[0].ID, "the endpoint is re-keyed in place")
	require.Equal(t, "current-slug", endpoints[0].Slug)

	rerun := f.run(t, Options{Apply: true})
	require.Equal(t, OutcomeAlreadyComplete, rowFor(t, rerun, toolsetID).Outcome)
	require.Zero(t, rerun.Writes)
}

func TestFreshIDServersAreReportedAndUntouched(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "with-member", public: true, enabled: true, domainID: uuid.NullUUID{}})
	freshID := uuid.New()
	f.seedServer(t, freshID, toolsetID, "gateway member", "member-"+uuid.NewString()[:8], "private")
	freshEndpoint := f.seedEndpoint(t, freshID, uuid.NullUUID{}, "member-endpoint-"+uuid.NewString()[:8])
	before := f.state(t)

	report := f.run(t, Options{Apply: true})

	row := rowFor(t, report, toolsetID)
	require.Equal(t, OutcomeCreated, row.Outcome)
	require.EqualValues(t, 1, row.FreshIDServers)
	require.Equal(t, 1, report.FreshIDServersPresent)
	after := f.state(t)
	require.Equal(t, before.server(t, freshID), after.server(t, freshID))
	require.Len(t, after.liveEndpoints(freshID), 1)
	require.Equal(t, freshEndpoint, after.liveEndpoints(freshID)[0].ID)
	require.Len(t, after.liveEndpoints(toolsetID), 1)
}

func TestDeadCustomDomainSyncsWrapperWithoutEndpoint(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "on-dead-domain", public: true, enabled: true, domainID: f.seedDomain(t, true)})

	report := f.run(t, Options{Apply: true})
	row := rowFor(t, report, toolsetID)
	require.Equal(t, OutcomeDeadCustomDomain, row.Outcome)
	require.True(t, row.Wrote)

	state := f.state(t)
	require.Equal(t, "public", state.server(t, toolsetID).Visibility)
	require.Empty(t, state.liveEndpoints(toolsetID))

	rerun := f.run(t, Options{Apply: true})
	require.Equal(t, OutcomeDeadCustomDomain, rowFor(t, rerun, toolsetID).Outcome)
	require.Zero(t, rerun.Writes)
	require.Equal(t, state, f.state(t))
}

func TestMultipleEndpointsBlockAndStayUntouched(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	domain := f.seedDomain(t, false)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "multi", public: true, enabled: false, domainID: domain})
	f.seedServer(t, toolsetID, toolsetID, "stale", "multi", "public")
	f.seedEndpoint(t, toolsetID, domain, "multi")
	f.seedEndpoint(t, toolsetID, uuid.NullUUID{}, "multi-platform-"+uuid.NewString()[:8])
	before := f.state(t)

	report := f.run(t, Options{Apply: true})

	row := rowFor(t, report, toolsetID)
	require.Equal(t, OutcomeBlockedMultipleEndpoints, row.Outcome)
	require.Equal(t, 2, row.LiveEndpoints)
	require.False(t, row.Wrote)
	require.Equal(t, before, f.state(t))
}

func TestEndpointAddressCollisionBlocks(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "taken-address", public: true, enabled: true, domainID: uuid.NullUUID{}})
	freshID := uuid.New()
	f.seedServer(t, freshID, toolsetID, "from source", "from-source-"+uuid.NewString()[:8], "public")
	f.seedEndpoint(t, freshID, uuid.NullUUID{}, "taken-address")
	before := f.state(t)

	report := f.run(t, Options{Apply: true})

	row := rowFor(t, report, toolsetID)
	require.Equal(t, OutcomeBlockedSlugCollision, row.Outcome)
	require.EqualValues(t, 1, row.FreshIDServers)
	require.Equal(t, before, f.state(t))
}

func TestServerSlugCollisionBlocks(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "taken-server-slug", public: true, enabled: true, domainID: uuid.NullUUID{}})
	f.seedServer(t, uuid.New(), toolsetID, "from source", "taken-server-slug", "public")
	before := f.state(t)

	report := f.run(t, Options{Apply: true})

	require.Equal(t, OutcomeBlockedSlugCollision, rowFor(t, report, toolsetID).Outcome)
	require.Equal(t, before, f.state(t))
}

func TestProjectFilterCursorAndLimit(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	other := newFixtureIn(t, f.pool)
	a := f.seedToolset(t, toolsetSpec{mcpSlug: "scope-a-" + uuid.NewString()[:8], public: true, enabled: true, domainID: uuid.NullUUID{}})
	b := f.seedToolset(t, toolsetSpec{mcpSlug: "scope-b-" + uuid.NewString()[:8], public: true, enabled: true, domainID: uuid.NullUUID{}})
	elsewhere := other.seedToolset(t, toolsetSpec{mcpSlug: "elsewhere-" + uuid.NewString()[:8], public: true, enabled: true, domainID: uuid.NullUUID{}})
	project := uuid.NullUUID{UUID: f.projectID, Valid: true}

	first := f.run(t, Options{ProjectID: project, Limit: 1, Apply: true})
	require.Equal(t, 1, first.Scanned)
	second := f.run(t, Options{ProjectID: project, Cursor: first.LastCursor, Apply: true})
	require.Equal(t, 1, second.Scanned)
	require.ElementsMatch(t, []uuid.UUID{a, b}, []uuid.UUID{first.Rows[0].ToolsetID, second.Rows[0].ToolsetID})
	require.Empty(t, other.state(t).servers, "other projects are untouched: %s", elsewhere)
}

func TestTombstonedCanonicalIDBlocks(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "tombstoned", public: true, enabled: true, domainID: uuid.NullUUID{}})
	require.NoError(t, New(f.pool).SeedServerFixture(t.Context(), SeedServerFixtureParams{
		ID: toolsetID, ProjectID: f.projectID, Name: conv.ToPGText("old"), Slug: conv.ToPGText("tombstoned-" + uuid.NewString()[:8]),
		ToolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true}, Visibility: "public", NetworkAccessMode: pgtype.Text{},
		DeletedAt: pgtype.Timestamptz{Time: time.Now(), InfinityModifier: pgtype.Finite, Valid: true},
	}))
	before := f.state(t)

	row := rowFor(t, f.run(t, Options{Apply: true}), toolsetID)
	require.Equal(t, OutcomeBlockedCanonicalConflict, row.Outcome)
	require.False(t, row.Wrote)
	require.Equal(t, before, f.state(t))
}

func TestCanonicalIDOwnedByAnotherToolsetBlocks(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "foreign-id", public: true, enabled: true, domainID: uuid.NullUUID{}})
	otherID := f.seedToolset(t, toolsetSpec{mcpSlug: "foreign-owner", public: true, enabled: true, domainID: uuid.NullUUID{}})
	f.seedServer(t, toolsetID, otherID, "foreign", "foreign-"+uuid.NewString()[:8], "public")
	before := f.state(t)

	row := rowFor(t, f.run(t, Options{Apply: true, ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true}}), toolsetID)
	require.Equal(t, OutcomeBlockedCanonicalConflict, row.Outcome)
	require.False(t, row.Wrote)
	require.Equal(t, before.server(t, toolsetID), f.state(t).server(t, toolsetID))
}

func TestSyncRejectionBlocksWithoutWriting(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "private-only", public: false, enabled: true, domainID: uuid.NullUUID{}})
	// Re-enabling a private-only wrapper needs an online private ingress, which this org lacks.
	require.NoError(t, New(f.pool).SeedServerFixture(t.Context(), SeedServerFixtureParams{
		ID: toolsetID, ProjectID: f.projectID, Name: conv.ToPGText("private"), Slug: conv.ToPGText("private-only"),
		ToolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true}, Visibility: "disabled",
		NetworkAccessMode: pgtype.Text{String: "private_only", Valid: true}, DeletedAt: pgtype.Timestamptz{},
	}))
	f.seedEndpoint(t, toolsetID, uuid.NullUUID{}, "private-only")
	before := f.state(t)

	row := rowFor(t, f.run(t, Options{Apply: true}), toolsetID)
	require.Equal(t, OutcomeBlockedSyncRejected, row.Outcome)
	require.False(t, row.Wrote)
	require.Equal(t, before, f.state(t))
}

func TestDeletedProjectIsNotBackfilled(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "in-deleted-project", public: true, enabled: true, domainID: uuid.NullUUID{}})
	require.NoError(t, New(f.pool).SoftDeleteProjectFixture(t.Context(), SoftDeleteProjectFixtureParams{ID: f.projectID, OrganizationID: f.orgID}))

	report := f.run(t, Options{Apply: true})
	for _, row := range report.Rows {
		require.NotEqual(t, toolsetID, row.ToolsetID)
	}
	require.Empty(t, f.state(t).liveEndpoints(toolsetID))
}
