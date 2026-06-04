package organizations_test

import (
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const testPortalLink = "https://id.workos.com/portal/launch?secret=abc123"

func TestService_GenerateWorkOSAdminPortalLink_IntentOnly(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)

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

	ctx, ti := newTestOrganizationsService(t)

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

	ctx, ti := newTestOrganizationsService(t)

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

	ctx, ti := newTestOrganizationsService(t)

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

	ctx, ti := newTestOrganizationsService(t)

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

	ctx, ti := newTestOrganizationsService(t)

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
