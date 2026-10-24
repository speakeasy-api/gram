package usersessions_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	platformusersessions "github.com/speakeasy-api/gram/server/internal/platformtools/usersessions"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func searchSessionIDs(t *testing.T, ctx context.Context, ti *testInstance, payload *gen.ListUserSessionsPayload) []string {
	t.Helper()

	got, err := ti.service.ListUserSessions(ctx, payload)
	require.NoError(t, err)
	ids := make([]string, 0, len(got.Items))
	for _, item := range got.Items {
		ids = append(ids, item.ID)
	}
	return ids
}

func seedNamedUser(t *testing.T, ctx context.Context, ti *testInstance, id, email, displayName string) {
	t.Helper()

	_, err := usersrepo.New(ti.conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          id,
		Email:       email,
		DisplayName: displayName,
		PhotoUrl:    conv.PtrToPGText(nil),
		Admin:       false,
	})
	require.NoError(t, err)
}

func seedAPIKeySubject(t *testing.T, ctx context.Context, ti *testInstance, name string) urn.SessionSubject {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	key, err := keysrepo.New(ti.conn).CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		CreatedByUserID: authCtx.UserID,
		Name:            name,
		KeyPrefix:       "gram_test_" + uuid.NewString()[:8],
		KeyHash:         "hash-" + uuid.NewString(),
		Scopes:          []string{"consumer"},
	})
	require.NoError(t, err)
	return urn.NewAPIKeySubject(key.ID)
}

func seedDashboardMintSession(t *testing.T, ctx context.Context, ti *testInstance, issuerID uuid.UUID, subject urn.SessionSubject) repo.UserSession {
	t.Helper()

	now := time.Now()
	row, err := repo.New(ti.conn).CreateUserSession(ctx, repo.CreateUserSessionParams{
		UserSessionIssuerID: issuerID,
		UserSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		SubjectUrn:          subject,
		Jti:                 "jti-" + uuid.NewString(),
		RefreshTokenHash:    conv.ToPGText(sessiontokens.DashboardMintRefreshTokenHashPrefix + ":" + uuid.NewString()),
		RefreshExpiresAt:    pgtype.Timestamptz{Time: now.Add(time.Hour), InfinityModifier: 0, Valid: true},
		ExpiresAt:           pgtype.Timestamptz{Time: now.Add(time.Hour), InfinityModifier: 0, Valid: true},
		SignInID:            uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	return row
}

func TestListUserSessions_SearchMatchesShownLabels(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectTier := uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true}
	orgTier := uuid.NullUUID{UUID: uuid.Nil, Valid: false}

	issuerID := seedIssuer(t, ctx, ti, "search-labels")
	serverID := seedIssuer(t, ctx, ti, "zephyr-server")
	session := func(issuer uuid.UUID, subject urn.SessionSubject) string {
		row, err := seedUserSession(t, ctx, ti.conn, issuer, subject)
		require.NoError(t, err)
		return row.ID.String()
	}

	seedNamedUser(t, ctx, ti, "search-named", "named@example.com", "Dana Quill")
	named := session(issuerID, urn.NewUserSubject("search-named"))
	seedNamedUser(t, ctx, ti, "search-email-only", "orbit@example.com", "")
	emailOnly := session(issuerID, urn.NewUserSubject("search-email-only"))
	unknownUser := session(issuerID, urn.NewUserSubject("ghost-user"))
	namedKey := session(issuerID, seedAPIKeySubject(t, ctx, ti, "Nightly sync key"))
	unnamedKey := session(issuerID, urn.NewAPIKeySubject(uuid.New()))
	anonymous := session(issuerID, urn.NewAnonymousSubject("mcp-session-1"))
	server := session(serverID, urn.NewUserSubject("server-user"))

	client, err := seedUserSessionClient(t, ctx, ti.conn, issuerID, "Cobalt Desktop")
	require.NoError(t, err)
	clientRow, err := seedUserSessionForClient(t, ctx, ti.conn, issuerID, client.ID, urn.NewUserSubject("client-user"))
	require.NoError(t, err)
	withClient := clientRow.ID.String()
	dashboard := seedDashboardMintSession(t, ctx, ti, issuerID, urn.NewUserSubject("dashboard-user")).ID.String()

	upstreamSubject := urn.NewUserSubject("upstream-user")
	withUpstream := session(issuerID, upstreamSubject)
	seedUpstream(t, ctx, ti.conn, projectTier, issuerID, upstreamSubject, "quasar.example.com")

	workloadIssuer := seedWorkloadIssuer(t, ctx, ti.conn, projectTier, "Harbor Actions")
	agent := seedWorkloadAgent(t, ctx, ti.conn, "Lantern bot")
	seedWorkloadAssignment(t, ctx, ti.conn, workloadIssuer, workloadTestSubject, agent.ID)
	workload := session(issuerID, urn.NewWorkloadSubject(workloadIssuer, workloadTestSubject))

	siblingProject := createSiblingProject(t, ctx, ti.conn, "search-sibling-project")
	siblingIssuer := seedWorkloadIssuer(t, ctx, ti.conn, uuid.NullUUID{UUID: siblingProject, Valid: true}, "Hidden Sibling Issuer")
	siblingWorkload := session(issuerID, urn.NewWorkloadSubject(siblingIssuer, "repo:sibling/app"))

	deletedIssuer := seedWorkloadIssuer(t, ctx, ti.conn, orgTier, "Retired Issuer")
	deletedAgent := seedWorkloadAgent(t, ctx, ti.conn, "Retired bot")
	seedWorkloadAssignment(t, ctx, ti.conn, deletedIssuer, "repo:retired/app", deletedAgent.ID)
	deletedWorkload := session(issuerID, urn.NewWorkloadSubject(deletedIssuer, "repo:retired/app"))
	softDeleteWorkloadIssuer(t, ctx, ti.conn, deletedIssuer)

	for _, tc := range []struct {
		search string
		want   []string
	}{
		{search: "dana quill", want: []string{named}},
		{search: "dana\x00 quill", want: []string{named}},
		{search: "ORBIT@", want: []string{emailOnly}},
		{search: "user:ghost", want: []string{unknownUser}},
		{search: "nightly sync", want: []string{namedKey}},
		{search: "API key", want: []string{unnamedKey}},
		{search: "anonymous client", want: []string{anonymous}},
		{search: "cobalt", want: []string{withClient}},
		{search: sessiontokens.FirstPartyClientName, want: []string{dashboard}},
		{search: "zephyr", want: []string{server}},
		{search: "quasar", want: []string{withUpstream}},
		{search: "acme/payments", want: []string{workload}},
		{search: "harbor actions", want: []string{workload}},
		{search: "token.actions.example.com", want: []string{workload}},
		{search: "lantern", want: []string{workload}},
		{search: "repo:sibling", want: []string{siblingWorkload}},
		{search: "hidden sibling", want: []string{}},
		{search: "retired", want: []string{deletedWorkload}},
		{search: "retired issuer", want: []string{}},
		{search: "retired bot", want: []string{}},
	} {
		t.Run(tc.search, func(t *testing.T) {
			t.Parallel()
			got := searchSessionIDs(t, ctx, ti, &gen.ListUserSessionsPayload{Search: &tc.search})
			require.ElementsMatch(t, tc.want, got)
		})
	}
}

func TestListUserSessions_SearchTreatsWildcardsLiterally(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := seedIssuer(t, ctx, ti, "search-wildcards")
	literal, err := seedUserSessionClient(t, ctx, ti.conn, issuerID, "50%_off")
	require.NoError(t, err)
	literalSession, err := seedUserSessionForClient(t, ctx, ti.conn, issuerID, literal.ID, urn.NewUserSubject("literal"))
	require.NoError(t, err)
	lookalike, err := seedUserSessionClient(t, ctx, ti.conn, issuerID, "50xyoff")
	require.NoError(t, err)
	_, err = seedUserSessionForClient(t, ctx, ti.conn, issuerID, lookalike.ID, urn.NewUserSubject("lookalike"))
	require.NoError(t, err)

	for _, search := range []string{"%", "_", "50%_off", `\`} {
		got := searchSessionIDs(t, ctx, ti, &gen.ListUserSessionsPayload{Search: &search})
		if search == `\` {
			require.Empty(t, got)
			continue
		}
		require.Equal(t, []string{literalSession.ID.String()}, got, "search %q", search)
	}

	for _, blank := range []string{"   ", "\x00", " \x00 "} {
		require.Len(t, searchSessionIDs(t, ctx, ti, &gen.ListUserSessionsPayload{Search: &blank}), 2, "search %q", blank)
	}
}

func TestListUserSessions_SearchPagesWithOtherFilters(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := seedIssuer(t, ctx, ti, "search-pages")
	otherIssuerID := seedIssuer(t, ctx, ti, "search-pages-other")
	want := make([]string, 0, 60)
	for i := range 60 {
		row, err := seedUserSession(t, ctx, ti.conn, issuerID, urn.NewUserSubject(fmt.Sprintf("paged-%02d", i)))
		require.NoError(t, err)
		want = append(want, row.ID.String())
	}
	for i := range 5 {
		_, err := seedUserSession(t, ctx, ti.conn, issuerID, urn.NewUserSubject(fmt.Sprintf("other-%02d", i)))
		require.NoError(t, err)
		_, err = seedUserSession(t, ctx, ti.conn, otherIssuerID, urn.NewUserSubject(fmt.Sprintf("paged-elsewhere-%02d", i)))
		require.NoError(t, err)
	}

	search := "user:paged-"
	issuer := issuerID.String()
	first, err := ti.service.ListUserSessions(ctx, &gen.ListUserSessionsPayload{Search: &search, UserSessionIssuerID: &issuer})
	require.NoError(t, err)
	require.Len(t, first.Items, 50)
	require.NotNil(t, first.NextCursor)

	second, err := ti.service.ListUserSessions(ctx, &gen.ListUserSessionsPayload{Search: &search, UserSessionIssuerID: &issuer, Cursor: first.NextCursor})
	require.NoError(t, err)
	require.Len(t, second.Items, 10)
	require.Nil(t, second.NextCursor)

	got := make([]string, 0, 60)
	for _, item := range append(first.Items, second.Items...) {
		require.Equal(t, issuer, item.UserSessionIssuerID)
		got = append(got, item.ID)
	}
	require.ElementsMatch(t, want, got)
}

func TestListUserSessions_SearchExcludesSiblingProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	sp := seedSiblingProject(t, ctx, ti, "search-isolated")
	issuerID := seedIssuer(t, ctx, ti, "search-own")
	own, err := seedUserSession(t, ctx, ti.conn, issuerID, urn.NewUserSubject("search-isolated-own"))
	require.NoError(t, err)

	for _, search := range []string{"search-isolated", sp.issuerSlug, "test-search-isolated-client"} {
		got := searchSessionIDs(t, ctx, ti, &gen.ListUserSessionsPayload{Search: &search})
		require.NotContains(t, got, sp.sessionID.String(), "search %q", search)
	}
	search := "search-isolated"
	require.Equal(t, []string{own.ID.String()}, searchSessionIDs(t, ctx, ti, &gen.ListUserSessionsPayload{Search: &search}))
}

func TestListUserSessions_SearchMatchesOnlyReadableAgentNames(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuerID := seedIssuer(t, ctx, ti, "search-agents")

	ownedID := uuid.New()
	_, err := testrepo.New(ti.conn).CreateAttachmentUpstreamAgentFixture(ctx, testrepo.CreateAttachmentUpstreamAgentFixtureParams{ID: ownedID, OrganizationID: authCtx.ActiveOrganizationID, OwnerUserID: authCtx.UserID})
	require.NoError(t, err)
	owned, err := seedUserSession(t, ctx, ti.conn, issuerID, urn.NewAgentSubject(ownedID))
	require.NoError(t, err)

	search := "upstream-agent"
	human := contextvalues.WithValidatedGramSession(ctx, authCtx, false)
	require.Equal(t, []string{owned.ID.String()}, searchSessionIDs(t, human, ti, &gen.ListUserSessionsPayload{Search: &search}))
	withNUL := "upstream\x00-agent"
	require.Equal(t, []string{owned.ID.String()}, searchSessionIDs(t, human, ti, &gen.ListUserSessionsPayload{Search: &withNUL}))

	twinA := seedWorkloadAgent(t, ctx, ti.conn, "Twin bot A")
	twinB := seedWorkloadAgent(t, ctx, ti.conn, "Twin bot B")
	grantedSession, err := seedUserSession(t, ctx, ti.conn, issuerID, urn.NewAgentSubject(twinA.ID))
	require.NoError(t, err)
	_, err = seedUserSession(t, ctx, ti.conn, issuerID, urn.NewAgentSubject(twinB.ID))
	require.NoError(t, err)
	twin := "twin bot"
	require.Empty(t, searchSessionIDs(t, human, ti, &gen.ListUserSessionsPayload{Search: &twin}), "another member's agents are hidden without agent:read")

	selectors, err := authz.NewSelector(authz.ScopeAgentRead, twinA.ID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		Scope:          string(authz.ScopeAgentRead),
		Selectors:      selectors,
	})
	require.NoError(t, err)
	require.Equal(t, []string{grantedSession.ID.String()}, searchSessionIDs(t, human, ti, &gen.ListUserSessionsPayload{Search: &twin}), "a read grant reveals only the agent it names")

	byURN := urn.NewAgentSubject(ownedID).String()
	apiKeyAuth := *authCtx
	apiKeyAuth.APIKeyID = uuid.NewString()
	apiKey := contextvalues.WithLegacyAPIKeyAuthorization(ctx, &apiKeyAuth)
	require.Empty(t, searchSessionIDs(t, apiKey, ti, &gen.ListUserSessionsPayload{Search: &search}), "agent names stay hidden from credentials that cannot manage agents")
	require.Equal(t, []string{owned.ID.String()}, searchSessionIDs(t, apiKey, ti, &gen.ListUserSessionsPayload{Search: &byURN}))
}

func TestPlatformListUserSessions_SearchesLiterally(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	sp := seedSiblingProject(t, ctx, ti, "platform-50%_off")
	issuerID := seedIssuer(t, ctx, ti, "platform-search")
	literal, err := seedUserSessionClient(t, ctx, ti.conn, issuerID, "50%_off")
	require.NoError(t, err)
	literalSession, err := seedUserSessionForClient(t, ctx, ti.conn, issuerID, literal.ID, urn.NewUserSubject("literal"))
	require.NoError(t, err)
	lookalike, err := seedUserSessionClient(t, ctx, ti.conn, issuerID, "50xyoff")
	require.NoError(t, err)
	_, err = seedUserSessionForClient(t, ctx, ti.conn, issuerID, lookalike.ID, urn.NewUserSubject("lookalike"))
	require.NoError(t, err)

	atLimit, err := json.Marshal(map[string]string{"search": strings.Repeat("é", 200)})
	require.NoError(t, err)
	require.NoError(t, platformusersessions.NewListUserSessionsTool(ti.conn).Call(ctx, toolconfig.ToolCallEnv{}, bytes.NewReader(atLimit), &bytes.Buffer{}), "200 multibyte characters are within the limit")

	input, err := json.Marshal(map[string]string{"search": "50%\x00_off"})
	require.NoError(t, err)
	var out bytes.Buffer
	require.NoError(t, platformusersessions.NewListUserSessionsTool(ti.conn).Call(ctx, toolconfig.ToolCallEnv{}, bytes.NewReader(input), &out))

	var result struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	ids := make([]string, 0, len(result.Items))
	for _, item := range result.Items {
		ids = append(ids, item.ID)
	}
	require.Equal(t, []string{literalSession.ID.String()}, ids)
	require.NotContains(t, ids, sp.sessionID.String())
}

func TestListUserSessions_OrganizationTierAgentBindingStaysInItsProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	fixtures := testrepo.New(ti.conn)

	issuerID := seedOrganizationTierIssuer(t, ctx, ti.conn, "org-tier-attachment")
	owner := urn.NewUserSubject(authCtx.UserID)
	agentID := uuid.New()
	_, err := fixtures.CreateAttachmentUpstreamAgentFixture(ctx, testrepo.CreateAttachmentUpstreamAgentFixtureParams{ID: agentID, OrganizationID: authCtx.ActiveOrganizationID, OwnerUserID: authCtx.UserID})
	require.NoError(t, err)
	agentSession, err := seedUserSession(t, ctx, ti.conn, issuerID, urn.NewAgentSubject(agentID))
	require.NoError(t, err)
	clientID := seedUpstream(t, ctx, ti.conn, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, issuerID, owner, "orgtier.example.com")
	sourceID, err := fixtures.GetAttachmentSourceIDFixture(ctx, testrepo.GetAttachmentSourceIDFixtureParams{RemoteSessionClientID: clientID, SubjectUrn: owner})
	require.NoError(t, err)

	otherProject := uuid.New()
	_, err = fixtures.CreateAttachmentForeignProjectFixture(ctx, testrepo.CreateAttachmentForeignProjectFixtureParams{ID: otherProject, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	attach := func(project uuid.UUID) {
		t.Helper()
		_, err := fixtures.InsertAttachmentFromSourceFixture(ctx, testrepo.InsertAttachmentFromSourceFixtureParams{
			Project: project, Organization: authCtx.ActiveOrganizationID, Agent: agentID, Requesting: issuerID, Client: clientID, Source: sourceID,
		})
		require.NoError(t, err)
	}

	provider := "orgtier.example.com"
	providerFacet := func() map[string]int64 {
		t.Helper()
		facets, err := ti.service.QueryFacets(ctx, &gen.QueryFacetsPayload{})
		require.NoError(t, err)
		return facetValues(facets.Provider)
	}
	attach(otherProject)
	require.NotContains(t, searchSessionIDs(t, ctx, ti, &gen.ListUserSessionsPayload{Search: &provider}), agentSession.ID.String(), "a binding made in another project is not this project's upstream")
	require.Equal(t, map[string]int64{"none": 1}, providerFacet(), "nor is it counted as this project's provider")

	attach(*authCtx.ProjectID)
	require.Contains(t, searchSessionIDs(t, ctx, ti, &gen.ListUserSessionsPayload{Search: &provider}), agentSession.ID.String())
	counts := providerFacet()
	require.Len(t, counts, 1)
	for providerID, count := range counts {
		require.Equal(t, int64(1), count)
		require.Equal(t, []string{agentSession.ID.String()}, searchSessionIDs(t, ctx, ti, &gen.ListUserSessionsPayload{Providers: []string{providerID}}))
	}
}
