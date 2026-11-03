package agentmanagement

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	srv "github.com/speakeasy-api/gram/server/gen/http/agents/server"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// seedPersonalConnection gives userID a live connection through a new
// upstream OAuth client that the organization offers as a personal client.
func seedPersonalConnection(t *testing.T, conn *pgxpool.Pool, organizationID, userID string) uuid.UUID {
	t.Helper()
	_, issuerID := seedManagedSession(t, conn, organizationID, "user:"+userID)
	upstream := seedManagedUpstreamWith(t, conn, organizationID, issuerID, "user:"+userID, nil)
	_, err := authclientbackfill.New(conn).SeedAuthClientFixture(t.Context(), authclientbackfill.SeedAuthClientFixtureParams{
		OrganizationID:        organizationID,
		ProjectID:             uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ClientType:            authz.AuthClientTypeRemoteSession,
		Source:                "admin",
		RemoteSessionClientID: uuid.NullUUID{UUID: upstream.RemoteSessionClientID, Valid: true},
	})
	require.NoError(t, err)
	return upstream.RemoteSessionClientID
}

func agentGrantRows(t *testing.T, conn *pgxpool.Pool, organizationID string, agentID uuid.UUID) []accessrepo.ListPrincipalGrantsByOrgRow {
	t.Helper()
	rows, err := accessrepo.New(conn).ListPrincipalGrantsByOrg(t.Context(), accessrepo.ListPrincipalGrantsByOrgParams{
		OrganizationID: organizationID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String()).String(),
	})
	require.NoError(t, err)
	return rows
}

func requireCode(t *testing.T, err error, code oops.Code, msg string) {
	t.Helper()
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable, msg)
	require.Equal(t, code, shareable.Code, msg)
}

func TestConnectionOwnerBlocksAgentWithoutManagingIt(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	const org = "org-restrict"
	seedOrganization(t, conn, org)
	for _, user := range []string{"owner", "member", "other"} {
		seedOrganizationUser(t, conn, org, user)
	}
	agent := createAgent(t, conn, org, "owner", "Restricted agent")
	upstream := seedPersonalConnection(t, conn, org, "member")
	// Nobody holds agent management grants: the connection owner needs none.
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	member := validatedHumanContext(t, org, "member")

	restriction, err := service.BlockAgentOnConnection(member, &gen.BlockAgentOnConnectionPayload{AgentID: agent.ID.String(), RemoteSessionClientID: upstream.String()})
	require.NoError(t, err)
	require.Equal(t, agent.ID.String(), restriction.AgentID)

	again, err := service.BlockAgentOnConnection(member, &gen.BlockAgentOnConnectionPayload{AgentID: agent.ID.String(), RemoteSessionClientID: upstream.String()})
	require.NoError(t, err)
	require.Equal(t, restriction.ID, again.ID, "blocking twice is the same restriction")

	rows := agentGrantRows(t, conn, org, agent.ID)
	require.Len(t, rows, 1)
	require.Equal(t, string(authz.ScopeMCPBlockedAuth), rows[0].Scope)
	selector, err := authz.SelectorFromRow(rows[0].Selectors)
	require.NoError(t, err)
	require.Equal(t, connectionBlockSelector(upstream, "member"), selector, "the server fixes the logical connection and a concrete owner")

	listed, err := service.ListConnectionBlocks(member, &gen.ListConnectionBlocksPayload{})
	require.NoError(t, err)
	require.Len(t, listed.Items, 1)
	otherListed, err := service.ListConnectionBlocks(validatedHumanContext(t, org, "other"), &gen.ListConnectionBlocksPayload{})
	require.NoError(t, err)
	require.Empty(t, otherListed.Items, "owners see only their own restrictions")

	manager := validatedHumanContext(t, org, "owner")
	grants, err := service.ListPolicyGrants(manager, &gen.ListPolicyGrantsPayload{AgentID: agent.ID.String()})
	require.NoError(t, err)
	require.Len(t, grants, 1)
	requireCode(t, service.DeletePolicyGrant(manager, &gen.DeletePolicyGrantPayload{AgentID: agent.ID.String(), GrantID: grants[0].ID}), oops.CodeConflict, "an agent manager cannot remove the owner's restriction")
	_, err = service.UpdatePolicyGrant(manager, &gen.UpdatePolicyGrantPayload{
		AgentID: agent.ID.String(), GrantID: grants[0].ID, Scope: string(authz.ScopeMCPBlockedAuth), Effect: "allow",
		Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindMCPAuthClient, ResourceID: "*"},
	})
	requireCode(t, err, oops.CodeConflict, "an agent manager cannot rewrite the owner's restriction")

	requireCode(t, service.RemoveConnectionBlock(validatedHumanContext(t, org, "other"), &gen.RemoveConnectionBlockPayload{BlockID: restriction.ID}), oops.CodeNotFound, "another user cannot remove it")
	require.NoError(t, service.RemoveConnectionBlock(member, &gen.RemoveConnectionBlockPayload{BlockID: restriction.ID}))
	require.Empty(t, agentGrantRows(t, conn, org, agent.ID))

	var actions []string
	for _, row := range auditLogs(t, conn, org, agent.ID.String()) {
		actions = append(actions, row.Action)
		require.Equal(t, "member", row.ActorID)
	}
	require.Equal(t, []string{"agent:connection_block_create", "agent:connection_block_delete"}, actions)
}

func TestConnectionOwnerBlockRequiresTheCallersOwnConnection(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	const org = "org-restrict-own"
	seedOrganization(t, conn, org)
	seedOrganization(t, conn, "org-foreign")
	for _, user := range []string{"owner", "member", "other"} {
		seedOrganizationUser(t, conn, org, user)
	}
	seedOrganizationUser(t, conn, "org-foreign", "owner")
	agent := createAgent(t, conn, org, "owner", "Agent")
	foreignAgent := createAgent(t, conn, "org-foreign", "owner", "Foreign agent")
	membersConnection := seedPersonalConnection(t, conn, org, "member")
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})

	restrict := func(ctx context.Context, agentID, upstream uuid.UUID) error {
		_, err := service.BlockAgentOnConnection(ctx, &gen.BlockAgentOnConnectionPayload{AgentID: agentID.String(), RemoteSessionClientID: upstream.String()})
		return err
	}
	requireCode(t, restrict(validatedHumanContext(t, org, "other"), agent.ID, membersConnection), oops.CodeNotFound, "another user's connection is not yours to restrict")
	requireCode(t, restrict(validatedHumanContext(t, org, "member"), agent.ID, uuid.New()), oops.CodeNotFound, "an unknown connection")
	requireCode(t, restrict(validatedHumanContext(t, org, "member"), foreignAgent.ID, membersConnection), oops.CodeNotFound, "an agent in another organization")

	apiKey := validatedHumanContext(t, org, "member")
	authCtx, ok := contextvalues.GetAuthContext(apiKey)
	require.True(t, ok)
	authCtx.APIKeyID = "key"
	requireCode(t, restrict(apiKey, agent.ID, membersConnection), oops.CodeForbidden, "only an ordinary human session can restrict")

	require.Empty(t, agentGrantRows(t, conn, org, agent.ID))
}

func TestConnectionOwnerBlockKeepsAManagersIdenticalGrant(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	const org = "org-restrict-shared"
	seedOrganization(t, conn, org)
	for _, user := range []string{"owner", "member"} {
		seedOrganizationUser(t, conn, org, user)
	}
	agent := createAgent(t, conn, org, "owner", "Agent")
	upstream := seedPersonalConnection(t, conn, org, "member")
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})

	// The agent's owner restricts it with no containment check: a block only
	// withdraws access.
	managed, err := service.CreatePolicyGrant(validatedHumanContext(t, org, "owner"), &gen.CreatePolicyGrantPayload{
		AgentID: agent.ID.String(), Scope: string(authz.ScopeMCPBlockedAuth), Effect: "allow",
		Selector: &gen.AgentPolicySelector{
			ResourceKind: authz.ResourceKindMCPAuthClient, ResourceID: "*", ClientType: new(authz.AuthClientTypeRemoteSession),
			UpstreamClient: new(upstream.String()), ConnectionOwner: new("member"),
		},
	})
	require.NoError(t, err)
	require.Equal(t, "member", *managed.Selector.ConnectionOwner)

	member := validatedHumanContext(t, org, "member")
	restriction, err := service.BlockAgentOnConnection(member, &gen.BlockAgentOnConnectionPayload{AgentID: agent.ID.String(), RemoteSessionClientID: upstream.String()})
	require.NoError(t, err)
	require.NoError(t, service.RemoveConnectionBlock(member, &gen.RemoveConnectionBlockPayload{BlockID: restriction.ID}))

	rows := agentGrantRows(t, conn, org, agent.ID)
	require.True(t, slices.ContainsFunc(rows, func(row accessrepo.ListPrincipalGrantsByOrgRow) bool { return row.ID.String() == managed.ID }),
		"the owner removes only their own authority, never the manager's grant")
}

func TestConnectionBlockRequestsRejectUnknownFields(t *testing.T) {
	t.Parallel()

	mux := goahttp.NewMuxer()
	block := srv.DecodeBlockAgentOnConnectionRequest(mux, requestDecoder)
	remove := srv.DecodeRemoveConnectionBlockRequest(mux, requestDecoder)
	request := func(path, body string) *http.Request {
		return httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, strings.NewReader(body))
	}
	ids := `"agent_id":"` + uuid.NewString() + `","remote_session_client_id":"` + uuid.NewString() + `"`

	_, err := block(request(srv.BlockAgentOnConnectionAgentsPath(), "{"+ids+"}"))
	require.NoError(t, err)
	for _, extra := range []string{
		`"scope":"mcp:auth"`,
		`"principal_urn":"role:admin"`,
		`"selectors":{"client_type":"*"}`,
		`"connection_owner":"$represented_user"`,
	} {
		_, err := block(request(srv.BlockAgentOnConnectionAgentsPath(), "{"+ids+","+extra+"}"))
		require.Error(t, err, extra)
	}

	xmlRequest := request(srv.BlockAgentOnConnectionAgentsPath(), "<payload/>")
	xmlRequest.Header.Set("Content-Type", "application/xml")
	_, err = block(xmlRequest)
	require.Error(t, err, "only JSON bodies are accepted")

	_, err = remove(request(srv.RemoveConnectionBlockAgentsPath(), `{"block_id":"`+uuid.NewString()+`"}`))
	require.NoError(t, err)
	_, err = remove(request(srv.RemoveConnectionBlockAgentsPath(), `{"block_id":"`+uuid.NewString()+`","scope":"mcp:auth"}`))
	require.Error(t, err)
}

func TestConnectionOwnerBlockIgnoresAnotherOrganizationsConsent(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	const org = "org-block-consent"
	seedOrganization(t, conn, org)
	seedOrganization(t, conn, "org-block-consent-foreign")
	for _, o := range []string{org, "org-block-consent-foreign"} {
		seedOrganizationUser(t, conn, o, "member")
	}
	seedOrganizationUser(t, conn, org, "owner")
	agent := createAgent(t, conn, org, "owner", "Agent")

	// The member consented through the other organization's sign-in, so the
	// connection is not theirs in this organization.
	_, foreignIssuer := seedManagedSession(t, conn, "org-block-consent-foreign", "user:member")
	upstream := seedManagedUpstreamWith(t, conn, org, foreignIssuer, "user:member", nil)
	_, err := authclientbackfill.New(conn).SeedAuthClientFixture(t.Context(), authclientbackfill.SeedAuthClientFixtureParams{
		OrganizationID:        org,
		ProjectID:             uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ClientType:            authz.AuthClientTypeRemoteSession,
		Source:                "admin",
		RemoteSessionClientID: uuid.NullUUID{UUID: upstream.RemoteSessionClientID, Valid: true},
	})
	require.NoError(t, err)

	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	_, err = service.BlockAgentOnConnection(validatedHumanContext(t, org, "member"), &gen.BlockAgentOnConnectionPayload{
		AgentID: agent.ID.String(), RemoteSessionClientID: upstream.RemoteSessionClientID.String(),
	})
	requireCode(t, err, oops.CodeNotFound, "a connection consented in another organization")
	require.Empty(t, agentGrantRows(t, conn, org, agent.ID))
}

func TestConnectionOwnerBlockRepairsAndOutlivesTheAgent(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	const org = "org-block-lifecycle"
	seedOrganization(t, conn, org)
	for _, user := range []string{"owner", "member"} {
		seedOrganizationUser(t, conn, org, user)
	}
	agent := createAgent(t, conn, org, "owner", "Agent")
	upstream := seedPersonalConnection(t, conn, org, "member")
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	member := validatedHumanContext(t, org, "member")
	payload := &gen.BlockAgentOnConnectionPayload{AgentID: agent.ID.String(), RemoteSessionClientID: upstream.String()}

	block, err := service.BlockAgentOnConnection(member, payload)
	require.NoError(t, err)
	selector, err := connectionBlockSelector(upstream, "member").MarshalJSON()
	require.NoError(t, err)
	require.NoError(t, repo.New(conn).DeleteConnectionBlockGrant(t.Context(), repo.DeleteConnectionBlockGrantParams{
		OrganizationID: org, AgentID: agent.ID, Selectors: selector,
	}))
	again, err := service.BlockAgentOnConnection(member, payload)
	require.NoError(t, err)
	require.Equal(t, block.ID, again.ID)
	require.Len(t, agentGrantRows(t, conn, org, agent.ID), 1, "repeating the block restores its grant")

	_, err = repo.New(conn).DeleteAgent(t.Context(), repo.DeleteAgentParams{OrganizationID: org, ID: agent.ID})
	require.NoError(t, err)
	require.NoError(t, service.RemoveConnectionBlock(member, &gen.RemoveConnectionBlockPayload{BlockID: block.ID}), "the owner can remove a block on a deleted agent")
	require.Empty(t, agentGrantRows(t, conn, org, agent.ID))
}

func TestListConnectionBlocksPages(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	const org = "org-block-pages"
	seedOrganization(t, conn, org)
	for _, user := range []string{"owner", "member"} {
		seedOrganizationUser(t, conn, org, user)
	}
	upstream := seedPersonalConnection(t, conn, org, "member")
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	member := validatedHumanContext(t, org, "member")
	var want []string
	for _, name := range []string{"First", "Second", "Third"} {
		agent := createAgent(t, conn, org, "owner", name)
		block, err := service.BlockAgentOnConnection(member, &gen.BlockAgentOnConnectionPayload{AgentID: agent.ID.String(), RemoteSessionClientID: upstream.String()})
		require.NoError(t, err)
		want = append(want, block.ID)
	}

	var got []string
	var cursor *string
	for range 3 {
		page, err := service.ListConnectionBlocks(member, &gen.ListConnectionBlocksPayload{Cursor: cursor, Limit: 2})
		require.NoError(t, err)
		for _, item := range page.Items {
			got = append(got, item.ID)
		}
		if page.NextCursor == nil {
			break
		}
		cursor = page.NextCursor
	}
	require.ElementsMatch(t, want, got)
	require.Len(t, got, 3)
}
