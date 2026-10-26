package remotesessions_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// pinnedCallbackBaseURL stands in for a callback origin recorded before the
// organization's current host, which a copied registration must keep.
const pinnedCallbackBaseURL = "https://pinned.example.com"

func newDuplicateClientPayload(id string) *orgclientsgen.DuplicateClientPayload {
	return &orgclientsgen.DuplicateClientPayload{
		SessionToken:            nil,
		ApikeyToken:             nil,
		ID:                      id,
		ClientID:                nil,
		ClientSecret:            nil,
		TokenEndpointAuthMethod: nil,
		ClientIDIssuedAt:        nil,
		ClientSecretExpiresAt:   nil,
	}
}

// storedOrganizationClient reads a client row as stored, including the
// secret ciphertext and callback origin no response carries.
func storedOrganizationClient(t *testing.T, ctx context.Context, ti *testInstance, id string) repo.RemoteSessionClient {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	row, err := repo.New(ti.conn).GetOrganizationRemoteSessionClientByID(ctx, repo.GetOrganizationRemoteSessionClientByIDParams{
		ID:             uuid.MustParse(id),
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	require.NoError(t, err)

	return row.RemoteSessionClient
}

// createOrganizationLevelSourceClient creates an organization-level client
// with a secret and explicit settings, recorded on a pinned callback origin.
func createOrganizationLevelSourceClient(t *testing.T, ctx context.Context, ti *testInstance, slug string) string {
	t.Helper()

	issuer, err := ti.service.CreateIssuer(ctx, newCreateIssuerPayload(slug, nil))
	require.NoError(t, err)

	secret := "source-secret-value"
	method := "client_secret_post"
	audience := "https://api.example.com"
	payload := newCreateClientPayload(issuer.ID, nil, &secret)
	payload.TokenEndpointAuthMethod = &method
	payload.Scope = []string{"read", "write"}
	payload.Audience = &audience
	created, err := ti.service.CreateClient(ctx, payload)
	require.NoError(t, err)

	err = repo.New(ti.conn).SetOrganizationRemoteSessionClientCallbackBaseURLFixture(ctx, repo.SetOrganizationRemoteSessionClientCallbackBaseURLFixtureParams{
		CallbackBaseUrl: conv.ToPGText(pinnedCallbackBaseURL),
		ID:              uuid.MustParse(created.ID),
		OrganizationID:  conv.ToPGText(activeOrganizationID(t, ctx)),
	})
	require.NoError(t, err)

	return created.ID
}

// TestDuplicateClient_CopiesConfigurationAndSecret proves a copy is a new
// standalone client with the source's settings, stored secret, and callback
// origin, records a duplicate audit event, and leaves the source unchanged.
func TestDuplicateClient_CopiesConfigurationAndSecret(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	sourceID := createOrganizationLevelSourceClient(t, ctx, ti, "dup-copy-issuer")
	_, err := repo.New(ti.conn).UpdateRemoteSessionClientResourceDisplay(ctx, repo.UpdateRemoteSessionClientResourceDisplayParams{
		ResourceIdentifier:    conv.ToPGText("https://mcp.example.com/mcp"),
		ResourceName:          "Example MCP",
		ResourceDocumentation: "https://mcp.example.com/docs",
		ResourcePolicyUri:     "",
		ResourceTosUri:        "",
		ID:                    uuid.MustParse(sourceID),
		ProjectID:             uuid.Nil,
		OrganizationID:        activeOrganizationID(t, ctx),
	})
	require.NoError(t, err)
	source := storedOrganizationClient(t, ctx, ti, sourceID)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientDuplicate)
	require.NoError(t, err)

	duplicate, err := ti.service.DuplicateClient(ctx, newDuplicateClientPayload(sourceID))
	require.NoError(t, err)
	require.NotEqual(t, sourceID, duplicate.ID)
	require.Equal(t, source.ClientID, duplicate.ClientID)
	require.Equal(t, source.RemoteSessionIssuerID.String(), duplicate.RemoteSessionIssuerID)
	require.Empty(t, duplicate.ProjectID, "an organization-level source yields an organization-level copy")
	require.Equal(t, []string{"read", "write"}, duplicate.Scope)
	require.NotNil(t, duplicate.Audience)
	require.Equal(t, "https://api.example.com", *duplicate.Audience)
	require.NotNil(t, duplicate.TokenEndpointAuthMethod)
	require.Equal(t, "client_secret_post", *duplicate.TokenEndpointAuthMethod)
	require.Empty(t, duplicate.UserSessionIssuerIds, "the copy has no attachments")
	require.NotNil(t, duplicate.CallbackURL)
	require.Equal(t, pinnedCallbackBaseURL+"/mcp/remote_login_callback", *duplicate.CallbackURL)

	stored := storedOrganizationClient(t, ctx, ti, duplicate.ID)
	require.True(t, stored.ClientSecretEncrypted.Valid)
	require.Equal(t, source.ClientSecretEncrypted, stored.ClientSecretEncrypted, "the copy reuses the stored secret")
	require.Equal(t, source.ClientIDIssuedAt, stored.ClientIDIssuedAt)
	require.Equal(t, source.CallbackBaseUrl, stored.CallbackBaseUrl)
	require.Equal(t, source.LegacyCallbackUrl, stored.LegacyCallbackUrl)
	require.Equal(t, source.ResourceIdentifier, stored.ResourceIdentifier)
	require.Equal(t, source.ResourceName, stored.ResourceName)
	require.Equal(t, source.ResourceDocumentation, stored.ResourceDocumentation)
	require.False(t, stored.ResourcePolicyUri.Valid)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientDuplicate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	entry, err := audittest.LatestAuditLogByActionAndSubject(ctx, ti.conn, audit.ActionRemoteSessionClientDuplicate, duplicate.ID)
	require.NoError(t, err)
	require.Equal(t, source.ClientID, entry.SubjectDisplay)
	metadata, err := audittest.DecodeAuditData(entry.Metadata)
	require.NoError(t, err)
	require.Equal(t, sourceID, metadata["source_remote_session_client_id"])
	require.Equal(t, string(audit.RemoteSessionClientDuplicateRegistrationCopied), metadata["registration"])
	snapshot, err := audittest.DecodeAuditData(entry.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, duplicate.ID, snapshot["ID"])

	unchanged := storedOrganizationClient(t, ctx, ti, sourceID)
	require.Equal(t, source.ClientSecretEncrypted, unchanged.ClientSecretEncrypted)
	require.Equal(t, source.UpdatedAt, unchanged.UpdatedAt)
}

// TestDuplicateClient_ProjectClientStaysInProject proves a project client's
// copy belongs to the same project.
func TestDuplicateClient_ProjectClientStaysInProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "dup-project-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "dup-project-usi")
	sourceID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "dup-project-client")

	source, err := ti.service.GetClient(ctx, &orgclientsgen.GetClientPayload{ID: sourceID, SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.NotEmpty(t, source.UserSessionIssuerIds)

	duplicate, err := ti.service.DuplicateClient(ctx, newDuplicateClientPayload(sourceID))
	require.NoError(t, err)
	require.Equal(t, source.ProjectID, duplicate.ProjectID)
	require.Equal(t, source.ClientID, duplicate.ClientID)
	require.Empty(t, duplicate.UserSessionIssuerIds, "the copy does not take over the source's MCP servers")
}

// TestDuplicateClient_WithRegistration proves supplied credentials replace the
// copied ones, are encrypted, and record the callback origin of a client
// created now, which is the one /oauth/proxy-register registers.
func TestDuplicateClient_WithRegistration(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	sourceID := createOrganizationLevelSourceClient(t, ctx, ti, "dup-register-issuer")
	source := storedOrganizationClient(t, ctx, ti, sourceID)

	payload := newDuplicateClientPayload(sourceID)
	payload.ClientID = new("registered-client-id")
	payload.ClientSecret = new("registered-secret-value")
	payload.TokenEndpointAuthMethod = new("client_secret_basic")
	payload.ClientSecretExpiresAt = new("2030-01-02T03:04:05Z")

	duplicate, err := ti.service.DuplicateClient(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "registered-client-id", duplicate.ClientID)
	require.NotNil(t, duplicate.TokenEndpointAuthMethod)
	require.Equal(t, "client_secret_basic", *duplicate.TokenEndpointAuthMethod)
	require.Equal(t, []string{"read", "write"}, duplicate.Scope, "settings other than credentials are still copied")
	require.NotNil(t, duplicate.ClientSecretExpiresAt)

	stored := storedOrganizationClient(t, ctx, ti, duplicate.ID)
	require.True(t, stored.ClientSecretEncrypted.Valid)
	require.NotEqual(t, "registered-secret-value", stored.ClientSecretEncrypted.String)
	require.NotEqual(t, source.ClientSecretEncrypted, stored.ClientSecretEncrypted)

	entry, err := audittest.LatestAuditLogByActionAndSubject(ctx, ti.conn, audit.ActionRemoteSessionClientDuplicate, duplicate.ID)
	require.NoError(t, err)
	metadata, err := audittest.DecodeAuditData(entry.Metadata)
	require.NoError(t, err)
	require.Equal(t, string(audit.RemoteSessionClientDuplicateRegistrationRegistered), metadata["registration"])

	fresh, err := ti.service.CreateClient(ctx, newCreateClientPayload(source.RemoteSessionIssuerID.String(), nil, nil))
	require.NoError(t, err)
	require.Equal(t, fresh.CallbackURL, duplicate.CallbackURL)
	require.NotEqual(t, pinnedCallbackBaseURL+"/mcp/remote_login_callback", *duplicate.CallbackURL)
}

// TestDuplicateClient_RefusesUpstreamRejectedCopy proves a registration the
// provider already refuses is not copied, while a fresh registration is still
// accepted.
func TestDuplicateClient_RefusesUpstreamRejectedCopy(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	sourceID := createOrganizationLevelSourceClient(t, ctx, ti, "dup-rejected-issuer")
	source := storedOrganizationClient(t, ctx, ti, sourceID)

	marked, err := repo.New(ti.conn).MarkRemoteSessionClientUpstreamRejected(ctx, repo.MarkRemoteSessionClientUpstreamRejectedParams{
		ID:       source.ID,
		ClientID: source.ClientID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), marked)

	_, err = ti.service.DuplicateClient(ctx, newDuplicateClientPayload(sourceID))
	requireOopsCode(t, err, oops.CodeBadRequest)

	payload := newDuplicateClientPayload(sourceID)
	payload.ClientID = new("replacement-client-id")
	duplicate, err := ti.service.DuplicateClient(ctx, payload)
	require.NoError(t, err)
	require.Nil(t, duplicate.UpstreamRejectedAt)
}

// TestDuplicateClient_RegistrationFieldsRequireClientID proves registration
// fields without a client_id are refused instead of silently dropped.
func TestDuplicateClient_RegistrationFieldsRequireClientID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	sourceID := createOrganizationLevelSourceClient(t, ctx, ti, "dup-orphan-secret-issuer")

	payload := newDuplicateClientPayload(sourceID)
	payload.ClientSecret = new("orphan-secret")
	_, err := ti.service.DuplicateClient(ctx, payload)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// TestDuplicateClient_MetadataDocumentClient proves a client metadata document
// client's copy gets its own hosted document as its client_id.
func TestDuplicateClient_MetadataDocumentClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createCIMDIssuer(t, ctx, ti, "dup-cimd-issuer", "https://idp.example.com/authorize", "https://idp.example.com/token")
	scope := []string{"profile"}
	source, err := ti.service.CreateCimdClient(ctx, &orgclientsgen.CreateCimdClientPayload{
		RemoteSessionIssuerID: issuerID.String(),
		Scope:                 scope,
	})
	require.NoError(t, err)

	duplicate, err := ti.service.DuplicateClient(ctx, newDuplicateClientPayload(source.ID))
	require.NoError(t, err)

	wantURL := cimdServerURL + "/.well-known/oauth-client/" + duplicate.ID
	require.NotEqual(t, source.ClientID, duplicate.ClientID)
	require.Equal(t, wantURL, duplicate.ClientID)
	require.NotNil(t, duplicate.ClientIDMetadataURI)
	require.Equal(t, wantURL, *duplicate.ClientIDMetadataURI)
	require.Equal(t, scope, duplicate.Scope)
	require.Equal(t, source.ProjectID, duplicate.ProjectID)

	payload := newDuplicateClientPayload(source.ID)
	payload.ClientID = new("not-a-document")
	_, err = ti.service.DuplicateClient(ctx, payload)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// TestDuplicateClient_RefusesManagedClient proves a client provisioned by an
// identity provider connection cannot be copied out from under it.
func TestDuplicateClient_RefusesManagedClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ti.enableCustomerManagedKeys(t, ctx, activeOrganizationID(t, ctx))

	_, fx := provisionManagedClient(t, ctx, ti, "dup-managed-issuer")

	_, err := ti.service.DuplicateClient(ctx, newDuplicateClientPayload(fx.Client.ClientRowID.String()))
	requireOopsCode(t, err, oops.CodeConflict)
}

// TestDuplicateClient_NotFound proves an unknown id is not found.
func TestDuplicateClient_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.DuplicateClient(ctx, newDuplicateClientPayload(uuid.NewString()))
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestDuplicateClient_OtherOrganizationNotFound proves another organization's
// client on a shared platform issuer cannot be copied, which would hand its
// secret to the caller's organization.
func TestDuplicateClient_OtherOrganizationNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	platformID := seedGlobalRemoteIssuer(t, ctx, ti.conn, "dup-xorg-platform")
	otherOrgID := createOrganization(t, ctx, ti.conn, "dup-xorg-other")
	otherClientID := seedOrgLevelRemoteClient(t, ctx, ti.conn, otherOrgID, platformID, "dup-xorg-client")

	_, err := ti.service.DuplicateClient(ctx, newDuplicateClientPayload(otherClientID.String()))
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestDuplicateClient_SelfClient proves a client that signs in as itself is
// copied with its owner and grant, and has no callback, while a fresh
// registration, which would request the wrong grant, is refused.
func TestDuplicateClient_SelfClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createTokenEndpointOnlyIssuer(t, ctx, ti, "dup-self-issuer", "https://tenant.example.com", "https://tenant.example.com/api/oauth/token")
	source, err := ti.service.CreateClient(ctx, newSelfCreateClientPayload(issuerID, new(oauthwire.AuthMethodClientSecretPost), new("tenant-secret"), nil))
	require.NoError(t, err)

	duplicate, err := ti.service.DuplicateClient(ctx, newDuplicateClientPayload(source.ID))
	require.NoError(t, err)
	require.Equal(t, string(remotesessions.CredentialOwnerSelf), duplicate.CredentialOwner)
	require.Equal(t, []string{oauthwire.GrantTypeClientCredentials}, duplicate.GrantTypes)
	require.NotNil(t, duplicate.TokenEndpointAuthMethod)
	require.Equal(t, oauthwire.AuthMethodClientSecretPost, *duplicate.TokenEndpointAuthMethod)
	require.Nil(t, duplicate.CallbackURL)
	require.Equal(t, storedOrganizationClient(t, ctx, ti, source.ID).ClientSecretEncrypted, storedOrganizationClient(t, ctx, ti, duplicate.ID).ClientSecretEncrypted)

	payload := newDuplicateClientPayload(source.ID)
	payload.ClientID = new("registered-self-client")
	_, err = ti.service.DuplicateClient(ctx, payload)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// TestDuplicateClient_RefusesPrivateKeyJWTClient proves a private_key_jwt
// client holding a provider-issued client_id is not copied: the provider knows
// only the source's key set address.
func TestDuplicateClient_RefusesPrivateKeyJWTClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	organizationID := activeOrganizationID(t, ctx)
	ti.enableCustomerManagedKeys(t, ctx, organizationID)

	issuerID := createRemoteIssuer(t, ctx, ti, "dup-pkjwt-issuer", "")
	setID := createJsonWebKeySet(t, ctx, ti.conn, organizationID, "dup-pkjwt-set")
	payload := newCreateClientPayload(issuerID, nil, nil)
	payload.TokenEndpointAuthMethod = new(string(remotesessions.TokenEndpointAuthMethodPrivateKeyJWT))
	payload.JSONWebKeySetID = new(setID.String())
	source, err := ti.service.CreateClient(ctx, payload)
	require.NoError(t, err)

	_, err = ti.service.DuplicateClient(ctx, newDuplicateClientPayload(source.ID))
	requireOopsCode(t, err, oops.CodeBadRequest)

	registered := newDuplicateClientPayload(source.ID)
	registered.ClientID = new("registered-pkjwt-client")
	_, err = ti.service.DuplicateClient(ctx, registered)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// TestDuplicateClient_CopiesKeySet proves a key set attached to a client that
// does not sign with it is copied too.
func TestDuplicateClient_CopiesKeySet(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	organizationID := activeOrganizationID(t, ctx)
	ti.enableCustomerManagedKeys(t, ctx, organizationID)

	issuerID := createRemoteIssuer(t, ctx, ti, "dup-keyset-issuer", "")
	setID := createJsonWebKeySet(t, ctx, ti.conn, organizationID, "dup-keyset-set")
	payload := newCreateClientPayload(issuerID, nil, new("keyset-secret"))
	payload.JSONWebKeySetID = new(setID.String())
	source, err := ti.service.CreateClient(ctx, payload)
	require.NoError(t, err)

	duplicate, err := ti.service.DuplicateClient(ctx, newDuplicateClientPayload(source.ID))
	require.NoError(t, err)
	require.NotNil(t, duplicate.JSONWebKeySetID)
	require.Equal(t, setID.String(), *duplicate.JSONWebKeySetID)
}

// TestDuplicateClient_MetadataDocumentClientKeepsAuthentication proves a
// document client signing private_key_jwt assertions is copied with its
// method and key set, and that the copy's document publishes the copy's own
// key set address.
func TestDuplicateClient_MetadataDocumentClientKeepsAuthentication(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	ti.enableCustomerManagedKeys(t, ctx, authCtx.ActiveOrganizationID)

	issuerID := createCIMDIssuer(t, ctx, ti, "dup-cimd-jwks", "https://idp.example.com/authorize", "https://idp.example.com/token")
	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "dup-cimd-jwks-usi")
	source := createCimdClient(t, ctx, ti, issuerID.String(), userIssuer.String(), nil)
	setID := createJsonWebKeySet(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "dup-cimd-jwks-set")
	attachJsonWebKeySet(t, ctx, ti, source.ID, setID)
	forceTokenEndpointAuthMethod(t, ctx, ti.conn, uuid.MustParse(source.ID), *authCtx.ProjectID, string(remotesessions.TokenEndpointAuthMethodPrivateKeyJWT))

	duplicate, err := ti.service.DuplicateClient(ctx, newDuplicateClientPayload(source.ID))
	require.NoError(t, err)
	require.NotEqual(t, source.ClientID, duplicate.ClientID)
	require.NotNil(t, duplicate.TokenEndpointAuthMethod)
	require.Equal(t, string(remotesessions.TokenEndpointAuthMethodPrivateKeyJWT), *duplicate.TokenEndpointAuthMethod)
	require.NotNil(t, duplicate.JSONWebKeySetID)
	require.Equal(t, setID.String(), *duplicate.JSONWebKeySetID)

	mgr := newCIMDChallengeManager(t, ti, cimdServerURL)
	rec := httptest.NewRecorder()
	require.NoError(t, mgr.HandleClientMetadataDocument(rec, cimdDocumentRequest(t, duplicate.ID, false)))

	var doc map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &doc))
	require.Equal(t, "private_key_jwt", doc["token_endpoint_auth_method"])
	require.Equal(t, remotesessions.ClientJSONWebKeySetURL(mustURL(t, cimdServerURL), uuid.MustParse(duplicate.ID)), doc["jwks_uri"])
}

// TestDuplicateClient_RBACForbidden proves duplicating requires org:admin.
func TestDuplicateClient_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	sourceID := createOrganizationLevelSourceClient(t, ctx, ti, "dup-rbac-issuer")

	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, activeOrganizationID(t, ctx)),
	})

	_, err := ti.service.DuplicateClient(ctx, newDuplicateClientPayload(sourceID))
	requireOopsCode(t, err, oops.CodeForbidden)
}
