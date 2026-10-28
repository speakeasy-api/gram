package mcpendpoints_test

import (
	"context"
	"fmt"
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
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
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

	created, err := toolsetsSvc.CreateToolset(ctx, &toolsetsgen.CreateToolsetPayload{Name: "Hosted address", ToolUrns: []string{}})
	require.NoError(t, err)
	toolset, err := toolsetsrepo.New(ti.conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: uuid.MustParse(created.ID), ProjectID: *clone.ProjectID})
	require.NoError(t, err)

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

// requireProjected returns the hosted server's single endpoint and asserts the toolset hosting columns stay unset.
func (f *hostedAddressFixture) requireProjected(t *testing.T, ctx context.Context) mcpendpointsrepo.McpEndpoint {
	t.Helper()

	endpoints := f.endpoints(t, ctx)
	require.Len(t, endpoints, 1)
	return endpoints[0]
}

func (f *hostedAddressFixture) endpoints(t *testing.T, ctx context.Context) []mcpendpointsrepo.McpEndpoint {
	t.Helper()

	toolset, err := toolsetsrepo.New(f.ti.conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: f.toolset.ID, ProjectID: f.projectID})
	require.NoError(t, err)
	require.False(t, toolset.McpSlug.Valid, "endpoint writes never touch the toolset hosting columns")
	require.False(t, toolset.CustomDomainID.Valid, "endpoint writes never touch the toolset hosting columns")
	endpoints, err := mcpendpointsrepo.New(f.ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: f.projectID, McpServerID: f.toolset.ID})
	require.NoError(t, err)
	return endpoints
}

func (f *hostedAddressFixture) resolves(t *testing.T, ctx context.Context, slug string) bool {
	t.Helper()

	input, err := mcpendpoints.FromContext(ctx, slug)
	require.NoError(t, err)
	resolved, err := mcpendpoints.Resolve(ctx, f.ti.conn, testenv.NewLogger(t), input)
	require.NoError(t, err)
	return resolved.Found && resolved.Allowed
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

func TestUpdateMcpEndpointHostedRenameMovesEndpointInPlace(t *testing.T) {
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
	require.True(t, f.resolves(t, ctx, newSlug))
	require.False(t, f.resolves(t, ctx, oldSlug), "a server created without a toolset alias stops serving its old address")

	record, err := audittest.LatestAuditLogByActionAndSubject(ctx, f.ti.conn, audit.ActionMcpEndpointUpdate, f.endpoint.ID.String())
	require.NoError(t, err)
	require.Contains(t, string(record.BeforeSnapshot), oldSlug)
	require.Contains(t, string(record.AfterSnapshot), newSlug)

	// A toolset rename leaves the address alone.
	_, err = f.toolsets.UpdateToolset(ctx, &toolsetsgen.UpdateToolsetPayload{Slug: types.Slug(f.toolset.Slug), Name: new("Renamed toolset")})
	require.NoError(t, err)
	require.Equal(t, endpoint, f.requireProjected(t, ctx))
}

func TestHostedServerManagesSeveralEndpoints(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedAddressFixture(t, "pro")
	secondSlug := f.authCtx.OrganizationSlug + "-second"

	created, err := f.ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{McpServerID: new(f.toolset.ID.String()), Slug: types.McpEndpointSlug(secondSlug)})
	require.NoError(t, err)
	require.Len(t, f.endpoints(t, ctx), 2)
	require.True(t, f.resolves(t, ctx, f.endpoint.Slug))
	require.True(t, f.resolves(t, ctx, secondSlug))

	// The second address cannot take the first's slug.
	_, err = f.ti.service.UpdateMcpEndpoint(ctx, &gen.UpdateMcpEndpointPayload{ID: created.ID, McpServerID: new(f.toolset.ID.String()), Slug: types.McpEndpointSlug(f.endpoint.Slug)})
	requireOopsCode(t, err, oops.CodeConflict)

	renamed := f.authCtx.OrganizationSlug + "-second-renamed"
	view, err := f.ti.service.UpdateMcpEndpoint(ctx, &gen.UpdateMcpEndpointPayload{ID: created.ID, McpServerID: new(f.toolset.ID.String()), Slug: types.McpEndpointSlug(renamed)})
	require.NoError(t, err)
	require.Equal(t, created.ID, view.ID)
	byID := map[uuid.UUID]mcpendpointsrepo.McpEndpoint{}
	for _, endpoint := range f.endpoints(t, ctx) {
		byID[endpoint.ID] = endpoint
	}
	require.Equal(t, f.endpoint, byID[f.endpoint.ID], "the other endpoint is untouched")
	require.Equal(t, renamed, byID[uuid.MustParse(created.ID)].Slug)

	require.NoError(t, f.ti.service.DeleteMcpEndpoint(ctx, &gen.DeleteMcpEndpointPayload{ID: f.endpoint.ID.String()}))
	remaining := f.requireProjected(t, ctx)
	require.Equal(t, created.ID, remaining.ID.String())
	require.False(t, f.resolves(t, ctx, f.endpoint.Slug))
	require.True(t, f.resolves(t, ctx, renamed))
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

func TestUpdateMcpEndpointHostedAppliesHostedAddressRules(t *testing.T) {
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

	require.Equal(t, f.endpoint, f.requireProjected(t, ctx), "rejected writes leave the address untouched")

	require.Equal(t, f.endpoint, f.requireProjected(t, ctx), "rejected writes leave the address untouched")
}

func TestUpdateMcpEndpointRejectsRepointingToOrFromHostedServer(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedAddressFixture(t, "pro")

	otherServer := seedMcpServer(t, ctx, f.ti.conn, f.projectID)
	_, err := f.ti.service.UpdateMcpEndpoint(ctx, f.payload(f.endpoint.Slug, func(p *gen.UpdateMcpEndpointPayload) {
		p.McpServerID = new(otherServer.String())
	}))
	requireOopsCode(t, err, oops.CodeInvalid)
	require.ErrorContains(t, err, "create or delete endpoints instead")
	require.Equal(t, f.endpoint, f.requireProjected(t, ctx))

	foreign, err := mcpendpointsrepo.New(f.ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: f.projectID, McpServerID: uuid.NullUUID{UUID: otherServer, Valid: true}, Slug: f.authCtx.OrganizationSlug + "-foreign",
	})
	require.NoError(t, err)
	_, err = f.ti.service.UpdateMcpEndpoint(ctx, &gen.UpdateMcpEndpointPayload{ID: foreign.ID.String(), McpServerID: new(f.toolset.ID.String()), Slug: types.McpEndpointSlug(foreign.Slug)})
	requireOopsCode(t, err, oops.CodeInvalid)
	require.Equal(t, f.endpoint, f.requireProjected(t, ctx), "another server's endpoint does not move onto the hosted server")
}

func TestCreateMcpEndpointHostedAppliesAddressRules(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedAddressFixture(t, "pro")
	create := func(slug string, domainID *string) error {
		_, err := f.ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{McpServerID: new(f.toolset.ID.String()), Slug: types.McpEndpointSlug(slug), CustomDomainID: domainID})
		if err != nil {
			return fmt.Errorf("create hosted endpoint: %w", err)
		}
		return nil
	}

	requireOopsCode(t, create(f.authCtx.OrganizationSlug+"-abcdefghijklmnopqrstuvwxyz0123456789abcdefgh", nil), oops.CodeBadRequest)
	unverified, err := customdomainsrepo.New(f.ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: f.authCtx.ActiveOrganizationID, Domain: "create-unverified.example.com", IngressName: conv.ToPGText("ingress-create-unverified"),
		CertSecretName: conv.ToPGText("cert-create-unverified"), ProvisionerKind: "ingress", IpAllowlist: []string{},
	})
	require.NoError(t, err)
	err = create("on-unverified", new(unverified.ID.String()))
	requireOopsCode(t, err, oops.CodeInvalid)
	require.ErrorContains(t, err, "verified custom domain")
	require.Len(t, f.endpoints(t, ctx), 1, "rejected creates add nothing")

	require.NoError(t, create(f.authCtx.OrganizationSlug+"-created", nil))
	require.Len(t, f.endpoints(t, ctx), 2)
}

func TestDeleteMcpEndpointKeepsLastAddressOfEnabledHostedServer(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedAddressFixture(t, "pro")

	err := f.ti.service.DeleteMcpEndpoint(ctx, &gen.DeleteMcpEndpointPayload{ID: f.endpoint.ID.String()})
	requireOopsCode(t, err, oops.CodeInvalid)
	require.ErrorContains(t, err, "disable the server before removing its last address")
	require.Equal(t, f.endpoint, f.requireProjected(t, ctx))

	server, err := mcpserversrepo.New(f.ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: f.toolset.ID, ProjectID: f.projectID})
	require.NoError(t, err)
	_, err = mcpserversrepo.New(f.ti.conn).SyncHostedMCPServer(ctx, mcpserversrepo.SyncHostedMCPServerParams{
		Name: server.Name, Slug: server.Slug, Visibility: "disabled", UserSessionIssuerID: server.UserSessionIssuerID,
		ToolVariationsGroupID: server.ToolVariationsGroupID, NetworkAccessMode: server.NetworkAccessMode, ID: server.ID, ProjectID: server.ProjectID,
	})
	require.NoError(t, err)
	require.NoError(t, f.ti.service.DeleteMcpEndpoint(ctx, &gen.DeleteMcpEndpointPayload{ID: f.endpoint.ID.String()}), "a disabled server may drop its last address")
	require.Empty(t, f.endpoints(t, ctx))
}

func TestHostedEndpointWritesFollowPrivateIngressNamespace(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedAddressFixture(t, "pro")
	fixtures := testrepo.New(f.ti.conn)
	require.NoError(t, fixtures.InsertNetworkIngressFixture(ctx, testrepo.InsertNetworkIngressFixtureParams{
		ID: uuid.New(), OrganizationID: f.authCtx.ActiveOrganizationID, DnsName: pgtype.Text{String: "private.example.ts.net", Valid: true},
	}))
	require.NoError(t, fixtures.SetNetworkIngressObservationFixture(ctx, testrepo.SetNetworkIngressObservationFixtureParams{
		Status: "online", OrganizationID: f.authCtx.ActiveOrganizationID,
	}))
	server, err := mcpserversrepo.New(f.ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: f.toolset.ID, ProjectID: f.projectID})
	require.NoError(t, err)
	_, err = mcpserversrepo.New(f.ti.conn).SyncHostedMCPServer(ctx, mcpserversrepo.SyncHostedMCPServerParams{
		Name: server.Name, Slug: server.Slug, Visibility: "private", UserSessionIssuerID: server.UserSessionIssuerID,
		ToolVariationsGroupID: server.ToolVariationsGroupID, NetworkAccessMode: pgtype.Text{String: "dual", Valid: true}, ID: server.ID, ProjectID: server.ProjectID,
	})
	require.NoError(t, err)
	domain := f.activeDomain(t, ctx, "ingress-namespace.example.com")

	// The ingress serves the platform namespace, so a custom-domain address cannot be private.
	_, err = f.ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{McpServerID: new(f.toolset.ID.String()), Slug: "on-domain", CustomDomainID: new(domain.ID.String())})
	requireOopsCode(t, err, oops.CodeInvalid)
	require.ErrorContains(t, err, "private ingress namespace")
	_, err = f.ti.service.UpdateMcpEndpoint(ctx, f.payload("on-domain", func(p *gen.UpdateMcpEndpointPayload) { p.CustomDomainID = new(domain.ID.String()) }))
	requireOopsCode(t, err, oops.CodeInvalid)
	require.ErrorContains(t, err, "private ingress namespace")
	require.Equal(t, f.endpoint, f.requireProjected(t, ctx))

	_, err = f.ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{McpServerID: new(f.toolset.ID.String()), Slug: types.McpEndpointSlug(f.authCtx.OrganizationSlug + "-private-second")})
	require.NoError(t, err, "a platform address matches the ingress namespace")
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

// placeOnDomain puts the hosted endpoint on a domain the product would not accept, as legacy rows sit.
func (f *hostedAddressFixture) placeOnDomain(t *testing.T, ctx context.Context, name string) customdomainsrepo.CustomDomain {
	t.Helper()

	domain, err := customdomainsrepo.New(f.ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: f.authCtx.ActiveOrganizationID, Domain: name, IngressName: conv.ToPGText("ingress-" + name),
		CertSecretName: conv.ToPGText("cert-" + name), ProvisionerKind: "ingress", IpAllowlist: []string{},
	})
	require.NoError(t, err)
	domainID := uuid.NullUUID{UUID: domain.ID, Valid: true}
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
	requireOopsCode(t, err, oops.CodeInvalid)
	require.ErrorContains(t, err, "cannot leave its custom domain")
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

func TestUpdateMcpEndpointHostedAcceptsServerScopedWrite(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedAddressFixture(t, "pro")

	otherServerID := seedMcpServer(t, ctx, f.ti.conn, f.projectID)
	other, err := f.ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      new(otherServerID.String()),
		Slug:             types.McpEndpointSlug(f.authCtx.OrganizationSlug + "-plain"),
	})
	require.NoError(t, err)

	scoped := withExactAuthzGrants(t, ctx, f.ti.conn,
		authz.NewGrant(authz.ScopeMCPWrite, f.toolset.ID.String()),
		authz.NewGrant(authz.ScopeMCPWrite, otherServerID.String()),
	)

	newSlug := f.authCtx.OrganizationSlug + "-scoped"
	view, err := f.ti.service.UpdateMcpEndpoint(scoped, f.payload(newSlug, nil))
	require.NoError(t, err)
	require.Equal(t, newSlug, string(view.Slug))
	require.Equal(t, newSlug, f.requireProjected(t, ctx).Slug)

	_, err = f.ti.service.UpdateMcpEndpoint(scoped, &gen.UpdateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               other.ID,
		CustomDomainID:   nil,
		McpServerID:      new(otherServerID.String()),
		Slug:             types.McpEndpointSlug(f.authCtx.OrganizationSlug + "-plain-renamed"),
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestUpdateMcpEndpointHostedRejectsOtherServerScopedWrite(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedAddressFixture(t, "pro")

	scoped := withExactAuthzGrants(t, ctx, f.ti.conn, authz.NewGrant(authz.ScopeMCPWrite, uuid.NewString()))

	_, err := f.ti.service.UpdateMcpEndpoint(scoped, f.payload(f.authCtx.OrganizationSlug+"-denied", nil))
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Equal(t, f.endpoint.Slug, f.requireProjected(t, ctx).Slug)
}

func TestCreateMcpEndpointOnDisabledHostedServerChecksIngress(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedAddressFixture(t, "pro")
	server, err := mcpserversrepo.New(f.ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: f.toolset.ID, ProjectID: f.projectID})
	require.NoError(t, err)
	_, err = mcpserversrepo.New(f.ti.conn).SyncHostedMCPServer(ctx, mcpserversrepo.SyncHostedMCPServerParams{
		Name: server.Name, Slug: server.Slug, Visibility: "disabled", UserSessionIssuerID: server.UserSessionIssuerID,
		ToolVariationsGroupID: server.ToolVariationsGroupID, NetworkAccessMode: pgtype.Text{String: "private_only", Valid: true}, ID: server.ID, ProjectID: server.ProjectID,
	})
	require.NoError(t, err)

	// A re-enable must not find an address added past the private ingress check.
	_, err = f.ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{McpServerID: new(f.toolset.ID.String()), Slug: types.McpEndpointSlug(f.authCtx.OrganizationSlug + "-disabled-private")})
	requireOopsCode(t, err, oops.CodeInvalid)
	require.ErrorContains(t, err, "online private network ingress is required")
	require.Len(t, f.endpoints(t, ctx), 1)
}

// A server with several endpoints and a slug of its own keeps its slug when an endpoint moves.
func TestUpdateMcpEndpointHostedLeavesIndependentServerSlug(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedAddressFixture(t, "pro")
	_, err := mcpendpointsrepo.New(f.ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: f.projectID, McpServerID: uuid.NullUUID{UUID: f.toolset.ID, Valid: true}, Slug: f.authCtx.OrganizationSlug + "-second",
	})
	require.NoError(t, err)
	servers := mcpserversrepo.New(f.ti.conn)
	server, err := servers.GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: f.toolset.ID, ProjectID: f.projectID})
	require.NoError(t, err)
	_, err = servers.SyncHostedMCPServer(ctx, mcpserversrepo.SyncHostedMCPServerParams{
		Name: server.Name, Slug: pgtype.Text{String: "own-server-slug", Valid: true}, Visibility: server.Visibility,
		UserSessionIssuerID: server.UserSessionIssuerID, ToolVariationsGroupID: server.ToolVariationsGroupID,
		NetworkAccessMode: server.NetworkAccessMode, ID: server.ID, ProjectID: server.ProjectID,
	})
	require.NoError(t, err)

	renamed := f.authCtx.OrganizationSlug + "-moved"
	_, err = f.ti.service.UpdateMcpEndpoint(ctx, f.payload(renamed, nil))
	require.NoError(t, err)
	after, err := servers.GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: f.toolset.ID, ProjectID: f.projectID})
	require.NoError(t, err)
	require.Equal(t, "own-server-slug", after.Slug.String, "the server slug does not follow an endpoint it was not named after")
}

// A new address whose slug another server uses as its name still saves; the server keeps its own slug.
func TestUpdateMcpEndpointHostedSkipsSlugFollowOnCollision(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedAddressFixture(t, "pro")
	taken := f.authCtx.OrganizationSlug + "-taken"
	remote := remotemcptest.SeedServer(t, ctx, f.ti.conn, remotemcprepo.CreateServerParams{
		ProjectID: f.projectID, TransportType: "streamable-http", Url: "https://test.example.com/mcp/" + uuid.NewString(),
	})
	_, err := mcpserversrepo.New(f.ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: f.projectID, Name: pgtype.Text{String: "Holder", Valid: true}, Slug: pgtype.Text{String: taken, Valid: true},
		RemoteMcpServerID: uuid.NullUUID{UUID: remote.ID, Valid: true}, Visibility: "private",
	})
	require.NoError(t, err)
	before, err := mcpserversrepo.New(f.ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: f.toolset.ID, ProjectID: f.projectID})
	require.NoError(t, err)

	_, err = f.ti.service.UpdateMcpEndpoint(ctx, f.payload(taken, nil))
	require.NoError(t, err)
	require.Equal(t, taken, f.requireProjected(t, ctx).Slug)
	after, err := mcpserversrepo.New(f.ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: f.toolset.ID, ProjectID: f.projectID})
	require.NoError(t, err)
	require.Equal(t, before.Slug, after.Slug)
}
