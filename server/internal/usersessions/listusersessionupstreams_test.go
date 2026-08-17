package usersessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	issuersgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	gen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// The outbound leg is joined on (subject_urn, user_session_issuer_id). These
// tests pin that join, because getting it wrong is not a visibly broken page —
// it is one person's upstream connections shown against another person's name.
func TestListUserSessionsReturnsUpstreams(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "upstream-issuer",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	issuerID := uuid.MustParse(issuer.ID)
	subject := urn.NewUserSubject("upstream-subject")
	_, err = seedUserSession(t, ctx, ti.conn, issuerID, subject)
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	seedUpstream(t, ctx, ti.conn, *authCtx.ProjectID, issuerID, subject, "mcp.linear.app")

	res, err := ti.service.ListUserSessions(ctx, &gen.ListUserSessionsPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		SubjectUrn:          nil,
		UserSessionIssuerID: nil,
		Status:              nil,
		ClientID:            nil,
		Cursor:              nil,
		Limit:               nil,
	})
	require.NoError(t, err)
	require.Len(t, res.Items, 1)

	upstreams := res.Items[0].Upstreams
	require.Len(t, upstreams, 1, "the subject's upstream session is attached to their inbound session")
	require.Equal(t, "mcp.linear.app", upstreams[0].IssuerSlug)
	require.True(t, upstreams[0].HasRefreshToken, "seeded with a refresh token")
	require.False(t, upstreams[0].AutoRefresh)
}

// A second subject sharing the issuer must not inherit the first subject's
// upstreams. This is the failure mode the parallel-array join exists to
// prevent: filtering subjects and issuers independently returns the cross
// product.
func TestListUserSessionsUpstreamsDoNotLeakAcrossSubjects(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "upstream-leak-issuer",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	issuerID := uuid.MustParse(issuer.ID)
	linked := urn.NewUserSubject("has-upstream")
	unlinked := urn.NewUserSubject("no-upstream")

	_, err = seedUserSession(t, ctx, ti.conn, issuerID, linked)
	require.NoError(t, err)
	_, err = seedUserSession(t, ctx, ti.conn, issuerID, unlinked)
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	seedUpstream(t, ctx, ti.conn, *authCtx.ProjectID, issuerID, linked, "mcp.notion.com")

	linkedURN := linked.String()
	res, err := ti.service.ListUserSessions(ctx, &gen.ListUserSessionsPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		SubjectUrn:          &linkedURN,
		UserSessionIssuerID: nil,
		Status:              nil,
		ClientID:            nil,
		Cursor:              nil,
		Limit:               nil,
	})
	require.NoError(t, err)
	require.Len(t, res.Items, 1)
	require.Len(t, res.Items[0].Upstreams, 1)

	unlinkedURN := unlinked.String()
	res, err = ti.service.ListUserSessions(ctx, &gen.ListUserSessionsPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		SubjectUrn:          &unlinkedURN,
		UserSessionIssuerID: nil,
		Status:              nil,
		ClientID:            nil,
		Cursor:              nil,
		Limit:               nil,
	})
	require.NoError(t, err)
	require.Len(t, res.Items, 1)
	require.Empty(t, res.Items[0].Upstreams,
		"a subject on the same issuer with no remote_session must report no upstreams")
}

// seedUpstream builds one outbound leg: the remote issuer, a client registered
// against it, the attachment binding that client to the user-session issuer,
// and the session Gram holds for the subject.
func seedUpstream(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, userSessionIssuerID uuid.UUID, subject urn.SessionSubject, slug string) {
	t.Helper()

	q := remotesessions_repo.New(conn)
	nullProject := uuid.NullUUID{UUID: projectID, Valid: true}

	issuer, err := q.CreateRemoteSessionIssuer(ctx, remotesessions_repo.CreateRemoteSessionIssuerParams{
		ProjectID:                   nullProject,
		OrganizationID:              pgtype.Text{String: "", Valid: false},
		Slug:                        slug,
		Issuer:                      "https://" + slug,
		Name:                        pgtype.Text{String: "", Valid: false},
		LogoAssetID:                 uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ClientSetupDocumentationUrl: pgtype.Text{String: "", Valid: false},
		AuthorizationEndpoint:       pgtype.Text{String: "", Valid: false},
		TokenEndpoint:               pgtype.Text{String: "", Valid: false},
		RevocationEndpoint:          pgtype.Text{String: "", Valid: false},
		RegistrationEndpoint:        pgtype.Text{String: "", Valid: false},
		JwksUri:                     pgtype.Text{String: "", Valid: false},
		ServiceDocumentation:        pgtype.Text{String: "", Valid: false},
		OpPolicyUri:                 pgtype.Text{String: "", Valid: false},
		OpTosUri:                    pgtype.Text{String: "", Valid: false},
		// NOT NULL with no default: an empty array means "the upstream
		// advertised no capability of this kind", which is the honest fixture.
		ScopesSupported:                   []string{},
		GrantTypesSupported:               []string{},
		ResponseTypesSupported:            []string{},
		TokenEndpointAuthMethodsSupported: []string{},
		ClientIDMetadataDocumentSupported: false,
		Oidc:                              false,
		Passthrough:                       false,
	})
	require.NoError(t, err)

	client, err := q.CreateRemoteSessionClient(ctx, remotesessions_repo.CreateRemoteSessionClientParams{
		ProjectID:               nullProject,
		OrganizationID:          pgtype.Text{String: "", Valid: false},
		RemoteSessionIssuerID:   issuer.ID,
		ClientID:                "client-" + slug,
		ClientSecretEncrypted:   pgtype.Text{String: "", Valid: false},
		ClientIDIssuedAt:        pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite},
		ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite},
		TokenEndpointAuthMethod: pgtype.Text{String: "", Valid: false},
		Scope:                   nil,
		Audience:                pgtype.Text{String: "", Valid: false},
		LegacyCallbackUrl:       false,
	})
	require.NoError(t, err)

	require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessions_repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: client.ID,
		UserSessionIssuerID:   userSessionIssuerID,
	}))

	_, err = q.UpsertRemoteSession(ctx, remotesessions_repo.UpsertRemoteSessionParams{
		SubjectUrn:             subject,
		UserSessionIssuerID:    userSessionIssuerID,
		RemoteSessionClientID:  client.ID,
		AccessTokenEncrypted:   "encrypted-access",
		AccessExpiresAt:        pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite},
		RefreshTokenEncrypted:  pgtype.Text{String: "encrypted-refresh", Valid: true},
		AuthorizationExpiresAt: pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite},
		RefreshExpiresAt:       pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite},
		Scopes:                 []string{"read"},
		Resource:               pgtype.Text{String: "", Valid: false},
		AutoRefresh:            false,
	})
	require.NoError(t, err)
}
