package usersessions_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/gateway_sessions"
	oldgen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	endpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metarepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func seedSessionGateway(t *testing.T, ctx context.Context, ti *testInstance, issuer uuid.UUID) uuid.UUID {
	t.Helper()
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	gateway, err := metarepo.New(ti.conn).CreateMetaMCPServer(ctx, metarepo.CreateMetaMCPServerParams{
		OrganizationID: auth.ActiveOrganizationID, ProjectID: *auth.ProjectID, Name: "Example gateway", UserSessionIssuerID: conv.ToNullUUID(issuer), Visibility: "private",
	})
	require.NoError(t, err)
	_, err = endpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, endpointsrepo.CreateMCPEndpointParams{ProjectID: *auth.ProjectID, MetaMcpServerID: conv.ToNullUUID(gateway.ID), Slug: "example-gateway"})
	require.NoError(t, err)
	return gateway.ID
}

func TestGatewaySessionsOrganizationProjectIndependence(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuer := seedOrganizationTierIssuer(t, ctx, ti.conn, "gateway-workforce")
	subject := urn.NewUserSubject("example-user")
	_, err := seedUserSession(t, ctx, ti.conn, issuer, subject)
	require.NoError(t, err)
	sibling := createSiblingProject(t, ctx, ti.conn, "other-project")
	seedUpstream(t, ctx, ti.conn, conv.ToNullUUID(*auth.ProjectID), issuer, subject, "first.example.test")
	seedUpstream(t, ctx, ti.conn, conv.ToNullUUID(sibling), issuer, subject, "second.example.test")
	old, err := ti.service.ListUserSessions(ctx, &oldgen.ListUserSessionsPayload{UserSessionIssuerID: new(issuer.String())})
	require.NoError(t, err)
	require.Len(t, old.Items, 1)
	require.Len(t, old.Items[0].Upstreams, 1, "project API keeps its access boundary")
	orgCtx := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeOrgRead, auth.ActiveOrganizationID))
	for _, project := range []*uuid.UUID{auth.ProjectID, &sibling, nil} {
		changed := *auth
		changed.ProjectID = project
		// Preserve the exact-grant context while varying the dashboard selection.
		grantsAuth, ok := contextvalues.GetAuthContext(orgCtx)
		require.True(t, ok)
		changed = *grantsAuth
		changed.ProjectID = project
		selectedCtx := contextvalues.SetAuthContext(orgCtx, &changed)
		got, err := ti.service.ListGatewaySessions(selectedCtx, &gen.ListGatewaySessionsPayload{})
		require.NoError(t, err)
		require.Len(t, got.Items, 1)
		require.Len(t, got.Items[0].Upstreams, 2)
	}
	deniedCtx := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeProjectRead, auth.ProjectID.String()))
	_, err = ti.service.ListGatewaySessions(deniedCtx, &gen.ListGatewaySessionsPayload{})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestGatewaySessionsEndpointFiltersAndPagination(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuer := seedIssuerInProject(t, ctx, ti.conn, *auth.ProjectID, "gateway-issuer")
	gateway := seedSessionGateway(t, ctx, ti, issuer)
	now := time.Now().UTC().Truncate(time.Second)
	var ids []string
	for i, name := range []string{"Ada", "Bea", "Cleo", "Different endpoint", "Expired", "Trailing slash"} {
		row, err := seedUserSession(t, ctx, ti.conn, issuer, urn.NewUserSubject(name))
		require.NoError(t, err)
		resource := pgtype.Text{}
		if i == 2 {
			resource = conv.ToPGText(testServerURL + "/mcp/example-gateway")
		}
		if i == 5 {
			resource = conv.ToPGText(testServerURL + "/mcp/example-gateway/")
		}
		if i == 3 {
			resource = conv.ToPGText(testServerURL + "/mcp/another-gateway")
		}
		expiry := now.Add(time.Duration(i+1) * time.Hour)
		if i == 4 {
			expiry = now.Add(-time.Hour)
		}
		err = testrepo.New(ti.conn).SetGatewaySessionListFixture(ctx, testrepo.SetGatewaySessionListFixtureParams{
			ID: row.ID, OrganizationID: conv.ToPGText(auth.ActiveOrganizationID), Resource: resource,
			CreatedAt:        pgtype.Timestamptz{Time: now, Valid: true},
			LastUsedAt:       pgtype.Timestamptz{Time: now.Add(time.Duration(i) * time.Minute), Valid: i != 1},
			RefreshExpiresAt: pgtype.Timestamptz{Time: expiry, Valid: true},
		})
		require.NoError(t, err)
		ids = append(ids, row.ID.String())
	}
	request := gen.ListGatewaySessionsPayload{GatewayID: new(gateway.String()), Limit: 1}
	var found []string
	for {
		result, err := ti.service.ListGatewaySessions(ctx, &request)
		require.NoError(t, err)
		require.Len(t, result.Items, 1)
		found = append(found, result.Items[0].ID)
		if result.NextCursor == nil {
			break
		}
		request.Cursor = result.NextCursor
	}
	expected := slices.Clone(ids[:3])
	slices.Sort(expected)
	slices.Reverse(expected)
	require.Equal(t, expected, found, "equal timestamps use a stable UUID tie-breaker; other endpoints and expired sessions are excluded")
	request.Cursor = nil
	request.Sort = "oldest"
	oldest, err := ti.service.ListGatewaySessions(ctx, &request)
	require.NoError(t, err)
	require.Equal(t, expected[len(expected)-1], oldest.Items[0].ID)
	request.Cursor = oldest.NextCursor
	request.Search = new("Cleo")
	_, err = ti.service.ListGatewaySessions(ctx, &request)
	requireOopsCode(t, err, oops.CodeBadRequest)
	request.Cursor = nil
	filtered, err := ti.service.ListGatewaySessions(ctx, &request)
	require.NoError(t, err)
	require.Equal(t, ids[2], filtered.Items[0].ID)
	require.Nil(t, filtered.NextCursor)
	request.Search = new(strings.Repeat("é", 201))
	_, err = ti.service.ListGatewaySessions(ctx, &request)
	requireOopsCode(t, err, oops.CodeBadRequest)
	request.Search = new("%")
	literal, err := ti.service.ListGatewaySessions(ctx, &request)
	require.NoError(t, err)
	require.Empty(t, literal.Items)
	request.Search = nil
	request.Sort = "last_used"
	request.Limit = 10
	used, err := ti.service.ListGatewaySessions(ctx, &request)
	require.NoError(t, err)
	require.Equal(t, []string{ids[2], ids[0], ids[1]}, []string{used.Items[0].ID, used.Items[1].ID, used.Items[2].ID})
	request.Sort = "expiry"
	expiring, err := ti.service.ListGatewaySessions(ctx, &request)
	require.NoError(t, err)
	require.Equal(t, ids[0], expiring.Items[0].ID)
	request.Status = "expired"
	expired, err := ti.service.ListGatewaySessions(ctx, &request)
	require.NoError(t, err)
	require.Len(t, expired.Items, 1)
	require.Equal(t, ids[4], expired.Items[0].ID)
	request.CreatedAfter = new(now.Add(time.Second).Format(time.RFC3339))
	none, err := ti.service.ListGatewaySessions(ctx, &request)
	require.NoError(t, err)
	require.Empty(t, none.Items)
	denied := withExactAuthzGrants(t, ctx, ti.conn)
	_, err = ti.service.ListGatewaySessions(denied, &gen.ListGatewaySessionsPayload{GatewayID: new(gateway.String())})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.ListGatewaySessions(ctx, &gen.ListGatewaySessionsPayload{GatewayID: new(uuid.NewString())})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGatewaySessionAssociationFilters(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuer := seedIssuerInProject(t, ctx, ti.conn, *auth.ProjectID, "associations")
	gateway := seedSessionGateway(t, ctx, ti, issuer)
	subject := urn.NewUserSubject("associated-user")
	session, err := seedUserSession(t, ctx, ti.conn, issuer, subject)
	require.NoError(t, err)
	_, err = seedUserSession(t, ctx, ti.conn, issuer, urn.NewUserSubject("no-accounts"))
	require.NoError(t, err)
	client := seedUpstream(t, ctx, ti.conn, conv.ToNullUUID(*auth.ProjectID), issuer, subject, "provider.example.test")
	toolset := attachIssuerToolset(t, ctx, ti, *auth.ProjectID, issuer, "associated-toolset")
	member, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: *auth.ProjectID, ToolsetID: conv.ToNullUUID(toolset.ID), Name: conv.ToPGText("Example member"), Slug: conv.ToPGText("example-member"), Visibility: "private",
	})
	require.NoError(t, err)
	_, err = metarepo.New(ti.conn).CreateMetaMCPMember(ctx, metarepo.CreateMetaMCPMemberParams{ProjectID: *auth.ProjectID, MetaMcpServerID: gateway, McpServerID: member.ID})
	require.NoError(t, err)
	require.NoError(t, testrepo.New(ti.conn).InsertMCPServerRemoteSessionClientFixture(ctx, testrepo.InsertMCPServerRemoteSessionClientFixtureParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, McpServerID: member.ID, RemoteSessionClientID: client}))
	facets, err := ti.service.ListGatewaySessionFacets(ctx, &gen.ListGatewaySessionFacetsPayload{GatewayID: new(gateway.String())})
	require.NoError(t, err)
	require.Len(t, facets.Servers, 1)
	require.Len(t, facets.Providers, 1)
	result, err := ti.service.ListGatewaySessions(ctx, &gen.ListGatewaySessionsPayload{GatewayID: new(gateway.String()), ServerID: new(member.ID.String()), ProviderID: new(facets.Providers[0].Value)})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, session.ID.String(), result.Items[0].ID)
	require.Len(t, result.Items[0].Upstreams, 1)
	empty, err := ti.service.ListGatewaySessions(ctx, &gen.ListGatewaySessionsPayload{GatewayID: new(gateway.String()), ServerID: new(uuid.NewString())})
	require.NoError(t, err)
	require.Empty(t, empty.Items)
}

func TestGatewaySessionsAlternatePlatformHosts(t *testing.T) {
	t.Parallel()
	hosts := testHosts
	hosts.PlatformHosts = map[string]string{"alternate.example.test": "https://alternate.example.test"}
	ctx, ti := newTestServiceWithHosts(t, hosts, nil)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	org, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, auth.ActiveOrganizationID)
	require.NoError(t, err)
	for _, organization := range []bool{false, true} {
		t.Run(map[bool]string{false: "manual", true: "organization"}[organization], func(t *testing.T) {
			t.Parallel()
			var issuer uuid.UUID
			payload := &gen.ListGatewaySessionsPayload{}
			path := "/mcp/example-gateway"
			if organization {
				issuer = seedOrganizationTierIssuer(t, ctx, ti.conn, "alternate-org")
				path = "/mcp/gateway/" + org.Slug
			} else {
				issuer = seedIssuerInProject(t, ctx, ti.conn, *auth.ProjectID, "alternate-manual")
				gateway := seedSessionGateway(t, ctx, ti, issuer)
				payload.GatewayID = new(gateway.String())
			}
			var expected []string
			for i, base := range []string{testServerURL, "https://alternate.example.test", "https://unserved.example.test"} {
				row, err := seedUserSession(t, ctx, ti.conn, issuer, urn.NewUserSubject(uuid.NewString()))
				require.NoError(t, err)
				require.NoError(t, testrepo.New(ti.conn).SetGatewaySessionListFixture(ctx, testrepo.SetGatewaySessionListFixtureParams{
					ID: row.ID, OrganizationID: conv.ToPGText(auth.ActiveOrganizationID), Resource: conv.ToPGText(base + path),
					CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}, RefreshExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
				}))
				if i < 2 {
					expected = append(expected, row.ID.String())
				}
			}
			result, err := ti.service.ListGatewaySessions(ctx, payload)
			require.NoError(t, err)
			var actual []string
			for _, session := range result.Items {
				actual = append(actual, session.ID)
			}
			require.ElementsMatch(t, expected, actual)
		})
	}
}

func TestGatewaySessionsHideOtherOrganizationClientMetadata(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	issuer := seedOrganizationTierIssuer(t, ctx, ti.conn, "client-metadata")
	otherIssuer := seedOtherOrganizationIssuer(t, ctx, ti)
	client, err := seedCimdUserSessionClient(t, ctx, ti.conn, otherIssuer, "https://client.example.test/metadata.json")
	require.NoError(t, err)
	session, err := seedUserSessionForClient(t, ctx, ti.conn, issuer, client.ID, urn.NewUserSubject("client-metadata-user"))
	require.NoError(t, err)

	result, err := ti.service.ListGatewaySessions(ctx, &gen.ListGatewaySessionsPayload{})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	item := result.Items[0]
	require.Equal(t, session.ID.String(), item.ID)
	require.Nil(t, item.ClientName)
	require.Nil(t, item.ClientIDMetadataURI)
	require.Nil(t, item.ClientTokenEndpointAuthMethod)
}

func TestGatewaySessionsHideOtherOrganizationAPIKeyMetadata(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuer := seedOrganizationTierIssuer(t, ctx, ti.conn, "key-metadata")
	otherIssuer := seedOtherOrganizationIssuer(t, ctx, ti)
	otherClient, err := seedUserSessionClient(t, ctx, ti.conn, otherIssuer, "other-client")
	require.NoError(t, err)
	key, err := keysrepo.New(ti.conn).CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{
		OrganizationID:  otherClient.OrganizationID.String,
		CreatedByUserID: auth.UserID,
		Name:            "Other organization credential",
		KeyPrefix:       "test", KeyHash: uuid.NewString(), Scopes: []string{},
	})
	require.NoError(t, err)
	session, err := seedUserSession(t, ctx, ti.conn, issuer, urn.NewAPIKeySubject(key.ID))
	require.NoError(t, err)

	result, err := ti.service.ListGatewaySessions(ctx, &gen.ListGatewaySessionsPayload{})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, session.ID.String(), result.Items[0].ID)
	require.Nil(t, result.Items[0].SubjectDisplayName)
	filtered, err := ti.service.ListGatewaySessions(ctx, &gen.ListGatewaySessionsPayload{Search: new(key.Name)})
	require.NoError(t, err)
	require.Empty(t, filtered.Items)
}

func TestGatewaySessionsHideDeletedUserMetadata(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	issuer := seedOrganizationTierIssuer(t, ctx, ti.conn, "deleted-user-metadata")
	user, err := usersrepo.New(ti.conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID: "user-" + uuid.NewString(), Email: "deleted-user@example.test", DisplayName: "Deleted user label",
		PhotoUrl: conv.ToPGText("https://profile.example.test/photo.png"),
	})
	require.NoError(t, err)
	session, err := seedUserSession(t, ctx, ti.conn, issuer, urn.NewUserSubject(user.ID))
	require.NoError(t, err)

	before, err := ti.service.ListGatewaySessions(ctx, &gen.ListGatewaySessionsPayload{Search: new(user.Email)})
	require.NoError(t, err)
	require.Len(t, before.Items, 1)
	require.Equal(t, new(user.DisplayName), before.Items[0].SubjectDisplayName)
	require.Equal(t, new(user.PhotoUrl.String), before.Items[0].SubjectPhotoURL)
	require.NoError(t, testrepo.New(ti.conn).ForceSoftDeleteUser(ctx, user.ID))

	result, err := ti.service.ListGatewaySessions(ctx, &gen.ListGatewaySessionsPayload{})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, session.ID.String(), result.Items[0].ID)
	require.Nil(t, result.Items[0].SubjectDisplayName)
	require.Nil(t, result.Items[0].SubjectPhotoURL)
	for _, search := range []string{user.DisplayName, user.Email} {
		filtered, err := ti.service.ListGatewaySessions(ctx, &gen.ListGatewaySessionsPayload{Search: new(search)})
		require.NoError(t, err)
		require.Empty(t, filtered.Items)
	}
}
