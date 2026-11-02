package mcpendpoints_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_endpoints"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp/hostingstate"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp/legacyalias"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var platformHost = uuid.NullUUID{UUID: uuid.Nil, Valid: false}

type aliasBackfillFixture struct {
	ti      *testInstance
	authCtx *contextvalues.AuthContext
}

func newAliasBackfillFixture(t *testing.T) (context.Context, *aliasBackfillFixture) {
	t.Helper()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	return ctx, &aliasBackfillFixture{ti: ti, authCtx: authCtx}
}

func (f *aliasBackfillFixture) slug(name string) string {
	return f.authCtx.OrganizationSlug + "-" + name + "-" + uuid.NewString()[:6]
}

// aliasedServer seeds a toolset with an alias at slug on domain and its canonical server.
func (f *aliasBackfillFixture) aliasedServer(t *testing.T, ctx context.Context, slug string, domain uuid.NullUUID) toolsetsrepo.Toolset {
	t.Helper()
	toolset := seedHostedToolset(t, ctx, f.ti.conn, f.authCtx.ActiveOrganizationID, *f.authCtx.ProjectID, slug)
	if domain.Valid {
		bindToolsetCustomDomain(t, ctx, f.ti.conn, toolset, slug, domain.UUID)
	}
	seedCanonicalToolsetWrapper(t, ctx, f.ti.conn, *f.authCtx.ProjectID, toolset.ID)
	return toolset
}

func (f *aliasBackfillFixture) endpoint(t *testing.T, ctx context.Context, serverID uuid.UUID, domain uuid.NullUUID, slug string) uuid.UUID {
	t.Helper()
	endpoint, err := mcpendpointsrepo.New(f.ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: *f.authCtx.ProjectID, CustomDomainID: domain, McpServerID: uuid.NullUUID{UUID: serverID, Valid: true},
		MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Slug: slug,
	})
	require.NoError(t, err)
	return endpoint.ID
}

func (f *aliasBackfillFixture) activeDomain(t *testing.T, ctx context.Context, allowlist []string) uuid.NullUUID {
	t.Helper()
	name := "alias-" + uuid.NewString()[:8] + ".example.com"
	domains := customdomainsrepo.New(f.ti.conn)
	domain, err := domains.CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: f.authCtx.ActiveOrganizationID, Domain: name, IngressName: conv.ToPGText("ingress-" + name),
		CertSecretName: conv.ToPGText("cert-" + name), ProvisionerKind: "ingress", IpAllowlist: allowlist,
	})
	require.NoError(t, err)
	_, err = domains.SetCustomDomainVerified(ctx, domain.ID)
	require.NoError(t, err)
	_, err = domains.ActivateVerifiedCustomDomain(ctx, customdomainsrepo.ActivateVerifiedCustomDomainParams{
		IngressName: conv.ToPGText("ingress-" + name), CertSecretName: conv.ToPGText("cert-" + name), ProvisionerKind: "ingress", ID: domain.ID,
	})
	require.NoError(t, err)
	return uuid.NullUUID{UUID: domain.ID, Valid: true}
}

func (f *aliasBackfillFixture) runResult(t *testing.T, ctx context.Context, toolset toolsetsrepo.Toolset, apply bool) mcpendpoints.AliasBackfillResult {
	t.Helper()
	result, err := mcpendpoints.BackfillAliasEndpoints(ctx, f.ti.conn, testenv.NewLogger(t), audit.NewLogger(audit.WithoutOutbox()), mcpendpoints.AliasBackfillCandidate{
		ToolsetID: toolset.ID, ProjectID: toolset.ProjectID, OrganizationID: f.authCtx.ActiveOrganizationID,
	}, apply, nil)
	require.NoError(t, err)
	return result
}

func (f *aliasBackfillFixture) run(t *testing.T, ctx context.Context, toolset toolsetsrepo.Toolset, apply bool) []mcpendpoints.AliasBackfillRow {
	t.Helper()
	return f.runResult(t, ctx, toolset, apply).Rows
}

func (f *aliasBackfillFixture) alias(t *testing.T, ctx context.Context, toolset toolsetsrepo.Toolset) legacyalias.Alias {
	t.Helper()
	alias, err := legacyalias.Get(ctx, f.ti.conn, toolset.ProjectID, toolset.ID)
	require.NoError(t, err)
	return alias
}

func (f *aliasBackfillFixture) serverWithMode(t *testing.T, ctx context.Context, toolset toolsetsrepo.Toolset, mode string) {
	t.Helper()
	_, err := mcpserversrepo.New(f.ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: toolset.ID, ProjectID: toolset.ProjectID, Name: conv.ToPGText("hosted"), Slug: conv.ToPGText("hosted-" + uuid.NewString()[:8]),
		EnvironmentID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, TunneledMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, UnproxiedMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Visibility: "private",
		NetworkAccessMode: pgtype.Text{String: mode, Valid: true},
	})
	require.NoError(t, err)
}

func (f *aliasBackfillFixture) endpoints(t *testing.T, ctx context.Context, serverID uuid.UUID) []mcpendpointsrepo.McpEndpoint {
	t.Helper()
	endpoints, err := mcpendpointsrepo.New(f.ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: *f.authCtx.ProjectID, McpServerID: serverID})
	require.NoError(t, err)
	return endpoints
}

type aliasBackfillOutcome struct {
	slug     string
	domain   uuid.NullUUID
	action   mcpendpoints.AliasBackfillAction
	reason   string
	platform bool
}

func requireOutcomes(t *testing.T, rows []mcpendpoints.AliasBackfillRow, want ...aliasBackfillOutcome) {
	t.Helper()
	got := make([]aliasBackfillOutcome, 0, len(rows))
	for _, row := range rows {
		got = append(got, aliasBackfillOutcome{slug: row.Slug, domain: row.CustomDomainID, action: row.Action, reason: row.Reason, platform: row.PlatformForCustomDomain})
	}
	require.Equal(t, want, got)
}

func TestAliasBackfillPlatformAliasWithDifferentSlugGetsEndpoint(t *testing.T) {
	t.Parallel()
	ctx, f := newAliasBackfillFixture(t)
	slug := f.slug("alias")
	toolset := f.aliasedServer(t, ctx, slug, platformHost)
	existing := f.endpoint(t, ctx, toolset.ID, platformHost, f.slug("renamed"))
	before, err := audittest.AuditLogCountByAction(ctx, f.ti.conn, audit.ActionMcpEndpointCreate)
	require.NoError(t, err)

	rows := f.run(t, ctx, toolset, true)
	requireOutcomes(t, rows, aliasBackfillOutcome{slug: slug, domain: platformHost, action: mcpendpoints.AliasBackfillCreate, reason: "", platform: false})

	endpoints := f.endpoints(t, ctx, toolset.ID)
	require.Len(t, endpoints, 2)
	created := rows[0].EndpointID.UUID
	require.NotEqual(t, existing, created)
	after, err := audittest.AuditLogCountByAction(ctx, f.ti.conn, audit.ActionMcpEndpointCreate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
	record, err := audittest.LatestAuditLogByActionAndSubject(ctx, f.ti.conn, audit.ActionMcpEndpointCreate, created.String())
	require.NoError(t, err)
	require.Equal(t, string(urn.PrincipalTypeSystem), record.ActorType)
	require.Equal(t, mcpendpoints.AliasBackfillActor, record.ActorID)

	alias, err := legacyalias.Get(ctx, f.ti.conn, toolset.ProjectID, toolset.ID)
	require.NoError(t, err)
	require.Equal(t, slug, alias.Slug.String, "the alias stays on its address")
}

func TestAliasBackfillLooseCustomDomainAliasGetsPlatformEndpoint(t *testing.T) {
	t.Parallel()
	ctx, f := newAliasBackfillFixture(t)
	domain := f.activeDomain(t, ctx, []string{})
	slug := f.slug("loose")
	toolset := f.aliasedServer(t, ctx, slug, domain)
	f.endpoint(t, ctx, toolset.ID, domain, slug)

	rows := f.run(t, ctx, toolset, true)
	requireOutcomes(t, rows,
		aliasBackfillOutcome{slug: slug, domain: domain, action: mcpendpoints.AliasBackfillAlreadyHeld, reason: "", platform: false},
		aliasBackfillOutcome{slug: slug, domain: platformHost, action: mcpendpoints.AliasBackfillCreate, reason: "", platform: true},
	)
	require.Len(t, f.endpoints(t, ctx, toolset.ID), 2)
	alias, err := legacyalias.Get(ctx, f.ti.conn, toolset.ProjectID, toolset.ID)
	require.NoError(t, err)
	require.Equal(t, domain, alias.CustomDomainID, "the alias stays on its custom domain")
}

func TestAliasBackfillDeletedDomainAliasGetsPlatformEndpoint(t *testing.T) {
	t.Parallel()
	ctx, f := newAliasBackfillFixture(t)
	domain := f.activeDomain(t, ctx, []string{})
	slug := f.slug("orphaned")
	toolset := f.aliasedServer(t, ctx, slug, domain)
	require.NoError(t, customdomainsrepo.New(f.ti.conn).DeleteCustomDomain(ctx, f.authCtx.ActiveOrganizationID))

	result := f.runResult(t, ctx, toolset, true)
	requireOutcomes(t, result.Rows,
		aliasBackfillOutcome{slug: slug, domain: domain, action: mcpendpoints.AliasBackfillSkip, reason: "custom domain deleted", platform: false},
		aliasBackfillOutcome{slug: slug, domain: platformHost, action: mcpendpoints.AliasBackfillCreate, reason: "", platform: true},
	)
	require.Len(t, f.endpoints(t, ctx, toolset.ID), 1)
	moved := legacyalias.Alias{Slug: pgtype.Text{String: slug, Valid: true}, CustomDomainID: platformHost}
	require.Equal(t, &mcpendpoints.AliasMove{From: legacyalias.Alias{Slug: pgtype.Text{String: slug, Valid: true}, CustomDomainID: domain}, To: moved}, result.AliasMove)
	require.Equal(t, moved, f.alias(t, ctx, toolset), "the alias leaves the deleted domain for the new platform endpoint")

	record, err := audittest.LatestAuditLogByActionAndSubject(ctx, f.ti.conn, audit.ActionToolsetMoveLegacyAlias, toolset.ID.String())
	require.NoError(t, err)
	require.Equal(t, string(urn.PrincipalTypeSystem), record.ActorType)
	require.Equal(t, mcpendpoints.AliasBackfillActor, record.ActorID)
	var before, after audit.ToolsetLegacyAliasAddress
	require.NoError(t, json.Unmarshal(record.BeforeSnapshot, &before))
	require.NoError(t, json.Unmarshal(record.AfterSnapshot, &after))
	require.Equal(t, audit.ToolsetLegacyAliasAddress{Slug: slug, CustomDomainID: &domain.UUID}, before)
	require.Equal(t, audit.ToolsetLegacyAliasAddress{Slug: slug, CustomDomainID: nil}, after)
}

func TestAliasBackfillKeepsUnprefixedPlatformSlug(t *testing.T) {
	t.Parallel()
	ctx, f := newAliasBackfillFixture(t)
	domain := f.activeDomain(t, ctx, []string{})
	slug := "unprefixed-" + uuid.NewString()[:6]
	toolset := f.aliasedServer(t, ctx, slug, domain)
	f.endpoint(t, ctx, toolset.ID, domain, slug)

	_, err := f.ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{McpServerID: new(toolset.ID.String()), Slug: types.McpEndpointSlug(slug)})
	requireOopsCode(t, err, oops.CodeInvalid)

	rows := f.run(t, ctx, toolset, true)
	requireOutcomes(t, rows,
		aliasBackfillOutcome{slug: slug, domain: domain, action: mcpendpoints.AliasBackfillAlreadyHeld, reason: "", platform: false},
		aliasBackfillOutcome{slug: slug, domain: platformHost, action: mcpendpoints.AliasBackfillCreate, reason: "", platform: true},
	)
	require.Len(t, f.endpoints(t, ctx, toolset.ID), 2)

	_, err = f.ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{McpServerID: new(toolset.ID.String()), Slug: types.McpEndpointSlug("unprefixed-other-" + uuid.NewString()[:6])})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestAliasBackfillUnprefixedEndpointSavesWithoutSlugChange(t *testing.T) {
	t.Parallel()
	ctx, f := newAliasBackfillFixture(t)
	slug := "unprefixed-" + uuid.NewString()[:6]
	toolset := f.aliasedServer(t, ctx, slug, platformHost)
	rows := f.run(t, ctx, toolset, true)
	require.Len(t, rows, 1)
	require.Equal(t, mcpendpoints.AliasBackfillCreate, rows[0].Action)
	endpointID := rows[0].EndpointID.UUID.String()

	view, err := f.ti.service.UpdateMcpEndpoint(ctx, &gen.UpdateMcpEndpointPayload{ID: endpointID, McpServerID: new(toolset.ID.String()), Slug: types.McpEndpointSlug(slug)})
	require.NoError(t, err)
	require.Equal(t, slug, string(view.Slug))

	_, err = f.ti.service.UpdateMcpEndpoint(ctx, &gen.UpdateMcpEndpointPayload{ID: endpointID, McpServerID: new(toolset.ID.String()), Slug: types.McpEndpointSlug("unprefixed-renamed-" + uuid.NewString()[:6])})
	requireOopsCode(t, err, oops.CodeInvalid)

	f.endpoint(t, ctx, toolset.ID, platformHost, f.slug("kept"))
	require.NoError(t, f.ti.service.DeleteMcpEndpoint(ctx, &gen.DeleteMcpEndpointPayload{ID: endpointID}))
}

func TestAliasBackfillLockdownSkipsPlatformAddress(t *testing.T) {
	t.Parallel()
	ctx, f := newAliasBackfillFixture(t)
	domain := f.activeDomain(t, ctx, []string{"203.0.113.0/24"})
	slug := f.slug("locked")
	toolset := f.aliasedServer(t, ctx, slug, domain)
	f.endpoint(t, ctx, toolset.ID, domain, slug)

	rows := f.run(t, ctx, toolset, true)
	requireOutcomes(t, rows,
		aliasBackfillOutcome{slug: slug, domain: domain, action: mcpendpoints.AliasBackfillAlreadyHeld, reason: "", platform: false},
		aliasBackfillOutcome{slug: slug, domain: platformHost, action: mcpendpoints.AliasBackfillSkip, reason: "not served on platform: custom domain lockdown", platform: true},
	)
	require.Len(t, f.endpoints(t, ctx, toolset.ID), 1)
}

func TestAliasBackfillAmbiguousCustomDomainAliasSkipsPlatformAddress(t *testing.T) {
	t.Parallel()
	ctx, f := newAliasBackfillFixture(t)
	domain := f.activeDomain(t, ctx, []string{})
	slug := f.slug("shared")
	toolset := f.aliasedServer(t, ctx, slug, domain)
	f.endpoint(t, ctx, toolset.ID, domain, slug)
	// Another organization's domain row holds the same slug; only one live domain per organization is allowed.
	other := seedHostedToolset(t, ctx, f.ti.conn, f.authCtx.ActiveOrganizationID, *f.authCtx.ProjectID, f.slug("other"))
	bindToolsetCustomDomain(t, ctx, f.ti.conn, other, slug, seedCustomDomain(t, ctx, f.ti.conn, "org-other-"+uuid.NewString()[:8]))

	rows := f.run(t, ctx, toolset, true)
	require.Len(t, rows, 2)
	require.Equal(t, mcpendpoints.AliasBackfillSkip, rows[1].Action)
	require.Equal(t, "not served on platform: another custom-domain alias has the slug", rows[1].Reason)
}

func TestAliasBackfillAliasAlreadyAnEndpointIsNoop(t *testing.T) {
	t.Parallel()
	ctx, f := newAliasBackfillFixture(t)
	slug := f.slug("same")
	toolset := f.aliasedServer(t, ctx, slug, platformHost)
	endpointID := f.endpoint(t, ctx, toolset.ID, platformHost, slug)

	rows := f.run(t, ctx, toolset, true)
	requireOutcomes(t, rows, aliasBackfillOutcome{slug: slug, domain: platformHost, action: mcpendpoints.AliasBackfillAlreadyHeld, reason: "", platform: false})
	require.Equal(t, endpointID, rows[0].EndpointID.UUID)
	require.Len(t, f.endpoints(t, ctx, toolset.ID), 1)
}

func TestAliasBackfillSlugHeldByAnotherServerIsConflict(t *testing.T) {
	t.Parallel()
	ctx, f := newAliasBackfillFixture(t)
	slug := f.slug("contested")
	toolset := f.aliasedServer(t, ctx, slug, platformHost)
	otherServer := seedMcpServer(t, ctx, f.ti.conn, *f.authCtx.ProjectID)
	holder := f.endpoint(t, ctx, otherServer, platformHost, slug)

	rows := f.run(t, ctx, toolset, true)
	requireOutcomes(t, rows, aliasBackfillOutcome{slug: slug, domain: platformHost, action: mcpendpoints.AliasBackfillConflict, reason: "another server's endpoint holds the address", platform: false})
	require.Equal(t, holder, rows[0].EndpointID.UUID)
	require.Empty(t, f.endpoints(t, ctx, toolset.ID))
	require.Len(t, f.endpoints(t, ctx, otherServer), 1)
}

func TestAliasBackfillSkipsDeletedToolsetAndProject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		delete func(t *testing.T, ctx context.Context, f *aliasBackfillFixture, toolset toolsetsrepo.Toolset)
		reason string
	}{
		{
			name: "deleted toolset",
			delete: func(t *testing.T, ctx context.Context, f *aliasBackfillFixture, toolset toolsetsrepo.Toolset) {
				t.Helper()
				_, err := toolsetsrepo.New(f.ti.conn).DeleteToolset(ctx, toolsetsrepo.DeleteToolsetParams{Slug: toolset.Slug, ProjectID: toolset.ProjectID})
				require.NoError(t, err)
			},
			reason: "toolset deleted",
		},
		{
			name: "deleted project",
			delete: func(t *testing.T, ctx context.Context, f *aliasBackfillFixture, toolset toolsetsrepo.Toolset) {
				t.Helper()
				_, err := projectsrepo.New(f.ti.conn).DeleteProject(ctx, toolset.ProjectID)
				require.NoError(t, err)
			},
			reason: "project deleted",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, f := newAliasBackfillFixture(t)
			slug := f.slug("gone")
			toolset := f.aliasedServer(t, ctx, slug, platformHost)
			tc.delete(t, ctx, f, toolset)

			rows := f.run(t, ctx, toolset, true)
			requireOutcomes(t, rows, aliasBackfillOutcome{slug: slug, domain: platformHost, action: mcpendpoints.AliasBackfillSkip, reason: tc.reason, platform: false})
			require.Empty(t, f.endpoints(t, ctx, toolset.ID))
		})
	}
}

func TestAliasBackfillPrivateServerFollowsIngressRules(t *testing.T) {
	t.Parallel()
	ctx, f := newAliasBackfillFixture(t)
	slug := f.slug("private")
	toolset := seedHostedToolset(t, ctx, f.ti.conn, f.authCtx.ActiveOrganizationID, *f.authCtx.ProjectID, slug)
	f.serverWithMode(t, ctx, toolset, "dual")

	rows := f.run(t, ctx, toolset, true)
	requireOutcomes(t, rows, aliasBackfillOutcome{slug: slug, domain: platformHost, action: mcpendpoints.AliasBackfillSkip, reason: "online private network ingress is required", platform: false})
	require.Empty(t, f.endpoints(t, ctx, toolset.ID))
}

func TestAliasBackfillSkipsPrivateOnlyServer(t *testing.T) {
	t.Parallel()
	ctx, f := newAliasBackfillFixture(t)
	slug := f.slug("private-only")
	toolset := seedHostedToolset(t, ctx, f.ti.conn, f.authCtx.ActiveOrganizationID, *f.authCtx.ProjectID, slug)
	f.serverWithMode(t, ctx, toolset, "private_only")

	rows := f.run(t, ctx, toolset, true)
	requireOutcomes(t, rows, aliasBackfillOutcome{slug: slug, domain: platformHost, action: mcpendpoints.AliasBackfillSkip, reason: "network mode does not serve the public host", platform: false})
	require.Empty(t, f.endpoints(t, ctx, toolset.ID))
}

func TestAliasBackfillRerunIsIdempotent(t *testing.T) {
	t.Parallel()
	ctx, f := newAliasBackfillFixture(t)
	domain := f.activeDomain(t, ctx, []string{})
	slug := f.slug("rerun")
	toolset := f.aliasedServer(t, ctx, slug, domain)

	first := f.run(t, ctx, toolset, true)
	requireOutcomes(t, first,
		aliasBackfillOutcome{slug: slug, domain: domain, action: mcpendpoints.AliasBackfillCreate, reason: "", platform: false},
		aliasBackfillOutcome{slug: slug, domain: platformHost, action: mcpendpoints.AliasBackfillCreate, reason: "", platform: true},
	)
	before, err := audittest.AuditLogCountByAction(ctx, f.ti.conn, audit.ActionMcpEndpointCreate)
	require.NoError(t, err)

	second := f.run(t, ctx, toolset, true)
	requireOutcomes(t, second,
		aliasBackfillOutcome{slug: slug, domain: domain, action: mcpendpoints.AliasBackfillAlreadyHeld, reason: "", platform: false},
		aliasBackfillOutcome{slug: slug, domain: platformHost, action: mcpendpoints.AliasBackfillAlreadyHeld, reason: "", platform: true},
	)
	require.Len(t, f.endpoints(t, ctx, toolset.ID), 2)
	after, err := audittest.AuditLogCountByAction(ctx, f.ti.conn, audit.ActionMcpEndpointCreate)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestAliasBackfillDryRunWritesNothing(t *testing.T) {
	t.Parallel()
	ctx, f := newAliasBackfillFixture(t)
	domain := f.activeDomain(t, ctx, []string{})
	slug := f.slug("dry")
	toolset := f.aliasedServer(t, ctx, slug, domain)
	before, err := audittest.AuditLogCount(ctx, f.ti.conn)
	require.NoError(t, err)

	rows := f.run(t, ctx, toolset, false)
	requireOutcomes(t, rows,
		aliasBackfillOutcome{slug: slug, domain: domain, action: mcpendpoints.AliasBackfillCreate, reason: "", platform: false},
		aliasBackfillOutcome{slug: slug, domain: platformHost, action: mcpendpoints.AliasBackfillCreate, reason: "", platform: true},
	)
	require.Empty(t, f.endpoints(t, ctx, toolset.ID))
	after, err := audittest.AuditLogCount(ctx, f.ti.conn)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestAliasBackfillPlatformAliasHoldsLooseSlug(t *testing.T) {
	t.Parallel()
	ctx, f := newAliasBackfillFixture(t)
	domain := f.activeDomain(t, ctx, []string{})
	slug := f.slug("held")
	toolset := f.aliasedServer(t, ctx, slug, domain)
	f.endpoint(t, ctx, toolset.ID, domain, slug)
	other := seedHostedToolset(t, ctx, f.ti.conn, f.authCtx.ActiveOrganizationID, *f.authCtx.ProjectID, slug)

	rows := f.run(t, ctx, toolset, true)
	requireOutcomes(t, rows,
		aliasBackfillOutcome{slug: slug, domain: domain, action: mcpendpoints.AliasBackfillAlreadyHeld, reason: "", platform: false},
		aliasBackfillOutcome{slug: slug, domain: platformHost, action: mcpendpoints.AliasBackfillSkip, reason: "not served on platform: a platform alias holds the slug", platform: true},
	)
	require.Empty(t, f.endpoints(t, ctx, other.ID))
}

func TestAliasBackfillSkipsToolsetsWithoutCanonicalServer(t *testing.T) {
	t.Parallel()
	ctx, f := newAliasBackfillFixture(t)
	slug := f.slug("serverless")
	toolset := seedHostedToolset(t, ctx, f.ti.conn, f.authCtx.ActiveOrganizationID, *f.authCtx.ProjectID, slug)

	rows := f.run(t, ctx, toolset, true)
	requireOutcomes(t, rows, aliasBackfillOutcome{slug: slug, domain: platformHost, action: mcpendpoints.AliasBackfillSkip, reason: "no live canonical server", platform: false})
}

func TestAliasBackfillSkipsServerThatIsNotCanonical(t *testing.T) {
	t.Parallel()
	ctx, f := newAliasBackfillFixture(t)
	slug := f.slug("moved")
	toolset := seedHostedToolset(t, ctx, f.ti.conn, f.authCtx.ActiveOrganizationID, *f.authCtx.ProjectID, slug)
	other := seedHostedToolset(t, ctx, f.ti.conn, f.authCtx.ActiveOrganizationID, *f.authCtx.ProjectID, f.slug("other"))
	seedToolsetBackedMcpServerWithID(t, ctx, f.ti.conn, toolset.ProjectID, other.ID, toolset.ID, "private")

	rows := f.run(t, ctx, toolset, true)
	requireOutcomes(t, rows, aliasBackfillOutcome{slug: slug, domain: platformHost, action: mcpendpoints.AliasBackfillSkip, reason: "server is not the toolset's canonical server", platform: false})
	require.Empty(t, f.endpoints(t, ctx, toolset.ID))
}

func TestAliasBackfillSkipsToolsetWithoutAlias(t *testing.T) {
	t.Parallel()
	ctx, f := newAliasBackfillFixture(t)
	toolset := f.aliasedServer(t, ctx, f.slug("cleared"), platformHost)
	require.NoError(t, legacyalias.SetFixture(ctx, f.ti.conn, toolset.ProjectID, toolset.ID, legacyalias.None()))

	rows := f.run(t, ctx, toolset, true)
	requireOutcomes(t, rows, aliasBackfillOutcome{slug: "", domain: platformHost, action: mcpendpoints.AliasBackfillSkip, reason: "toolset no longer has an alias", platform: false})
}

func TestAliasBackfillSkipsOrganizationDrift(t *testing.T) {
	t.Parallel()
	ctx, f := newAliasBackfillFixture(t)
	slug := f.slug("drift")
	toolset := f.aliasedServer(t, ctx, slug, platformHost)

	result, err := mcpendpoints.BackfillAliasEndpoints(ctx, f.ti.conn, testenv.NewLogger(t), audit.NewLogger(audit.WithoutOutbox()), mcpendpoints.AliasBackfillCandidate{
		ToolsetID: toolset.ID, ProjectID: toolset.ProjectID, OrganizationID: "org-" + uuid.NewString(),
	}, true, nil)
	require.NoError(t, err)
	requireOutcomes(t, result.Rows, aliasBackfillOutcome{slug: slug, domain: platformHost, action: mcpendpoints.AliasBackfillSkip, reason: "toolset or project organization changed; rerun", platform: false})
	require.Empty(t, f.endpoints(t, ctx, toolset.ID))
}

func TestAliasBackfillFreeAccountKeepsUnprefixedCustomDomainSlug(t *testing.T) {
	t.Parallel()
	ctx, f := newAliasBackfillFixture(t)
	require.NoError(t, orgrepo.New(f.ti.conn).SetAccountType(ctx, orgrepo.SetAccountTypeParams{GramAccountType: "free", ID: f.authCtx.ActiveOrganizationID}))
	domain := f.activeDomain(t, ctx, []string{})
	slug := "free-" + uuid.NewString()[:6]
	toolset := f.aliasedServer(t, ctx, slug, domain)

	rows := f.run(t, ctx, toolset, true)
	requireOutcomes(t, rows,
		aliasBackfillOutcome{slug: slug, domain: domain, action: mcpendpoints.AliasBackfillCreate, reason: "", platform: false},
		aliasBackfillOutcome{slug: slug, domain: platformHost, action: mcpendpoints.AliasBackfillCreate, reason: "", platform: true},
	)
}

func TestAliasBackfillLeavesAliasOnPendingDomain(t *testing.T) {
	t.Parallel()
	ctx, f := newAliasBackfillFixture(t)
	domain := uuid.NullUUID{UUID: seedCustomDomain(t, ctx, f.ti.conn, f.authCtx.ActiveOrganizationID), Valid: true}
	slug := f.slug("pending")
	toolset := f.aliasedServer(t, ctx, slug, domain)

	result := f.runResult(t, ctx, toolset, true)
	requireOutcomes(t, result.Rows,
		aliasBackfillOutcome{slug: slug, domain: domain, action: mcpendpoints.AliasBackfillSkip, reason: "a hosted MCP server can only use the organization's verified custom domain", platform: false},
		aliasBackfillOutcome{slug: slug, domain: platformHost, action: mcpendpoints.AliasBackfillCreate, reason: "", platform: true},
	)
	require.Nil(t, result.AliasMove)
	require.Equal(t, domain, f.alias(t, ctx, toolset).CustomDomainID, "the alias stays on the domain it will serve once verified")
}

func TestAliasBackfillPublishesNoOutboxEvents(t *testing.T) {
	t.Parallel()
	ctx, f := newAliasBackfillFixture(t)
	toolset := f.aliasedServer(t, ctx, f.slug("quiet"), platformHost)
	before, err := testrepo.New(f.ti.conn).CountPublishOutboxRows(ctx)
	require.NoError(t, err)

	rows := f.run(t, ctx, toolset, true)
	require.Equal(t, mcpendpoints.AliasBackfillCreate, rows[0].Action)
	after, err := testrepo.New(f.ti.conn).CountPublishOutboxRows(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestUpdateMcpEndpointAddressKeepsUnchangedUnprefixedSlug(t *testing.T) {
	t.Parallel()
	ctx, f := newAliasBackfillFixture(t)
	serverID := seedMcpServer(t, ctx, f.ti.conn, *f.authCtx.ProjectID)
	slug := "unprefixed-" + uuid.NewString()[:6]
	endpointID := f.endpoint(t, ctx, serverID, platformHost, slug)

	tx, err := f.ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: caller-owned transaction exercises the write seam
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	updated, _, err := f.ti.service.UpdateMcpEndpointAddressInTransaction(ctx, tx, mcpendpoints.UpdateMcpEndpointAddressInput{
		AuthContext: f.authCtx, EndpointID: endpointID, CustomDomainID: platformHost, Slug: slug,
	})
	require.NoError(t, err)
	require.Equal(t, slug, string(updated.Slug))
}

func TestUpdateMcpEndpointAddressRechecksPrefixOnDomainChange(t *testing.T) {
	t.Parallel()
	ctx, f := newAliasBackfillFixture(t)
	domain := f.activeDomain(t, ctx, []string{})
	serverID := seedMcpServer(t, ctx, f.ti.conn, *f.authCtx.ProjectID)
	slug := "unprefixed-" + uuid.NewString()[:6]
	endpointID := f.endpoint(t, ctx, serverID, domain, slug)

	tx, err := f.ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: caller-owned transaction exercises the write seam
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, _, err = f.ti.service.UpdateMcpEndpointAddressInTransaction(ctx, tx, mcpendpoints.UpdateMcpEndpointAddressInput{
		AuthContext: f.authCtx, EndpointID: endpointID, CustomDomainID: platformHost, Slug: slug,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestCanonicalHostingAdvertisesLiveCustomDomainFirst(t *testing.T) {
	t.Parallel()
	ctx, f := newAliasBackfillFixture(t)
	toolset := f.aliasedServer(t, ctx, f.slug("advertised"), platformHost)
	f.endpoint(t, ctx, toolset.ID, platformHost, f.slug("platform"))
	domain := f.activeDomain(t, ctx, []string{})
	domainSlug := f.slug("domain")
	f.endpoint(t, ctx, toolset.ID, domain, domainSlug)

	rows, err := hostingstate.Load(ctx, f.ti.conn, f.authCtx.ActiveOrganizationID, []uuid.UUID{toolset.ID})
	require.NoError(t, err)
	_, address := hostingstate.Derive(uuid.NullUUID{UUID: uuid.Nil, Valid: false}, hostingstate.For(rows, toolset.ID))
	require.Equal(t, hostingstate.Address{Slug: pgtype.Text{String: domainSlug, Valid: true}, CustomDomainID: domain}, address)
}

func TestAliasBackfillRecordsIntentBeforeCommit(t *testing.T) {
	t.Parallel()
	ctx, f := newAliasBackfillFixture(t)
	toolset := f.aliasedServer(t, ctx, f.slug("intent"), platformHost)
	candidate := mcpendpoints.AliasBackfillCandidate{ToolsetID: toolset.ID, ProjectID: toolset.ProjectID, OrganizationID: f.authCtx.ActiveOrganizationID}

	_, err := mcpendpoints.BackfillAliasEndpoints(ctx, f.ti.conn, testenv.NewLogger(t), audit.NewLogger(audit.WithoutOutbox()), candidate, true,
		func(mcpendpoints.AliasBackfillResult) error { return errors.New("journal full") })
	require.ErrorIs(t, err, mcpendpoints.ErrAliasBackfillNotRecorded)
	require.Empty(t, f.endpoints(t, ctx, toolset.ID), "nothing commits when its intent is not recorded")

	var recorded mcpendpoints.AliasBackfillResult
	result, err := mcpendpoints.BackfillAliasEndpoints(ctx, f.ti.conn, testenv.NewLogger(t), audit.NewLogger(audit.WithoutOutbox()), candidate, true,
		func(pending mcpendpoints.AliasBackfillResult) error {
			recorded = pending
			require.Empty(t, f.endpoints(t, ctx, toolset.ID), "the intent is recorded before the commit")
			return nil
		})
	require.NoError(t, err)
	require.Equal(t, result, recorded)
	endpoints := f.endpoints(t, ctx, toolset.ID)
	require.Len(t, endpoints, 1)
	require.Equal(t, recorded.Rows[0].EndpointID.UUID, endpoints[0].ID, "the recorded endpoint id is the committed one")
}

func TestAliasBackfillCancelledCommitIsUnknown(t *testing.T) {
	t.Parallel()
	ctx, f := newAliasBackfillFixture(t)
	toolset := f.aliasedServer(t, ctx, f.slug("cancelled"), platformHost)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	_, err := mcpendpoints.BackfillAliasEndpoints(runCtx, f.ti.conn, testenv.NewLogger(t), audit.NewLogger(audit.WithoutOutbox()), mcpendpoints.AliasBackfillCandidate{
		ToolsetID: toolset.ID, ProjectID: toolset.ProjectID, OrganizationID: f.authCtx.ActiveOrganizationID,
	}, true, func(mcpendpoints.AliasBackfillResult) error {
		cancel()
		return nil
	})
	require.ErrorIs(t, err, mcpendpoints.ErrAliasBackfillCommitUnknown)
}
