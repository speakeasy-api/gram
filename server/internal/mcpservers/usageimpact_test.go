package mcpservers_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

type usageSession struct {
	subject  string
	resource string
	expired  bool
	usedAt   time.Time
}

func mintUsageSession(t *testing.T, ctx context.Context, ti *testInstance, issuerID string, projectID uuid.UUID, organizationID string, s usageSession) {
	t.Helper()

	subject, err := urn.ParseSessionSubject(s.subject)
	require.NoError(t, err)
	refreshExpires := time.Now().Add(24 * time.Hour)
	if s.expired {
		refreshExpires = time.Now().Add(-time.Hour)
	}
	resource := pgtype.Text{String: "", Valid: false}
	if s.resource != "" {
		resource = conv.ToPGText(s.resource)
	}
	jti := uuid.NewString()
	q := usersessionsrepo.New(ti.conn)
	_, err = q.CreateUserSession(ctx, usersessionsrepo.CreateUserSessionParams{
		UserSessionIssuerID: uuid.MustParse(issuerID),
		SubjectUrn:          subject,
		Jti:                 jti,
		RefreshExpiresAt:    conv.ToPGTimestamptz(refreshExpires),
		ExpiresAt:           conv.ToPGTimestamptz(refreshExpires),
		Resource:            resource,
	})
	require.NoError(t, err)
	if !s.usedAt.IsZero() {
		require.NoError(t, q.TouchUserSessionLastUsed(ctx, usersessionsrepo.TouchUserSessionLastUsedParams{
			NowTs:               conv.ToPGTimestamptz(s.usedAt),
			ProjectID:           projectID,
			OrganizationID:      organizationID,
			UserSessionIssuerID: uuid.MustParse(issuerID),
			Jti:                 jti,
			UsedCutoff:          conv.ToPGTimestamptz(s.usedAt),
		}))
	}
}

func createUsageServer(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, name string, issuer *string) string {
	t.Helper()

	remoteID := seedRemoteMcpServer(t, ctx, ti.conn, projectID).String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{Name: name, RemoteMcpServerID: &remoteID, UserSessionIssuerID: issuer, Visibility: types.McpServerVisibility("private")})
	require.NoError(t, err)
	return created.ID
}

func TestGetMcpServerUsageImpact_CountsSessionsThatReachTheServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID
	issuer := createOrganizationIssuer(t, ctx, ti, orgID)
	serverID := createUsageServer(t, ctx, ti, projectID, "usage exclusive", &issuer)
	slug := "usage-" + uuid.NewString()[:8]
	_, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: projectID, McpServerID: uuid.NullUUID{UUID: uuid.MustParse(serverID), Valid: true}, Slug: slug,
	})
	require.NoError(t, err)

	recent := time.Now().Add(-10 * time.Minute).UTC().Truncate(time.Second)
	older := recent.Add(-48 * time.Hour)
	for _, s := range []usageSession{
		{subject: "user:" + authCtx.UserID, usedAt: older},
		{subject: "user:" + authCtx.UserID, resource: "https://app.example.com/mcp/" + slug, usedAt: recent},
		{subject: "user:usage-other"},
		// Bound to another address, expired, or both: none reach this server.
		{subject: "user:usage-elsewhere", resource: "https://app.example.com/mcp/not-" + slug, usedAt: recent},
		{subject: "user:usage-expired", expired: true, usedAt: recent},
	} {
		mintUsageSession(t, ctx, ti, issuer, projectID, orgID, s)
	}

	impact, err := ti.service.GetMcpServerUsageImpact(ctx, &gen.GetMcpServerUsageImpactPayload{ID: serverID})
	require.NoError(t, err)
	require.Equal(t, "usage exclusive", impact.ServerName)
	require.True(t, impact.SignInRequired)
	require.False(t, impact.SharedSignIn)
	require.Equal(t, 3, impact.ActiveConnectionCount)
	require.Equal(t, 2, impact.ActiveSubjectCount)
	require.Equal(t, 0, impact.SharedConnectionCount)
	require.NotNil(t, impact.LastUsedAt)
	require.Equal(t, recent.Format(time.RFC3339), *impact.LastUsedAt)
	require.False(t, impact.SubjectsTruncated)
	require.Len(t, impact.Subjects, 2)

	first := impact.Subjects[0]
	require.Equal(t, "user:"+authCtx.UserID, first.SubjectUrn, "most recently used first")
	require.Equal(t, "user", first.SubjectType)
	require.Equal(t, 2, first.ConnectionCount)
	require.Equal(t, recent.Format(time.RFC3339), *first.LastUsedAt)
	require.False(t, first.SharedSignIn)
	require.Equal(t, "user:usage-other", impact.Subjects[1].SubjectUrn)
	require.Nil(t, impact.Subjects[1].LastUsedAt, "unknown use sorts last")
}

func TestGetMcpServerUsageImpact_LabelsSharedSignIn(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID
	issuer := createOrganizationIssuer(t, ctx, ti, orgID)
	serverID := createUsageServer(t, ctx, ti, projectID, "usage shared one", &issuer)
	_ = createUsageServer(t, ctx, ti, projectID, "usage shared two", &issuer)
	slug := "usage-shared-" + uuid.NewString()[:8]
	_, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: projectID, McpServerID: uuid.NullUUID{UUID: uuid.MustParse(serverID), Valid: true}, Slug: slug,
	})
	require.NoError(t, err)

	mintUsageSession(t, ctx, ti, issuer, projectID, orgID, usageSession{subject: "user:usage-shared"})
	mintUsageSession(t, ctx, ti, issuer, projectID, orgID, usageSession{subject: "user:usage-bound", resource: "https://app.example.com/mcp/" + slug})

	impact, err := ti.service.GetMcpServerUsageImpact(ctx, &gen.GetMcpServerUsageImpactPayload{ID: serverID})
	require.NoError(t, err)
	require.True(t, impact.SharedSignIn)
	require.Equal(t, 2, impact.ActiveConnectionCount)
	require.Equal(t, 1, impact.SharedConnectionCount)
	bySubject := map[string]*types.McpServerUsageSubject{}
	for _, s := range impact.Subjects {
		bySubject[s.SubjectUrn] = s
	}
	require.True(t, bySubject["user:usage-shared"].SharedSignIn, "an issuer-wide session may be using the other server")
	require.False(t, bySubject["user:usage-bound"].SharedSignIn, "a session bound to this address is certainly this server's")
}

func TestGetMcpServerUsageImpact_WithoutSignIn(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	serverID := createUsageServer(t, ctx, ti, *authCtx.ProjectID, "usage no sign-in", nil)
	remote := storedServer(t, ctx, ti, *authCtx.ProjectID, serverID).RemoteMcpServerID.UUID.String()
	_, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		ID: serverID, RemoteMcpServerID: &remote, ClearUserSessionIssuer: new(true), Visibility: types.McpServerVisibility("private"),
	})
	require.NoError(t, err)

	impact, err := ti.service.GetMcpServerUsageImpact(ctx, &gen.GetMcpServerUsageImpactPayload{ID: serverID})
	require.NoError(t, err)
	require.False(t, impact.SignInRequired)
	require.Equal(t, 0, impact.ActiveConnectionCount)
	require.Empty(t, impact.Subjects)
}

func TestGetMcpServerUsageImpact_RequiresWriteAccess(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	serverID := createUsageServer(t, ctx, ti, *authCtx.ProjectID, "usage denied", nil)

	_, err := ti.service.GetMcpServerUsageImpact(withExactAuthzGrants(t, ctx, ti.conn), &gen.GetMcpServerUsageImpactPayload{ID: serverID})
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = ti.service.GetMcpServerUsageImpact(ctx, &gen.GetMcpServerUsageImpactPayload{ID: uuid.NewString()})
	requireOopsCode(t, err, oops.CodeNotFound)
}
