package mcpendpoints_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp/legacyalias"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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

func (f *aliasBackfillFixture) run(t *testing.T, ctx context.Context, toolset toolsetsrepo.Toolset, apply bool) []mcpendpoints.AliasBackfillRow {
	t.Helper()
	rows, err := mcpendpoints.BackfillAliasEndpoints(ctx, f.ti.conn, testenv.NewLogger(t), audit.NewLogger(audit.WithoutOutbox()), mcpendpoints.AliasBackfillCandidate{
		ToolsetID: toolset.ID, ProjectID: toolset.ProjectID, OrganizationID: f.authCtx.ActiveOrganizationID,
	}, apply)
	require.NoError(t, err)
	return rows
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

	rows := f.run(t, ctx, toolset, true)
	requireOutcomes(t, rows,
		aliasBackfillOutcome{slug: slug, domain: domain, action: mcpendpoints.AliasBackfillSkip, reason: "custom domain deleted", platform: false},
		aliasBackfillOutcome{slug: slug, domain: platformHost, action: mcpendpoints.AliasBackfillCreate, reason: "", platform: true},
	)
	require.Len(t, f.endpoints(t, ctx, toolset.ID), 1)
}

func TestAliasBackfillUnprefixedPlatformSlugIsSkipped(t *testing.T) {
	t.Parallel()
	ctx, f := newAliasBackfillFixture(t)
	domain := f.activeDomain(t, ctx, []string{})
	slug := "unprefixed-" + uuid.NewString()[:6]
	toolset := f.aliasedServer(t, ctx, slug, domain)
	f.endpoint(t, ctx, toolset.ID, domain, slug)

	rows := f.run(t, ctx, toolset, true)
	require.Len(t, rows, 2)
	require.Equal(t, mcpendpoints.AliasBackfillSkip, rows[1].Action)
	require.Contains(t, rows[1].Reason, "prefixed with the org slug")
	require.Len(t, f.endpoints(t, ctx, toolset.ID), 1)
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
	_, err := mcpserversrepo.New(f.ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: toolset.ID, ProjectID: toolset.ProjectID, Name: conv.ToPGText("private hosted"), Slug: conv.ToPGText("private-hosted-" + uuid.NewString()[:8]),
		EnvironmentID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, TunneledMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, UnproxiedMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Visibility: "private",
		NetworkAccessMode: pgtype.Text{String: "dual", Valid: true},
	})
	require.NoError(t, err)

	rows := f.run(t, ctx, toolset, true)
	requireOutcomes(t, rows, aliasBackfillOutcome{slug: slug, domain: platformHost, action: mcpendpoints.AliasBackfillSkip, reason: "online private network ingress is required", platform: false})
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
