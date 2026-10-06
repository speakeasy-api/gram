package principalcredential_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func newSigner(t *testing.T) *mcpauthz.Issuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	private, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	signer, err := mcpauthz.New(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})), string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})), "https://platform.example.invalid", false)
	require.NoError(t, err)
	return signer
}

func agentCredential() principalcredential.Credential {
	project := uuid.New()
	return principalcredential.Credential{
		OrganizationID:   "org-test",
		ProjectID:        project,
		Principal:        urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString()),
		AuthorizerUserID: "user-test",
		Grants:           []authz.Grant{authz.NewGrant(authz.ScopeMCPConnect, uuid.NewString())},
	}
}

func TestMintAndValidate(t *testing.T) {
	t.Parallel()
	issuer := principalcredential.New(newSigner(t), nil)
	for name, c := range map[string]principalcredential.Credential{
		"agent": agentCredential(),
		"workload": {
			OrganizationID: "org-test", ProjectID: uuid.New(), AuthorizerUserID: "",
			Principal: urn.NewWorkloadPrincipal(uuid.New(), "assistant-trigger:"+uuid.NewString()),
			Grants:    []authz.Grant{authz.NewGrant(authz.ScopeMCPConnect, uuid.NewString())},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			raw, id, err := issuer.Mint(c)
			require.NoError(t, err)
			require.True(t, principalcredential.IsToken(raw))
			got, err := issuer.Validate(raw)
			require.NoError(t, err)
			require.Equal(t, id, got.ID)
			require.Equal(t, c.Principal, got.Credential.Principal)
			require.Equal(t, c.AuthorizerUserID, got.Credential.AuthorizerUserID)
			require.Equal(t, c.ProjectID, got.Credential.ProjectID)
		})
	}
}

func TestMintRejectsMalformedPrincipals(t *testing.T) {
	t.Parallel()
	issuer := principalcredential.New(newSigner(t), nil)
	noAuthorizer := agentCredential()
	noAuthorizer.AuthorizerUserID = ""
	workloadWithUser := agentCredential()
	workloadWithUser.Principal = urn.NewWorkloadPrincipal(uuid.New(), "subject")
	user := agentCredential()
	user.Principal = urn.NewPrincipal(urn.PrincipalTypeUser, "user-test")
	for _, c := range []principalcredential.Credential{noAuthorizer, workloadWithUser, user} {
		_, _, err := issuer.Mint(c)
		require.ErrorIs(t, err, principalcredential.ErrInvalid)
	}
}

func TestValidateRejectsOtherSignersAndTokens(t *testing.T) {
	t.Parallel()
	raw, _, err := principalcredential.New(newSigner(t), nil).Mint(agentCredential())
	require.NoError(t, err)
	_, err = principalcredential.New(newSigner(t), nil).Validate(raw)
	require.ErrorIs(t, err, principalcredential.ErrInvalid, "a token from another signing key is rejected")

	other, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "agent:x"}).SignedString([]byte("secret"))
	require.NoError(t, err)
	_, err = principalcredential.New(newSigner(t), nil).Validate(other)
	require.ErrorIs(t, err, principalcredential.ErrNotCredential)
}

func TestLegacyRuntimeTokenValidatorRejectsPrincipalCredentials(t *testing.T) {
	t.Parallel()
	raw, _, err := principalcredential.New(newSigner(t), nil).Mint(agentCredential())
	require.NoError(t, err)
	_, err = assistanttokens.New("secret", nil, nil).Validate(raw)
	require.Error(t, err, "an RS256 principal credential never passes the HS256 assistant runtime token validator")
}
