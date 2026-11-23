package usersessions_test

import (
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	gen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestCreateUserSessionIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionIssuerCreate)
	require.NoError(t, err)

	created, err := ti.service.CreateUserSessionIssuer(ctx, &gen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "issuer-one",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	require.Equal(t, "issuer-one", created.Slug)
	require.Equal(t, "chain", created.AuthnChallengeMode)
	require.Equal(t, 24, created.SessionDurationHours)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	row, err := repo.New(ti.conn).GetUserSessionIssuerByID(ctx, repo.GetUserSessionIssuerByIDParams{
		ID:        uuid.MustParse(created.ID),
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	requireOrganizationID(t, ctx, row.OrganizationID)
	require.Equal(t, "endpoint", row.AuthorizationServerMode)
	require.False(t, row.UseAuthenticationHost)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionIssuerCreate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

func TestCreateUserSessionIssuer_BadDuration(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.CreateUserSessionIssuer(ctx, &gen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "bad-duration",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 0,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateUserSessionIssuer_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Hold only project:read; create requires project:write.
	ctx = withExactAuthzGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()),
	)

	_, err := ti.service.CreateUserSessionIssuer(ctx, &gen.CreateUserSessionIssuerPayload{
		SessionToken:            nil,
		ApikeyToken:             nil,
		ProjectSlugInput:        nil,
		Slug:                    "rbac-denied",
		AuthorizationServerMode: "shared",
		AuthnChallengeMode:      "chain",
		SessionDurationHours:    24,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCreateUserSessionIssuer_Shared(t *testing.T) {
	t.Parallel()
	for _, authenticationHost := range []string{"", testAuthenticationHostURL} {
		t.Run(authenticationHost, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestServiceWithHosts(t, authserver.Hosts{ServerURL: testServerURL, AuthenticationHostBaseURL: authenticationHost, PlatformHosts: nil}, nil)
			created, err := ti.service.CreateUserSessionIssuer(ctx, &gen.CreateUserSessionIssuerPayload{
				Slug: "standalone-shared", AuthnChallengeMode: "chain", SessionDurationHours: 24, AuthorizationServerMode: "shared",
			})
			require.NoError(t, err)
			require.Equal(t, "shared", created.AuthorizationServerMode)
			require.Equal(t, authenticationHost != "", created.UseAuthenticationHost)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			row, err := repo.New(ti.conn).GetSharedUserSessionIssuerByID(ctx, uuid.MustParse(created.ID))
			require.NoError(t, err)
			require.Equal(t, *authCtx.ProjectID, row.UserSessionIssuer.ProjectID.UUID)
			require.Equal(t, "shared", row.UserSessionIssuer.AuthorizationServerMode)
			host := testServerURL
			if authenticationHost != "" {
				host = authenticationHost
			}
			shared, err := (authserver.Hosts{ServerURL: testServerURL, AuthenticationHostBaseURL: authenticationHost, PlatformHosts: nil}).SharedAuthorizationServer(row.UserSessionIssuer)
			require.NoError(t, err)
			require.Equal(t, host+"/oauth/usi/"+created.ID+"/token", shared.TokenEndpoint)
		})
	}
}

func TestCreateUserSessionIssuer_InvalidMode(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	_, err := ti.service.CreateUserSessionIssuer(ctx, &gen.CreateUserSessionIssuerPayload{Slug: "invalid-mode", AuthnChallengeMode: "chain", SessionDurationHours: 24, AuthorizationServerMode: "invalid"})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateUserSessionIssuer_SharedUnavailable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestServiceWithHosts(t, authserver.Hosts{}, nil)
	_, err := ti.service.CreateUserSessionIssuer(ctx, &gen.CreateUserSessionIssuerPayload{Slug: "unavailable-shared", AuthnChallengeMode: "chain", SessionDurationHours: 24, AuthorizationServerMode: "shared"})
	requireOopsCode(t, err, oops.CodeBadRequest)
	// Failed host validation rolls back creation; legacy endpoint mode needs no shared host.
	created, err := ti.service.CreateUserSessionIssuer(ctx, &gen.CreateUserSessionIssuerPayload{Slug: "unavailable-shared", AuthnChallengeMode: "chain", SessionDurationHours: 24, AuthorizationServerMode: "endpoint"})
	require.NoError(t, err)
	require.Equal(t, "endpoint", created.AuthorizationServerMode)
}

// Organization creation must not advertise a project-only option its handler ignores.
func TestCreateUserSessionIssuer_ModeIsProjectOnly(t *testing.T) {
	t.Parallel()
	_, projectHasMode := reflect.TypeFor[gen.CreateUserSessionIssuerPayload]().FieldByName("AuthorizationServerMode")
	require.True(t, projectHasMode)
	_, organizationHasMode := reflect.TypeFor[orggen.CreateIssuerPayload]().FieldByName("AuthorizationServerMode")
	require.False(t, organizationHasMode)
}
