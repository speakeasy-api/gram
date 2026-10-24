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
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	platformusersessions "github.com/speakeasy-api/gram/server/internal/platformtools/usersessions"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// filterFixture is one project's sessions across every filter field.
type filterFixture struct {
	serverA, serverB         uuid.UUID
	dcrClient, cimdClient    uuid.UUID
	alice, bob, dashboard    string
	apiKey, workload, recent string
	stale, unused            string
}

func seedFilterFixture(t *testing.T, ctx context.Context, ti *testInstance) filterFixture {
	t.Helper()

	var f filterFixture
	f.serverA = seedIssuer(t, ctx, ti, "filter-server-a")
	f.serverB = seedIssuer(t, ctx, ti, "filter-server-b")
	dcr, err := seedUserSessionClient(t, ctx, ti.conn, f.serverA, "Cursor")
	require.NoError(t, err)
	f.dcrClient = dcr.ID
	cimd, err := seedCimdUserSessionClient(t, ctx, ti.conn, f.serverB, "https://client.example.com/metadata.json")
	require.NoError(t, err)
	f.cimdClient = cimd.ID

	seedNamedUser(t, ctx, ti, "filter-alice", "alice@example.com", "Alice Filter")
	seedNamedUser(t, ctx, ti, "filter-bob", "bob@example.com", "Bob Filter")

	session := func(issuer uuid.UUID, client uuid.UUID, subject urn.SessionSubject) string {
		t.Helper()
		var row repo.UserSession
		var err error
		if client == uuid.Nil {
			row, err = seedUserSession(t, ctx, ti.conn, issuer, subject)
		} else {
			row, err = seedUserSessionForClient(t, ctx, ti.conn, issuer, client, subject)
		}
		require.NoError(t, err)
		return row.ID.String()
	}
	f.alice = session(f.serverA, f.dcrClient, urn.NewUserSubject("filter-alice"))
	f.bob = session(f.serverB, f.cimdClient, urn.NewUserSubject("filter-bob"))
	f.dashboard = seedDashboardMintSession(t, ctx, ti, f.serverA, urn.NewUserSubject("filter-alice")).ID.String()
	f.apiKey = session(f.serverA, uuid.Nil, seedAPIKeySubject(t, ctx, ti, "Filter key"))
	f.workload = session(f.serverB, uuid.Nil, urn.NewWorkloadSubject(uuid.New(), "repo:filter/app"))
	f.recent = session(f.serverA, f.dcrClient, urn.NewUserSubject("filter-recent"))
	f.stale = session(f.serverA, f.dcrClient, urn.NewUserSubject("filter-stale"))
	f.unused = session(f.serverB, f.cimdClient, urn.NewUserSubject("filter-unused"))

	now := time.Now()
	touch := func(id string, at time.Time) {
		t.Helper()
		row, err := repo.New(ti.conn).GetUserSessionByID(ctx, repo.GetUserSessionByIDParams{ID: uuid.MustParse(id), ProjectID: *mustAuth(t, ctx).ProjectID, OrganizationID: mustAuth(t, ctx).ActiveOrganizationID})
		require.NoError(t, err)
		require.NoError(t, repo.New(ti.conn).TouchUserSessionLastUsed(ctx, repo.TouchUserSessionLastUsedParams{
			NowTs:               pgtype.Timestamptz{Time: at, InfinityModifier: 0, Valid: true},
			ProjectID:           *mustAuth(t, ctx).ProjectID,
			OrganizationID:      mustAuth(t, ctx).ActiveOrganizationID,
			UserSessionIssuerID: row.UserSessionIssuerID,
			Jti:                 row.Jti,
			UsedCutoff:          pgtype.Timestamptz{Time: now.Add(time.Hour), InfinityModifier: 0, Valid: true},
		}))
	}
	for _, id := range []string{f.alice, f.bob, f.dashboard, f.apiKey, f.workload} {
		touch(id, now.Add(-2*time.Hour))
	}
	touch(f.recent, now.Add(-10*time.Minute))
	touch(f.stale, now.Add(-10*24*time.Hour))

	return f
}

func mustAuth(t *testing.T, ctx context.Context) *contextvalues.AuthContext {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	return authCtx
}

func TestListUserSessions_FilterFields(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	f := seedFilterFixture(t, ctx, ti)
	hour := int(time.Hour / time.Second)
	week := int(7 * 24 * time.Hour / time.Second)
	alice := urn.NewUserSubject("filter-alice").String()

	for _, tc := range []struct {
		name    string
		payload gen.ListUserSessionsPayload
		want    []string
	}{
		{name: "identities", payload: gen.ListUserSessionsPayload{Identities: []string{alice}}, want: []string{f.alice, f.dashboard}},
		{name: "exclude identities", payload: gen.ListUserSessionsPayload{ExcludeIdentities: []string{alice}}, want: []string{f.bob, f.apiKey, f.workload, f.recent, f.stale, f.unused}},
		{name: "identity types", payload: gen.ListUserSessionsPayload{IdentityTypes: []string{"apikey", "workload"}}, want: []string{f.apiKey, f.workload}},
		{name: "exclude identity types", payload: gen.ListUserSessionsPayload{ExcludeIdentityTypes: []string{"user"}}, want: []string{f.apiKey, f.workload}},
		{name: "servers", payload: gen.ListUserSessionsPayload{Servers: []string{f.serverB.String()}}, want: []string{f.bob, f.workload, f.unused}},
		{name: "exclude servers", payload: gen.ListUserSessionsPayload{ExcludeServers: []string{f.serverA.String()}}, want: []string{f.bob, f.workload, f.unused}},
		{name: "clients include dashboard and none", payload: gen.ListUserSessionsPayload{Clients: []string{"dashboard", "none"}}, want: []string{f.dashboard, f.apiKey, f.workload}},
		{name: "exclude clients", payload: gen.ListUserSessionsPayload{ExcludeClients: []string{f.dcrClient.String(), "none"}}, want: []string{f.bob, f.dashboard, f.unused}},
		{name: "registration cimd", payload: gen.ListUserSessionsPayload{ClientRegistrations: []string{"cimd"}}, want: []string{f.bob, f.unused}},
		{name: "registration dcr", payload: gen.ListUserSessionsPayload{ClientRegistrations: []string{"dcr"}}, want: []string{f.alice, f.recent, f.stale}},
		{name: "registration none", payload: gen.ListUserSessionsPayload{ClientRegistrations: []string{"none"}}, want: []string{f.dashboard, f.apiKey, f.workload}},
		{name: "exclude registration", payload: gen.ListUserSessionsPayload{ExcludeClientRegistrations: []string{"dcr", "none"}}, want: []string{f.bob, f.unused}},
		{name: "used within an hour", payload: gen.ListUserSessionsPayload{LastUsedWithin: &hour}, want: []string{f.recent}},
		{name: "last used over a week ago", payload: gen.ListUserSessionsPayload{LastUsedOlderThan: &week}, want: []string{f.stale}},
		{name: "no recorded use", payload: gen.ListUserSessionsPayload{LastUsedNever: new(true)}, want: []string{f.unused}},
		{name: "fields combine with AND", payload: gen.ListUserSessionsPayload{Servers: []string{f.serverA.String()}, ClientRegistrations: []string{"dcr"}, ExcludeIdentities: []string{alice}}, want: []string{f.recent, f.stale}},
		{name: "fields combine with search and scalars", payload: gen.ListUserSessionsPayload{Search: new("filter-st"), UserSessionIssuerID: new(f.serverA.String()), ClientRegistrations: []string{"dcr"}}, want: []string{f.stale}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := searchSessionIDs(t, ctx, ti, &tc.payload)
			require.ElementsMatch(t, tc.want, got)
		})
	}
}

func TestListUserSessions_FilterRejectsOversizedAndInvalidValues(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	tooMany := make([]string, 51)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("user:%d", i)
	}
	for name, payload := range map[string]gen.ListUserSessionsPayload{
		"too many identities":  {Identities: tooMany},
		"server not a uuid":    {Servers: []string{"not-a-uuid"}},
		"unknown client value": {Clients: []string{"somewhere"}},
		"unknown registration": {ClientRegistrations: []string{"preregistered"}},
		"zero seconds":         {LastUsedWithin: new(0)},
		"empty identity":       {Identities: []string{""}},
		"empty exclusion":      {ExcludeClients: []string{" "}},
		"NUL subject":          {SubjectUrn: new("user:a\x00b")},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := ti.service.ListUserSessions(ctx, &payload)
			requireOopsCode(t, err, oops.CodeBadRequest)
		})
	}
	for name, payload := range map[string]gen.QueryFacetsPayload{
		"facets with empty identity": {ExcludeIdentities: []string{""}},
		"facets with NUL subject":    {SubjectUrn: new("user:a\x00b")},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := ti.service.QueryFacets(ctx, &payload)
			requireOopsCode(t, err, oops.CodeBadRequest)
		})
	}
}

func facetValues(facet *gen.UserSessionFilterFacet) map[string]int64 {
	out := map[string]int64{}
	for _, v := range facet.Values {
		out[v.Value] = v.Count
	}
	return out
}

func TestQueryFacets_CountsIgnoreOwnSelection(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	f := seedFilterFixture(t, ctx, ti)

	all, err := ti.service.QueryFacets(ctx, &gen.QueryFacetsPayload{})
	require.NoError(t, err)
	require.Equal(t, map[string]int64{f.serverA.String(): 5, f.serverB.String(): 3}, facetValues(all.McpServer))
	require.Equal(t, map[string]int64{"dcr": 3, "cimd": 2, "none": 3}, facetValues(all.ClientRegistration))

	narrowed, err := ti.service.QueryFacets(ctx, &gen.QueryFacetsPayload{Servers: []string{f.serverB.String()}})
	require.NoError(t, err)
	require.Equal(t, facetValues(all.McpServer), facetValues(narrowed.McpServer), "a field's counts ignore its own selection")
	require.Equal(t, map[string]int64{"cimd": 2, "none": 1}, facetValues(narrowed.ClientRegistration), "other fields apply the selection")
	require.Equal(t, map[string]int64{"user": 2, "workload": 1}, facetValues(narrowed.IdentityType))

	searched, err := ti.service.QueryFacets(ctx, &gen.QueryFacetsPayload{Search: new("filter-st"), Servers: []string{f.serverB.String()}})
	require.NoError(t, err)
	require.Equal(t, map[string]int64{f.serverA.String(): 1, f.serverB.String(): 0}, facetValues(searched.McpServer), "own-field counts still apply search, and the selected server stays listed")
	require.Empty(t, facetValues(searched.ClientRegistration))
}

func TestQueryFacets_CountsMatchTheList(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	f := seedFilterFixture(t, ctx, ti)
	alice := urn.NewUserSubject("filter-alice").String()

	for name, base := range map[string]gen.QueryFacetsPayload{
		"no filters":          {},
		"with search":         {Search: new("filter")},
		"with exclusions":     {ExcludeIdentities: []string{alice}, ExcludeClients: []string{"none"}},
		"with scalar and use": {UserSessionIssuerID: new(f.serverA.String()), LastUsedOlderThan: new(60)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			facets, err := ti.service.QueryFacets(ctx, &base)
			require.NoError(t, err)
			check := func(field string, facet *gen.UserSessionFilterFacet, with func(*gen.ListUserSessionsPayload, string)) {
				for _, v := range facet.Values {
					payload := gen.ListUserSessionsPayload{
						Search: base.Search, UserSessionIssuerID: base.UserSessionIssuerID, LastUsedOlderThan: base.LastUsedOlderThan,
						Identities: base.Identities, ExcludeIdentities: base.ExcludeIdentities, Clients: base.Clients, ExcludeClients: base.ExcludeClients,
					}
					with(&payload, v.Value)
					require.Len(t, searchSessionIDs(t, ctx, ti, &payload), int(v.Count), "%s=%s", field, v.Value)
				}
			}
			check("identity", facets.Identity, func(p *gen.ListUserSessionsPayload, v string) { p.Identities, p.ExcludeIdentities = []string{v}, nil })
			check("identity_type", facets.IdentityType, func(p *gen.ListUserSessionsPayload, v string) { p.IdentityTypes = []string{v} })
			check("mcp_server", facets.McpServer, func(p *gen.ListUserSessionsPayload, v string) { p.Servers = []string{v} })
			check("client", facets.Client, func(p *gen.ListUserSessionsPayload, v string) { p.Clients, p.ExcludeClients = []string{v}, nil })
			check("client_registration", facets.ClientRegistration, func(p *gen.ListUserSessionsPayload, v string) { p.ClientRegistrations = []string{v} })
		})
	}
}

func TestQueryFacets_DefaultStatusLeavesOutRevokedSessions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := seedIssuer(t, ctx, ti, "facet-status")
	live, err := seedUserSession(t, ctx, ti.conn, issuerID, urn.NewUserSubject("facet-live"))
	require.NoError(t, err)
	revoked, err := seedUserSession(t, ctx, ti.conn, issuerID, urn.NewUserSubject("facet-revoked"))
	require.NoError(t, err)
	_, err = repo.New(ti.conn).RevokeUserSession(ctx, repo.RevokeUserSessionParams{ID: revoked.ID, ProjectID: *mustAuth(t, ctx).ProjectID, OrganizationID: mustAuth(t, ctx).ActiveOrganizationID})
	require.NoError(t, err)

	defaults, err := ti.service.QueryFacets(ctx, &gen.QueryFacetsPayload{})
	require.NoError(t, err)
	require.Equal(t, map[string]int64{live.SubjectUrn.String(): 1}, facetValues(defaults.Identity))

	every, err := ti.service.QueryFacets(ctx, &gen.QueryFacetsPayload{Status: new("all")})
	require.NoError(t, err)
	require.Equal(t, map[string]int64{live.SubjectUrn.String(): 1, revoked.SubjectUrn.String(): 1}, facetValues(every.Identity))
}

func TestQueryFacets_BoundsValuesAndKeepsSelections(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := seedIssuer(t, ctx, ti, "facet-bounds")
	for i := range 30 {
		id := fmt.Sprintf("bounds-%02d", i)
		seedNamedUser(t, ctx, ti, id, id+"@example.com", fmt.Sprintf("Bounds Person %02d", i))
		for range 30 - i {
			_, err := seedUserSession(t, ctx, ti.conn, issuerID, urn.NewUserSubject(id))
			require.NoError(t, err)
		}
	}
	sp := seedSiblingProject(t, ctx, ti, "facet-bounds-sibling")
	seedNamedUser(t, ctx, ti, "facet-bounds-sibling-subject", "hidden@example.com", "Hidden Sibling Person")
	tail := urn.NewUserSubject("bounds-29").String()
	unseen := sp.subject.String()

	facets, err := ti.service.QueryFacets(ctx, &gen.QueryFacetsPayload{Identities: []string{tail, unseen}})
	require.NoError(t, err)
	require.Equal(t, int64(30), facets.Identity.DistinctCount)
	require.Len(t, facets.Identity.Values, 27, "the top 25 plus two selected values")
	byValue := map[string]*gen.UserSessionFilterFacetValue{}
	for _, v := range facets.Identity.Values {
		byValue[v.Value] = v
	}
	require.Equal(t, int64(1), byValue[tail].Count, "a selected value outside the top 25 keeps its count")
	require.Equal(t, "Bounds Person 29", *byValue[tail].DisplayName)
	require.Equal(t, int64(0), byValue[unseen].Count)
	require.Nil(t, byValue[unseen].DisplayName, "a value from a link that the caller cannot see is not named")
}

func TestQueryFacets_FieldSearchFindsValuesOutsideTheTop(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := mustAuth(t, ctx)
	human := contextvalues.WithValidatedGramSession(ctx, authCtx, false)
	issuerID := seedIssuer(t, ctx, ti, "facet-field-search")
	for i := range 30 {
		_, err := seedUserSession(t, ctx, ti.conn, issuerID, urn.NewUserSubject(fmt.Sprintf("crowd-%02d", i)))
		require.NoError(t, err)
		_, err = seedUserSession(t, ctx, ti.conn, issuerID, urn.NewUserSubject(fmt.Sprintf("crowd-%02d", i)))
		require.NoError(t, err)
	}
	seedNamedUser(t, ctx, ti, "field-search-person", "quiet@example.com", "Quiet Person")
	_, err := seedUserSession(t, ctx, ti.conn, issuerID, urn.NewUserSubject("field-search-person"))
	require.NoError(t, err)

	readableID := uuid.New()
	_, err = testrepo.New(ti.conn).CreateAttachmentUpstreamAgentFixture(ctx, testrepo.CreateAttachmentUpstreamAgentFixtureParams{ID: readableID, OrganizationID: authCtx.ActiveOrganizationID, OwnerUserID: authCtx.UserID})
	require.NoError(t, err)
	_, err = seedUserSession(t, ctx, ti.conn, issuerID, urn.NewAgentSubject(readableID))
	require.NoError(t, err)
	hidden := seedWorkloadAgent(t, ctx, ti.conn, "upstream-hidden")
	_, err = seedUserSession(t, ctx, ti.conn, issuerID, urn.NewAgentSubject(hidden.ID))
	require.NoError(t, err)

	identities := func(c context.Context, search string) []string {
		t.Helper()
		facets, err := ti.service.QueryFacets(c, &gen.QueryFacetsPayload{Field: new("identity"), FieldSearch: &search})
		require.NoError(t, err)
		out := []string{}
		for _, v := range facets.Identity.Values {
			out = append(out, v.Value)
		}
		return out
	}
	require.Equal(t, []string{urn.NewUserSubject("field-search-person").String()}, identities(human, "quiet"))
	require.Equal(t, []string{urn.NewAgentSubject(readableID).String()}, identities(human, "upstream-agent"), "a readable agent is found by its name")
	require.Empty(t, identities(human, "upstream-hidden"), "an agent the caller cannot read is not found by its name")
	require.Equal(t, []string{urn.NewAgentSubject(hidden.ID).String()}, identities(human, strings.ToUpper(hidden.ID.String()[:13])), "its URN still matches")

	_, err = ti.service.QueryFacets(ctx, &gen.QueryFacetsPayload{FieldSearch: new("quiet")})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestPlatformListUserSessions_FiltersMatchTheAPI(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	f := seedFilterFixture(t, ctx, ti)
	tool := platformusersessions.NewListUserSessionsTool(ti.conn)
	call := func(input map[string]any) ([]string, error) {
		t.Helper()
		body, err := json.Marshal(input)
		require.NoError(t, err)
		var out bytes.Buffer
		if err := tool.Call(ctx, toolconfig.ToolCallEnv{}, bytes.NewReader(body), &out); err != nil {
			return nil, fmt.Errorf("call list tool: %w", err)
		}
		var result struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		}
		require.NoError(t, json.Unmarshal(out.Bytes(), &result))
		ids := []string{}
		for _, item := range result.Items {
			ids = append(ids, item.ID)
		}
		return ids, nil
	}

	got, err := call(map[string]any{"identity_types": []string{"apikey", "workload"}, "exclude_servers": []string{f.serverB.String()}})
	require.NoError(t, err)
	want := searchSessionIDs(t, ctx, ti, &gen.ListUserSessionsPayload{IdentityTypes: []string{"apikey", "workload"}, ExcludeServers: []string{f.serverB.String()}})
	require.ElementsMatch(t, want, got)
	require.Equal(t, []string{f.apiKey}, got)

	got, err = call(map[string]any{"client_registrations": []string{"cimd"}, "last_used_never": true})
	require.NoError(t, err)
	require.Equal(t, []string{f.unused}, got)

	tooMany := make([]string, 51)
	for i := range tooMany {
		tooMany[i] = "none"
	}
	_, err = call(map[string]any{"clients": tooMany})
	requireOopsCode(t, err, oops.CodeBadRequest)
	_, err = call(map[string]any{"identities": []string{""}})
	requireOopsCode(t, err, oops.CodeBadRequest)
	_, err = call(map[string]any{"subject_urn": "user:a\x00b"})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestQueryFacets_FieldSearchKeepsSelectedCounts(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := seedIssuer(t, ctx, ti, "facet-selected-search")
	clientA, err := seedUserSessionClient(t, ctx, ti.conn, issuerID, "Alpha Desk")
	require.NoError(t, err)
	clientB, err := seedUserSessionClient(t, ctx, ti.conn, issuerID, "Bravo Desk")
	require.NoError(t, err)
	for _, subject := range []string{"selected-1", "selected-2"} {
		_, err := seedUserSessionForClient(t, ctx, ti.conn, issuerID, clientA.ID, urn.NewUserSubject(subject))
		require.NoError(t, err)
	}
	_, err = seedUserSessionForClient(t, ctx, ti.conn, issuerID, clientB.ID, urn.NewUserSubject("selected-3"))
	require.NoError(t, err)
	sp := seedSiblingProject(t, ctx, ti, "facet-selected-sibling")
	unknown := uuid.New().String()

	facets, err := ti.service.QueryFacets(ctx, &gen.QueryFacetsPayload{
		Clients:     []string{clientA.ID.String(), clientB.ID.String(), sp.clientID.String(), unknown},
		Field:       new("client"),
		FieldSearch: new("bravo"),
	})
	require.NoError(t, err)
	byValue := map[string]*gen.UserSessionFilterFacetValue{}
	for _, v := range facets.Client.Values {
		byValue[v.Value] = v
	}
	require.Equal(t, int64(1), facets.Client.DistinctCount, "only Bravo matches the search")
	require.Equal(t, int64(1), byValue[clientB.ID.String()].Count)
	require.Equal(t, int64(2), byValue[clientA.ID.String()].Count, "a selected client keeps its real count outside the search")
	require.Equal(t, "test-Alpha Desk", *byValue[clientA.ID.String()].DisplayName)
	require.True(t, byValue[clientB.ID.String()].MatchesSearch, "a selected value that matches the search counts as a match")
	require.False(t, byValue[clientA.ID.String()].MatchesSearch, "a selected value outside the search is returned only to stay removable")
	for _, hidden := range []string{sp.clientID.String(), unknown} {
		require.Equal(t, int64(0), byValue[hidden].Count)
		require.Nil(t, byValue[hidden].DisplayName)
		require.False(t, byValue[hidden].MatchesSearch)
	}

	byID, err := ti.service.QueryFacets(ctx, &gen.QueryFacetsPayload{
		Clients:     []string{clientA.ID.String()},
		Field:       new("client"),
		FieldSearch: new(strings.ToUpper(clientB.ID.String()[:13])),
	})
	require.NoError(t, err)
	matched := map[string]bool{}
	for _, v := range byID.Client.Values {
		matched[v.Value] = v.MatchesSearch
	}
	require.Equal(t, map[string]bool{clientB.ID.String(): true, clientA.ID.String(): false}, matched, "the raw id matches without a label")
	require.Equal(t, int64(1), byID.Client.DistinctCount)
}

func TestListUserSessions_IdentityFilterMatchesExactly(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := seedIssuer(t, ctx, ti, "identity-exact")
	workloadIssuer := uuid.New()
	padded := urn.NewWorkloadSubject(workloadIssuer, " repo:exact/app ")
	trimmed := urn.NewWorkloadSubject(workloadIssuer, "repo:exact/app")
	paddedSession, err := seedUserSession(t, ctx, ti.conn, issuerID, padded)
	require.NoError(t, err)
	trimmedSession, err := seedUserSession(t, ctx, ti.conn, issuerID, trimmed)
	require.NoError(t, err)

	require.Equal(t, []string{paddedSession.ID.String()}, searchSessionIDs(t, ctx, ti, &gen.ListUserSessionsPayload{Identities: []string{padded.String()}}))
	require.Equal(t, []string{trimmedSession.ID.String()}, searchSessionIDs(t, ctx, ti, &gen.ListUserSessionsPayload{Identities: []string{trimmed.String()}}))

	_, err = ti.service.ListUserSessions(ctx, &gen.ListUserSessionsPayload{Identities: []string{"user:a\x00b"}})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestQueryFacets_FieldSearchKeepsSelectedUnlabelledValues(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	f := seedFilterFixture(t, ctx, ti)

	types, err := ti.service.QueryFacets(ctx, &gen.QueryFacetsPayload{
		IdentityTypes: []string{"user"},
		Field:         new("identity_type"),
		FieldSearch:   new("work"),
	})
	require.NoError(t, err)
	matched := map[string]bool{}
	for _, v := range types.IdentityType.Values {
		matched[v.Value] = v.MatchesSearch
	}
	require.Equal(t, map[string]bool{"workload": true, "user": false}, matched, "a selected value with no label is kept and is not a match")

	clients, err := ti.service.QueryFacets(ctx, &gen.QueryFacetsPayload{
		Clients:     []string{"none"},
		Field:       new("client"),
		FieldSearch: new("cursor"),
	})
	require.NoError(t, err)
	byValue := map[string]*gen.UserSessionFilterFacetValue{}
	for _, v := range clients.Client.Values {
		byValue[v.Value] = v
	}
	require.True(t, byValue[f.dcrClient.String()].MatchesSearch)
	require.False(t, byValue["none"].MatchesSearch)
	require.Equal(t, "No client", *byValue["none"].DisplayName)

	none, err := ti.service.QueryFacets(ctx, &gen.QueryFacetsPayload{Field: new("client"), FieldSearch: new("no client")})
	require.NoError(t, err)
	require.Equal(t, []string{"none"}, []string{none.Client.Values[0].Value}, "the shown label is searchable")
	require.Len(t, none.Client.Values, 1)
}
