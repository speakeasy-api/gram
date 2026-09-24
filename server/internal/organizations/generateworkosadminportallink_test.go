package organizations_test

import (
	"errors"
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
	ctx = withAccountType(t, ctx, string(billing.TierEnterprise))

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
	ctx = withAccountType(t, ctx, string(billing.TierPro))

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
	ctx = withAccountType(t, ctx, string(billing.TierEnterprise))

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
	ctx = withAccountType(t, ctx, string(billing.TierBase))

	_, err := ti.service.GenerateWorkOSAdminPortalLink(ctx, &gen.GenerateWorkOSAdminPortalLinkPayload{
		Intent: "log_streams",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)

	ti.orgs.AssertNotCalled(t, "GenerateAdminPortalLink", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// A WorkOS response body carries the organization id and the request path. It
// belongs in the logs, never in the message the admin reads.
const testWorkOSErrorBody = `{"code":"organization_domain_not_verified","message":"Organization org_01SECRET has no verified domain","organization_id":"org_01SECRET"}`

func workosAPIError(status int) *thirdpartyworkos.APIError {
	return &thirdpartyworkos.APIError{
		Method:     "POST",
		Path:       "/portal/generate_link",
		StatusCode: status,
		Body:       testWorkOSErrorBody,
	}
}

func requirePortalLinkFailure(t *testing.T, err error, code oops.Code, wantMessage string) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
	require.Equal(t, wantMessage, oopsErr.Error())
	require.NotContains(t, oopsErr.Error(), "org_01SECRET", "the WorkOS response body must not reach the client")
}

func TestService_GenerateWorkOSAdminPortalLink_DSyncRejectedNeedsVerifiedDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceWithFeatures(t, enabledFeatures(productfeatures.FeatureSCIM))

	ti.orgs.On("GenerateAdminPortalLink", mock.Anything, mock.Anything, thirdpartyworkos.PortalIntentDSync, mock.Anything).
		Return("", workosAPIError(422)).Once()

	_, err := ti.service.GenerateWorkOSAdminPortalLink(ctx, &gen.GenerateWorkOSAdminPortalLinkPayload{
		Intent: "dsync",
	})
	requirePortalLinkFailure(t, err, oops.CodeBadRequest, "WorkOS rejected the Directory Sync setup request. Verify a domain for this organization, then try again.")

	ti.orgs.AssertExpectations(t)
}

func TestService_GenerateWorkOSAdminPortalLink_DomainVerificationRejectedKeepsGenericNextStep(t *testing.T) {
	t.Parallel()

	// Telling an admin to verify a domain is no help when verifying a domain is
	// what WorkOS just refused.
	ctx, ti := newTestOrganizationsService(t)

	ti.orgs.On("GenerateAdminPortalLink", mock.Anything, mock.Anything, thirdpartyworkos.PortalIntentDomainVerification, mock.Anything).
		Return("", workosAPIError(400)).Once()

	_, err := ti.service.GenerateWorkOSAdminPortalLink(ctx, &gen.GenerateWorkOSAdminPortalLinkPayload{
		Intent: "domain_verification",
	})
	requirePortalLinkFailure(t, err, oops.CodeBadRequest, "WorkOS rejected the domain verification setup request. Contact Speakeasy support if it keeps failing.")

	ti.orgs.AssertExpectations(t)
}

func TestService_GenerateWorkOSAdminPortalLink_DSyncOrganizationMissingInWorkOS(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceWithFeatures(t, enabledFeatures(productfeatures.FeatureSCIM))

	ti.orgs.On("GenerateAdminPortalLink", mock.Anything, mock.Anything, thirdpartyworkos.PortalIntentDSync, mock.Anything).
		Return("", workosAPIError(404)).Once()

	_, err := ti.service.GenerateWorkOSAdminPortalLink(ctx, &gen.GenerateWorkOSAdminPortalLinkPayload{
		Intent: "dsync",
	})
	requirePortalLinkFailure(t, err, oops.CodeBadRequest, "this organization no longer exists in WorkOS, so Directory Sync setup cannot start. Contact Speakeasy support to relink it.")

	ti.orgs.AssertExpectations(t)
}

func TestService_GenerateWorkOSAdminPortalLink_DSyncCredentialsRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceWithFeatures(t, enabledFeatures(productfeatures.FeatureSCIM))

	ti.orgs.On("GenerateAdminPortalLink", mock.Anything, mock.Anything, thirdpartyworkos.PortalIntentDSync, mock.Anything).
		Return("", workosAPIError(401)).Once()

	_, err := ti.service.GenerateWorkOSAdminPortalLink(ctx, &gen.GenerateWorkOSAdminPortalLinkPayload{
		Intent: "dsync",
	})
	requirePortalLinkFailure(t, err, oops.CodeUnexpected, "Speakeasy is not authorized to start Directory Sync setup in WorkOS. Contact Speakeasy support.")

	ti.orgs.AssertExpectations(t)
}

func TestService_GenerateWorkOSAdminPortalLink_DSyncRateLimited(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceWithFeatures(t, enabledFeatures(productfeatures.FeatureSCIM))

	ti.orgs.On("GenerateAdminPortalLink", mock.Anything, mock.Anything, thirdpartyworkos.PortalIntentDSync, mock.Anything).
		Return("", workosAPIError(429)).Once()

	_, err := ti.service.GenerateWorkOSAdminPortalLink(ctx, &gen.GenerateWorkOSAdminPortalLinkPayload{
		Intent: "dsync",
	})
	requirePortalLinkFailure(t, err, oops.CodeGatewayError, "WorkOS is rate limiting Directory Sync setup. Wait a moment and try again.")

	ti.orgs.AssertExpectations(t)
}

func TestService_GenerateWorkOSAdminPortalLink_DSyncUpstreamFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceWithFeatures(t, enabledFeatures(productfeatures.FeatureSCIM))

	ti.orgs.On("GenerateAdminPortalLink", mock.Anything, mock.Anything, thirdpartyworkos.PortalIntentDSync, mock.Anything).
		Return("", workosAPIError(503)).Once()

	_, err := ti.service.GenerateWorkOSAdminPortalLink(ctx, &gen.GenerateWorkOSAdminPortalLinkPayload{
		Intent: "dsync",
	})
	requirePortalLinkFailure(t, err, oops.CodeGatewayError, "WorkOS could not start Directory Sync setup right now. Try again in a few minutes.")

	ti.orgs.AssertExpectations(t)
}

func TestService_GenerateWorkOSAdminPortalLink_DSyncUnreachable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceWithFeatures(t, enabledFeatures(productfeatures.FeatureSCIM))

	ti.orgs.On("GenerateAdminPortalLink", mock.Anything, mock.Anything, thirdpartyworkos.PortalIntentDSync, mock.Anything).
		Return("", errors.New("dial tcp 1.2.3.4:443: connect: connection refused")).Once()

	_, err := ti.service.GenerateWorkOSAdminPortalLink(ctx, &gen.GenerateWorkOSAdminPortalLinkPayload{
		Intent: "dsync",
	})
	requirePortalLinkFailure(t, err, oops.CodeGatewayError, "could not reach WorkOS to start Directory Sync setup. Try again in a few minutes.")

	ti.orgs.AssertExpectations(t)
}

func TestService_GenerateWorkOSAdminPortalLink_DSyncBlankLink(t *testing.T) {
	t.Parallel()

	// A blank link leaves the dashboard with nothing to open and no reason why,
	// which is the silent failure this path exists to avoid.
	ctx, ti := newTestOrganizationsServiceWithFeatures(t, enabledFeatures(productfeatures.FeatureSCIM))

	ti.orgs.On("GenerateAdminPortalLink", mock.Anything, mock.Anything, thirdpartyworkos.PortalIntentDSync, mock.Anything).
		Return("  ", nil).Once()

	_, err := ti.service.GenerateWorkOSAdminPortalLink(ctx, &gen.GenerateWorkOSAdminPortalLinkPayload{
		Intent: "dsync",
	})
	requirePortalLinkFailure(t, err, oops.CodeGatewayError, "WorkOS did not return a Directory Sync setup link. Try again in a few minutes.")

	ti.orgs.AssertExpectations(t)
}

func TestService_GenerateWorkOSAdminPortalLink_UnknownIntentDenied(t *testing.T) {
	t.Parallel()

	// Fail closed: an intent added at the design layer without an entitlement
	// mapping must be denied, even for a fully entitled enterprise org.
	ctx, ti := newTestOrganizationsServiceRBAC(t)
	ctx = withAccountType(t, ctx, string(billing.TierEnterprise))

	_, err := ti.service.GenerateWorkOSAdminPortalLink(ctx, &gen.GenerateWorkOSAdminPortalLinkPayload{
		Intent: "certificate_renewal",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)

	ti.orgs.AssertNotCalled(t, "GenerateAdminPortalLink", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}
