package remotesessions_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// attachFixture is one isolated organization with a project to attach in.
type attachFixture struct {
	ti        *testInstance
	projectID uuid.UUID
	orgID     string
}

func newAttachFixture(t *testing.T) (context.Context, attachFixture) {
	t.Helper()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	return ctx, attachFixture{ti: ti, projectID: *authCtx.ProjectID, orgID: authCtx.ActiveOrganizationID}
}

func (f attachFixture) attach(ctx context.Context, serverID, clientID uuid.UUID) (*gen.AttachServerClientAssociationResult, error) {
	result, err := f.ti.service.AttachServerClientAssociation(ctx, &gen.AttachServerClientAssociationPayload{
		McpServerID:           serverID.String(),
		RemoteSessionClientID: clientID.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("attach server client: %w", err)
	}
	return result, nil
}

func (f attachFixture) issuer(t *testing.T, ctx context.Context, slug string) uuid.UUID {
	t.Helper()
	issuer, err := usersessionsrepo.New(f.ti.conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          f.projectID,
		OrganizationID:     conv.ToPGText(f.orgID),
		Slug:               slug,
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)
	return issuer.ID
}

func (f attachFixture) serverAt(t *testing.T, ctx context.Context, slug, url string, userIssuerID uuid.UUID) uuid.UUID {
	t.Helper()
	remote, err := remotemcprepo.New(f.ti.conn).CreateServer(ctx, remotemcprepo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     f.projectID,
		Name:          conv.ToPGText(slug),
		Slug:          conv.ToPGText(slug),
		TransportType: "streamable-http",
		Url:           url,
	})
	require.NoError(t, err)
	server, err := mcpserversrepo.New(f.ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           f.projectID,
		Name:                conv.ToPGText(slug),
		Slug:                conv.ToPGText(slug),
		UserSessionIssuerID: conv.ToNullUUID(userIssuerID),
		RemoteMcpServerID:   conv.ToNullUUID(remote.ID),
		Visibility:          "private",
	})
	require.NoError(t, err)
	return server.ID
}

// hostedServer creates a toolset-backed server, which serving reads no upstream
// address for.
func (f attachFixture) hostedServer(t *testing.T, ctx context.Context, slug string, userIssuerID uuid.NullUUID) uuid.UUID {
	t.Helper()
	toolset, err := toolsetsrepo.New(f.ti.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID:         f.orgID,
		ProjectID:              f.projectID,
		Name:                   slug,
		Slug:                   slug,
		Description:            pgtype.Text{String: "", Valid: false},
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	server, err := mcpserversrepo.New(f.ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           f.projectID,
		Name:                conv.ToPGText(slug),
		Slug:                conv.ToPGText(slug),
		UserSessionIssuerID: userIssuerID,
		ToolsetID:           conv.ToNullUUID(toolset.ID),
		Visibility:          "private",
	})
	require.NoError(t, err)
	return server.ID
}

func (f attachFixture) client(t *testing.T, ctx context.Context, slug string, userIssuerID uuid.UUID) (uuid.UUID, uuid.UUID) {
	t.Helper()
	return seedActiveClient(t, ctx, f.ti.conn, f.projectID, userIssuerID, f.orgID, slug)
}

// clientOfProvider creates another client of an existing provider, bound to the
// given issuer.
func (f attachFixture) clientOfProvider(t *testing.T, ctx context.Context, slug string, providerID, userIssuerID uuid.UUID) uuid.UUID {
	t.Helper()
	q := repo.New(f.ti.conn)
	client, err := q.CreateRemoteSessionClientFixture(ctx, repo.CreateRemoteSessionClientFixtureParams{
		ProjectID:               conv.ToNullUUID(f.projectID),
		OrganizationID:          conv.ToPGTextEmpty(f.orgID),
		RemoteSessionIssuerID:   providerID,
		ClientID:                "cid-" + slug,
		ClientSecretEncrypted:   pgtype.Text{String: "", Valid: false},
		ClientIDIssuedAt:        conv.ToPGTimestamptz(time.Now()),
		ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		TokenEndpointAuthMethod: conv.ToPGText("client_secret_post"),
		Scope:                   nil,
		Audience:                pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	if userIssuerID != uuid.Nil {
		placeClientForSignInInProject(t, ctx, f.ti.conn, f.projectID, f.orgID, client.ID, userIssuerID)
	}
	return client.ID
}

func (f attachFixture) member(t *testing.T, ctx context.Context, gatewayID, serverID uuid.UUID) {
	t.Helper()
	_, err := metamcprepo.New(f.ti.conn).CreateMetaMCPMember(ctx, metamcprepo.CreateMetaMCPMemberParams{
		ProjectID:       f.projectID,
		MetaMcpServerID: gatewayID,
		McpServerID:     serverID,
		SortOrder:       0,
	})
	require.NoError(t, err)
}

func requireConflict(t *testing.T, err error, message string) {
	t.Helper()
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeConflict, shareable.Code)
	require.ErrorContains(t, err, message)
}

func TestAttachServerClientAssociation_SharedIssuer(t *testing.T) {
	t.Parallel()
	ctx, f := newAttachFixture(t)

	issuer := f.issuer(t, ctx, "attach-shared")
	target := f.serverAt(t, ctx, "attach-shared-a", "https://a.example.com/mcp", issuer)
	sibling := f.serverAt(t, ctx, "attach-shared-b", "https://b.example.com/mcp", issuer)
	client, _ := f.client(t, ctx, "attach-shared", uuid.Nil)
	resync(t, ctx, f.ti.conn, f.orgID, f.projectID, issuer)
	require.Empty(t, associatedClients(t, ctx, f.ti.conn, f.projectID, target), "a shared issuer alone does not configure the target")
	siblingBefore, err := f.ti.service.ListServerClientAssociations(ctx, &gen.ListServerClientAssociationsPayload{McpServerID: sibling.String()})
	require.NoError(t, err)
	require.Empty(t, siblingBefore.RequiredClientIds, "sharing a sign-in does not configure the sibling")

	before, err := audittest.AuditLogCountByAction(ctx, f.ti.conn, audit.ActionRemoteSessionClientAttachMcpServer)
	require.NoError(t, err)

	result, err := f.attach(ctx, target, client)
	require.NoError(t, err)
	require.Equal(t, client.String(), result.Association.RemoteSessionClientID)
	require.Equal(t, "project", result.Association.Scope)
	require.Empty(t, result.BoundUserSessionIssuerIds, "an exact placement does not add an issuer binding")

	require.Equal(t, []uuid.UUID{client}, associatedClients(t, ctx, f.ti.conn, f.projectID, target))
	require.Empty(t, associatedClients(t, ctx, f.ti.conn, f.projectID, sibling), "attach names one server")
	require.Len(t, offeredAuthClients(t, ctx, f.ti.conn, target), 1, "the typed link commits with the association")

	after, err := audittest.AuditLogCountByAction(ctx, f.ti.conn, audit.ActionRemoteSessionClientAttachMcpServer)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
	entry, err := audittest.LatestAuditLogByAction(ctx, f.ti.conn, audit.ActionRemoteSessionClientAttachMcpServer)
	require.NoError(t, err)
	require.Equal(t, client.String(), entry.SubjectID)

	resync(t, ctx, f.ti.conn, f.orgID, f.projectID, issuer)
	require.Equal(t, []uuid.UUID{client}, associatedClients(t, ctx, f.ti.conn, f.projectID, target), "a reconcile keeps an attached client")

	listed, err := f.ti.service.ListServerClientAssociations(ctx, &gen.ListServerClientAssociationsPayload{McpServerID: target.String()})
	require.NoError(t, err)
	require.Equal(t, []string{client.String()}, listed.RequiredClientIds, "the server's callers now need its client")
	siblingAfter, err := f.ti.service.ListServerClientAssociations(ctx, &gen.ListServerClientAssociationsPayload{McpServerID: sibling.String()})
	require.NoError(t, err)
	require.Empty(t, siblingAfter.RequiredClientIds, "attaching on the target does not configure the sibling")

	_, err = f.attach(ctx, target, client)
	require.NoError(t, err, "attaching again is a no-op")
	repeated, err := audittest.AuditLogCountByAction(ctx, f.ti.conn, audit.ActionRemoteSessionClientAttachMcpServer)
	require.NoError(t, err)
	require.Equal(t, after, repeated, "a no-op records nothing")
}

func TestAttachServerClientAssociation_ReattachAfterDetach(t *testing.T) {
	t.Parallel()
	ctx, f := newAttachFixture(t)

	issuer := f.issuer(t, ctx, "attach-again")
	target := f.serverAt(t, ctx, "attach-again-a", "https://a.example.com/mcp", issuer)
	f.serverAt(t, ctx, "attach-again-b", "https://b.example.com/mcp", issuer)
	client, _ := f.client(t, ctx, "attach-again", issuer)

	_, err := f.attach(ctx, target, client)
	require.NoError(t, err)
	require.NoError(t, detachServerClient(ctx, f.ti, target, client))
	require.Empty(t, associatedClients(t, ctx, f.ti.conn, f.projectID, target))
	resync(t, ctx, f.ti.conn, f.orgID, f.projectID, issuer)
	require.Empty(t, associatedClients(t, ctx, f.ti.conn, f.projectID, target), "the explicit detach holds until an attach")

	_, err = f.attach(ctx, target, client)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{client}, associatedClients(t, ctx, f.ti.conn, f.projectID, target))
	require.Len(t, offeredAuthClients(t, ctx, f.ti.conn, target), 1, "the typed link returns after an association detach")
	resync(t, ctx, f.ti.conn, f.orgID, f.projectID, issuer)
	require.Equal(t, []uuid.UUID{client}, associatedClients(t, ctx, f.ti.conn, f.projectID, target))
}

// An administrator's typed detach is durable, so a later attach writes the
// association alone. With enforcement off that doesn't change serving.
func TestAttachServerClientAssociation_KeepsAdministratorTypedDetach(t *testing.T) {
	t.Parallel()
	ctx, f := newAttachFixture(t)

	issuer := f.issuer(t, ctx, "attach-typed")
	target := f.serverAt(t, ctx, "attach-typed-a", "https://a.example.com/mcp", issuer)
	f.serverAt(t, ctx, "attach-typed-b", "https://b.example.com/mcp", issuer)
	client, _ := f.client(t, ctx, "attach-typed", issuer)

	_, err := f.attach(ctx, target, client)
	require.NoError(t, err)
	links := offeredAuthClients(t, ctx, f.ti.conn, target)
	require.Len(t, links, 1)
	_, err = authclientsrepo.New(f.ti.conn).DetachAuthClientFromServer(ctx, authclientsrepo.DetachAuthClientFromServerParams{AuthClientID: links[0], McpServerID: target, OrganizationID: f.orgID})
	require.NoError(t, err)
	require.NoError(t, detachServerClient(ctx, f.ti, target, client))

	_, err = f.attach(ctx, target, client)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{client}, associatedClients(t, ctx, f.ti.conn, f.projectID, target))
	require.Empty(t, offeredAuthClients(t, ctx, f.ti.conn, target))
}

// Under enforcement, detaching a server's own sign-in retires only its typed
// link, and attaching the client again is what brings the link back.
func TestAttachServerClientAssociation_RestoresAnEnforcedDetach(t *testing.T) {
	t.Parallel()
	ctx, f := newAttachFixture(t)
	enforceUpstreamAuth(t, ctx, f.ti.conn, f.orgID)

	issuer := f.issuer(t, ctx, "attach-enforced")
	target := f.serverAt(t, ctx, "attach-enforced", "https://example.com/mcp", issuer)
	client, _ := f.client(t, ctx, "attach-enforced", issuer)

	_, err := f.attach(ctx, target, client)
	require.NoError(t, err)
	links := offeredAuthClients(t, ctx, f.ti.conn, target)
	require.Len(t, links, 1)

	require.NoError(t, detachServerClient(ctx, f.ti, target, client))
	require.Empty(t, associatedClients(t, ctx, f.ti.conn, f.projectID, target))
	require.Empty(t, offeredAuthClients(t, ctx, f.ti.conn, target))

	before, err := audittest.AuditLogCountByAction(ctx, f.ti.conn, audit.ActionAuthClientAttachMcpServer)
	require.NoError(t, err)

	_, err = f.attach(ctx, target, client)
	require.NoError(t, err)
	require.Equal(t, links, offeredAuthClients(t, ctx, f.ti.conn, target))

	after, err := audittest.AuditLogCountByAction(ctx, f.ti.conn, audit.ActionAuthClientAttachMcpServer)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
	entry, err := audittest.LatestAuditLogByAction(ctx, f.ti.conn, audit.ActionAuthClientAttachMcpServer)
	require.NoError(t, err)
	require.Equal(t, links[0].String(), entry.SubjectID)

	resync(t, ctx, f.ti.conn, f.orgID, f.projectID, issuer)
	require.Equal(t, links, offeredAuthClients(t, ctx, f.ti.conn, target), "a reconcile keeps the restored link")

	_, err = f.attach(ctx, target, client)
	require.NoError(t, err)
	repeated, err := audittest.AuditLogCountByAction(ctx, f.ti.conn, audit.ActionAuthClientAttachMcpServer)
	require.NoError(t, err)
	require.Equal(t, after, repeated, "attaching a served client again records nothing")
}

func TestAttachServerClientAssociation_RefreshesTheDerivedProvider(t *testing.T) {
	t.Parallel()
	ctx, f := newAttachFixture(t)

	issuer := f.issuer(t, ctx, "attach-derived")
	target := f.serverAt(t, ctx, "attach-derived-a", "https://a.example.com/mcp", issuer)
	f.serverAt(t, ctx, "attach-derived-b", "https://b.example.com/mcp", issuer)
	clientA, providerA := f.client(t, ctx, "attach-derived-a", issuer)
	f.client(t, ctx, "attach-derived-b", issuer)
	resync(t, ctx, f.ti.conn, f.orgID, f.projectID, issuer)

	derived := func() uuid.NullUUID {
		server, err := mcpserversrepo.New(f.ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: target, ProjectID: f.projectID})
		require.NoError(t, err)
		return server.RemoteSessionIssuerID
	}
	require.False(t, derived().Valid, "two providers and no association leave the provider undecided")

	_, err := f.attach(ctx, target, clientA)
	require.NoError(t, err)
	require.Equal(t, conv.ToNullUUID(providerA), derived(), "the association makes provider A the server's")
}

func TestAttachServerClientAssociation_NotFound(t *testing.T) {
	t.Parallel()
	ctx, f := newAttachFixture(t)

	issuer := f.issuer(t, ctx, "attach-missing")
	server := f.serverAt(t, ctx, "attach-missing", "https://a.example.com/mcp", issuer)
	client, _ := f.client(t, ctx, "attach-missing", issuer)

	for name, ids := range map[string][2]uuid.UUID{
		"unknown server": {uuid.New(), client},
		"unknown client": {server, uuid.New()},
	} {
		_, err := f.attach(ctx, ids[0], ids[1])
		var shareable *oops.ShareableError
		require.ErrorAs(t, err, &shareable, name)
		require.Equal(t, oops.CodeNotFound, shareable.Code, name)
	}
}

func TestAttachServerClientAssociation_RefusesAnotherTenantsResources(t *testing.T) {
	t.Parallel()
	ctx, f := newAttachFixture(t)

	otherOrgID := createOrganization(t, ctx, f.ti.conn, "attach-tenant-other")
	otherProject, err := projectsrepo.New(f.ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "attach-tenant-other",
		Slug:           "attach-tenant-other",
		OrganizationID: otherOrgID,
	})
	require.NoError(t, err)
	foreignIssuer := createUserSessionIssuerInProject(t, ctx, f.ti.conn, otherProject.ID, "attach-tenant-foreign")
	foreignClient, _ := seedActiveClient(t, ctx, f.ti.conn, otherProject.ID, foreignIssuer, otherOrgID, "attach-tenant-foreign")
	foreignServer := seedServerOnIssuer(t, ctx, f.ti.conn, otherProject.ID, foreignIssuer, "attach-tenant-foreign")

	issuer := f.issuer(t, ctx, "attach-tenant")
	server := f.serverAt(t, ctx, "attach-tenant", "https://a.example.com/mcp", issuer)
	client, _ := f.client(t, ctx, "attach-tenant", issuer)

	for name, ids := range map[string][2]uuid.UUID{
		"another tenant's client": {server, foreignClient},
		"another tenant's server": {foreignServer, client},
	} {
		_, err := f.attach(ctx, ids[0], ids[1])
		var shareable *oops.ShareableError
		require.ErrorAs(t, err, &shareable, name)
		require.Equal(t, oops.CodeNotFound, shareable.Code, name)
	}
	require.Empty(t, associatedClients(t, ctx, f.ti.conn, f.projectID, server))
	require.Empty(t, associatedClients(t, ctx, f.ti.conn, otherProject.ID, foreignServer))
}

func TestAttachServerClientAssociation_RequiresWriteOnTheServer(t *testing.T) {
	t.Parallel()
	ctx, f := newAttachFixture(t)

	issuer := f.issuer(t, ctx, "attach-rbac")
	server := f.serverAt(t, ctx, "attach-rbac", "https://a.example.com/mcp", issuer)
	client, _ := f.client(t, ctx, "attach-rbac", issuer)

	readOnly := withExactAccessGrants(t, ctx, f.ti.conn, authz.Grant{
		Scope:    authz.ScopeMCPRead,
		Selector: authz.NewSelector(authz.ScopeMCPRead, server.String()),
	})
	_, err := f.attach(readOnly, server, client)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeForbidden, shareable.Code)
	require.Empty(t, associatedClients(t, ctx, f.ti.conn, f.projectID, server))
}

func TestAttachServerClientAssociation_OffersRegistrationWithoutServersIssuerPair(t *testing.T) {
	t.Parallel()
	ctx, f := newAttachFixture(t)

	own := f.issuer(t, ctx, "attach-direct-own")
	gatewayIssuer := f.issuer(t, ctx, "attach-direct-gateway")
	server := f.serverAt(t, ctx, "attach-direct", "https://a.example.com/mcp", own)
	f.member(t, ctx, createImpactGateway(t, ctx, f.ti, "attach-direct", gatewayIssuer), server)
	client, _ := f.client(t, ctx, "attach-direct", gatewayIssuer)

	_, err := f.attach(ctx, server, client)
	require.NoError(t, err)
	requireNoAttachmentParent(t, ctx, f.ti, client, own)
	require.NotEmpty(t, offeredAuthClients(t, ctx, f.ti.conn, server))
	require.Equal(t, []uuid.UUID{client}, associatedClients(t, ctx, f.ti.conn, f.projectID, server))
}

func TestAttachServerClientAssociation_OffersHostedRegistrationWithoutIssuer(t *testing.T) {
	t.Parallel()
	ctx, f := newAttachFixture(t)

	elsewhere := f.issuer(t, ctx, "attach-unreachable")
	client, _ := f.client(t, ctx, "attach-unreachable", elsewhere)
	server := f.hostedServer(t, ctx, "attach-unreachable", uuid.NullUUID{UUID: uuid.Nil, Valid: false})

	_, err := f.attach(ctx, server, client)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{client}, associatedClients(t, ctx, f.ti.conn, f.projectID, server))
	require.Len(t, offeredAuthClients(t, ctx, f.ti.conn, server), 1)
}

func TestAttachServerClientAssociation_RefusesASecondClient(t *testing.T) {
	t.Parallel()
	ctx, f := newAttachFixture(t)

	issuer := f.issuer(t, ctx, "attach-second")
	server := f.serverAt(t, ctx, "attach-second", "https://a.example.com/mcp", issuer)
	first, _ := f.client(t, ctx, "attach-second-a", issuer)
	second, _ := f.client(t, ctx, "attach-second-b", issuer)

	_, err := f.attach(ctx, server, first)
	require.NoError(t, err)
	_, err = f.attach(ctx, server, second)
	requireConflict(t, err, "detach it")
	require.Equal(t, []uuid.UUID{first}, associatedClients(t, ctx, f.ti.conn, f.projectID, server))
}

func TestAttachServerClientAssociation_RefusesClientAtAnotherAddress(t *testing.T) {
	t.Parallel()
	ctx, f := newAttachFixture(t)

	issuer := f.issuer(t, ctx, "attach-address")
	first := f.serverAt(t, ctx, "attach-address-a", "https://a.example.com/mcp", issuer)
	different := f.serverAt(t, ctx, "attach-address-b", "https://b.example.com/mcp", issuer)
	client, _ := f.client(t, ctx, "attach-address", issuer)
	_, err := f.attach(ctx, first, client)
	require.NoError(t, err)

	_, err = f.attach(ctx, different, client)
	requireConflict(t, err, "different upstream address")
	require.Empty(t, associatedClients(t, ctx, f.ti.conn, f.projectID, different))
}

func TestAttachServerClientAssociation_AllowsClientAtTheSameAddress(t *testing.T) {
	t.Parallel()
	ctx, f := newAttachFixture(t)

	issuer := f.issuer(t, ctx, "attach-same")
	first := f.serverAt(t, ctx, "attach-same-a", "https://a.example.com/mcp", issuer)
	same := f.serverAt(t, ctx, "attach-same-b", "https://a.example.com/mcp/", issuer)
	client, _ := f.client(t, ctx, "attach-same", issuer)
	_, err := f.attach(ctx, first, client)
	require.NoError(t, err)

	_, err = f.attach(ctx, same, client)
	require.NoError(t, err, "token routing ignores a trailing slash")
}

// A self client is one credential, with no person's grant recording a resource.
func TestAttachServerClientAssociation_AllowsSelfClientAtAnotherAddress(t *testing.T) {
	t.Parallel()
	ctx, f := newAttachFixture(t)

	issuer := f.issuer(t, ctx, "attach-self")
	first := f.serverAt(t, ctx, "attach-self-a", "https://a.example.com/mcp", issuer)
	second := f.serverAt(t, ctx, "attach-self-b", "https://b.example.com/mcp", issuer)
	client, _ := f.client(t, ctx, "attach-self", issuer)
	rows, err := testrepo.New(f.ti.conn).ForceRemoteSessionClientCredentialOwnerFixture(ctx, testrepo.ForceRemoteSessionClientCredentialOwnerFixtureParams{
		CredentialOwner: string(remotesessions.CredentialOwnerSelf),
		ID:              client,
		ProjectID:       conv.ToNullUUID(f.projectID),
		OrganizationID:  conv.ToPGText(f.orgID),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)

	_, err = f.attach(ctx, first, client)
	require.NoError(t, err)
	_, err = f.attach(ctx, second, client)
	require.NoError(t, err)
}

// An unqualified hosted registration can be placed on more than one server.
func TestAttachServerClientAssociation_HostedServersShareAClient(t *testing.T) {
	t.Parallel()
	ctx, f := newAttachFixture(t)

	issuer := f.issuer(t, ctx, "attach-hosted")
	first := f.hostedServer(t, ctx, "attach-hosted-a", conv.ToNullUUID(issuer))
	second := f.hostedServer(t, ctx, "attach-hosted-b", conv.ToNullUUID(issuer))
	client, _ := f.client(t, ctx, "attach-hosted", issuer)

	_, err := f.attach(ctx, first, client)
	require.NoError(t, err)
	_, err = f.attach(ctx, second, client)
	require.NoError(t, err)
	require.Len(t, offeredAuthClients(t, ctx, f.ti.conn, first), 1)
	require.Len(t, offeredAuthClients(t, ctx, f.ti.conn, second), 1)

	resync(t, ctx, f.ti.conn, f.orgID, f.projectID, issuer)
	require.Equal(t, []uuid.UUID{client}, associatedClients(t, ctx, f.ti.conn, f.projectID, first))
	require.Equal(t, []uuid.UUID{client}, associatedClients(t, ctx, f.ti.conn, f.projectID, second))
}

func TestHostedServerAssociationDoesNotRequireOrCreateIssuerLink(t *testing.T) {
	t.Parallel()
	ctx, f := newAttachFixture(t)
	issuer := f.issuer(t, ctx, "hosted-exact-unlinked")
	first := f.hostedServer(t, ctx, "hosted-exact-a", conv.ToNullUUID(issuer))
	second := f.hostedServer(t, ctx, "hosted-exact-b", conv.ToNullUUID(issuer))
	client, _ := f.client(t, ctx, "hosted-exact", uuid.Nil)
	q := repo.New(f.ti.conn)
	for _, server := range []uuid.UUID{first, second} {
		result, err := f.attach(ctx, server, client)
		require.NoError(t, err)
		require.Empty(t, result.BoundUserSessionIssuerIds)
	}
	require.NoError(t, detachServerClient(ctx, f.ti, first, client))
	require.Empty(t, associatedClients(t, ctx, f.ti.conn, f.projectID, first))
	require.Equal(t, []uuid.UUID{client}, associatedClients(t, ctx, f.ti.conn, f.projectID, second))
	count, err := q.CountRemoteSessionClientUserSessionIssuerBindings(ctx, repo.CountRemoteSessionClientUserSessionIssuerBindingsParams{RemoteSessionClientID: client, UserSessionIssuerID: issuer})
	require.NoError(t, err)
	require.Zero(t, count)
}

// A member with no issuer of its own reaches a client only through its gateway.
func TestAttachServerClientAssociation_HostedMemberThroughItsGateway(t *testing.T) {
	t.Parallel()
	ctx, f := newAttachFixture(t)

	gatewayIssuer := f.issuer(t, ctx, "attach-hosted-member")
	hosted := f.hostedServer(t, ctx, "attach-hosted-member", uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	f.member(t, ctx, createImpactGateway(t, ctx, f.ti, "attach-hosted-member", gatewayIssuer), hosted)
	client, _ := f.client(t, ctx, "attach-hosted-member", gatewayIssuer)

	result, err := f.attach(ctx, hosted, client)
	require.NoError(t, err)
	require.Empty(t, result.BoundUserSessionIssuerIds)
	resync(t, ctx, f.ti.conn, f.orgID, f.projectID, gatewayIssuer)
	require.Equal(t, []uuid.UUID{client}, associatedClients(t, ctx, f.ti.conn, f.projectID, hosted))
}

func TestAttachServerClientAssociation_DoesNotBindFrontingGateway(t *testing.T) {
	t.Parallel()
	ctx, f := newAttachFixture(t)
	issuer := f.issuer(t, ctx, "attach-mirror-own")
	gatewayIssuer := f.issuer(t, ctx, "attach-mirror-gateway")
	server := f.serverAt(t, ctx, "attach-mirror", "https://a.example.com/mcp", issuer)
	f.member(t, ctx, createImpactGateway(t, ctx, f.ti, "attach-mirror", gatewayIssuer), server)
	client, _ := f.client(t, ctx, "attach-mirror", issuer)
	before, err := audittest.AuditLogCountByAction(ctx, f.ti.conn, audit.ActionRemoteSessionClientAttachUserSessionIssuer)
	require.NoError(t, err)
	result, err := f.attach(ctx, server, client)
	require.NoError(t, err)
	require.Empty(t, result.BoundUserSessionIssuerIds)
	requireClientBound(t, ctx, f.ti, client, gatewayIssuer, false)
	after, err := audittest.AuditLogCountByAction(ctx, f.ti.conn, audit.ActionRemoteSessionClientAttachUserSessionIssuer)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestAttachServerClientAssociation_GatesTheNewClientsRegistrationInPreflight(t *testing.T) {
	t.Parallel()

	// probeTimeout bounds the gate probe, independently of goroutine scheduling.
	const probeTimeout = 100 * time.Millisecond
	ctx, ti := newTestServiceWithConfig(t, testServiceConfig{lockTimeout: probeTimeout})
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	f := attachFixture{ti: ti, projectID: *authCtx.ProjectID, orgID: authCtx.ActiveOrganizationID}

	issuer := f.issuer(t, ctx, "attach-gate")
	server := f.serverAt(t, ctx, "attach-gate", "https://a.example.com/mcp", issuer)
	f.serverAt(t, ctx, "attach-gate-b", "https://b.example.com/mcp", issuer)
	client, _ := f.client(t, ctx, "attach-gate", issuer)

	holder := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, authclientbackfill.New(holder).LockOAuthRegistrationSyncScope(ctx, authclientbackfill.LockOAuthRegistrationSyncScopeParams{
		OrganizationID:  f.orgID,
		RegistrationIds: []uuid.UUID{client},
	}))

	// The server has no association with the client yet, so only the preflight
	// can reach this gate this early; left to the sync it would report the sync.
	_, probeErr := f.attach(ctx, server, client)
	testenv.RequireLockNotAvailable(t, probeErr)
	require.ErrorContains(t, probeErr, "lock mcp server auth client scope", "the new client's gate is taken by the preflight")

	require.NoError(t, holder.Rollback(ctx))
	_, err := f.attach(ctx, server, client)
	require.NoError(t, err)
}

func TestDetachServerClientAssociation_PreservesExistingGatewayPair(t *testing.T) {
	t.Parallel()
	ctx, f := newAttachFixture(t)
	own := f.issuer(t, ctx, "detach-mirror-own")
	gatewayIssuer := f.issuer(t, ctx, "detach-mirror-gateway")
	server := f.serverAt(t, ctx, "detach-mirror", "https://a.example.com/mcp", own)
	f.member(t, ctx, createImpactGateway(t, ctx, f.ti, "detach-mirror", gatewayIssuer), server)
	client, _ := f.client(t, ctx, "detach-mirror", gatewayIssuer)
	seedLiveAgentBinding(t, ctx, f.ti, client, gatewayIssuer)
	_, err := f.attach(ctx, server, client)
	require.NoError(t, err)
	require.NoError(t, detachServerClient(ctx, f.ti, server, client))
	require.Empty(t, offeredAuthClients(t, ctx, f.ti.conn, server))
	requireClientBound(t, ctx, f.ti, client, gatewayIssuer, true)
	requireLiveAgentBindings(t, ctx, f.ti, client, gatewayIssuer)
}

// A client swap changes the offer, not the gateway issuer.
func TestAttachServerClientAssociation_SwapsAClientAfterDetach(t *testing.T) {
	t.Parallel()
	ctx, f := newAttachFixture(t)

	own := f.issuer(t, ctx, "swap-own")
	gatewayIssuer := f.issuer(t, ctx, "swap-gateway")
	server := f.serverAt(t, ctx, "swap", "https://a.example.com/mcp", own)
	f.serverAt(t, ctx, "swap-own-sibling", "https://c.example.com/mcp", own)
	f.member(t, ctx, createImpactGateway(t, ctx, f.ti, "swap", gatewayIssuer), server)
	first, provider := f.client(t, ctx, "swap-first", own)
	second := f.clientOfProvider(t, ctx, "swap-second", provider, own)

	_, err := f.attach(ctx, server, first)
	require.NoError(t, err)
	require.NoError(t, detachServerClient(ctx, f.ti, server, first))

	result, err := f.attach(ctx, server, second)
	require.NoError(t, err)
	require.Empty(t, result.BoundUserSessionIssuerIds)
	require.Equal(t, []uuid.UUID{second}, associatedClients(t, ctx, f.ti.conn, f.projectID, server))
}

// A server with no issuer of its own reaches a client only through its gateway,
// so that binding is what an attach needs to undo the detach.
func TestDetachServerClientAssociation_KeepsTheOnlyBindingReachingTheServer(t *testing.T) {
	t.Parallel()
	ctx, f := newAttachFixture(t)

	gatewayIssuer := f.issuer(t, ctx, "release-only")
	hosted := f.hostedServer(t, ctx, "release-only", uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	f.member(t, ctx, createImpactGateway(t, ctx, f.ti, "release-only", gatewayIssuer), hosted)
	client, _ := f.client(t, ctx, "release-only", gatewayIssuer)

	_, err := f.attach(ctx, hosted, client)
	require.NoError(t, err)
	require.NoError(t, detachServerClient(ctx, f.ti, hosted, client))
	requireClientBound(t, ctx, f.ti, client, gatewayIssuer, true)

	_, err = f.attach(ctx, hosted, client)
	require.NoError(t, err, "the detach has an undo")
}

// An organization-level issuer's bindings are shared by every project's servers.
func TestDetachServerClientAssociation_KeepsAnOrganizationIssuersBinding(t *testing.T) {
	t.Parallel()
	ctx, f := newAttachFixture(t)

	own := f.issuer(t, ctx, "release-org-own")
	organizationIssuer := seedOrganizationTierUserSessionIssuer(t, ctx, f.ti.conn, "release-org")
	server := f.serverAt(t, ctx, "release-org", "https://a.example.com/mcp", own)
	f.serverAt(t, ctx, "release-org-sibling", "https://c.example.com/mcp", own)
	f.member(t, ctx, createImpactGateway(t, ctx, f.ti, "release-org", organizationIssuer), server)
	client, _ := f.client(t, ctx, "release-org", own)
	other := placeClientForSignIn(t, ctx, f.ti.conn, client, organizationIssuer)

	_, err := f.attach(ctx, server, client)
	require.NoError(t, err)
	require.NoError(t, detachServerClient(ctx, f.ti, server, client))
	require.Empty(t, associatedClients(t, ctx, f.ti.conn, f.projectID, server))
	require.Equal(t, []uuid.UUID{client}, associatedClients(t, ctx, f.ti.conn, f.projectID, other))
}

// A captured association can predate the rules a new attach follows.
func TestAttachServerClientAssociation_AgainRefusesNothingAnExistingAssociationAlreadyIs(t *testing.T) {
	t.Parallel()
	ctx, f := newAttachFixture(t)

	own := f.issuer(t, ctx, "again-own")
	boundGateway := f.issuer(t, ctx, "again-bound")
	heldGateway := f.issuer(t, ctx, "again-held")
	openGateway := f.issuer(t, ctx, "again-open")
	server := f.serverAt(t, ctx, "again", "https://a.example.com/mcp", own)
	f.member(t, ctx, createImpactGateway(t, ctx, f.ti, "again-bound", boundGateway), server)
	f.member(t, ctx, createImpactGateway(t, ctx, f.ti, "again-held", heldGateway), server)
	f.member(t, ctx, createImpactGateway(t, ctx, f.ti, "again-open", openGateway), server)
	client, provider := f.client(t, ctx, "again", boundGateway)
	holder := f.clientOfProvider(t, ctx, "again-holder", provider, heldGateway)
	associate(t, ctx, f.ti.conn, f.orgID, f.projectID, server, client)

	result, err := f.attach(ctx, server, client)
	require.NoError(t, err, "the client's missing own-issuer binding doesn't refuse an association that already stands")
	require.Empty(t, result.BoundUserSessionIssuerIds)
	requireClientBound(t, ctx, f.ti, client, openGateway, false)
	requireClientBound(t, ctx, f.ti, client, heldGateway, false)
	requireClientBound(t, ctx, f.ti, holder, heldGateway, true)
}
