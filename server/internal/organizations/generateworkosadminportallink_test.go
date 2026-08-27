package organizations_test

import (
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const testPortalLink = "https://id.workos.com/portal/launch?secret=abc123"

func TestService_GenerateWorkOSAdminPortalLink_IntentOnly(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceWithFeatures(t, enabledFeatures(productfeatures.FeatureSSO))

	ti.orgs.On("GenerateAdminPortalLink", mock.Anything, mock.Anything, thirdpartyworkos.PortalIntentSSO, thirdpartyworkos.GenerateAdminPortalLinkOpts{}).
		Return(testPortalLink, nil).Once()

	res, err := ti.service.GenerateWorkOSAdminPortalLink(ctx, &gen.GenerateWorkOSAdminPortalLinkPayload{
		Intent: "sso",
	})
	require.NoError(t, err)
	require.Equal(t, testPortalLink, res.URL)

	ti.orgs.AssertExpectations(t)
}

func TestService_GenerateWorkOSAdminPortalLink_WithReturnAndSuccessURLs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceWithFeatures(t, enabledFeatures(productfeatures.FeatureSSO))

	expectedOpts := thirdpartyworkos.GenerateAdminPortalLinkOpts{
		ReturnURL:  "https://app.example.com/settings",
		SuccessURL: "https://app.example.com/settings?setup=complete",
	}
	ti.orgs.On("GenerateAdminPortalLink", mock.Anything, mock.Anything, thirdpartyworkos.PortalIntentSSO, expectedOpts).
		Return(testPortalLink, nil).Once()

	res, err := ti.service.GenerateWorkOSAdminPortalLink(ctx, &gen.GenerateWorkOSAdminPortalLinkPayload{
		Intent:     "sso",
		ReturnURL:  conv.PtrEmpty("https://app.example.com/settings"),
		SuccessURL: conv.PtrEmpty("https://app.example.com/settings?setup=complete"),
	})
	require.NoError(t, err)
	require.Equal(t, testPortalLink, res.URL)

	ti.orgs.AssertExpectations(t)
}

func TestService_GenerateWorkOSAdminPortalLink_WithITContactEmails(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceWithFeatures(t, enabledFeatures(productfeatures.FeatureSCIM))

	expectedOpts := thirdpartyworkos.GenerateAdminPortalLinkOpts{
		ITContactEmails: []string{"admin@example.com", "security@example.com"},
	}
	ti.orgs.On("GenerateAdminPortalLink", mock.Anything, mock.Anything, thirdpartyworkos.PortalIntentDSync, expectedOpts).
		Return(testPortalLink, nil).Once()

	res, err := ti.service.GenerateWorkOSAdminPortalLink(ctx, &gen.GenerateWorkOSAdminPortalLinkPayload{
		Intent:          "dsync",
		ItContactEmails: []string{"admin@example.com", "security@example.com"},
	})
	require.NoError(t, err)
	require.Equal(t, testPortalLink, res.URL)

	ti.orgs.AssertExpectations(t)
}

func TestService_GenerateWorkOSAdminPortalLink_WithSSOIntentOptions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceWithFeatures(t, enabledFeatures(productfeatures.FeatureSSO))

	expectedOpts := thirdpartyworkos.GenerateAdminPortalLinkOpts{
		IntentOptions: &thirdpartyworkos.IntentOptions{
			SSO: &thirdpartyworkos.SSOIntentOptions{
				ProviderType: "OktaSAML",
			},
		},
	}
	ti.orgs.On("GenerateAdminPortalLink", mock.Anything, mock.Anything, thirdpartyworkos.PortalIntentSSO, expectedOpts).
		Return(testPortalLink, nil).Once()

	res, err := ti.service.GenerateWorkOSAdminPortalLink(ctx, &gen.GenerateWorkOSAdminPortalLinkPayload{
		Intent: "sso",
		IntentOptions: &gen.WorkOSIntentOptions{
			Sso: &gen.WorkOSSSOIntentOptions{
				ProviderType: conv.PtrEmpty("OktaSAML"),
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, testPortalLink, res.URL)

	ti.orgs.AssertExpectations(t)
}

func TestService_GenerateWorkOSAdminPortalLink_WithDomainVerificationIntentOptions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)

	expectedOpts := thirdpartyworkos.GenerateAdminPortalLinkOpts{
		IntentOptions: &thirdpartyworkos.IntentOptions{
			DomainVerification: &thirdpartyworkos.DomainVerificationIntentOptions{
				DomainName: "example.com",
			},
		},
	}
	ti.orgs.On("GenerateAdminPortalLink", mock.Anything, mock.Anything, thirdpartyworkos.PortalIntentDomainVerification, expectedOpts).
		Return(testPortalLink, nil).Once()

	res, err := ti.service.GenerateWorkOSAdminPortalLink(ctx, &gen.GenerateWorkOSAdminPortalLinkPayload{
		Intent: "domain_verification",
		IntentOptions: &gen.WorkOSIntentOptions{
			DomainVerification: &gen.WorkOSDomainVerificationIntentOptions{
				DomainName: conv.PtrEmpty("example.com"),
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, testPortalLink, res.URL)

	ti.orgs.AssertExpectations(t)
}

func TestService_GenerateWorkOSAdminPortalLink_AllOptions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceWithFeatures(t, enabledFeatures(productfeatures.FeatureSSO))

	expectedOpts := thirdpartyworkos.GenerateAdminPortalLinkOpts{
		ReturnURL:       "https://app.example.com/return",
		SuccessURL:      "https://app.example.com/success",
		ITContactEmails: []string{"it@example.com"},
		IntentOptions: &thirdpartyworkos.IntentOptions{
			SSO: &thirdpartyworkos.SSOIntentOptions{
				BookmarkSlug: "my-app",
				ProviderType: "GoogleSAML",
			},
		},
	}
	ti.orgs.On("GenerateAdminPortalLink", mock.Anything, mock.Anything, thirdpartyworkos.PortalIntentSSO, expectedOpts).
		Return(testPortalLink, nil).Once()

	res, err := ti.service.GenerateWorkOSAdminPortalLink(ctx, &gen.GenerateWorkOSAdminPortalLinkPayload{
		Intent:          "sso",
		ReturnURL:       conv.PtrEmpty("https://app.example.com/return"),
		SuccessURL:      conv.PtrEmpty("https://app.example.com/success"),
		ItContactEmails: []string{"it@example.com"},
		IntentOptions: &gen.WorkOSIntentOptions{
			Sso: &gen.WorkOSSSOIntentOptions{
				BookmarkSlug: conv.PtrEmpty("my-app"),
				ProviderType: conv.PtrEmpty("GoogleSAML"),
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, testPortalLink, res.URL)

	ti.orgs.AssertExpectations(t)
}

func TestService_GenerateWorkOSAdminPortalLink_OrgNotLinkedToWorkOS(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceWithFeatures(t, enabledFeatures(productfeatures.FeatureSSO))

	// Clear the WorkOS org ID so the handler hits the "not linked" guard.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	err := orgrepo.New(ti.conn).ClearWorkosOrgID(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)

	_, err = ti.service.GenerateWorkOSAdminPortalLink(ctx, &gen.GenerateWorkOSAdminPortalLinkPayload{
		Intent: "sso",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestService_GenerateWorkOSAdminPortalLink_SSONotEntitled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)

	_, err := ti.service.GenerateWorkOSAdminPortalLink(ctx, &gen.GenerateWorkOSAdminPortalLinkPayload{
		Intent: "sso",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)

	ti.orgs.AssertNotCalled(t, "GenerateAdminPortalLink", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestService_GenerateWorkOSAdminPortalLink_DSyncNotEntitled(t *testing.T) {
	t.Parallel()

	// SSO alone must not unlock dsync — the mapping is per intent.
	ctx, ti := newTestOrganizationsServiceWithFeatures(t, enabledFeatures(productfeatures.FeatureSSO))

	_, err := ti.service.GenerateWorkOSAdminPortalLink(ctx, &gen.GenerateWorkOSAdminPortalLinkPayload{
		Intent: "dsync",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)

	ti.orgs.AssertNotCalled(t, "GenerateAdminPortalLink", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestService_GenerateWorkOSAdminPortalLink_AuditLogsEnterprise(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.AccountType = string(billing.TierEnterprise)

	ti.orgs.On("GenerateAdminPortalLink", mock.Anything, mock.Anything, thirdpartyworkos.PortalIntentAuditLogs, thirdpartyworkos.GenerateAdminPortalLinkOpts{}).
		Return(testPortalLink, nil).Once()

	res, err := ti.service.GenerateWorkOSAdminPortalLink(ctx, &gen.GenerateWorkOSAdminPortalLinkPayload{
		Intent: "audit_logs",
	})
	require.NoError(t, err)
	require.Equal(t, testPortalLink, res.URL)

	ti.orgs.AssertExpectations(t)
}

func TestService_GenerateWorkOSAdminPortalLink_AuditLogsNonEnterpriseDenied(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.AccountType = string(billing.TierPro)

	_, err := ti.service.GenerateWorkOSAdminPortalLink(ctx, &gen.GenerateWorkOSAdminPortalLinkPayload{
		Intent: "audit_logs",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)

	ti.orgs.AssertNotCalled(t, "GenerateAdminPortalLink", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestService_GenerateWorkOSAdminPortalLink_LogStreamsEnterprise(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.AccountType = string(billing.TierEnterprise)

	ti.orgs.On("GenerateAdminPortalLink", mock.Anything, mock.Anything, thirdpartyworkos.PortalIntentLogStreams, thirdpartyworkos.GenerateAdminPortalLinkOpts{}).
		Return(testPortalLink, nil).Once()

	res, err := ti.service.GenerateWorkOSAdminPortalLink(ctx, &gen.GenerateWorkOSAdminPortalLinkPayload{
		Intent: "log_streams",
	})
	require.NoError(t, err)
	require.Equal(t, testPortalLink, res.URL)

	ti.orgs.AssertExpectations(t)
}

func TestService_GenerateWorkOSAdminPortalLink_LogStreamsNonEnterpriseDenied(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.AccountType = string(billing.TierBase)

	_, err := ti.service.GenerateWorkOSAdminPortalLink(ctx, &gen.GenerateWorkOSAdminPortalLinkPayload{
		Intent: "log_streams",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)

	ti.orgs.AssertNotCalled(t, "GenerateAdminPortalLink", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestService_GenerateWorkOSAdminPortalLink_UnknownIntentDenied(t *testing.T) {
	t.Parallel()

	// Fail closed: an intent added at the design layer without an entitlement
	// mapping must be denied, even for a fully entitled enterprise org.
	ctx, ti := newTestOrganizationsServiceRBAC(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.AccountType = string(billing.TierEnterprise)

	_, err := ti.service.GenerateWorkOSAdminPortalLink(ctx, &gen.GenerateWorkOSAdminPortalLinkPayload{
		Intent: "certificate_renewal",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)

	ti.orgs.AssertNotCalled(t, "GenerateAdminPortalLink", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}
