package mcpconnectionhealth_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_connection_health"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestGetSignInSummary_SplitsSucceededAndDidNot(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	ti.insert(t, ti.org, ti.project, ti.now.Add(-2*time.Hour), mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, mcpconnectionattempts.Grant(oauthwire.GrantTypeAuthorizationCode))
	ti.insert(t, ti.org, ti.project, ti.now.Add(-2*time.Hour), mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeRefreshTokenExpired, mcpconnectionattempts.Grant(oauthwire.GrantTypeRefreshToken))

	res, err := ti.service.GetSignInSummary(ctx, &gen.GetSignInSummaryPayload{UserSessionIssuerID: ti.issuer.String(), From: ti.rangeFrom(), To: ti.rangeTo()})
	require.NoError(t, err)
	require.Equal(t, "hour", *res.Bucket)
	var signIns, renewalsDidNot int64
	for _, b := range res.Buckets {
		signIns += b.SignIns.Succeeded
		renewalsDidNot += b.Renewals.DidNot
	}
	require.Equal(t, int64(1), signIns)
	require.Equal(t, int64(1), renewalsDidNot)
}

func TestListDeniedClients_ListsBlockedSignIns(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	ti.insert(t, ti.org, ti.project, ti.now.Add(-time.Hour), mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.CodeCIMDAdmissionDenied)
	ti.insert(t, ti.org, ti.project, ti.now.Add(-time.Hour), mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeToolPermissionDenied)

	res, err := ti.service.ListDeniedClients(ctx, &gen.ListDeniedClientsPayload{UserSessionIssuerID: ti.issuer.String(), From: ti.rangeFrom(), To: ti.rangeTo()})
	require.NoError(t, err)
	require.Len(t, res.Clients, 1)
	require.Equal(t, "cimd_admission_denied", res.Clients[0].Reason)
	require.Equal(t, int64(1), res.Clients[0].Attempts)
}

func TestGetSessionActivity_CountsSessionsPerDay(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead)
	sessions := usersessionsrepo.New(ti.conn)
	create := func() uuid.UUID {
		row, err := sessions.CreateUserSession(ctx, usersessionsrepo.CreateUserSessionParams{
			SubjectUrn:          urn.NewUserSubject(ti.userID),
			Jti:                 uuid.NewString(),
			RefreshExpiresAt:    pgtype.Timestamptz{Time: ti.now.Add(14 * 24 * time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
			ExpiresAt:           pgtype.Timestamptz{Time: ti.now.Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
			UserSessionIssuerID: ti.issuer,
		})
		require.NoError(t, err)
		return row.ID
	}
	create()
	revoked := create()
	_, err := sessions.RevokeUserSession(ctx, usersessionsrepo.RevokeUserSessionParams{ID: revoked, ProjectID: ti.project, OrganizationID: ti.org})
	require.NoError(t, err)

	res, err := ti.service.GetSessionActivity(ctx, &gen.GetSessionActivityPayload{
		UserSessionIssuerID: ti.issuer.String(),
		From:                ti.now.Add(-48 * time.Hour).Format(time.RFC3339),
		To:                  ti.now.Add(time.Minute).Format(time.RFC3339),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), res.ActiveNow)
	require.Equal(t, int64(14*24*60*60), res.ConfiguredSessionSeconds)
	require.Equal(t, int64(2), res.Days[len(res.Days)-1].ActiveSessions)
	require.Equal(t, int64(0), res.Days[0].ActiveSessions)
	require.NotNil(t, res.AverageSessionSeconds)

	// The average looks at the last 30 days whatever range the chart shows.
	older, err := ti.service.GetSessionActivity(ctx, &gen.GetSessionActivityPayload{
		UserSessionIssuerID: ti.issuer.String(),
		From:                ti.now.Add(-10 * 24 * time.Hour).Format(time.RFC3339),
		To:                  ti.now.Add(-9 * 24 * time.Hour).Format(time.RFC3339),
	})
	require.NoError(t, err)
	require.NotNil(t, older.AverageSessionSeconds)
	from, err := time.Parse(time.RFC3339, older.AverageWindowFrom)
	require.NoError(t, err)
	to, err := time.Parse(time.RFC3339, older.AverageWindowTo)
	require.NoError(t, err)
	require.Equal(t, 30*24*time.Hour, to.Sub(from))
	require.WithinDuration(t, time.Now(), to, time.Minute)
}

func TestGetSessionActivity_ADashboardMintEndsWithItsAccessToken(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead)
	_, err := usersessionsrepo.New(ti.conn).CreateUserSession(ctx, usersessionsrepo.CreateUserSessionParams{
		SubjectUrn:          urn.NewUserSubject(ti.userID),
		Jti:                 uuid.NewString(),
		RefreshTokenHash:    pgtype.Text{String: sessiontokens.DashboardMintRefreshTokenHashPrefix + ":" + uuid.NewString(), Valid: true},
		RefreshExpiresAt:    pgtype.Timestamptz{Time: ti.now.Add(14 * 24 * time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		ExpiresAt:           pgtype.Timestamptz{Time: ti.now.Add(-time.Minute), InfinityModifier: pgtype.Finite, Valid: true},
		UserSessionIssuerID: ti.issuer,
	})
	require.NoError(t, err)

	res, err := ti.service.GetSessionActivity(ctx, &gen.GetSessionActivityPayload{
		UserSessionIssuerID: ti.issuer.String(),
		From:                ti.now.Add(-24 * time.Hour).Format(time.RFC3339),
		To:                  ti.now.Add(time.Minute).Format(time.RFC3339),
	})
	require.NoError(t, err)
	require.Equal(t, int64(0), res.ActiveNow)
}

func TestGetSessionActivity_AnOrganizationSessionTiedToAnotherProjectIsNotCounted(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead)
	other, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Other", Slug: "other-" + uuid.NewString()[:8], OrganizationID: ti.org})
	require.NoError(t, err)
	otherGateway, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID: ti.org, ProjectID: other.ID, Name: "Elsewhere", UserSessionIssuerID: uuid.NullUUID{}, Visibility: "private", NetworkAccessMode: pgtype.Text{},
	})
	require.NoError(t, err)
	endpoints := mcpendpointsrepo.New(ti.conn)
	otherSlug, ownSlug := "elsewhere-"+uuid.NewString()[:8], "here-"+uuid.NewString()[:8]
	_, err = endpoints.CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{ProjectID: other.ID, CustomDomainID: uuid.NullUUID{}, McpServerID: uuid.NullUUID{}, MetaMcpServerID: uuid.NullUUID{UUID: otherGateway.ID, Valid: true}, Slug: otherSlug})
	require.NoError(t, err)
	_, err = endpoints.CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{ProjectID: ti.project, CustomDomainID: uuid.NullUUID{}, McpServerID: uuid.NullUUID{}, MetaMcpServerID: uuid.NullUUID{UUID: ti.server, Valid: true}, Slug: ownSlug})
	require.NoError(t, err)

	sessions := usersessionsrepo.New(ti.conn)
	issuer, err := sessions.CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID: pgtype.Text{String: ti.org, Valid: true}, Slug: "org-" + uuid.NewString()[:8], AuthnChallengeMode: "interactive",
		SessionDuration: pgtype.Interval{Microseconds: 0, Days: 14, Months: 0, Valid: true}, TrustedRemoteSessionIssuerID: uuid.NullUUID{}, TrustedRemoteSessionClientID: uuid.NullUUID{},
	})
	require.NoError(t, err)
	resources := []string{
		"", "https://example.com/mcp/" + ownSlug + "/", "urn:speakeasy:inference:" + ti.project.String(),
		"https://example.com/mcp/" + otherSlug, "https://example.com/mcp/" + otherSlug + "?x=1", "urn:speakeasy:inference:" + other.ID.String(),
	}
	for _, resource := range resources {
		_, err := sessions.CreateUserSession(ctx, usersessionsrepo.CreateUserSessionParams{
			SubjectUrn:          urn.NewUserSubject(ti.userID),
			Jti:                 uuid.NewString(),
			RefreshExpiresAt:    pgtype.Timestamptz{Time: ti.now.Add(14 * 24 * time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
			ExpiresAt:           pgtype.Timestamptz{Time: ti.now.Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
			Resource:            pgtype.Text{String: resource, Valid: resource != ""},
			UserSessionIssuerID: issuer.ID,
		})
		require.NoError(t, err)
	}

	res, err := ti.service.GetSessionActivity(ctx, &gen.GetSessionActivityPayload{
		UserSessionIssuerID: issuer.ID.String(),
		From:                ti.now.Add(-24 * time.Hour).Format(time.RFC3339),
		To:                  ti.now.Add(time.Minute).Format(time.RFC3339),
	})
	require.NoError(t, err)
	require.Equal(t, int64(3), res.ActiveNow)
	require.Equal(t, int64(3), res.Days[len(res.Days)-1].ActiveSessions)
}
