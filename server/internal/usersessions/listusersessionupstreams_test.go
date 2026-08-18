package usersessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	issuersgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	gen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
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

	seedUpstream(t, ctx, ti.conn, conv.ToNullUUID(*authCtx.ProjectID), issuerID, subject, "mcp.linear.app")

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

	seedUpstream(t, ctx, ti.conn, conv.ToNullUUID(*authCtx.ProjectID), issuerID, linked, "mcp.notion.com")

	// Deliberately unfiltered: both subjects have to land on the same page for
	// this to test anything. Filtered to one subject at a time, a page holds a
	// single (subject, issuer) pair, and the cross-product join the parallel
	// arrays exist to prevent returns exactly the same rows as the correct one.
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
	require.Len(t, res.Items, 2)

	bySubject := make(map[string][]*types.UserSessionUpstream, len(res.Items))
	for _, item := range res.Items {
		bySubject[item.SubjectUrn] = item.Upstreams
	}

	require.Len(t, bySubject[linked.String()], 1,
		"the subject holding the remote_session reports their upstream")
	require.Empty(t, bySubject[unlinked.String()],
		"a subject on the same issuer with no remote_session must report no upstreams")
}

// An upstream held through a client with no project of its own — the
// organization-level and global registrations, whose project_id is NULL — still
// belongs to the project whose user_session_issuer minted the session. Scoping
// the page query on the client's project instead would drop it and report the
// brokered session as reaching nothing.
func TestListUserSessionsReturnsUpstreamsForOrgLevelClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "org-level-upstream-issuer",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	issuerID := uuid.MustParse(issuer.ID)
	subject := urn.NewUserSubject("org-level-subject")
	_, err = seedUserSession(t, ctx, ti.conn, issuerID, subject)
	require.NoError(t, err)

	seedUpstream(t, ctx, ti.conn, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, issuerID, subject, "mcp.sentry.dev")

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
	require.Len(t, res.Items[0].Upstreams, 1,
		"an upstream on a client with no project of its own still belongs to the issuer's project")
	require.Equal(t, "mcp.sentry.dev", res.Items[0].Upstreams[0].IssuerSlug)
}

// seedUpstream builds one outbound leg: the remote issuer, a client registered
// against it, the attachment binding that client to the user-session issuer,
// and the session Gram holds for the subject.
// clientProject is a NullUUID because the client and issuer it registers can be
// project-scoped, organization-level, or global; the invalid case is what the
// org-level coverage below leans on.
func seedUpstream(t *testing.T, ctx context.Context, conn *pgxpool.Pool, clientProject uuid.NullUUID, userSessionIssuerID uuid.UUID, subject urn.SessionSubject, slug string) {
	t.Helper()

	q := remotesessions_repo.New(conn)

	issuer, err := q.CreateRemoteSessionIssuer(ctx, remotesessions_repo.CreateRemoteSessionIssuerParams{
		ProjectID:                   clientProject,
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
		ProjectID:               clientProject,
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
