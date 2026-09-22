package delegation

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"slices"
	"sync"
	"testing"
	"time"
)

// The core's fixtures implement policy facts only. OIDC parsing and credential
// consumption are covered separately through the production adapter tests.
type fixtureProvider struct {
	metadata       struct{ TokenEndpoint string }
	organizationID string
	issuer         repo.RemoteSessionIssuer
	client         repo.RemoteSessionClient
}

func federatedFixture(t *testing.T) *fixtureProvider {
	t.Helper()
	return &fixtureProvider{organizationID: "org-test", issuer: repo.RemoteSessionIssuer{ID: uuid.New(), Issuer: "https://idp.example.test/tenant"}, client: repo.RemoteSessionClient{ID: uuid.New(), ClientID: "upstream-client", Scope: []string{"openid", "email", "offline_access"}}}
}
func (p *fixtureProvider) Binding(human string) Binding {
	return Binding{OrganizationID: p.organizationID, IssuerID: p.issuer.ID, ClientID: p.client.ID, HumanID: human}
}
func (p *fixtureProvider) IssuerURL() string { return p.issuer.Issuer }
func (p *fixtureProvider) DelegationConfigurationHash() string {
	return fmt.Sprintf("%x", sha256.Sum256(fmt.Appendf(nil, "%s:%s:%s:%s", p.organizationID, p.issuer.ID, p.client.ID, p.client.ClientID)))
}
func (p *fixtureProvider) OfflineConfigurationHash() string {
	if !slices.Contains(p.client.Scope, "email") {
		return ""
	}
	return p.DelegationConfigurationHash()
}
func (p *fixtureProvider) OfflineRequested() bool {
	return slices.Contains(p.client.Scope, "offline_access")
}
func (p *fixtureProvider) OfflineSupported() bool { return p.OfflineRequested() }

type EphemeralFederatedCredentials struct {
	idToken, refreshToken string
	receivedAt            time.Time
	refreshExpiresAt      *time.Time
}

func (c EphemeralFederatedCredentials) IDToken() string              { return c.idToken }
func (c EphemeralFederatedCredentials) RefreshToken() string         { return c.refreshToken }
func (c EphemeralFederatedCredentials) ReceivedAt() time.Time        { return c.receivedAt }
func (c EphemeralFederatedCredentials) RefreshExpiresAt() *time.Time { return c.refreshExpiresAt }
func (c EphemeralFederatedCredentials) RefreshExpiresIn() int64      { return 0 }

type FederatedRefreshCredentials struct{ EphemeralFederatedCredentials }
type federatedCredentialState struct {
	mu    sync.Mutex
	value EphemeralFederatedCredentials
}
type FederatedIdentity struct {
	Issuer, Subject, Nonce string
	ExpiresAt              time.Time
	credentials            *federatedCredentialState
}

func (i *FederatedIdentity) IssuerURL() string     { return i.Issuer }
func (i *FederatedIdentity) SubjectID() string     { return i.Subject }
func (i *FederatedIdentity) NonceValue() string    { return i.Nonce }
func (i *FederatedIdentity) Expiration() time.Time { return i.ExpiresAt }
func (i *FederatedIdentity) WithCredentials(consume func(Credentials) error) error {
	if i.credentials == nil {
		return errors.New("credentials unavailable")
	}
	i.credentials.mu.Lock()
	c := i.credentials.value
	i.credentials.value = EphemeralFederatedCredentials{}
	i.credentials.mu.Unlock()
	if c.idToken == "" {
		return errors.New("credentials consumed")
	}
	return consume(c)
}
func (i *FederatedIdentity) DiscardCredentials() {
	if i.credentials != nil {
		i.credentials.mu.Lock()
		i.credentials.value = EphemeralFederatedCredentials{}
		i.credentials.mu.Unlock()
	}
}
