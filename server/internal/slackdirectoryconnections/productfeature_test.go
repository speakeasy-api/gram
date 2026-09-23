package slackdirectoryconnections_test

import (
	"context"
	"testing"
	"time"

	featuregen "github.com/speakeasy-api/gram/server/gen/features"
	gen "github.com/speakeasy-api/gram/server/gen/slack_directory_connections"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
	"github.com/stretchr/testify/require"
)

func requireConnectionGate(t *testing.T, ctx context.Context, f *fixture, code oops.Code, state string) {
	t.Helper()
	_, listErr := f.service.List(ctx, &gen.ListPayload{SessionToken: nil})
	_, beginErr := f.service.Begin(ctx, &gen.BeginPayload{SessionToken: nil, ConnectionID: nil})
	_, callbackErr := f.service.Callback(ctx, &slackdirectoryconnections.CallbackPayload{SessionToken: nil, State: state, Code: conv.PtrEmpty("unused"), Error: nil})
	_, disconnectErr := f.service.Disconnect(ctx, &gen.DisconnectPayload{SessionToken: nil, ID: "unused", Generation: "unused"})
	for _, err := range []error{listErr, beginErr, callbackErr, disconnectErr} {
		var failure *oops.ShareableError
		require.ErrorAs(t, err, &failure)
		require.Equal(t, code, failure.Code)
	}
	f.provider.AssertNotCalled(t, "Exchange")
}

func TestAbsentProductFeatureDeniesConnections(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	other := *f.auth
	other.ActiveOrganizationID = "org_without_claude_tag_support"
	ctx = authztest.WithExactGrants(t, contextvalues.SetAuthContext(ctx, &other), authz.NewGrant(authz.ScopeOrgAdmin, other.ActiveOrganizationID))
	requireConnectionGate(t, ctx, f, oops.CodeNotFound, "unused")
}

func TestProductFeatureAPIToggleControlsConnections(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	state := begin(t, ctx, f, nil)
	payload := &featuregen.SetProductFeaturePayload{
		OrganizationID: f.auth.ActiveOrganizationID,
		FeatureName:    featuregen.ProductFeatureName(productfeatures.FeatureClaudeTagSupport),
		Enabled:        false,
	}
	staff := *f.auth
	staff.IsAdmin = true
	staffCtx := contextvalues.SetAuthContext(ctx, &staff)
	require.NoError(t, f.featureService.SetProductFeature(staffCtx, payload))

	// A stale enabled cache entry cannot override a durable disable.
	require.NoError(t, f.cache.Set(ctx, productfeatures.FeatureCacheKey(f.auth.ActiveOrganizationID, productfeatures.FeatureClaudeTagSupport), productfeatures.FeatureCache{
		OrganizationID: f.auth.ActiveOrganizationID, Feature: productfeatures.FeatureClaudeTagSupport, Enabled: true,
	}, time.Minute))
	requireConnectionGate(t, ctx, f, oops.CodeNotFound, state)

	payload.Enabled = true
	member := *f.auth
	member.IsAdmin = false
	err := f.featureService.SetProductFeature(contextvalues.SetAuthContext(ctx, &member), payload)
	var failure *oops.ShareableError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, oops.CodeForbidden, failure.Code)
	requireConnectionGate(t, ctx, f, oops.CodeNotFound, state)

	require.NoError(t, f.featureService.SetProductFeature(staffCtx, payload))
	// Disabled callbacks leave OAuth state intact; re-enabling resumes consent.
	authorize(t, ctx, f, state, "TEXAMPLE01")
}

func TestProductFeatureLookupFailureDeniesConnections(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	f.db.Close()
	requireConnectionGate(t, ctx, f, oops.CodeUnavailable, "unused")
}

func TestCanceledProductFeatureLookupIsUnavailable(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	requireConnectionGate(t, ctx, f, oops.CodeUnavailable, "unused")
}

func TestProductFeatureDoesNotBypassSessionRestrictions(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	support := *f.auth
	support.IsAdmin = true
	support.SupportOrganizationID = support.ActiveOrganizationID
	requireConnectionGate(t, contextvalues.WithValidatedSupportSession(ctx, &support), f, oops.CodeForbidden, "unused")
	requireConnectionGate(t, contextvalues.WithLegacyAPIKeyAuthorization(ctx, f.auth), f, oops.CodeForbidden, "unused")
	requireConnectionGate(t, authztest.WithExactGrants(t, ctx), f, oops.CodeForbidden, "unused")
	missingSession := *f.auth
	missingSession.SessionID = nil
	requireConnectionGate(t, contextvalues.SetAuthContext(ctx, &missingSession), f, oops.CodeUnauthorized, "unused")
}

func TestSharedDemoConnectionsAreReadOnly(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	state := begin(t, ctx, f, nil)
	demo := *f.auth
	demo.ActiveOrganizationID = constants.DemoOrganizationID
	demoCtx := authztest.WithExactGrants(t, contextvalues.SetAuthContext(ctx, &demo), authz.NewGrant(authz.ScopeOrgAdmin, demo.ActiveOrganizationID))
	require.NoError(t, f.productFeatures.SetFeatureEnabled(ctx, demo.ActiveOrganizationID, productfeatures.FeatureClaudeTagSupport, true))
	list, err := f.service.List(demoCtx, &gen.ListPayload{SessionToken: nil})
	require.NoError(t, err)
	require.True(t, list.AuthorizationConfigured)

	_, beginErr := f.service.Begin(demoCtx, &gen.BeginPayload{SessionToken: nil, ConnectionID: nil})
	_, callbackErr := f.service.Callback(demoCtx, &slackdirectoryconnections.CallbackPayload{SessionToken: nil, State: state, Code: conv.PtrEmpty("unused"), Error: nil})
	_, disconnectErr := f.service.Disconnect(demoCtx, &gen.DisconnectPayload{SessionToken: nil, ID: "unused", Generation: "unused"})
	for _, err := range []error{beginErr, callbackErr, disconnectErr} {
		var failure *oops.ShareableError
		require.ErrorAs(t, err, &failure)
		require.Equal(t, oops.CodeForbidden, failure.Code)
	}
	f.provider.AssertNotCalled(t, "Exchange")
	// The demo callback refused before consuming state. Ordinary organizations,
	// including the retargeted local seed, can still complete authorization.
	connection := authorize(t, ctx, f, state, "TEXAMPLE01")
	_, err = f.service.Disconnect(ctx, &gen.DisconnectPayload{SessionToken: nil, ID: connection.ID, Generation: connection.Generation})
	require.NoError(t, err)
}
