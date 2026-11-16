package internalsessionissuers

import (
	"context"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
)

func TestSignerAvailable(t *testing.T) {
	t.Parallel()
	origin, err := url.Parse("https://issuer.example.test")
	require.NoError(t, err)
	valid := Signer{db: new(pgxpool.Pool), gate: new(Gate), gcpIdentity: gcpauth.NewIdentity(gcpauth.NewStubResolver()), kmsClients: func(context.Context, oauth2.TokenSource) (gcpkms.SigningClient, error) { return nil, nil }, managedSigner: "signer@example.test", deployment: Deployment{ServerURL: origin, KeyRing: "projects/project/locations/global/keyRings/issuer"}}
	require.True(t, valid.Available())
	status := Status{State: StateDisabled, RolledOut: true, HasVerifiedDomain: true, Issuer: &repo.InternalSessionIssuer{OrganizationID: "org-test", Issuer: "https://issuer.example.test/oauth/idp/org/org-test"}}
	gate := NewGate(nil, nil, GateConfig{Provisioning: true})
	gate.SetEnableAuthority(valid.CheckEnableAuthority)
	require.True(t, gate.CanEnable("org-test", status), "matching disabled issuer may be enabled")
	status.Issuer.Issuer = "https://other-deployment.example.test/oauth/idp/org/org-test"
	require.False(t, gate.CanEnable("org-test", status), "copied disabled issuer must not offer enable")
	status.Issuer = nil
	status.State = StateSetupRequired
	require.True(t, gate.CanEnable("org-test", status), "new issuer may be provisioned")
	unavailable := valid
	unavailable.managedSigner = ""
	gate.SetEnableAuthority(unavailable.CheckEnableAuthority)
	require.False(t, gate.CanEnable("org-test", status), "missing signer must not offer enable")
	var absent *Signer
	require.False(t, absent.Available())
	for _, tc := range []struct {
		name   string
		change func(*Signer)
	}{
		{"missing database", func(s *Signer) { s.db = nil }},
		{"missing gate", func(s *Signer) { s.gate = nil }},
		{"missing identity", func(s *Signer) { s.gcpIdentity = nil }},
		{"missing KMS", func(s *Signer) { s.kmsClients = nil }},
		{"missing signer", func(s *Signer) { s.managedSigner = "" }},
		{"missing origin", func(s *Signer) { s.deployment.ServerURL = nil }},
		{"invalid ring", func(s *Signer) { s.deployment.KeyRing = "invalid" }},
	} {
		t.Run(tc.name, func(t *testing.T) { t.Parallel(); s := valid; tc.change(&s); require.False(t, s.Available()) })
	}
}
