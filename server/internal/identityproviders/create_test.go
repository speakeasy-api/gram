package identityproviders_test

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"testing"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/identity_providers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/identityproviders/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestCreateCreatesOktaConnectionAndRS256SigningKey(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	beforeAudits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionCreated)
	require.NoError(t, err)

	connection := createConnection(t, ctx, ti, "  https://Acme.Okta.com./  ")

	require.Equal(t, "okta", connection.Kind)
	require.Equal(t, "acme.okta.com", connection.TenantIdentifier)
	require.Nil(t, connection.DisplayName)
	require.Equal(t, "pending", connection.Status)
	require.Nil(t, connection.StatusDetail)
	require.Empty(t, connection.Capabilities)
	require.Empty(t, connection.GrantedScopes)
	require.NotEmpty(t, connection.SigningKeyKid)
	require.Equal(t, "https://api.example.test/.well-known/identity-provider/"+connection.ID+"/jwks.json", connection.JwksURL)
	require.NotEmpty(t, connection.CreatedAt)
	require.NotEmpty(t, connection.UpdatedAt)

	stored, err := repo.New(ti.conn).GetIdentityProviderSigningKey(ctx, repo.GetIdentityProviderSigningKeyParams{
		OrganizationID:               ti.orgID,
		IdentityProviderConnectionID: mustUUID(t, connection.ID),
	})
	require.NoError(t, err)
	require.Equal(t, connection.SigningKeyKid, stored.Kid)
	require.Equal(t, string(jose.RS256), stored.Algorithm)
	require.Equal(t, "active", stored.State)
	require.False(t, stored.Deleted)

	var publicFields map[string]any
	require.NoError(t, json.Unmarshal(stored.PublicJwk, &publicFields))
	require.Equal(t, "RSA", publicFields["kty"])
	require.Equal(t, string(jose.RS256), publicFields["alg"])
	require.Equal(t, "sig", publicFields["use"])
	require.Equal(t, stored.Kid, publicFields["kid"])
	for _, privateField := range []string{"d", "p", "q", "dp", "dq", "qi", "oth"} {
		require.NotContains(t, publicFields, privateField)
	}

	var publicJWK jose.JSONWebKey
	require.NoError(t, json.Unmarshal(stored.PublicJwk, &publicJWK))
	publicKey, ok := publicJWK.Key.(*rsa.PublicKey)
	require.True(t, ok)
	require.Equal(t, 2048, publicKey.N.BitLen())
	require.Equal(t, 65537, publicKey.E)

	plaintext, err := ti.encryption.Decrypt(stored.PrivateKeyEncrypted)
	require.NoError(t, err)
	block, rest := pem.Decode([]byte(plaintext))
	require.NotNil(t, block)
	require.Empty(t, rest)
	require.Equal(t, "PRIVATE KEY", block.Type)
	parsedPrivateKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	require.NoError(t, err)
	privateKey, ok := parsedPrivateKey.(*rsa.PrivateKey)
	require.True(t, ok)
	require.Equal(t, 2048, privateKey.N.BitLen())
	require.Equal(t, publicKey.N, privateKey.N)

	afterAudits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionCreated)
	require.NoError(t, err)
	require.Equal(t, beforeAudits+1, afterAudits)
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionCreated)
	require.NoError(t, err)
	require.Equal(t, ti.orgID, record.OrganizationID)
	require.Equal(t, connection.ID, record.SubjectID)
	require.Equal(t, "identity_provider_connection", record.SubjectType)
	require.Equal(t, "acme.okta.com", record.SubjectDisplay)
}

func TestCreateRequiresOrganizationAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.Create(ctx, &gen.CreatePayload{
		Kind:         "okta",
		TenantURL:    "https://acme.okta.com",
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCreateRejectsUnsupportedKind(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.Create(ctx, &gen.CreatePayload{
		Kind:         "other",
		TenantURL:    "https://acme.okta.com",
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateRejectsInvalidTenantURLs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	invalidURLs := []string{
		"http://acme.okta.com",
		"https://user@acme.okta.com",
		"https://acme.okta.com:443",
		"https://acme.okta.com/path",
		"https://acme.okta.com?query=value",
		"https://acme.okta.com#fragment",
		"https://127.0.0.1",
		"https://-acme.okta.com",
		"not a url",
	}

	for _, tenantURL := range invalidURLs {
		_, err := ti.service.Create(ctx, &gen.CreatePayload{
			Kind:         "okta",
			TenantURL:    tenantURL,
			SessionToken: nil,
			ApikeyToken:  nil,
		})
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
}

func TestCreateRejectsSecondConnectionForOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	createConnection(t, ctx, ti, "https://acme.okta.com")

	_, err := ti.service.Create(ctx, &gen.CreatePayload{
		Kind:         "okta",
		TenantURL:    "https://other.okta.com",
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestCreateNormalizesOktaAdminConsoleDomains(t *testing.T) {
	t.Parallel()

	cases := []struct {
		tenantURL string
		expected  string
	}{
		{tenantURL: "https://example-admin.okta.com", expected: "example.okta.com"},
		{tenantURL: "https://example-admin.oktapreview.com", expected: "example.oktapreview.com"},
		{tenantURL: "https://example-admin.okta-emea.com", expected: "example.okta-emea.com"},
		{tenantURL: "https://example-admin.example.com", expected: "example-admin.example.com"},
	}
	for _, testCase := range cases {
		ctx, ti := newTestService(t)
		connection := createConnection(t, ctx, ti, testCase.tenantURL)
		require.Equal(t, testCase.expected, connection.TenantIdentifier)
	}
}
