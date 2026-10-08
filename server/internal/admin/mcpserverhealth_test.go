package admin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	adminserver "github.com/speakeasy-api/gram/server/gen/http/admin/server"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversRepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	oauthRepo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	toolsetsRepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

type stubHealthReader struct {
	outcomes *telemetry.MCPServerOutcomes
	series   []telemetry.MCPServerSeriesPoint
	targets  []MCPServerTelemetryTarget
	buckets  []time.Duration
}

func (r *stubHealthReader) Outcomes(_ context.Context, target MCPServerTelemetryTarget, _, _ time.Time) (*telemetry.MCPServerOutcomes, error) {
	r.targets = append(r.targets, target)
	return r.outcomes, nil
}

func (r *stubHealthReader) Series(_ context.Context, _ MCPServerTelemetryTarget, _, _ time.Time, bucket time.Duration) ([]telemetry.MCPServerSeriesPoint, error) {
	r.buckets = append(r.buckets, bucket)
	return r.series, nil
}

type healthFixture struct {
	svc       *Service
	conn      *pgxpool.Pool
	reader    *stubHealthReader
	orgID     string
	projectID uuid.UUID
}

// newHealthFixture seeds an organization and project under an id unique to the
// test, because the feature cache lives in a Redis shared across tests.
func newHealthFixture(t *testing.T, logsEnabled bool) healthFixture {
	t.Helper()

	ctx, svc, conn := newTestAdminService(t)
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	orgID := "org_health_" + suffix
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: "Health " + suffix, slug: "health-" + suffix})
	projectID := seedProject(t, ctx, conn, orgID, "health-"+suffix)
	require.NoError(t, svc.productFeatures.SetFeatureEnabled(ctx, orgID, productfeatures.FeatureLogs, logsEnabled))

	reader := &stubHealthReader{
		outcomes: &telemetry.MCPServerOutcomes{
			Success: 7, Unauthorized: 2, ClientError: 1, ServerError: 3, Blocked: 0, Failed: 1, Unknown: 0,
			Watermark: time.Date(2026, time.September, 28, 12, 0, 0, 0, time.UTC),
		},
		series: []telemetry.MCPServerSeriesPoint{
			{BucketStart: time.Date(2026, time.September, 27, 0, 0, 0, 0, time.UTC), Total: 5, Failed: 1},
			{BucketStart: time.Date(2026, time.September, 28, 0, 0, 0, 0, time.UTC), Total: 9, Failed: 3},
		},
		targets: nil,
		buckets: nil,
	}
	svc.mcpServerHealth = reader

	return healthFixture{svc: svc, conn: conn, reader: reader, orgID: orgID, projectID: projectID}
}

func (f healthFixture) describe(t *testing.T, serverID uuid.UUID, windowDays int) (*gen.AdminMcpServerHealth, error) {
	t.Helper()

	return f.svc.DescribeMcpServerHealth(t.Context(), &gen.DescribeMcpServerHealthPayload{
		AdminSessionToken: nil,
		OrganizationID:    f.orgID,
		ProjectID:         f.projectID.String(),
		McpServerID:       serverID.String(),
		WindowDays:        windowDays,
	})
}

func (f healthFixture) toolCalls(t *testing.T, serverID uuid.UUID, windowDays int) (*gen.AdminMcpServerToolCalls, error) {
	t.Helper()

	return f.svc.GetMcpServerToolCalls(t.Context(), &gen.GetMcpServerToolCallsPayload{
		AdminSessionToken: nil,
		OrganizationID:    f.orgID,
		ProjectID:         f.projectID.String(),
		McpServerID:       serverID.String(),
		WindowDays:        windowDays,
	})
}

// requireNoLeaks checks a wire body for the secrets, error text and subjects
// the fixtures plant.
func requireNoLeaks(t *testing.T, body any) {
	t.Helper()

	rendered, err := json.Marshal(body)
	require.NoError(t, err)
	for _, leaked := range []string{"secret-ciphertext", "token-ciphertext", "upstream said no", "alice", "user:"} {
		require.NotContains(t, string(rendered), leaked)
	}
}

func (f healthFixture) seedUserSessionIssuer(t *testing.T, slug string) uuid.UUID {
	t.Helper()

	issuer, err := usersessionsrepo.New(f.conn).CreateUserSessionIssuer(t.Context(), usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          f.projectID,
		OrganizationID:     pgtype.Text{String: f.orgID, Valid: true},
		Slug:               slug,
		AuthnChallengeMode: "interactive",
		SessionDuration:    conv.PtrToPGInterval(new(720 * time.Hour)),
	})
	require.NoError(t, err)
	return issuer.ID
}

// seedServerWithIssuer is a toolset-backed mcp_servers row that authenticates
// through the given issuer. uuid.Nil leaves it without one, and an empty slug
// leaves it without a slug.
func (f healthFixture) seedServerWithIssuer(t *testing.T, toolsetID, issuerID uuid.UUID, slug string) uuid.UUID {
	t.Helper()

	srv, err := mcpserversRepo.New(f.conn).CreateMCPServer(t.Context(), mcpserversRepo.CreateMCPServerParams{
		ID:                    uuid.New(),
		ProjectID:             f.projectID,
		Name:                  conv.ToPGTextEmpty(slug),
		Slug:                  conv.ToPGTextEmpty(slug),
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID:   uuid.NullUUID{UUID: issuerID, Valid: issuerID != uuid.Nil},
		RemoteMcpServerID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TunneledMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:             uuid.NullUUID{UUID: toolsetID, Valid: true},
		UnproxiedMcpServerID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:            "private",
		NetworkAccessMode:     pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	return srv.ID
}

func (f healthFixture) seedGlobalRemoteSessionIssuer(t *testing.T, slug string) uuid.UUID {
	t.Helper()

	params := remotesessionsrepo.CreateRemoteSessionIssuerParams{ //exhaustruct:ignore
		Slug:                                slug,
		Issuer:                              "https://" + slug + ".example.com",
		ScopesSupported:                     []string{},
		GrantTypesSupported:                 []string{},
		AuthorizationGrantProfilesSupported: []string{},
		ResponseTypesSupported:              []string{},
		TokenEndpointAuthMethodsSupported:   []string{},
		CodeChallengeMethodsSupported:       []string{"S256"},
		ClientIDMetadataDocumentSupported:   true,
		OmitScopeFallback:                   pgtype.Bool{Bool: true, Valid: true},
		Oidc:                                true,
		MetadataFetchedAt:                   conv.ToPGTimestamptz(time.Now()),
		MetadataLastError:                   "upstream said no",
		MetadataLastErrorUrl:                "https://" + slug + ".example.com/.well-known/oauth-authorization-server",
	}
	issuer, err := remotesessionsrepo.New(f.conn).CreateRemoteSessionIssuer(t.Context(), params)
	require.NoError(t, err)
	return issuer.ID
}

func (f healthFixture) seedRemoteSessionClient(t *testing.T, remoteIssuerID, userIssuerID uuid.UUID, clientID string, dcr bool) uuid.UUID {
	t.Helper()

	queries := remotesessionsrepo.New(f.conn)
	issuedAt := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	if dcr {
		issuedAt = conv.ToPGTimestamptz(time.Now())
	}
	client, err := queries.CreateRemoteSessionClient(t.Context(), remotesessionsrepo.CreateRemoteSessionClientParams{
		ProjectID:                       uuid.NullUUID{UUID: f.projectID, Valid: true},
		OrganizationID:                  pgtype.Text{String: "", Valid: false},
		RemoteSessionIssuerID:           remoteIssuerID,
		ClientID:                        clientID,
		ClientSecretEncrypted:           pgtype.Text{String: "secret-ciphertext", Valid: true},
		ClientIDIssuedAt:                issuedAt,
		ClientSecretExpiresAt:           pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		TokenEndpointAuthMethod:         pgtype.Text{String: "client_secret_basic", Valid: true},
		TokenEndpointAuthAudienceFormat: pgtype.Text{String: "", Valid: false},
		Scope:                           []string{"read"},
		Audience:                        pgtype.Text{String: "", Valid: false},
		LegacyCallbackUrl:               false,
		JsonWebKeySetID:                 uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		IdentityProviderConnectionID:    uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	require.NoError(t, queries.AttachRemoteSessionClientToUserSessionIssuer(t.Context(), remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: client.ID,
		UserSessionIssuerID:   userIssuerID,
	}))
	return client.ID
}

// seedRemoteSession authorizes the subject grants times; every authorization
// after the first raises grant_generation, as a fresh consent does.
func (f healthFixture) seedRemoteSession(t *testing.T, userIssuerID, clientID uuid.UUID, subject string, grants int) remotesessionsrepo.RemoteSession {
	t.Helper()

	var session remotesessionsrepo.RemoteSession
	for range grants {
		var err error
		session, err = remotesessionsrepo.New(f.conn).UpsertRemoteSession(t.Context(), remotesessionsrepo.UpsertRemoteSessionParams{
			SubjectUrn:             urn.NewUserSubject(subject),
			UserSessionIssuerID:    userIssuerID,
			RemoteSessionClientID:  clientID,
			AccessTokenEncrypted:   "token-ciphertext",
			AccessExpiresAt:        pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
			RefreshTokenEncrypted:  pgtype.Text{String: "", Valid: false},
			AuthorizationExpiresAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
			RefreshExpiresAt:       pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
			Scopes:                 []string{"read"},
			Resource:               pgtype.Text{String: "", Valid: false},
			AutoRefresh:            false,
			UpstreamSubject:        pgtype.Text{String: "", Valid: false},
			UpstreamEmail:          pgtype.Text{String: "", Valid: false},
			UpstreamDisplayName:    pgtype.Text{String: "", Valid: false},
			IdentitySource:         pgtype.Text{String: "", Valid: false},
			Enrichment:             nil,
		})
		require.NoError(t, err)
	}
	return session
}

func (f healthFixture) validateRemoteSession(t *testing.T, session remotesessionsrepo.RemoteSession, status string) {
	t.Helper()

	rows, err := remotesessionsrepo.New(f.conn).SetRemoteSessionValidation(t.Context(), remotesessionsrepo.SetRemoteSessionValidationParams{
		LastValidatedAt:       conv.ToPGTimestamptz(time.Now()),
		ValidationStatus:      status,
		ValidationReason:      pgtype.Text{String: "", Valid: false},
		ID:                    session.ID,
		SubjectUrn:            session.SubjectUrn,
		RemoteSessionClientID: session.RemoteSessionClientID,
		ExpectedUpdatedAt:     session.UpdatedAt,
		ProjectID:             f.projectID,
		OrganizationID:        f.orgID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)
}

func (f healthFixture) revokeRemoteSession(t *testing.T, session remotesessionsrepo.RemoteSession) {
	t.Helper()

	_, err := remotesessionsrepo.New(f.conn).SoftDeleteRemoteSessionBySubjectAndClient(t.Context(), remotesessionsrepo.SoftDeleteRemoteSessionBySubjectAndClientParams{
		SubjectUrn:            session.SubjectUrn,
		RemoteSessionClientID: session.RemoteSessionClientID,
		UserSessionIssuerID:   session.UserSessionIssuerID,
		ProjectID:             f.projectID,
		OrganizationID:        f.orgID,
	})
	require.NoError(t, err)
}

func (f healthFixture) seedUserSession(t *testing.T, userIssuerID uuid.UUID, subject urn.SessionSubject, live bool) uuid.UUID {
	t.Helper()

	refreshExpiresAt := time.Now().Add(time.Hour)
	if !live {
		refreshExpiresAt = time.Now().Add(-time.Hour)
	}
	session, err := usersessionsrepo.New(f.conn).CreateUserSession(t.Context(), usersessionsrepo.CreateUserSessionParams{
		UserSessionClientID:    uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		SubjectUrn:             subject,
		AuthorizerUserID:       pgtype.Text{String: "", Valid: false},
		DelegatedGrants:        nil,
		DelegatedGrantsVersion: pgtype.Int4{Int32: 0, Valid: false},
		Jti:                    uuid.NewString(),
		RefreshTokenHash:       pgtype.Text{String: "", Valid: false},
		RefreshExpiresAt:       conv.ToPGTimestamptz(refreshExpiresAt),
		ExpiresAt:              conv.ToPGTimestamptz(refreshExpiresAt),
		ToolSelection:          nil,
		UserSessionIssuerID:    userIssuerID,
	})
	require.NoError(t, err)
	return session.ID
}

// backdateUserSession moves a session's issue time before the window.
func (f healthFixture) backdateUserSession(t *testing.T, sessionID uuid.UUID, createdAt time.Time) {
	t.Helper()

	//nolint:glint // notestingrawsql: No writer backdates a session; the in-window count needs one issued before the window.
	_, err := f.conn.Exec(t.Context(), `UPDATE user_sessions SET created_at = $1 WHERE id = $2`, createdAt, sessionID)
	require.NoError(t, err)
}

func (f healthFixture) setToolsetIssuer(t *testing.T, slug string, issuerID uuid.UUID) {
	t.Helper()

	_, err := toolsetsRepo.New(f.conn).UpdateToolsetUserSessionIssuer(t.Context(), toolsetsRepo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true},
		Slug:                slug,
		ProjectID:           f.projectID,
	})
	require.NoError(t, err)
}

func requireHealthCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

func TestDescribeMcpServerHealth_IssuerWithTwoClients(t *testing.T) {
	t.Parallel()

	f := newHealthFixture(t, true)
	ctx := t.Context()
	toolsetID := seedToolset(t, ctx, f.conn, f.orgID, f.projectID, "backing", false)
	issuerID := f.seedUserSessionIssuer(t, "served-issuer")
	serverID := f.seedServerWithIssuer(t, toolsetID, issuerID, "served")

	remoteIssuerID := f.seedGlobalRemoteSessionIssuer(t, "upstream-"+uuid.NewString()[:8])
	dcrClient := f.seedRemoteSessionClient(t, remoteIssuerID, issuerID, "dcr-client", true)
	staticClient := f.seedRemoteSessionClient(t, remoteIssuerID, issuerID, "static-client", false)

	alice := f.seedRemoteSession(t, issuerID, dcrClient, "alice", 3)
	f.validateRemoteSession(t, alice, "valid")
	f.seedRemoteSession(t, issuerID, dcrClient, "bob", 1)
	carol := f.seedRemoteSession(t, issuerID, dcrClient, "carol", 2)
	f.validateRemoteSession(t, carol, "valid")
	f.revokeRemoteSession(t, carol)

	f.seedUserSession(t, issuerID, urn.NewUserSubject("alice"), true)
	old := f.seedUserSession(t, issuerID, urn.NewUserSubject("bob"), false)
	f.backdateUserSession(t, old, time.Now().Add(-40*24*time.Hour))
	f.seedUserSession(t, issuerID, urn.NewAgentSubject(uuid.New()), true)

	got, err := f.describe(t, serverID, 14)
	require.NoError(t, err)

	require.Equal(t, serverID.String(), got.Server.ID)
	require.Equal(t, "toolset", got.Server.Source)
	require.Equal(t, "private", got.Server.Visibility)
	require.Equal(t, "served", *got.Correlation.URLSlug)
	require.Equal(t, serverID.String(), *got.Correlation.McpServerID)
	require.Equal(t, "backing", *got.Correlation.ToolsetSlug)
	require.Nil(t, got.LegacyAuth, "an issuer is in force")

	issuer := got.UserSessionIssuer
	require.NotNil(t, issuer)
	require.Equal(t, issuerID.String(), issuer.ID)
	require.Equal(t, "custom", issuer.Classification)
	require.Equal(t, "interactive", issuer.AuthnChallengeMode)
	require.Equal(t, int64(720), issuer.SessionDurationHours)
	require.Equal(t, "project", issuer.AttachmentScope)
	require.Equal(t, "open", *issuer.ClientIDMetadataAdmissionMode, "new issuers are written with the resting policy")
	require.Nil(t, issuer.TrustedRemoteSession)
	require.Empty(t, issuer.OtherServersUsingIssuer)

	require.Equal(t, int64(2), issuer.Sessions.DistinctSubjectsEver, "only user subjects are counted")
	require.Equal(t, int64(1), issuer.Sessions.DistinctSubjectsInWindow)
	require.Equal(t, int64(2), issuer.Sessions.Live)
	require.NotNil(t, issuer.Sessions.FirstIssuedAt)
	require.NotNil(t, issuer.Sessions.LastIssuedAt)

	require.Len(t, issuer.RemoteSessionClients, 2)
	dcr := issuer.RemoteSessionClients[0]
	require.Equal(t, dcrClient.String(), dcr.ID)
	require.Equal(t, "dcr", dcr.Registration)
	require.Equal(t, "client_secret_basic", *dcr.TokenEndpointAuthMethod)
	require.Equal(t, []string{"read"}, dcr.Scope)
	require.Equal(t, []string{}, dcr.GrantTypes)
	require.Equal(t, "project", dcr.AttachmentScope)
	require.Equal(t, "global", dcr.Issuer.AttachmentScope)
	require.Equal(t, "public", dcr.Issuer.Networking)
	require.Equal(t, "supported", dcr.Issuer.Pkce)
	require.True(t, dcr.Issuer.CimdSupported)
	require.NotNil(t, dcr.Issuer.OmitScopeFallback)
	require.True(t, *dcr.Issuer.OmitScopeFallback)
	require.True(t, dcr.Issuer.Oidc)
	require.NotNil(t, dcr.Issuer.MetadataFetchedAt)
	require.NotNil(t, dcr.Issuer.MetadataLastErrorAt)
	require.Nil(t, dcr.Issuer.JwksLastErrorAt)
	require.Equal(t, int64(2), dcr.Sessions.LinkedSubjects, "the revoked session is not linked")
	require.Equal(t, int64(3), dcr.Sessions.Reauthorizations)
	require.NotNil(t, dcr.Sessions.FirstLinkedAt)
	require.Equal(t, map[string]int64{"valid": 1}, dcr.Sessions.ValidationStatusCounts, "never-validated and revoked sessions are left out")

	static := issuer.RemoteSessionClients[1]
	require.Equal(t, staticClient.String(), static.ID)
	require.Equal(t, "static", static.Registration)
	require.Zero(t, static.Sessions.LinkedSubjects)
	require.Nil(t, static.Sessions.FirstLinkedAt)
	require.Empty(t, static.Sessions.ValidationStatusCounts)

	require.Empty(t, f.reader.targets, "describe reads no telemetry")

	// Secrets, error text and subjects never reach either wire body.
	requireNoLeaks(t, adminserver.NewDescribeMcpServerHealthResponseBody(got))

	calls, err := f.toolCalls(t, serverID, 14)
	require.NoError(t, err)
	require.Equal(t, "logging:enabled", calls.Type)
	require.Equal(t, 14, *calls.WindowDays)
	require.Equal(t, int64(86400), *calls.BucketSeconds)
	require.Equal(t, "2026-09-28T12:00:00Z", *calls.Watermark)
	require.Equal(t, int64(7), calls.Outcomes.Success)
	require.Equal(t, int64(3), calls.Outcomes.ServerError)
	require.Len(t, calls.Daily, 2)
	require.Equal(t, "2026-09-28T00:00:00Z", calls.Daily[1].BucketStart)
	require.Equal(t, int64(9), calls.Daily[1].Total)
	require.Equal(t, []MCPServerTelemetryTarget{{
		ProjectID: f.projectID.String(), MCPServerID: serverID.String(), ToolsetSlug: "backing", URLSlug: "served",
	}}, f.reader.targets)
	requireNoLeaks(t, adminserver.NewGetMcpServerToolCallsResponseBody(calls))
}

func TestDescribeMcpServerHealth_SharedIssuerAndToolset(t *testing.T) {
	t.Parallel()

	f := newHealthFixture(t, true)
	ctx := t.Context()
	toolsetID := seedToolset(t, ctx, f.conn, f.orgID, f.projectID, "shared", false)
	issuerID := f.seedUserSessionIssuer(t, "shared-issuer")
	first := f.seedServerWithIssuer(t, toolsetID, issuerID, "first")
	second := f.seedServerWithIssuer(t, toolsetID, issuerID, "second")
	legacyToolset := seedToolset(t, ctx, f.conn, f.orgID, f.projectID, "legacy", true)
	f.setToolsetIssuer(t, "legacy", issuerID)

	got, err := f.describe(t, first, 30)
	require.NoError(t, err)

	require.Nil(t, got.Correlation.ToolsetSlug, "a toolset slug shared by two wrappers would count both")
	others := make([]string, 0, len(got.UserSessionIssuer.OtherServersUsingIssuer))
	for _, other := range got.UserSessionIssuer.OtherServersUsingIssuer {
		others = append(others, other.ID)
	}
	require.ElementsMatch(t, []string{second.String(), legacyToolset.String()}, others)

	_, err = f.toolCalls(t, first, 30)
	require.NoError(t, err)
	require.Empty(t, f.reader.targets[0].ToolsetSlug)
	require.Equal(t, []time.Duration{24 * time.Hour}, f.reader.buckets)
}

func TestDescribeMcpServerHealth_LegacyModes(t *testing.T) {
	t.Parallel()

	f := newHealthFixture(t, true)
	ctx := t.Context()

	external := seedToolset(t, ctx, f.conn, f.orgID, f.projectID, "external", true)
	metadata, err := oauthRepo.New(f.conn).CreateExternalOAuthServerMetadata(ctx, oauthRepo.CreateExternalOAuthServerMetadataParams{
		ProjectID:                 f.projectID,
		Slug:                      "external",
		Metadata:                  []byte(`{}`),
		AuthorizationServerIssuer: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	_, err = toolsetsRepo.New(f.conn).UpdateToolsetExternalOAuthServer(ctx, toolsetsRepo.UpdateToolsetExternalOAuthServerParams{
		ExternalOauthServerID: uuid.NullUUID{UUID: metadata.ID, Valid: true},
		Slug:                  "external",
		ProjectID:             f.projectID,
	})
	require.NoError(t, err)

	proxied := seedToolset(t, ctx, f.conn, f.orgID, f.projectID, "proxied", true)
	//nolint:glint // notestingrawsql: oauth_proxy_servers is retired and has no writer left; this models an existing legacy toolset.
	_, err = f.conn.Exec(ctx, `
		WITH proxy AS (INSERT INTO oauth_proxy_servers (project_id, slug) VALUES ($1, 'proxy') RETURNING id)
		UPDATE toolsets SET oauth_proxy_server_id = (SELECT id FROM proxy) WHERE id = $2`, f.projectID, proxied)
	require.NoError(t, err)

	private := seedToolset(t, ctx, f.conn, f.orgID, f.projectID, "private", true)
	public := seedToolset(t, ctx, f.conn, f.orgID, f.projectID, "public", true)
	_, err = toolsetsRepo.New(f.conn).UpdateToolset(ctx, toolsetsRepo.UpdateToolsetParams{
		Name:                   "public",
		Description:            pgtype.Text{String: "", Valid: false},
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                pgtype.Text{String: "public", Valid: true},
		McpIsPublic:            true,
		CustomDomainID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpEnabled:             true,
		ToolSelectionMode:      "static",
		Slug:                   "public",
		ProjectID:              f.projectID,
	})
	require.NoError(t, err)

	cases := map[uuid.UUID]*string{
		external: new("external_oauth"),
		proxied:  new("oauth_proxy"),
		private:  new("gram_private"),
		public:   nil,
	}
	for serverID, want := range cases {
		got, err := f.describe(t, serverID, 14)
		require.NoError(t, err)
		require.Equal(t, "toolset_only", got.Server.Source)
		require.Nil(t, got.UserSessionIssuer)
		require.Equal(t, want, got.LegacyAuth, "server %s", got.Server.Name)
		require.Nil(t, got.Correlation.McpServerID)
		require.Equal(t, got.Server.Name, *got.Correlation.ToolsetSlug)
	}
}

func TestGetMcpServerToolCalls_LoggingDisabled(t *testing.T) {
	t.Parallel()

	f := newHealthFixture(t, false)
	serverID := seedToolset(t, t.Context(), f.conn, f.orgID, f.projectID, "quiet", true)

	calls, err := f.toolCalls(t, serverID, 90)
	require.NoError(t, err)
	require.Equal(t, "logging:disabled", calls.Type)
	require.Nil(t, calls.Outcomes)
	require.Nil(t, calls.Daily)
	require.Empty(t, f.reader.targets, "telemetry is not read while logs are off")

	_, err = f.describe(t, serverID, 90)
	require.NoError(t, err, "the configuration view does not depend on logs")
}

func TestGetMcpServerToolCalls_WeeklyBucketsAtNinetyDays(t *testing.T) {
	t.Parallel()

	f := newHealthFixture(t, true)
	serverID := seedToolset(t, t.Context(), f.conn, f.orgID, f.projectID, "weekly", true)

	calls, err := f.toolCalls(t, serverID, 90)
	require.NoError(t, err)
	require.Equal(t, int64(604800), *calls.BucketSeconds)
	require.Equal(t, []time.Duration{7 * 24 * time.Hour}, f.reader.buckets)
}

func TestDescribeMcpServerHealth_ProjectInAnotherOrganizationIsNotFound(t *testing.T) {
	t.Parallel()

	f := newHealthFixture(t, true)
	serverID := seedToolset(t, t.Context(), f.conn, f.orgID, f.projectID, "theirs", true)
	otherOrg := "org_other_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	seedOrg(t, t.Context(), f.conn, orgFixture{id: otherOrg, name: "Other", slug: otherOrg})

	_, err := f.svc.DescribeMcpServerHealth(t.Context(), &gen.DescribeMcpServerHealthPayload{
		AdminSessionToken: nil,
		OrganizationID:    otherOrg,
		ProjectID:         f.projectID.String(),
		McpServerID:       serverID.String(),
		WindowDays:        14,
	})
	requireHealthCode(t, err, oops.CodeNotFound)

	_, err = f.svc.GetMcpServerToolCalls(t.Context(), &gen.GetMcpServerToolCallsPayload{
		AdminSessionToken: nil,
		OrganizationID:    otherOrg,
		ProjectID:         f.projectID.String(),
		McpServerID:       serverID.String(),
		WindowDays:        14,
	})
	requireHealthCode(t, err, oops.CodeNotFound)
	require.Empty(t, f.reader.targets)
}

func TestDescribeMcpServerHealth_ServerInAnotherProjectIsNotFound(t *testing.T) {
	t.Parallel()

	f := newHealthFixture(t, true)
	otherProject := seedProject(t, t.Context(), f.conn, f.orgID, "other-"+uuid.NewString()[:8])
	toolsetID := seedToolset(t, t.Context(), f.conn, f.orgID, otherProject, "elsewhere", false)
	serverID := seedMCPServer(t, t.Context(), f.conn, otherProject, toolsetID, "elsewhere")
	toolsetOnly := seedToolset(t, t.Context(), f.conn, f.orgID, otherProject, "elsewhere-only", true)

	_, err := f.describe(t, serverID, 14)
	requireHealthCode(t, err, oops.CodeNotFound)
	_, err = f.describe(t, toolsetOnly, 14)
	requireHealthCode(t, err, oops.CodeNotFound)
	_, err = f.describe(t, uuid.New(), 14)
	requireHealthCode(t, err, oops.CodeNotFound)
	_, err = f.toolCalls(t, serverID, 14)
	requireHealthCode(t, err, oops.CodeNotFound)
}

func TestDescribeMcpServerHealth_WindowOutsideEnumIsBadRequest(t *testing.T) {
	t.Parallel()

	f := newHealthFixture(t, true)
	serverID := seedToolset(t, t.Context(), f.conn, f.orgID, f.projectID, "window", true)

	_, err := f.describe(t, serverID, 7)
	requireHealthCode(t, err, oops.CodeBadRequest)
	_, err = f.toolCalls(t, serverID, 7)
	requireHealthCode(t, err, oops.CodeBadRequest)
}

func TestDescribeMcpServerHealth_UnknownValidationStatusFailsClosed(t *testing.T) {
	t.Parallel()

	f := newHealthFixture(t, true)
	toolsetID := seedToolset(t, t.Context(), f.conn, f.orgID, f.projectID, "strict", false)
	issuerID := f.seedUserSessionIssuer(t, "strict-issuer")
	serverID := f.seedServerWithIssuer(t, toolsetID, issuerID, "strict")
	remoteIssuerID := f.seedGlobalRemoteSessionIssuer(t, "strict-"+uuid.NewString()[:8])
	clientID := f.seedRemoteSessionClient(t, remoteIssuerID, issuerID, "strict-client", true)
	session := f.seedRemoteSession(t, issuerID, clientID, "dave", 1)
	f.validateRemoteSession(t, session, "surprising")

	_, err := f.describe(t, serverID, 14)
	requireHealthCode(t, err, oops.CodeUnexpected)
}

func TestDescribeMcpServerHealth_OmitsURLSlugWhenServerHasNone(t *testing.T) {
	t.Parallel()

	f := newHealthFixture(t, true)
	toolset, err := toolsetsRepo.New(f.conn).CreateToolset(t.Context(), toolsetsRepo.CreateToolsetParams{
		OrganizationID:         f.orgID,
		ProjectID:              f.projectID,
		Name:                   "unslugged",
		Slug:                   "unslugged",
		Description:            pgtype.Text{String: "", Valid: false},
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                pgtype.Text{String: "", Valid: false},
		McpEnabled:             false,
	})
	require.NoError(t, err)
	serverID := f.seedServerWithIssuer(t, toolset.ID, uuid.Nil, "")

	got, err := f.describe(t, serverID, 14)
	require.NoError(t, err)
	require.Nil(t, got.Correlation.URLSlug)

	_, err = f.toolCalls(t, serverID, 14)
	require.NoError(t, err)
	require.Empty(t, f.reader.targets[0].URLSlug)
}

// Hook-observed calls carry the URL the client called, so the endpoint slug
// wins over the server's own slug.
func TestDescribeMcpServerHealth_URLSlugPrefersPrimaryEndpoint(t *testing.T) {
	t.Parallel()

	f := newHealthFixture(t, true)
	toolsetID := seedToolset(t, t.Context(), f.conn, f.orgID, f.projectID, "addressed", false)
	serverID := f.seedServerWithIssuer(t, toolsetID, uuid.Nil, "server-slug")
	seedMCPEndpoint(t, t.Context(), f.conn, f.projectID, serverID, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, "endpoint-slug-"+uuid.NewString()[:8])

	got, err := f.describe(t, serverID, 14)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(*got.Correlation.URLSlug, "endpoint-slug-"))
}

// A toolset's mcp_slug addresses the toolset, not one of several wrappers of
// it, so it is no fallback for a wrapper that shares the toolset.
func TestDescribeMcpServerHealth_SharedToolsetSlugIsNoURLFallback(t *testing.T) {
	t.Parallel()

	f := newHealthFixture(t, true)
	toolsetID := seedToolset(t, t.Context(), f.conn, f.orgID, f.projectID, "fanned", false)
	first := f.seedServerWithIssuer(t, toolsetID, uuid.Nil, "")
	f.seedServerWithIssuer(t, toolsetID, uuid.Nil, "")

	got, err := f.describe(t, first, 14)
	require.NoError(t, err)
	require.Nil(t, got.Correlation.URLSlug)
	require.Nil(t, got.Correlation.ToolsetSlug)
}

// An issuer attached to another project is not visible here, so the server
// reads as having none.
func TestDescribeMcpServerHealth_IssuerOutsideProjectIsIgnored(t *testing.T) {
	t.Parallel()

	f := newHealthFixture(t, true)
	otherProject := seedProject(t, t.Context(), f.conn, f.orgID, "other-"+uuid.NewString()[:8])
	foreign, err := usersessionsrepo.New(f.conn).CreateUserSessionIssuer(t.Context(), usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          otherProject,
		OrganizationID:     pgtype.Text{String: f.orgID, Valid: true},
		Slug:               "foreign",
		AuthnChallengeMode: "interactive",
		SessionDuration:    conv.PtrToPGInterval(new(time.Hour)),
	})
	require.NoError(t, err)
	toolsetID := seedToolset(t, t.Context(), f.conn, f.orgID, f.projectID, "borrowing", false)
	serverID := f.seedServerWithIssuer(t, toolsetID, foreign.ID, "borrowing")

	got, err := f.describe(t, serverID, 14)
	require.NoError(t, err)
	require.Nil(t, got.UserSessionIssuer)
	require.Equal(t, "gram_private", *got.LegacyAuth)
}

// A remote session belongs to its subject and client; the issuer that first
// linked it is provenance. A client shared by two issuers reports the same
// sessions under both.
func TestDescribeMcpServerHealth_SharedClientCountsSessionsFromEitherIssuer(t *testing.T) {
	t.Parallel()

	f := newHealthFixture(t, true)
	toolsetID := seedToolset(t, t.Context(), f.conn, f.orgID, f.projectID, "sharing", false)
	issuerA := f.seedUserSessionIssuer(t, "issuer-a")
	issuerB := f.seedUserSessionIssuer(t, "issuer-b")
	serverID := f.seedServerWithIssuer(t, toolsetID, issuerA, "sharing")

	remoteIssuerID := f.seedGlobalRemoteSessionIssuer(t, "shared-"+uuid.NewString()[:8])
	clientID := f.seedRemoteSessionClient(t, remoteIssuerID, issuerA, "shared-client", true)
	require.NoError(t, remotesessionsrepo.New(f.conn).AttachRemoteSessionClientToUserSessionIssuer(t.Context(), remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: clientID,
		UserSessionIssuerID:   issuerB,
	}))
	session := f.seedRemoteSession(t, issuerB, clientID, "erin", 2)
	f.validateRemoteSession(t, session, "valid")

	got, err := f.describe(t, serverID, 14)
	require.NoError(t, err)
	require.Len(t, got.UserSessionIssuer.RemoteSessionClients, 1)
	sessions := got.UserSessionIssuer.RemoteSessionClients[0].Sessions
	require.Equal(t, int64(1), sessions.LinkedSubjects)
	require.Equal(t, int64(1), sessions.Reauthorizations)
	require.Equal(t, map[string]int64{"valid": 1}, sessions.ValidationStatusCounts)
}
