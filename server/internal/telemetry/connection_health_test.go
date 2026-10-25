package telemetry_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/metamcp/visibility"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// connectionAttempt is one attempt row as a Direct listing writes it.
type connectionAttempt struct {
	project, gateway          string
	stage, outcome, reason    string
	subject, provider, client string
	retries                   int
	at                        time.Time
}

func logConnectionAttempt(t *testing.T, ctx context.Context, ti *testInstance, a connectionAttempt) {
	t.Helper()
	attrs := map[attr.Key]any{
		attr.EventURNKey:                      "urn:telemetry:gram_service:log:mcp_connection_attempt",
		attr.MetaMcpServerIDKey:               a.gateway,
		attr.ConnectionAttemptStageKey:        a.stage,
		attr.ConnectionAttemptOutcomeKey:      a.outcome,
		attr.ConnectionAttemptReasonKey:       a.reason,
		attr.ConnectionAttemptSubjectKey:      a.subject,
		attr.ConnectionAttemptProviderIDKey:   a.provider,
		attr.ConnectionAttemptClientKeyKey:    a.client,
		attr.ConnectionAttemptRetriesKey:      a.retries,
		attr.ConnectionAttemptFailureClassKey: "",
	}
	if a.stage == telemetry.ConnectionStageDiscoveryMember {
		attrs[attr.McpServerIDKey] = uuid.NewString()
	}
	require.NoError(t, ti.telemLogger.LogSyncForTest(ctx, telemetry.LogParams{
		Timestamp:  a.at,
		ToolInfo:   telemetry.ToolInfo{ID: "", URN: "", Name: "", ProjectID: a.project, DeploymentID: "", FunctionID: nil, OrganizationID: ti.orgID},
		UserInfo:   telemetry.UserInfoByID(""),
		Attributes: attrs,
	}))
}

func outcomeMap(counts []*gen.GatewayConnectionOutcomeCount) map[string]int64 {
	totals := map[string]int64{}
	for _, count := range counts {
		totals[count.Outcome] = count.Count
	}
	return totals
}

func TestGetGatewayConnectionHealth_CountsEachStageExactly(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	gateway := createGateway(t, ctx, ti, "Health Gateway").ID.String()
	day := time.Now().UTC().Truncate(24 * time.Hour).Add(-24 * time.Hour)
	at := func(hour, minute int) time.Time {
		return day.Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute)
	}
	attempt := func(stage, outcome string, retries int, when time.Time) {
		logConnectionAttempt(t, ctx, ti, connectionAttempt{project: ti.projectID, gateway: gateway, stage: stage, outcome: outcome, reason: "", subject: "", provider: "", client: "anonymous", retries: retries, at: when})
	}
	listing, member := telemetry.ConnectionStageDiscoveryListing, telemetry.ConnectionStageDiscoveryMember
	attempt(listing, "complete", 0, at(10, 15))
	attempt(listing, "partial", 0, at(10, 45))
	attempt(listing, "failed", 0, at(11, 40))
	attempt(member, "listed", 0, at(10, 15))
	attempt(member, "listed", 2, at(10, 45))
	attempt(member, "failed", 2, at(11, 0))
	attempt(member, "consent_blocked", 0, at(11, 35))
	// Outside the window, another gateway, and another project: none count.
	attempt(member, "failed", 0, at(9, 0))
	logConnectionAttempt(t, ctx, ti, connectionAttempt{project: ti.projectID, gateway: uuid.NewString(), stage: member, outcome: "failed", client: "anonymous", at: at(10, 45)})
	logConnectionAttempt(t, ctx, ti, connectionAttempt{project: uuid.NewString(), gateway: gateway, stage: member, outcome: "failed", client: "anonymous", at: at(10, 45)})

	from, to := at(10, 10), at(11, 50)
	result, err := ti.service.GetGatewayConnectionHealth(ctx, &gen.GetGatewayConnectionHealthPayload{MetaMcpServerID: gateway, From: new(from.Format(time.RFC3339)), To: new(to.Format(time.RFC3339))})
	require.NoError(t, err)
	require.Equal(t, from.Format(time.RFC3339Nano), result.From)
	require.Equal(t, to.Format(time.RFC3339Nano), result.To)
	require.Equal(t, int64(1800), result.IntervalSeconds)
	require.Len(t, result.Stages, 2)

	listings, members := result.Stages[0], result.Stages[1]
	require.Equal(t, listing, listings.Stage)
	require.Equal(t, int64(3), listings.Attempts, "listings are counted on their own")
	require.Equal(t, map[string]int64{"complete": 1, "partial": 1, "failed": 1}, outcomeMap(listings.Outcomes))
	require.Equal(t, member, members.Stage)
	require.Equal(t, int64(4), members.Attempts, "blocked members stay in the denominator")
	require.Equal(t, map[string]int64{"listed": 2, "failed": 1, "consent_blocked": 1}, outcomeMap(members.Outcomes))
	require.Equal(t, int64(4), members.Retries)
	require.Equal(t, int64(1), members.Recovered)
	require.NotNil(t, members.LastSeenAt)
	require.Equal(t, at(11, 35).Format(time.RFC3339Nano), *members.LastSeenAt)

	buckets := map[string]map[string]int64{}
	for _, bucket := range members.Buckets {
		nanos, err := strconv.ParseInt(bucket.BucketTimeUnixNano, 10, 64)
		require.NoError(t, err)
		buckets[time.Unix(0, nanos).UTC().Format("15:04")] = outcomeMap(bucket.Outcomes)
	}
	require.Equal(t, map[string]map[string]int64{"10:00": {"listed": 1}, "10:30": {"listed": 1}, "11:00": {"failed": 1}, "11:30": {"consent_blocked": 1}}, buckets)

	notRecorded := map[string]bool{}
	for _, coverage := range result.NotRecorded {
		notRecorded[coverage.Stage] = true
	}
	require.True(t, notRecorded["inbound_token"], "sign-in is unknown, not healthy")
	require.NotEmpty(t, result.Scope)
}

func TestGetGatewayConnectionHealth_RequiresProjectLogAndGatewayRead(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	gateway := createGateway(t, ctx, ti, "Guarded Gateway")
	project, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Other", Slug: "other-" + uuid.NewString()[:8], OrganizationID: ti.orgID})
	require.NoError(t, err)
	elsewhere, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{OrganizationID: ti.orgID, ProjectID: project.ID, Name: "Elsewhere", UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Visibility: visibility.Private})
	require.NoError(t, err)

	projectRead := authz.NewGrant(authz.ScopeProjectRead, ti.projectID)
	logRead := authz.NewGrant(authz.ScopeLogRead, ti.projectID)
	gatewayRead := authz.NewGrant(authz.ScopeMCPRead, gateway.ID.String())
	// read returns the code both methods refuse with, or "" when both read.
	read := func(grants []authz.Grant, gatewayID uuid.UUID) oops.Code {
		t.Helper()
		caller := authztest.WithExactGrants(t, ctx, grants...)
		codes := []oops.Code{}
		for _, err := range []error{
			func() error {
				_, err := ti.service.GetGatewayConnectionHealth(caller, &gen.GetGatewayConnectionHealthPayload{MetaMcpServerID: gatewayID.String(), From: nil, To: nil, Key: nil})
				return err //nolint:wrapcheck // the test reads the handler's own error
			}(),
			func() error {
				_, err := ti.service.ListGatewayConnectionHotspots(caller, &gen.ListGatewayConnectionHotspotsPayload{MetaMcpServerID: gatewayID.String(), From: nil, To: nil, Stage: telemetry.ConnectionStageDiscoveryMember, RankBy: "count", MinAttempts: 5, Limit: 25})
				return err //nolint:wrapcheck // the test reads the handler's own error
			}(),
		} {
			if err == nil {
				codes = append(codes, "")
				continue
			}
			shareable, ok := errors.AsType[*oops.ShareableError](err)
			require.True(t, ok, "%v", err)
			codes = append(codes, shareable.Code)
		}
		require.Equal(t, codes[0], codes[1], "both methods apply one policy")
		return codes[0]
	}

	require.Empty(t, read([]authz.Grant{projectRead, logRead, gatewayRead}, gateway.ID))
	require.Equal(t, oops.CodeForbidden, read([]authz.Grant{logRead, gatewayRead}, gateway.ID), "log and gateway read alone name no providers or clients")
	require.Equal(t, oops.CodeForbidden, read([]authz.Grant{projectRead, gatewayRead}, gateway.ID))
	require.Equal(t, oops.CodeNotFound, read([]authz.Grant{projectRead, logRead, authz.NewGrant(authz.ScopeMCPRead, uuid.NewString())}, gateway.ID))
	require.Equal(t, oops.CodeNotFound, read([]authz.Grant{projectRead, logRead, gatewayRead, authz.NewGrant(authz.ScopeMCPRead, elsewhere.ID.String())}, elsewhere.ID))
}

func TestGetGatewayConnectionHealth_RejectsWindowsOutsideItsBounds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	gateway := createGateway(t, ctx, ti, "Window Gateway").ID.String()
	for name, bounds := range map[string][2]string{
		"far future":     {"9999-12-24T00:00:00Z", "9999-12-31T00:00:00Z"},
		"before 1970":    {"1969-12-25T00:00:00Z", "1969-12-31T00:00:00Z"},
		"just past 2100": {"2099-12-31T00:00:00Z", "2100-01-01T00:00:00.000000001Z"},
		"over 30 days":   {"2026-01-01T00:00:00Z", "2026-01-31T00:00:00.000000001Z"},
		"reversed":       {"2026-01-02T00:00:00Z", "2026-01-01T00:00:00Z"},
		"not RFC 3339":   {"yesterday", "2026-01-01T00:00:00Z"},
	} {
		_, err := ti.service.GetGatewayConnectionHealth(ctx, &gen.GetGatewayConnectionHealthPayload{MetaMcpServerID: gateway, From: new(bounds[0]), To: new(bounds[1])})
		shareable, ok := errors.AsType[*oops.ShareableError](err)
		require.True(t, ok, "%s: %v", name, err)
		require.Equal(t, oops.CodeBadRequest, shareable.Code, name)
	}
	for name, bounds := range map[string][2]string{
		"exactly 30 days": {"2026-01-01T00:00:00Z", "2026-01-31T00:00:00Z"},
		"the epoch":       {"1970-01-01T00:00:00Z", "1970-01-02T00:00:00Z"},
		"up to 2100":      {"2099-12-31T00:00:00Z", "2100-01-01T00:00:00Z"},
	} {
		_, err := ti.service.GetGatewayConnectionHealth(ctx, &gen.GetGatewayConnectionHealthPayload{MetaMcpServerID: gateway, From: new(bounds[0]), To: new(bounds[1])})
		require.NoError(t, err, name)
	}

	result, err := ti.service.GetGatewayConnectionHealth(ctx, &gen.GetGatewayConnectionHealthPayload{MetaMcpServerID: gateway, From: nil, To: nil})
	require.NoError(t, err)
	from, err := time.Parse(time.RFC3339Nano, result.From)
	require.NoError(t, err)
	to, err := time.Parse(time.RFC3339Nano, result.To)
	require.NoError(t, err)
	require.Equal(t, 7*24*time.Hour, to.Sub(from), "the default window is 7 days")
}

func TestListGatewayConnectionHotspots_RanksAndNamesOnlyVisibleKeys(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	projectID := uuid.MustParse(ti.projectID)
	sessions := usersessionsrepo.New(ti.conn)
	issuer := func(slug string) uuid.UUID {
		row, err := sessions.CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
			ProjectID:               projectID,
			OrganizationID:          conv.ToPGText(ti.orgID),
			Slug:                    slug + "-" + uuid.NewString()[:8],
			AuthnChallengeMode:      "interactive",
			SessionDuration:         pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Days: 0, Months: 0, Valid: true},
			AuthorizationServerMode: pgtype.Text{String: "", Valid: false},
			UseAuthenticationHost:   pgtype.Bool{Bool: false, Valid: false},
		})
		require.NoError(t, err)
		return row.ID
	}
	client := func(issuerID uuid.UUID, name string) string {
		row, err := sessions.CreateUserSessionClient(ctx, usersessionsrepo.CreateUserSessionClientParams{
			ClientID:                "client-" + uuid.NewString(),
			ClientSecretHash:        pgtype.Text{String: "", Valid: false},
			ClientName:              name,
			RedirectUris:            []string{"http://127.0.0.1/callback"},
			ClientSecretExpiresAt:   pgtype.Timestamptz{},
			TokenEndpointAuthMethod: "none",
			ClientJwks:              nil,
			ClientJwksUri:           pgtype.Text{String: "", Valid: false},
			UserSessionIssuerID:     issuerID,
		})
		require.NoError(t, err)
		return row.ID.String()
	}
	gatewayIssuer := issuer("gateway")
	gateway, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{OrganizationID: ti.orgID, ProjectID: projectID, Name: "Hotspot Gateway", UserSessionIssuerID: uuid.NullUUID{UUID: gatewayIssuer, Valid: true}, Visibility: visibility.Private})
	require.NoError(t, err)
	registered := client(gatewayIssuer, "Example Desktop")
	otherIssuersClient := client(issuer("other"), "Other Client")
	provider, err := remotesessionsrepo.New(ti.conn).CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID:                           conv.ToNullUUID(projectID),
		OrganizationID:                      conv.ToPGText(ti.orgID),
		Name:                                conv.ToPGTextEmpty("Example Provider"),
		Slug:                                "provider-" + uuid.NewString()[:8],
		Issuer:                              "https://provider-" + uuid.NewString()[:8] + ".example.test",
		AuthorizationEndpoint:               conv.ToPGText("https://provider.example.test/authorize"),
		TokenEndpoint:                       conv.ToPGText("https://provider.example.test/token"),
		ScopesSupported:                     []string{},
		ScopeOverride:                       []string{},
		AuthorizationGrantProfilesSupported: []string{},
		IntrospectionEndpointAuthMethodsSupported: []string{},
		IDTokenSigningAlgValuesSupported:          []string{},
		ClaimsSupported:                           []string{},
		GrantTypesSupported:                       []string{"authorization_code"},
		ResponseTypesSupported:                    []string{"code"},
		TokenEndpointAuthMethodsSupported:         []string{"client_secret_basic"},
		CodeChallengeMethodsSupported:             []string{"S256"},
	})
	require.NoError(t, err)
	memberID, _ := seedConnectedOrgUser(t, ctx, ti, "Example Member")
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	agent := func(owner, name string) string {
		row, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{OrganizationID: ti.orgID, OwnerUserID: owner, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, Name: name})
		require.NoError(t, err)
		return urn.NewAgentSubject(row.ID).String()
	}
	// The caller owns one agent and may read it; another member's agent it
	// holds no grant for.
	ownAgent := agent(authCtx.UserID, "Example Agent")
	othersAgent := agent(memberID, "Someone Else's Agent")

	member := urn.NewUserSubject(memberID).String()
	stranger := urn.NewUserSubject("user_" + uuid.NewString()).String()
	unknownProvider := uuid.NewString()
	at := time.Now().UTC().Add(-time.Hour)
	attempts := func(n int, subject, providerID, clientKey, outcome, reason string) {
		for range n {
			logConnectionAttempt(t, ctx, ti, connectionAttempt{project: ti.projectID, gateway: gateway.ID.String(), stage: telemetry.ConnectionStageDiscoveryMember, outcome: outcome, reason: reason, subject: subject, provider: providerID, client: clientKey, at: at})
		}
	}
	// A: 3 of 10 failed, one blocked. B: 2 of 2 failed. C: 1 attempt. D: 6 listed.
	attempts(6, member, provider.ID.String(), registered, "listed", "")
	attempts(3, member, provider.ID.String(), registered, "failed", "rate_limited")
	attempts(1, member, provider.ID.String(), registered, "consent_blocked", "consent_not_approved")
	attempts(2, stranger, unknownProvider, otherIssuersClient, "failed", "upstream_unavailable")
	attempts(1, ownAgent, "", "credential:agent", "failed", "listing_timeout")
	attempts(1, othersAgent, "", "credential:agent", "failed", "listing_timeout")
	attempts(6, "", "", "anonymous", "listed", "")

	rank := func(caller context.Context, sort string, minAttempts int64) []*gen.GatewayConnectionHotspot {
		t.Helper()
		result, err := ti.service.ListGatewayConnectionHotspots(caller, &gen.ListGatewayConnectionHotspotsPayload{MetaMcpServerID: gateway.ID.String(), Stage: telemetry.ConnectionStageDiscoveryMember, RankBy: sort, MinAttempts: minAttempts, Limit: 25})
		require.NoError(t, err)
		return result.Hotspots
	}
	labels := func(hotspots []*gen.GatewayConnectionHotspot) []string {
		names := []string{}
		for _, hotspot := range hotspots {
			names = append(names, hotspot.Subject.Label)
		}
		return names
	}

	byCount := rank(ctx, "count", 2)
	require.Equal(t, []string{"Example Member", "Unknown person", "No signed-in caller"}, labels(byCount), "the agents' single attempts are below the floor")
	require.Equal(t, []string{"Unknown person", "Example Member", "No signed-in caller"}, labels(rank(ctx, "rate", 2)))

	top := byCount[0]
	require.Equal(t, []int64{10, 3, 1}, []int64{top.Attempts, top.Failed, top.Blocked})
	require.Equal(t, member, *top.Subject.ID)
	require.Equal(t, []string{"Example Provider", provider.ID.String()}, []string{top.Provider.Label, *top.Provider.ID})
	require.Equal(t, []string{"Example Desktop", registered}, []string{top.Client.Label, *top.Client.ID})
	require.Equal(t, "rate_limited", top.Reasons[0].Reason)
	require.Equal(t, int64(3), top.Reasons[0].Count)

	hidden := byCount[1]
	require.Nil(t, hidden.Subject.ID, "a caller outside the organization is not identified")
	require.Equal(t, "Unknown provider", hidden.Provider.Label)
	require.Nil(t, hidden.Provider.ID)
	require.Equal(t, "Unknown client", hidden.Client.Label, "a client of another sign-in server is not named")
	require.Nil(t, hidden.Client.ID)

	// Agents are named only for a validated dashboard session, as the agents
	// pages name them, and only those the caller may read.
	agents := map[string]*gen.GatewayConnectionLabel{}
	for _, hotspot := range rank(contextvalues.WithValidatedGramSession(ctx, authCtx, false), "count", 1) {
		if hotspot.Subject.Kind == "agent" {
			agents[hotspot.Subject.Label] = hotspot.Subject
		}
	}
	require.Len(t, agents, 2)
	readable := agents["Example Agent"]
	require.NotNil(t, readable)
	require.Equal(t, ownAgent, *readable.ID)
	denied := agents["Unknown agent"]
	require.NotNil(t, denied, "an agent the caller may not read is not named")
	require.Nil(t, denied.ID)
	require.Nil(t, denied.Key)
}
