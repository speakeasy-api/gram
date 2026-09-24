//nolint:glint // Production delegation adapter regressions require tenant-scoped database fixtures.
package remotesessions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/delegation"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

// DelegationTestDatabase shares the package TestMain infrastructure with internal
// adapter tests; it is compiled only into the test binary.
var DelegationTestDatabase testenv.PostgresDBCloneFunc

// Production adapter regressions use the real repository. Private state-machine
// interleavings and in-memory CAS fixtures live with the delegation package.
type delegationAdapterFixture struct {
	db              *pgxpool.Pool
	enc             *encryption.Client
	now             func() time.Time
	loadBinding     func(context.Context, string, uuid.UUID, uuid.UUID) (*FederatedProvider, error)
	loadProvider    func(context.Context, string, uuid.UUID, uuid.UUID) (*FederatedProvider, error)
	refreshIdentity func(context.Context, *FederatedProvider, string, string, string) (*FederatedRefreshResult, error)
}

func (s *delegationAdapterFixture) service() *DelegationService {
	return &DelegationService{service: delegation.New(s.db, s.enc, delegationDependencies(s.loadBinding, s.loadProvider, s.refreshIdentity, func() time.Time { return s.now() }))}
}
func (s *delegationAdapterFixture) RetainVerifiedLogin(ctx context.Context, p *FederatedProvider, human string, i *FederatedIdentity, requested bool) error {
	return s.service().RetainVerifiedLogin(ctx, p, human, i, requested)
}
func (s *delegationAdapterFixture) Resolve(ctx context.Context, b DelegationBinding, a DelegationAuthorizer) (DelegationAssertion, error) {
	return s.service().Resolve(ctx, b, a)
}
func (s *delegationAdapterFixture) decrypt(v string) (string, error) {
	value, err := s.enc.Decrypt(v)
	if err != nil {
		return "", fmt.Errorf("decrypt delegation test credential: %w", err)
	}
	return value, nil
}

type delegationAdapterStore struct{ db *pgxpool.Pool }

func (s *delegationAdapterStore) load(ctx context.Context, b DelegationBinding) (repo.TrustedIssuerSession, error) {
	row, err := repo.New(s.db).GetTrustedDelegationCredential(ctx, repo.GetTrustedDelegationCredentialParams{OrganizationID: b.OrganizationID, ClientID: b.ClientID, IssuerID: b.IssuerID, SubjectUrn: urn.NewUserSubject(b.HumanID).String()})
	if err != nil {
		return repo.TrustedIssuerSession{}, fmt.Errorf("load delegation test credential: %w", err)
	}
	return row, nil
}

type delegationTestAuthority func(context.Context, DelegationBinding) error

func (f delegationTestAuthority) AuthorizeDelegation(ctx context.Context, b DelegationBinding) error {
	return f(ctx, b)
}
func delegationBinding(p *FederatedProvider, human string) DelegationBinding {
	return delegationProvider{p}.Binding(human)
}
func newDelegationUnitFixture(t *testing.T) (*delegationAdapterFixture, *delegationAdapterStore, *FederatedProvider, DelegationBinding, DelegationAuthorizer) {
	t.Helper()
	ctx := t.Context()
	require.NotNil(t, DelegationTestDatabase, "TestMain must provide the shared database clone factory")
	db, err := DelegationTestDatabase(t, "delegation_adapter")
	require.NoError(t, err)
	p := federatedFixture(t)
	b := delegationBinding(p, "human-test")
	_, err = db.Exec(ctx, `INSERT INTO organization_metadata (id,name,slug) VALUES ($1,'Test organization','delegation-adapter')`, b.OrganizationID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO users (id,email,display_name) VALUES ($1,'delegation@example.test','Test user')`, b.HumanID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO organization_user_relationships (organization_id,user_id) VALUES ($1,$2)`, b.OrganizationID, b.HumanID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO remote_session_issuers (id,organization_id,slug,issuer) VALUES ($1,$2,'delegation-adapter',$3)`, b.IssuerID, b.OrganizationID, p.issuer.Issuer)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO remote_session_clients (id,organization_id,remote_session_issuer_id,client_id) VALUES ($1,$2,$3,'test-client')`, b.ClientID, b.OrganizationID, b.IssuerID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO user_session_issuers (organization_id,slug,authn_challenge_mode,session_duration,trusted_remote_session_issuer_id,trusted_remote_session_client_id) VALUES ($1,'delegation-adapter','interactive',interval '1 hour',$2,$3)`, b.OrganizationID, b.IssuerID, b.ClientID)
	require.NoError(t, err)
	enc, err := encryption.NewWithBytes(bytes.Repeat([]byte{0x42}, 32))
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Microsecond)
	s := &delegationAdapterFixture{db: db, enc: enc, now: func() time.Time { return now }}
	s.loadProvider = func(_ context.Context, org string, issuer, client uuid.UUID) (*FederatedProvider, error) {
		if org != p.organizationID || issuer != p.issuer.ID || client != p.client.ID {
			return nil, ErrFederatedConfiguration
		}
		return p, nil
	}
	s.loadBinding = s.loadProvider
	s.refreshIdentity = func(context.Context, *FederatedProvider, string, string, string) (*FederatedRefreshResult, error) {
		return nil, errors.New("unexpected refresh")
	}
	allow := delegationTestAuthority(func(_ context.Context, got DelegationBinding) error {
		if got != b {
			return errors.New("not authorized")
		}
		return nil
	})
	return s, &delegationAdapterStore{db}, p, b, allow
}

func delegationLogin(p *FederatedProvider, now time.Time, id, refresh string, ttl time.Duration) *FederatedIdentity {
	return &FederatedIdentity{Issuer: p.issuer.Issuer, Subject: "secret-subject", Nonce: "secret-nonce", ExpiresAt: now.Add(ttl), credentials: &federatedCredentialState{value: EphemeralFederatedCredentials{idToken: id, refreshToken: refresh, receivedAt: now}}}
}
func delegationRenewal(p *FederatedProvider, now time.Time, id, refresh string) *FederatedRefreshResult {
	r := &FederatedRefreshResult{Credentials: FederatedRefreshCredentials{EphemeralFederatedCredentials: EphemeralFederatedCredentials{idToken: id, refreshToken: refresh, receivedAt: now}}}
	if id != "" {
		r.Identity = &FederatedIdentity{Issuer: p.issuer.Issuer, Subject: "secret-subject", ExpiresAt: now.Add(time.Hour)}
	}
	return r
}
func delegationPlain(t *testing.T, s *delegationAdapterFixture, ciphertext string) string {
	t.Helper()
	plain, err := s.decrypt(ciphertext)
	require.NoError(t, err)
	return plain
}
