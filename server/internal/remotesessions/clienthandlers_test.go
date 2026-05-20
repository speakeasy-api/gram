package remotesessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	issuersgen "github.com/speakeasy-api/gram/server/gen/remote_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	environmentsrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	oauthrepo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func createUserSessionIssuer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, slug string) uuid.UUID {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuer, err := usersessionsrepo.New(conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          *authCtx.ProjectID,
		Slug:               slug,
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
	})
	require.NoError(t, err)
	return issuer.ID
}

func createRemoteIssuer(t *testing.T, ctx context.Context, svc *remoteServiceUnderTest, slug, regEndpoint string) string {
	t.Helper()
	authEP := "https://idp.example.com/authorize"
	tokenEP := "https://idp.example.com/token"
	regEP := regEndpoint
	created, err := svc.service.CreateRemoteSessionIssuer(ctx, &issuersgen.CreateRemoteSessionIssuerPayload{
		Slug:                              slug,
		Issuer:                            "https://idp.example.com",
		AuthorizationEndpoint:             &authEP,
		TokenEndpoint:                     &tokenEP,
		RegistrationEndpoint:              &regEP,
		JwksURI:                           nil,
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
		Oidc:                              nil,
		Passthrough:                       nil,
	})
	require.NoError(t, err)
	return created.ID
}

type remoteServiceUnderTest = testInstance

func TestCreateRemoteSessionClient_Manual(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-manual", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-manual").String()

	clientID := "manual-client-id"
	clientSecret := "manual-client-secret"

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientCreate)
	require.NoError(t, err)

	result, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerID:   userIssuerID,
		ClientID:              clientID,
		ClientSecret:          &clientSecret,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, clientID, result.ClientID)
	require.Equal(t, issuerID, result.RemoteSessionIssuerID)
	require.Equal(t, userIssuerID, result.UserSessionIssuerID)
	require.NotEmpty(t, result.ID)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestCreateRemoteSessionClient_Manual_WithAuthMethodPost(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-post", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-post").String()

	clientID := "post-client-id"
	clientSecret := "post-client-secret"
	authMethod := "client_secret_post"

	result, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID:   issuerID,
		UserSessionIssuerID:     userIssuerID,
		ClientID:                clientID,
		ClientSecret:            &clientSecret,
		TokenEndpointAuthMethod: &authMethod,
		SessionToken:            nil,
		ApikeyToken:             nil,
		ProjectSlugInput:        nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result.TokenEndpointAuthMethod)
	require.Equal(t, "client_secret_post", *result.TokenEndpointAuthMethod)

	// Round-trip via Get to confirm the column survives a read after the
	// transaction closes.
	fetched, err := ti.service.GetRemoteSessionClient(ctx, &clientsgen.GetRemoteSessionClientPayload{
		ID:               result.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, fetched.TokenEndpointAuthMethod)
	require.Equal(t, "client_secret_post", *fetched.TokenEndpointAuthMethod)
}

func TestCreateRemoteSessionClient_Manual_AuthMethodOmittedStaysNil(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-nil", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-nil").String()

	clientID := "nil-client-id"
	result, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID:   issuerID,
		UserSessionIssuerID:     userIssuerID,
		ClientID:                clientID,
		ClientSecret:            nil,
		TokenEndpointAuthMethod: nil,
		SessionToken:            nil,
		ApikeyToken:             nil,
		ProjectSlugInput:        nil,
	})
	require.NoError(t, err)
	// NULL in storage surfaces as a nil pointer; runtime resolves to
	// client_secret_basic via resolveClientAuthMethod, but the API surface
	// preserves the unset state.
	require.Nil(t, result.TokenEndpointAuthMethod)
}

func TestCreateRemoteSessionClient_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-rbac", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-rbac").String()
	clientID := "rbac-client-id"

	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeProjectRead,
		Selector: authz.NewSelector(authz.ScopeProjectRead, authCtx.ProjectID.String()),
	})

	_, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerID:   userIssuerID,
		ClientID:              clientID,
		ClientSecret:          nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestGetRemoteSessionClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-get", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-get").String()
	clientID := "get-client-id"

	created, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerID:   userIssuerID,
		ClientID:              clientID,
		ClientSecret:          nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)

	fetched, err := ti.service.GetRemoteSessionClient(ctx, &clientsgen.GetRemoteSessionClientPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, fetched.ID)
	require.Equal(t, clientID, fetched.ClientID)

	_, err = ti.service.GetRemoteSessionClient(ctx, &clientsgen.GetRemoteSessionClientPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestListRemoteSessionClients(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-list", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-list").String()

	for _, c := range []string{"list-client-1", "list-client-2"} {
		clientID := c
		_, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
			RemoteSessionIssuerID: issuerID,
			UserSessionIssuerID:   userIssuerID,
			ClientID:              clientID,
			ClientSecret:          nil,
			SessionToken:          nil,
			ApikeyToken:           nil,
			ProjectSlugInput:      nil,
		})
		require.NoError(t, err)
	}

	result, err := ti.service.ListRemoteSessionClients(ctx, &clientsgen.ListRemoteSessionClientsPayload{
		RemoteSessionIssuerID: &issuerID,
		UserSessionIssuerID:   nil,
		Cursor:                nil,
		Limit:                 nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(result.Items), 2)
	for _, item := range result.Items {
		require.Equal(t, issuerID, item.RemoteSessionIssuerID)
	}
}

func TestListRemoteSessionClients_PaginationTraversal(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-page", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-page").String()

	const total = 5
	wantIDs := make(map[string]bool, total)
	for range total {
		id := createRemoteClient(t, ctx, ti, issuerID, userIssuerID, uuid.NewString())
		wantIDs[id] = true
	}

	pageSize := 2
	gotIDs := make(map[string]bool, total)
	var cursor *string
	pages := 0
	for {
		pages++
		require.Less(t, pages, 10, "pagination did not terminate")
		result, err := ti.service.ListRemoteSessionClients(ctx, &clientsgen.ListRemoteSessionClientsPayload{
			RemoteSessionIssuerID: &issuerID,
			UserSessionIssuerID:   nil,
			Cursor:                cursor,
			Limit:                 &pageSize,
			SessionToken:          nil,
			ApikeyToken:           nil,
			ProjectSlugInput:      nil,
		})
		require.NoError(t, err)
		for _, item := range result.Items {
			require.False(t, gotIDs[item.ID], "duplicate id across pages: %s", item.ID)
			gotIDs[item.ID] = true
		}
		if result.NextCursor == nil {
			break
		}
		cursor = result.NextCursor
	}
	for id := range wantIDs {
		require.True(t, gotIDs[id], "client %s missing from paginated traversal", id)
	}
}

func TestUpdateRemoteSessionClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-update", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-update").String()
	otherUserIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-update-2").String()
	clientID := "update-client-id"

	created, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerID:   userIssuerID,
		ClientID:              clientID,
		ClientSecret:          nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientUpdate)
	require.NoError(t, err)

	newSecret := "rotated-secret"
	updated, err := ti.service.UpdateRemoteSessionClient(ctx, &clientsgen.UpdateRemoteSessionClientPayload{
		ID:                      created.ID,
		ClientSecret:            &newSecret,
		UserSessionIssuerID:     &otherUserIssuerID,
		TokenEndpointAuthMethod: nil,
		SessionToken:            nil,
		ApikeyToken:             nil,
		ProjectSlugInput:        nil,
	})
	require.NoError(t, err)
	require.Equal(t, otherUserIssuerID, updated.UserSessionIssuerID)
	require.Equal(t, created.ClientID, updated.ClientID)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestUpdateRemoteSessionClient_SwitchAuthMethod(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-switch", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-switch").String()
	clientID := "switch-client-id"

	// Start with default (NULL) auth method.
	created, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID:   issuerID,
		UserSessionIssuerID:     userIssuerID,
		ClientID:                clientID,
		ClientSecret:            nil,
		TokenEndpointAuthMethod: nil,
		SessionToken:            nil,
		ApikeyToken:             nil,
		ProjectSlugInput:        nil,
	})
	require.NoError(t, err)
	require.Nil(t, created.TokenEndpointAuthMethod)

	post := "client_secret_post"
	updated, err := ti.service.UpdateRemoteSessionClient(ctx, &clientsgen.UpdateRemoteSessionClientPayload{
		ID:                      created.ID,
		ClientSecret:            nil,
		UserSessionIssuerID:     nil,
		TokenEndpointAuthMethod: &post,
		SessionToken:            nil,
		ApikeyToken:             nil,
		ProjectSlugInput:        nil,
	})
	require.NoError(t, err)
	require.NotNil(t, updated.TokenEndpointAuthMethod)
	require.Equal(t, "client_secret_post", *updated.TokenEndpointAuthMethod)
}

func TestDeleteRemoteSessionClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-delete", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-delete").String()
	clientID := "delete-client-id"

	created, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerID:   userIssuerID,
		ClientID:              clientID,
		ClientSecret:          nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)

	clientUUID, err := uuid.Parse(created.ID)
	require.NoError(t, err)
	userIssuerUUID, err := uuid.Parse(userIssuerID)
	require.NoError(t, err)

	_, err = repo.New(ti.conn).InsertRemoteSession(ctx, repo.InsertRemoteSessionParams{
		SubjectUrn:            urn.NewUserSubject("test-principal"),
		UserSessionIssuerID:   userIssuerUUID,
		RemoteSessionClientID: clientUUID,
		AccessTokenEncrypted:  "ciphertext",
		AccessExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientDelete)
	require.NoError(t, err)

	err = ti.service.DeleteRemoteSessionClient(ctx, &clientsgen.DeleteRemoteSessionClientPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	// Get should now miss.
	_, err = ti.service.GetRemoteSessionClient(ctx, &clientsgen.GetRemoteSessionClientPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)

	activeSessions, err := repo.New(ti.conn).CountActiveRemoteSessionsByClientID(ctx, clientUUID)
	require.NoError(t, err)
	require.Equal(t, int64(0), activeSessions)
}

// withAdmin returns ctx with the auth context's IsAdmin flag flipped to true.
// Tests for admin-only endpoints opt in explicitly so non-admin paths exercise
// the realistic default produced by testenv.InitAuthContext.
func withAdmin(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	authCtx.IsAdmin = true
	return contextvalues.SetAuthContext(ctx, authCtx)
}

// insertProxyProvider seeds an oauth_proxy_server + oauth_proxy_provider row
// with the supplied secrets JSONB for the clone tests.
func insertProxyProvider(t *testing.T, ctx context.Context, conn *pgxpool.Pool, slug, providerType string, secrets []byte) uuid.UUID {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	q := oauthrepo.New(conn)
	srv, err := q.UpsertOAuthProxyServer(ctx, oauthrepo.UpsertOAuthProxyServerParams{
		ProjectID: *authCtx.ProjectID,
		Slug:      "srv-" + slug,
		Audience:  conv.ToPGText("https://example.com"),
	})
	require.NoError(t, err)

	prov, err := q.UpsertOAuthProxyProvider(ctx, oauthrepo.UpsertOAuthProxyProviderParams{
		ProjectID:                         *authCtx.ProjectID,
		OauthProxyServerID:                srv.ID,
		Slug:                              slug,
		ProviderType:                      providerType,
		AuthorizationEndpoint:             conv.ToPGText("https://idp.example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://idp.example.com/token"),
		RegistrationEndpoint:              conv.ToPGText("https://idp.example.com/register"),
		ScopesSupported:                   []string{"openid"},
		ResponseTypesSupported:            []string{"code"},
		ResponseModesSupported:            []string{"query"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
		SecurityKeyNames:                  []string{},
		Secrets:                           secrets,
	})
	require.NoError(t, err)
	return prov.ID
}

func TestCloneClientFromOAuthProxyProvider_HappyPath(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	secrets := []byte(`{"client_id":"upstream-cid","client_secret":"upstream-shhh"}`)
	proxyProviderID := insertProxyProvider(t, ctx, ti.conn, "clone-happy", "custom", secrets)
	issuerID := createRemoteIssuer(t, ctx, ti, "clone-happy", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "clone-happy").String()

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientCreate)
	require.NoError(t, err)

	result, err := ti.service.CloneClientFromOAuthProxyProvider(ctx, &clientsgen.CloneClientFromOAuthProxyProviderPayload{
		OauthProxyProviderID:  proxyProviderID.String(),
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerID:   userIssuerID,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.Equal(t, "upstream-cid", result.ClientID, "preserves upstream client_id so existing registrations keep working")
	require.Equal(t, issuerID, result.RemoteSessionIssuerID)
	require.Equal(t, userIssuerID, result.UserSessionIssuerID)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestCloneClientFromOAuthProxyProvider_NonAdminRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	// No withAdmin: the realistic default user is not an admin.

	secrets := []byte(`{"client_id":"upstream-cid","client_secret":"upstream-shhh"}`)
	proxyProviderID := insertProxyProvider(t, ctx, ti.conn, "clone-non-admin", "custom", secrets)
	issuerID := createRemoteIssuer(t, ctx, ti, "clone-non-admin", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "clone-non-admin").String()

	_, err := ti.service.CloneClientFromOAuthProxyProvider(ctx, &clientsgen.CloneClientFromOAuthProxyProviderPayload{
		OauthProxyProviderID:  proxyProviderID.String(),
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerID:   userIssuerID,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCloneClientFromOAuthProxyProvider_RejectsGramProvider(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	// "gram" providers don't store a usable upstream client; clone should refuse.
	secrets := []byte(`{}`)
	proxyProviderID := insertProxyProvider(t, ctx, ti.conn, "clone-gram", "gram", secrets)
	issuerID := createRemoteIssuer(t, ctx, ti, "clone-gram", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "clone-gram").String()

	_, err := ti.service.CloneClientFromOAuthProxyProvider(ctx, &clientsgen.CloneClientFromOAuthProxyProviderPayload{
		OauthProxyProviderID:  proxyProviderID.String(),
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerID:   userIssuerID,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// seedEnvironmentWithEntries creates an environment + entries via the same
// EnvironmentEntries helper the production code uses, so values land encrypted
// under the test encryption key. Returns the environment slug.
func seedEnvironmentWithEntries(t *testing.T, ctx context.Context, ti *testInstance, slug string, entries map[string]string) string {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	envRow, err := environmentsrepo.New(ti.conn).CreateEnvironment(ctx, environmentsrepo.CreateEnvironmentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           slug,
		Slug:           slug,
		Description:    pgtype.Text{},
	})
	require.NoError(t, err)

	names := make([]string, 0, len(entries))
	values := make([]string, 0, len(entries))
	for name, value := range entries {
		names = append(names, name)
		values = append(values, value)
	}
	_, err = ti.envEntries.CreateEnvironmentEntries(ctx, environmentsrepo.CreateEnvironmentEntriesParams{
		EnvironmentID: envRow.ID,
		Names:         names,
		Values:        values,
	})
	require.NoError(t, err)
	return envRow.Slug
}

func TestCloneClientFromOAuthProxyProvider_EnvBackedSecrets(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	// Operators commonly store CLIENT_ID / CLIENT_SECRET in an environment and
	// reference it from the proxy provider's secrets via environment_slug. The
	// clone path resolves these the same way the runtime OAuth proxy does so
	// cutover works for existing env-backed providers without forcing operators
	// to inline credentials first.
	envSlug := seedEnvironmentWithEntries(t, ctx, ti, "envback-ok", map[string]string{
		"CLIENT_ID":     "env-upstream-cid",
		"CLIENT_SECRET": "env-upstream-shhh",
	})
	secrets := []byte(`{"environment_slug":"` + envSlug + `"}`)
	proxyProviderID := insertProxyProvider(t, ctx, ti.conn, "clone-envback-ok", "custom", secrets)
	issuerID := createRemoteIssuer(t, ctx, ti, "clone-envback-ok", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "clone-envback-ok").String()

	result, err := ti.service.CloneClientFromOAuthProxyProvider(ctx, &clientsgen.CloneClientFromOAuthProxyProviderPayload{
		OauthProxyProviderID:  proxyProviderID.String(),
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerID:   userIssuerID,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.Equal(t, "env-upstream-cid", result.ClientID, "resolves CLIENT_ID from the linked environment case-insensitively")
}

func TestCloneClientFromOAuthProxyProvider_EnvMissingCredential(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	// Environment exists and is linked, but CLIENT_SECRET is absent. The clone
	// must surface a bad-request rather than persist a half-populated client.
	envSlug := seedEnvironmentWithEntries(t, ctx, ti, "envback-missing", map[string]string{
		"CLIENT_ID": "only-cid",
	})
	secrets := []byte(`{"environment_slug":"` + envSlug + `"}`)
	proxyProviderID := insertProxyProvider(t, ctx, ti.conn, "clone-envback-missing", "custom", secrets)
	issuerID := createRemoteIssuer(t, ctx, ti, "clone-envback-missing", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "clone-envback-missing").String()

	_, err := ti.service.CloneClientFromOAuthProxyProvider(ctx, &clientsgen.CloneClientFromOAuthProxyProviderPayload{
		OauthProxyProviderID:  proxyProviderID.String(),
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerID:   userIssuerID,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCloneClientFromOAuthProxyProvider_ProviderNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	issuerID := createRemoteIssuer(t, ctx, ti, "clone-missing", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "clone-missing").String()

	_, err := ti.service.CloneClientFromOAuthProxyProvider(ctx, &clientsgen.CloneClientFromOAuthProxyProviderPayload{
		OauthProxyProviderID:  uuid.NewString(),
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerID:   userIssuerID,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDeleteRemoteSessionClient_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientDelete)
	require.NoError(t, err)

	err = ti.service.DeleteRemoteSessionClient(ctx, &clientsgen.DeleteRemoteSessionClientPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "delete is idempotent: missing client returns success")

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount, "no audit entry when there was nothing to delete")
}
