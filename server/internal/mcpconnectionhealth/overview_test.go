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
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
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
}
