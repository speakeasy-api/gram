package mcpendpoints_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_endpoints"
	toolsetsgen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

type hostedAddressFixture struct {
	ti        *testInstance
	toolsets  *toolsets.Service
	authCtx   *contextvalues.AuthContext
	projectID uuid.UUID
	toolset   toolsetsrepo.Toolset
	endpoint  mcpendpointsrepo.McpEndpoint
}

func newHostedAddressFixture(t *testing.T, accountType string) (context.Context, *hostedAddressFixture) {
	t.Helper()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	clone := *authCtx
	clone.AccountType = accountType
	ctx = contextvalues.SetAuthContext(ctx, &clone)

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)
	authzEngine := authz.NewEngine(logger, ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	toolsetsSvc := toolsets.NewService(logger, tracerProvider, guardianPolicy, ti.conn, ti.sessionManager, nil, authzEngine, audit.NewLogger(), nil, false)
	ti.service.WithHostedAddressWriter(toolsetsSvc)

	created, err := toolsetsSvc.CreateToolset(ctx, &toolsetsgen.CreateToolsetPayload{Name: "Hosted address", ToolUrns: []string{}})
	require.NoError(t, err)
	toolset, err := toolsetsrepo.New(ti.conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: uuid.MustParse(created.ID), ProjectID: *clone.ProjectID})
	require.NoError(t, err)
	require.True(t, toolset.McpSlug.Valid)

	f := &hostedAddressFixture{ti: ti, toolsets: toolsetsSvc, authCtx: &clone, projectID: *clone.ProjectID, toolset: toolset}
	f.endpoint = f.requireProjected(t, ctx)
	return ctx, f
}

func (f *hostedAddressFixture) payload(slug string, mutate func(*gen.UpdateMcpEndpointPayload)) *gen.UpdateMcpEndpointPayload {
	payload := &gen.UpdateMcpEndpointPayload{ID: f.endpoint.ID.String(), McpServerID: new(f.toolset.ID.String()), Slug: types.McpEndpointSlug(slug)}
	if mutate != nil {
		mutate(payload)
	}
	return payload
}

// requireProjected asserts toolset.mcp_slug == server.slug == the single endpoint's slug, on the same domain.
func (f *hostedAddressFixture) requireProjected(t *testing.T, ctx context.Context) mcpendpointsrepo.McpEndpoint {
	t.Helper()

	toolset, err := toolsetsrepo.New(f.ti.conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: f.toolset.ID, ProjectID: f.projectID})
	require.NoError(t, err)
	server, err := mcpserversrepo.New(f.ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: f.toolset.ID, ProjectID: f.projectID})
	require.NoError(t, err)
	endpoints, err := mcpendpointsrepo.New(f.ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: f.projectID, McpServerID: f.toolset.ID})
	require.NoError(t, err)
	require.Len(t, endpoints, 1)
	require.Equal(t, toolset.McpSlug, server.Slug)
	require.Equal(t, toolset.McpSlug.String, endpoints[0].Slug)
	require.Equal(t, toolset.CustomDomainID, endpoints[0].CustomDomainID)
	return endpoints[0]
}

func (f *hostedAddressFixture) activeDomain(t *testing.T, ctx context.Context, name string) customdomainsrepo.CustomDomain {
	t.Helper()

	domains := customdomainsrepo.New(f.ti.conn)
	domain, err := domains.CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: f.authCtx.ActiveOrganizationID, Domain: name, IngressName: conv.ToPGText("ingress-" + name),
		CertSecretName: conv.ToPGText("cert-" + name), ProvisionerKind: "ingress", IpAllowlist: []string{},
	})
	require.NoError(t, err)
	_, err = domains.SetCustomDomainVerified(ctx, domain.ID)
	require.NoError(t, err)
	activated, err := domains.ActivateVerifiedCustomDomain(ctx, customdomainsrepo.ActivateVerifiedCustomDomainParams{
		IngressName: conv.ToPGText("ingress-" + name), CertSecretName: conv.ToPGText("cert-" + name), ProvisionerKind: "ingress", ID: domain.ID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, activated)
	return domain
}

func TestUpdateMcpEndpointHostedRenameWritesThroughToolset(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedAddressFixture(t, "pro")
	oldSlug := f.endpoint.Slug
	newSlug := f.authCtx.OrganizationSlug + "-renamed"

	view, err := f.ti.service.UpdateMcpEndpoint(ctx, f.payload(newSlug, nil))
	require.NoError(t, err)
	require.Equal(t, f.endpoint.ID.String(), view.ID, "the endpoint moves in place")
	require.Equal(t, newSlug, string(view.Slug))

	endpoint := f.requireProjected(t, ctx)
	require.Equal(t, f.endpoint.ID, endpoint.ID)
	require.Equal(t, newSlug, endpoint.Slug)

	input, err := mcpendpoints.FromContext(ctx, newSlug)
	require.NoError(t, err)
	resolved, err := mcpendpoints.Resolve(ctx, f.ti.conn, testenv.NewLogger(t), input)
	require.NoError(t, err)
	require.True(t, resolved.Found)
	require.True(t, resolved.Allowed, "serving drift checks see matching toolset columns")
	input, err = mcpendpoints.FromContext(ctx, oldSlug)
	require.NoError(t, err)
	resolved, err = mcpendpoints.Resolve(ctx, f.ti.conn, testenv.NewLogger(t), input)
	require.NoError(t, err)
	require.False(t, resolved.Found, "the old address stops serving, as with a toolset mcp_slug change")

	// A later toolset save, as an old pod would make it, leaves the projection unchanged.
	_, err = f.toolsets.UpdateToolset(ctx, &toolsetsgen.UpdateToolsetPayload{Slug: types.Slug(f.toolset.Slug), Name: new("Renamed toolset")})
	require.NoError(t, err)
	require.Equal(t, f.endpoint.ID, f.requireProjected(t, ctx).ID)
}

func TestUpdateMcpEndpointHostedMovesOntoVerifiedDomain(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedAddressFixture(t, "pro")
	domain := f.activeDomain(t, ctx, "hosted-move.example.com")

	view, err := f.ti.service.UpdateMcpEndpoint(ctx, f.payload("on-domain", func(p *gen.UpdateMcpEndpointPayload) {
		p.CustomDomainID = new(domain.ID.String())
	}))
	require.NoError(t, err)
	require.Equal(t, f.endpoint.ID.String(), view.ID)

	endpoint := f.requireProjected(t, ctx)
	require.Equal(t, uuid.NullUUID{UUID: domain.ID, Valid: true}, endpoint.CustomDomainID)
	require.Equal(t, "on-domain", endpoint.Slug)

	// A live domain cannot be left: clients may hold URLs on it.
	_, err = f.ti.service.UpdateMcpEndpoint(ctx, f.payload(f.authCtx.OrganizationSlug+"-off-domain", nil))
	requireOopsCode(t, err, oops.CodeInvalid)
	require.ErrorContains(t, err, "cannot leave its custom domain")
	require.Equal(t, "on-domain", f.requireProjected(t, ctx).Slug)
}

func TestUpdateMcpEndpointHostedAppliesToolsetAddressRules(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedAddressFixture(t, "pro")

	other, err := f.toolsets.CreateToolset(ctx, &toolsetsgen.CreateToolsetPayload{Name: "Other hosted", ToolUrns: []string{}})
	require.NoError(t, err)
	require.NotNil(t, other.McpSlug)
	_, err = f.ti.service.UpdateMcpEndpoint(ctx, f.payload(string(*other.McpSlug), nil))
	requireOopsCode(t, err, oops.CodeConflict)

	_, err = f.ti.service.UpdateMcpEndpoint(ctx, f.payload(f.authCtx.OrganizationSlug+"-"+"abcdefghijklmnopqrstuvwxyz0123456789abcdefgh", nil))
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.ErrorContains(t, err, "at most 40 characters")

	unverified, err := customdomainsrepo.New(f.ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: f.authCtx.ActiveOrganizationID, Domain: "unverified.example.com", IngressName: conv.ToPGText("ingress-unverified"),
		CertSecretName: conv.ToPGText("cert-unverified"), ProvisionerKind: "ingress", IpAllowlist: []string{},
	})
	require.NoError(t, err)
	_, err = f.ti.service.UpdateMcpEndpoint(ctx, f.payload("on-unverified", func(p *gen.UpdateMcpEndpointPayload) {
		p.CustomDomainID = new(unverified.ID.String())
	}))
	requireOopsCode(t, err, oops.CodeInvalid)
	require.ErrorContains(t, err, "verified custom domain")

	otherServer := seedMcpServer(t, ctx, f.ti.conn, f.projectID)
	_, err = f.ti.service.UpdateMcpEndpoint(ctx, f.payload(f.endpoint.Slug, func(p *gen.UpdateMcpEndpointPayload) {
		p.McpServerID = new(otherServer.String())
	}))
	requireOopsCode(t, err, oops.CodeInvalid)
	require.ErrorContains(t, err, "keeps its server")

	require.Equal(t, f.endpoint, f.requireProjected(t, ctx), "rejected writes leave the address untouched")

	// Creating a second endpoint and deleting the hosted one stay rejected.
	_, err = f.ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{McpServerID: new(f.toolset.ID.String()), Slug: types.McpEndpointSlug(f.authCtx.OrganizationSlug + "-second")})
	requireOopsCode(t, err, oops.CodeInvalid)
	err = f.ti.service.DeleteMcpEndpoint(ctx, &gen.DeleteMcpEndpointPayload{ID: f.endpoint.ID.String()})
	requireOopsCode(t, err, oops.CodeInvalid)
	f.requireProjected(t, ctx)
}

func TestUpdateMcpEndpointHostedFreeAccountNeedsPrefixOnDomain(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedAddressFixture(t, "pro")
	domain := f.activeDomain(t, ctx, "hosted-free.example.com")
	_, err := f.ti.service.UpdateMcpEndpoint(ctx, f.payload(f.authCtx.OrganizationSlug+"-on-domain", func(p *gen.UpdateMcpEndpointPayload) {
		p.CustomDomainID = new(domain.ID.String())
	}))
	require.NoError(t, err)

	free := *f.authCtx
	free.AccountType = "free"
	freeCtx := contextvalues.SetAuthContext(ctx, &free)
	_, err = f.ti.service.UpdateMcpEndpoint(freeCtx, f.payload("no-prefix", func(p *gen.UpdateMcpEndpointPayload) {
		p.CustomDomainID = new(domain.ID.String())
	}))
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.ErrorContains(t, err, "prefixed with the org slug")
	require.Equal(t, f.authCtx.OrganizationSlug+"-on-domain", f.requireProjected(t, ctx).Slug)
}

func TestUpdateMcpEndpointHostedRejectedWithoutWriter(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedAddressFixture(t, "pro")
	logger := testenv.NewLogger(t)
	unwired := mcpendpoints.NewService(logger, testenv.NewTracerProvider(t), f.ti.conn, f.ti.sessionManager, authz.NewEngine(logger, f.ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()), audit.NewLogger(), nil, false)

	_, err := unwired.UpdateMcpEndpoint(ctx, f.payload(f.authCtx.OrganizationSlug+"-renamed", nil))
	requireOopsCode(t, err, oops.CodeInvalid)
	require.ErrorContains(t, err, "manage hosted MCP endpoint addresses through the toolset")
	require.Equal(t, f.endpoint, f.requireProjected(t, ctx))
}

// placeOnDomain puts the hosted toolset and its endpoint on a domain the product would not accept, as legacy rows sit.
func (f *hostedAddressFixture) placeOnDomain(t *testing.T, ctx context.Context, name string) customdomainsrepo.CustomDomain {
	t.Helper()

	domain, err := customdomainsrepo.New(f.ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: f.authCtx.ActiveOrganizationID, Domain: name, IngressName: conv.ToPGText("ingress-" + name),
		CertSecretName: conv.ToPGText("cert-" + name), ProvisionerKind: "ingress", IpAllowlist: []string{},
	})
	require.NoError(t, err)
	domainID := uuid.NullUUID{UUID: domain.ID, Valid: true}
	require.NoError(t, toolsetsrepo.New(f.ti.conn).SetToolsetCustomDomain(ctx, toolsetsrepo.SetToolsetCustomDomainParams{CustomDomainID: domainID, Slug: f.toolset.Slug, ProjectID: f.projectID}))
	_, err = mcpendpointsrepo.New(f.ti.conn).UpdateMCPEndpointAddress(ctx, mcpendpointsrepo.UpdateMCPEndpointAddressParams{
		CustomDomainID: domainID, Slug: f.endpoint.Slug, IsDomainRoot: pgtype.Bool{Bool: false, Valid: false}, ID: f.endpoint.ID, ProjectID: f.projectID,
	})
	require.NoError(t, err)
	f.requireProjected(t, ctx)
	return domain
}

func TestUpdateMcpEndpointHostedLeavesUnservableDomain(t *testing.T) {
	t.Parallel()

	type degrade func(t *testing.T, ctx context.Context, f *hostedAddressFixture, domain customdomainsrepo.CustomDomain)
	cases := map[string]degrade{
		"never verified": func(t *testing.T, ctx context.Context, f *hostedAddressFixture, domain customdomainsrepo.CustomDomain) {
			t.Helper()
		},
		"verified, not activated": func(t *testing.T, ctx context.Context, f *hostedAddressFixture, domain customdomainsrepo.CustomDomain) {
			t.Helper()
			_, err := customdomainsrepo.New(f.ti.conn).UpdateCustomDomain(ctx, customdomainsrepo.UpdateCustomDomainParams{
				Verified: true, Activated: false, IngressName: domain.IngressName, CertSecretName: domain.CertSecretName, ProvisionerKind: domain.ProvisionerKind, ID: domain.ID,
			})
			require.NoError(t, err)
		},
		"soft-deleted": func(t *testing.T, ctx context.Context, f *hostedAddressFixture, domain customdomainsrepo.CustomDomain) {
			t.Helper()
			require.NoError(t, customdomainsrepo.New(f.ti.conn).DeleteCustomDomain(ctx, f.authCtx.ActiveOrganizationID))
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, f := newHostedAddressFixture(t, "free")
			slug := f.endpoint.Slug
			domain := f.placeOnDomain(t, ctx, "mcp.unservable.example.com")
			setup(t, ctx, f, domain)

			input, err := mcpendpoints.FromContext(ctx, slug)
			require.NoError(t, err)
			resolved, err := mcpendpoints.Resolve(ctx, f.ti.conn, testenv.NewLogger(t), input)
			require.NoError(t, err)
			require.False(t, resolved.Found, "the platform address has no endpoint yet")

			view, err := f.ti.service.UpdateMcpEndpoint(ctx, f.payload(slug, nil))
			require.NoError(t, err)
			require.Equal(t, f.endpoint.ID.String(), view.ID, "the endpoint moves in place")
			require.Nil(t, view.CustomDomainID)

			endpoint := f.requireProjected(t, ctx)
			require.False(t, endpoint.CustomDomainID.Valid)
			require.Equal(t, slug, endpoint.Slug)

			resolved, err = mcpendpoints.Resolve(ctx, f.ti.conn, testenv.NewLogger(t), input)
			require.NoError(t, err)
			require.True(t, resolved.Found)
			require.True(t, resolved.Allowed)

			record, err := audittest.LatestAuditLogByActionAndSubject(ctx, f.ti.conn, audit.ActionMcpEndpointUpdate, f.endpoint.ID.String())
			require.NoError(t, err)
			require.Contains(t, string(record.BeforeSnapshot), domain.ID.String())
			require.NotContains(t, string(record.AfterSnapshot), domain.ID.String())
			record, err = audittest.LatestAuditLogByActionAndSubject(ctx, f.ti.conn, audit.ActionToolsetUpdate, f.toolset.ID.String())
			require.NoError(t, err)
			require.Contains(t, string(record.BeforeSnapshot), domain.ID.String())
			require.NotContains(t, string(record.AfterSnapshot), domain.ID.String())
		})
	}
}

func TestUpdateMcpEndpointHostedLeavingDomainAppliesPlatformRules(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedAddressFixture(t, "pro")
	f.placeOnDomain(t, ctx, "mcp.leave-rules.example.com")
	placed := f.requireProjected(t, ctx)

	_, err := f.ti.service.UpdateMcpEndpoint(ctx, f.payload("unprefixed", nil))
	requireOopsCode(t, err, oops.CodeInvalid)
	require.ErrorContains(t, err, "invalid slug")

	other, err := f.toolsets.CreateToolset(ctx, &toolsetsgen.CreateToolsetPayload{Name: "Platform neighbour", ToolUrns: []string{}})
	require.NoError(t, err)
	require.NotNil(t, other.McpSlug)
	_, err = f.ti.service.UpdateMcpEndpoint(ctx, f.payload(string(*other.McpSlug), nil))
	requireOopsCode(t, err, oops.CodeConflict)
	require.Equal(t, placed, f.requireProjected(t, ctx), "rejected moves leave the address untouched")

	renamed := f.authCtx.OrganizationSlug + "-moved"
	view, err := f.ti.service.UpdateMcpEndpoint(ctx, f.payload(renamed, nil))
	require.NoError(t, err)
	require.Equal(t, f.endpoint.ID.String(), view.ID)
	endpoint := f.requireProjected(t, ctx)
	require.False(t, endpoint.CustomDomainID.Valid)
	require.Equal(t, renamed, endpoint.Slug)
}

func TestUpdateMcpEndpointHostedKeepsHealthDisabledDomain(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedAddressFixture(t, "pro")
	domain := f.activeDomain(t, ctx, "mcp.health-disabled.example.com")
	_, err := f.ti.service.UpdateMcpEndpoint(ctx, f.payload(f.endpoint.Slug, func(p *gen.UpdateMcpEndpointPayload) {
		p.CustomDomainID = new(domain.ID.String())
	}))
	require.NoError(t, err)

	domains := customdomainsrepo.New(f.ti.conn)
	_, err = domains.UpdateCustomDomainHealth(ctx, customdomainsrepo.UpdateCustomDomainHealthParams{
		HealthStatus: conv.ToPGText("unhealthy"), HealthIssue: conv.ToPGText("dns"), CheckedAt: pgtype.Timestamptz{Time: time.Now(), InfinityModifier: pgtype.Finite, Valid: true},
		UnhealthySince: pgtype.Timestamptz{Time: time.Now(), InfinityModifier: pgtype.Finite, Valid: true}, CertificateExpiresAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		ConsecutiveFailures: pgtype.Int4{Int32: 5, Valid: true}, ID: domain.ID, OrganizationID: f.authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	_, err = domains.DisableCustomDomainForHealth(ctx, customdomainsrepo.DisableCustomDomainForHealthParams{ID: domain.ID, OrganizationID: f.authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	placed := f.requireProjected(t, ctx)

	_, err = f.ti.service.UpdateMcpEndpoint(ctx, f.payload(f.endpoint.Slug, nil))
	requireOopsCode(t, err, oops.CodeInvalid)
	require.ErrorContains(t, err, "cannot leave its custom domain")
	require.Equal(t, placed, f.requireProjected(t, ctx), "clients on a domain that can be reverified keep their URL")
}

func TestUpdateMcpEndpointHostedLeavingDomainProbesKeptSlug(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedAddressFixture(t, "pro")
	slug := f.endpoint.Slug
	f.placeOnDomain(t, ctx, "mcp.kept-slug.example.com")

	// Another server holds the platform address the hosted server would keep.
	_, err := mcpendpointsrepo.New(f.ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: f.projectID, CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, McpServerID: uuid.NullUUID{UUID: seedMcpServer(t, ctx, f.ti.conn, f.projectID), Valid: true},
		MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Slug: slug,
	})
	require.NoError(t, err)
	placed := f.requireProjected(t, ctx)

	_, err = f.ti.service.UpdateMcpEndpoint(ctx, f.payload(slug, nil))
	requireOopsCode(t, err, oops.CodeConflict)
	require.ErrorContains(t, err, "already taken")
	require.Equal(t, placed, f.requireProjected(t, ctx))
}

func TestUpdateMcpEndpointHostedLeavingDomainChecksEndpointDomain(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedAddressFixture(t, "pro")
	f.placeOnDomain(t, ctx, "mcp.drifted.example.com")
	require.NoError(t, customdomainsrepo.New(f.ti.conn).DeleteCustomDomain(ctx, f.authCtx.ActiveOrganizationID))
	live := f.activeDomain(t, ctx, "mcp.live-drift.example.com")
	moved, err := mcpendpointsrepo.New(f.ti.conn).UpdateMCPEndpointAddress(ctx, mcpendpointsrepo.UpdateMCPEndpointAddressParams{
		CustomDomainID: uuid.NullUUID{UUID: live.ID, Valid: true}, Slug: f.endpoint.Slug, IsDomainRoot: pgtype.Bool{Bool: false, Valid: false}, ID: f.endpoint.ID, ProjectID: f.projectID,
	})
	require.NoError(t, err)

	_, err = f.ti.service.UpdateMcpEndpoint(ctx, f.payload(f.endpoint.Slug, nil))
	requireOopsCode(t, err, oops.CodeConflict)
	after, err := mcpendpointsrepo.New(f.ti.conn).GetMCPEndpointByID(ctx, mcpendpointsrepo.GetMCPEndpointByIDParams{ID: f.endpoint.ID, ProjectID: f.projectID})
	require.NoError(t, err)
	require.Equal(t, moved.CustomDomainID, after.CustomDomainID, "an endpoint on a live domain stays there")
}

func TestUpdateMcpEndpointHostedLeavingDomainDropsRoot(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedAddressFixture(t, "pro")
	domain := f.placeOnDomain(t, ctx, "mcp.root-leave.example.com")
	_, err := mcpendpointsrepo.New(f.ti.conn).UpdateMCPEndpointAddress(ctx, mcpendpointsrepo.UpdateMCPEndpointAddressParams{
		CustomDomainID: uuid.NullUUID{UUID: domain.ID, Valid: true}, Slug: f.endpoint.Slug, IsDomainRoot: pgtype.Bool{Bool: true, Valid: true}, ID: f.endpoint.ID, ProjectID: f.projectID,
	})
	require.NoError(t, err)

	_, err = f.ti.service.UpdateMcpEndpoint(ctx, f.payload(f.endpoint.Slug, nil))
	require.NoError(t, err)
	endpoint := f.requireProjected(t, ctx)
	require.Equal(t, f.endpoint.ID, endpoint.ID)
	require.False(t, endpoint.CustomDomainID.Valid)
	require.False(t, endpoint.IsDomainRoot.Valid && endpoint.IsDomainRoot.Bool, "the platform endpoint is not a domain root")
}
