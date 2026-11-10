package identityproviderconnections_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/identity_provider_connections"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections"
	"github.com/speakeasy-api/gram/server/internal/oktaapplications"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/okta"
	"github.com/stretchr/testify/require"
)

// bindingFactory exercises the production credential lease while retaining the
// fixture client's programmable token observations and subsequent read failures.
type bindingFactory struct{ okta.ClientFactory }

func (f bindingFactory) Client(cfg okta.Config) (okta.Client, error) {
	if cfg.AuthMethod != remotesessions.TokenEndpointAuthMethodBasic && cfg.Credentials != nil {
		return nil, errors.New("private-key client must not acquire a credential lease")
	}
	c, err := f.ClientFactory.Client(cfg)
	if err != nil {
		return nil, fmt.Errorf("binding test client: %w", err)
	}
	return bindingClient{Client: c, cfg: cfg}, nil
}

type bindingClient struct {
	okta.Client
	cfg okta.Config
}

func (c bindingClient) VerifyScopes(ctx context.Context, scopes []string) (*okta.ScopeVerification, error) {
	result, err := c.Client.VerifyScopes(ctx, scopes)
	if err != nil {
		return nil, fmt.Errorf("binding test scopes: %w", err)
	}
	if c.cfg.Credentials == nil {
		return result, nil
	}
	lease, err := c.cfg.Credentials.Acquire(ctx, c.cfg)
	if err != nil {
		return nil, fmt.Errorf("binding test client: %w", err)
	}
	defer lease.Close()
	if err := lease.Observe(ctx, result.DPoPBound); err != nil {
		return nil, fmt.Errorf("binding test client: %w", err)
	}
	return result, nil
}
func newBindingService(t *testing.T) (context.Context, *serviceInstance) {
	t.Helper()
	return newTestServiceWithClientFactory(t, nil, 0, func(base okta.ClientFactory) okta.ClientFactory { return bindingFactory{ClientFactory: base} })
}
func rejectBindingReads(si *serviceInstance) {
	si.oktaFakes.Fake(fullOrgURL).SetMethodError("ListApps", &okta.APIError{StatusCode: http.StatusUnauthorized, ErrorCode: "invalid_client"})
}

func TestSubmitClientID_RolledBackBindingDoesNotPinDifferentClient(t *testing.T) {
	t.Parallel()
	ctx, si := newBindingService(t)
	created := createOINConnection(t, ctx, si)
	rejectBindingReads(si)
	_, err := submitOIN(ctx, si, created.ID, oinSecret)
	require.Error(t, err)
	require.Empty(t, storedOINSecret(t, ctx, si, created.ID))
	fetched, err := si.svc.Get(ctx, &gen.GetPayload{ID: &created.ID})
	require.NoError(t, err)
	require.False(t, fetched.Connection.DpopRequired, "a rolled-back submit must not pin the placeholder")
	fake := si.oktaFakes.Fake(fullOrgURL)
	fake.SetMethodError("ListApps", nil)
	fake.SetBearerOnly(true)
	retried, err := si.svc.SubmitClientID(ctx, &gen.SubmitClientIDPayload{ID: created.ID, ClientID: "0oaotherclient000001", ClientSecret: new(oinOtherSecret)})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusVerified, retried.Status)
	require.False(t, retried.DpopRequired)
}

func TestReplaceClientSecret_RolledBackBindingPreservesSecretAndPin(t *testing.T) {
	t.Parallel()
	ctx, si := newBindingService(t)
	fake := si.oktaFakes.Fake(fullOrgURL)
	fake.SetBearerOnly(true)
	verified := verifiedOINConnection(t, ctx, si)
	require.False(t, verified.DpopRequired)
	fake.SetBearerOnly(false)
	fake.RequireClientSecret(oinOtherSecret, si.enc)
	rejectBindingReads(si)
	_, err := si.svc.ReplaceClientSecret(ctx, &gen.ReplaceClientSecretPayload{ID: verified.ID, ClientSecret: oinOtherSecret})
	require.Error(t, err)
	require.Equal(t, oinSecret, storedOINSecret(t, ctx, si, verified.ID), "failed replacement rolls back the secret")
	fetched, err := si.svc.Get(ctx, &gen.GetPayload{ID: &verified.ID})
	require.NoError(t, err)
	require.True(t, fetched.Connection.DpopRequired, "same client's binding observation survives rollback")
	fake.SetMethodError("ListApps", nil)
	fake.SetBearerOnly(true)
	fake.RequireClientSecret(oinSecret, si.enc)
	retried, err := si.svc.Verify(ctx, &gen.VerifyPayload{ID: verified.ID})
	require.NoError(t, err)
	require.Contains(t, retried.VerificationReasons, identityproviderconnections.ReasonDPoPNotBound)
}

func TestVerify_PrivateKeyBindingReflectsCurrentObservation(t *testing.T) {
	t.Parallel()
	ctx, si := newBindingService(t)
	verified := verifiedConnection(t, ctx, si)
	require.True(t, verified.DpopRequired)
	si.oktaFakes.Fake(fullOrgURL).SetBearerOnly(true)
	retried, err := si.svc.Verify(ctx, &gen.VerifyPayload{ID: verified.ID})
	require.NoError(t, err)
	require.False(t, retried.DpopRequired, "private-key checklist records the current observation, not a sticky secret pin")
	require.Contains(t, retried.VerificationReasons, identityproviderconnections.ReasonDPoPNotBound)
}

func TestSync_PrivateKeyDoesNotAcquireCredentialLease(t *testing.T) {
	t.Parallel()
	ctx, si := newBindingService(t)
	verified := verifiedConnection(t, ctx, si)
	id := mustParseUUID(t, verified.ID)
	syncer := oktaapplications.NewSyncer(testenv.NewLogger(t), testenv.NewMeterProvider(t), si.conn.conn, bindingFactory{ClientFactory: si.oktaFakes})
	require.NoError(t, syncer.Run(ctx, id, false))
	run, err := oktaapplications.LatestRun(ctx, si.conn.conn, si.orgID, id)
	require.NoError(t, err)
	require.NotNil(t, run)
	require.Equal(t, "succeeded", run.Status)
}
