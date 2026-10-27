package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	goahttp "goa.design/goa/v3/http"
	"testing"

	"github.com/jackc/pgx/v5"
	gen "github.com/speakeasy-api/gram/server/gen/auth"
	adminrepo "github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featureRepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	trialsRepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	usageRepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type signupPromotionCodeResolver struct {
	mock.Mock
}

func newSignupPromotionCodeResolver(t *testing.T) *signupPromotionCodeResolver {
	t.Helper()
	resolver := &signupPromotionCodeResolver{}
	resolver.Test(t)
	t.Cleanup(func() { resolver.AssertExpectations(t) })
	return resolver
}

func (v *signupPromotionCodeResolver) ResolveGatewayPromotionCode(ctx context.Context, code string) (string, error) {
	args := v.Called(ctx, code)
	return args.String(0), args.Error(1)
}

func partnerSignupConfiguration(t *testing.T, resolver *signupPromotionCodeResolver) func(*auth.AuthConfigurations) {
	t.Helper()
	return func(config *auth.AuthConfigurations) { config.PartnerPromotionCodeResolver = resolver }
}

func seedPartnerOffer(t *testing.T, ctx context.Context, inst *testInstance) {
	t.Helper()
	offer, err := adminrepo.New(inst.conn).CreateSignupOffer(ctx, adminrepo.CreateSignupOfferParams{Name: "Fixture offer", StripePromotionCodeID: "promo_fixture", SignupFlow: "tailscale_partner_gateway"})
	require.NoError(t, err)
	_, err = adminrepo.New(inst.conn).UpdateSignupOffer(ctx, adminrepo.UpdateSignupOfferParams{ID: offer.ID, Name: offer.Name, StripePromotionCodeID: offer.StripePromotionCodeID, SignupFlow: offer.SignupFlow, Enabled: true})
	require.NoError(t, err)
}

func changePartnerOffer(t *testing.T, ctx context.Context, inst *testInstance, promotionID, flow string, enabled bool) {
	t.Helper()
	offers, err := adminrepo.New(inst.conn).ListSignupOffers(ctx)
	require.NoError(t, err)
	require.Len(t, offers, 1)
	_, err = adminrepo.New(inst.conn).UpdateSignupOffer(ctx, adminrepo.UpdateSignupOfferParams{ID: offers[0].ID, Name: offers[0].Name, StripePromotionCodeID: promotionID, SignupFlow: flow, Enabled: enabled})
	require.NoError(t, err)
}

func assertPartnerSignup(t *testing.T, ctx context.Context, inst *testInstance, orgID string) {
	t.Helper()
	application, err := orgRepo.New(inst.conn).GetOrganizationPromotionApplication(ctx, orgID)
	require.NoError(t, err)
	require.Equal(t, "promo_fixture", application.StripePromotionCodeID)
	require.False(t, application.RedeemedAt.Valid)
	require.False(t, application.StripeSubscriptionID.Valid)
	_, err = trialsRepo.New(inst.conn).GetTrial(ctx, orgID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	enabled, err := featureRepo.New(inst.conn).IsFeatureEnabled(ctx, featureRepo.IsFeatureEnabledParams{OrganizationID: orgID, FeatureName: string(productfeatures.FeatureNetworkIngress)})
	require.NoError(t, err)
	require.False(t, enabled, "private networking requires checkout activation")
	require.Empty(t, inst.trialNotifier.trialStarted)
	entry, err := audittest.LatestAuditLogByAction(ctx, inst.conn, audit.ActionOrganizationPartnerSignupAttributed)
	require.NoError(t, err)
	require.JSONEq(t, `{"stripe_promotion_code_id":"promo_fixture"}`, string(entry.AfterSnapshot))
	require.NotEmpty(t, entry.ActorDisplay)
	count, err := audittest.AuditLogCountByAction(ctx, inst.conn, audit.ActionOrganizationPartnerSignupAttributed)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}

// partnerSignupLogin follows public login with the browser nonce binding.
func partnerSignupLogin(t *testing.T, ctx context.Context, inst *testInstance, company, code, destination string) (context.Context, string) {
	t.Helper()
	ctx = auth.TestNonceBindingContext(ctx, testNonceBinding)
	result, err := inst.service.Login(ctx, &gen.LoginPayload{OrgName: &company, PartnerCode: &code, Redirect: &destination})
	require.NoError(t, err)
	return ctx, nonceStateFromLocation(t, result.Location)
}

func TestPartnerSignupCallbackSuccess(t *testing.T) {
	t.Parallel()
	testPartnerSignupCallback(t, nil, nil)
}
func TestPartnerSignupCallbackUnavailable(t *testing.T) {
	t.Parallel()
	testPartnerSignupCallback(t, errors.New("internal Stripe identifier"), nil)
}
func TestPartnerSignupCallbackDisabledOffer(t *testing.T) {
	t.Parallel()
	testPartnerSignupCallback(t, nil, errors.New("disabled offer"))
}
func testPartnerSignupCallback(t *testing.T, resolutionErr, offerErr error) {
	t.Helper()
	user := defaultMockUserInfo()
	user.Organizations = nil
	resolver := newSignupPromotionCodeResolver(t)
	promotionID := "promo_fixture"
	if resolutionErr != nil {
		promotionID = ""
	}
	resolver.On("ResolveGatewayPromotionCode", mock.Anything, "fixture").Return(promotionID, resolutionErr).Once()
	ctx, inst := newTestAuthServiceForOrganizationProvisioning(t, user, partnerSignupConfiguration(t, resolver))
	seedPartnerOffer(t, ctx, inst)
	if offerErr != nil {
		changePartnerOffer(t, ctx, inst, "promo_fixture", "tailscale_partner_gateway", false)
	}
	ctx, state := partnerSignupLogin(t, ctx, inst, "Fixture Company", " FiXtUrE ", "")
	resolver.AssertNumberOfCalls(t, "ResolveGatewayPromotionCode", 0)
	result, err := inst.service.Callback(ctx, &gen.CallbackPayload{Code: new("mock_code"), State: &state})
	require.NoError(t, err)
	resolver.AssertExpectations(t)
	if resolutionErr != nil || offerErr != nil {
		require.Contains(t, result.Location, "signin_error=")
		require.Contains(t, result.Location, "partner_code=fixture")
		require.NotContains(t, result.Location, "internal")
		_, err := orgRepo.New(inst.conn).GetOrganizationMetadataBySlug(ctx, "fixture-company")
		require.ErrorIs(t, err, pgx.ErrNoRows)
		require.Empty(t, inst.trialNotifier.trialStarted)
		count, err := audittest.AuditLogCountByAction(ctx, inst.conn, audit.ActionOrganizationPartnerSignupAttributed)
		require.NoError(t, err)
		require.Zero(t, count, "disabled offers must not be attributed")
		return
	}
	session, err := inst.sessionManager.GetSession(ctx, result.SessionToken)
	require.NoError(t, err)
	assertPartnerSignup(t, ctx, inst, session.ActiveOrganizationID)
	playbookID, err := orgRepo.New(inst.conn).GetOrganizationOnboardingPlaybookID(ctx, session.ActiveOrganizationID)
	require.NoError(t, err)
	require.False(t, playbookID.PlaybookID.Valid, "partner signup must not assign a playbook")
	org, err := orgRepo.New(inst.conn).GetOrganizationMetadata(ctx, session.ActiveOrganizationID)
	require.NoError(t, err)
	require.Equal(t, inst.authConfigs.SignInRedirectURL+"/"+org.Slug+"/billing", result.Location)
}

func TestPartnerSignupRegister(t *testing.T) {
	t.Parallel()
	user := defaultMockUserInfo()
	user.Organizations = nil
	resolver := newSignupPromotionCodeResolver(t)
	resolver.On("ResolveGatewayPromotionCode", mock.Anything, "fixture").Return("promo_fixture", nil).Once()
	ctx, inst := newTestAuthServiceForOrganizationProvisioning(t, user, partnerSignupConfiguration(t, resolver))
	seedPartnerOffer(t, ctx, inst)
	callback, err := inst.callbackWithNonce(ctx, t)
	require.NoError(t, err)
	sessionID := callback.SessionToken
	ctx, err = inst.sessionManager.Authenticate(ctx, sessionID)
	require.NoError(t, err)
	require.NoError(t, inst.service.Register(ctx, &gen.RegisterPayload{OrgName: "Fixture Company", PartnerCode: conv.PtrEmpty(" FIXTURE ")}))
	session, err := inst.sessionManager.GetSession(ctx, sessionID)
	require.NoError(t, err)
	assertPartnerSignup(t, ctx, inst, session.ActiveOrganizationID)
	// The attribution query must refuse both a different promotion and replay.
	count, err := orgRepo.New(inst.conn).AttributePartnerSignup(ctx, orgRepo.AttributePartnerSignupParams{OrganizationID: conv.ToPGText(session.ActiveOrganizationID), StripePromotionCodeID: "promo_other"})
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestPartnerSignupExistingOrganizationIsNotAttributed(t *testing.T) {
	t.Parallel()
	user := defaultMockUserInfo()
	ctx, inst := newTestAuthServiceWithWorkOSClient(t, user, nil, partnerSignupConfiguration(t, newSignupPromotionCodeResolver(t)))
	require.NoError(t, inst.createTestUser(ctx, user))
	for _, org := range user.Organizations {
		require.NoError(t, inst.createTestOrganization(ctx, org, user.UserID))
	}
	ctx, state := partnerSignupLogin(t, ctx, inst, "Fixture Company", " FiXtUrE ", "")
	result, err := inst.service.Callback(ctx, &gen.CallbackPayload{Code: new("mock_code"), State: &state})
	require.NoError(t, err)
	require.NotContains(t, result.Location, "signin_error")
	_, err = orgRepo.New(inst.conn).GetOrganizationPromotionApplication(ctx, user.Organizations[0].ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestPartnerSignupCallbackToRegisterPreservesCode(t *testing.T) {
	t.Parallel()
	user := defaultMockUserInfo()
	user.Organizations = nil
	resolver := newSignupPromotionCodeResolver(t)
	resolver.On("ResolveGatewayPromotionCode", mock.Anything, "fixture").Return("promo_fixture", nil).Once()
	ctx, inst := newTestAuthServiceForOrganizationProvisioning(t, user, partnerSignupConfiguration(t, resolver))
	seedPartnerOffer(t, ctx, inst)
	ctx, state := partnerSignupLogin(t, ctx, inst, "", "fixture", "")
	result, err := inst.service.Callback(ctx, &gen.CallbackPayload{Code: new("mock_code"), State: &state})
	require.NoError(t, err)
	location, err := url.Parse(result.Location)
	require.NoError(t, err)
	require.Contains(t, location.Path, "/register")
	require.Equal(t, "fixture", location.Query().Get("partner_code"))
	ctx, err = inst.sessionManager.Authenticate(ctx, result.SessionToken)
	require.NoError(t, err)
	code := location.Query().Get("partner_code")
	require.NoError(t, inst.service.Register(ctx, &gen.RegisterPayload{OrgName: "Fixture Company", PartnerCode: &code}))
	session, err := inst.sessionManager.GetSession(ctx, result.SessionToken)
	require.NoError(t, err)
	assertPartnerSignup(t, ctx, inst, session.ActiveOrganizationID)
}

func TestPartnerSignupCommittedOrganizationRetry(t *testing.T) {
	t.Parallel()
	testPartnerSignupCommittedOrganizationRetry(t, false, "")
}

func TestPartnerSignupCommittedOrganizationRetryAfterDisable(t *testing.T) {
	t.Parallel()
	testPartnerSignupCommittedOrganizationRetry(t, false, "disabled")
}

func TestPartnerSignupCommittedOrganizationRetryAfterOfferChange(t *testing.T) {
	t.Parallel()
	testPartnerSignupCommittedOrganizationRetry(t, false, "changed")
}

func TestPartnerSignupRedeemedOrganizationRetry(t *testing.T) {
	t.Parallel()
	testPartnerSignupCommittedOrganizationRetry(t, true, "")
}

func testPartnerSignupCommittedOrganizationRetry(t *testing.T, redeemed bool, offerChange string) {
	t.Helper()
	user := defaultMockUserInfo()
	resolver := newSignupPromotionCodeResolver(t)
	ctx, inst := newTestAuthServiceWithWorkOSClient(t, user, nil, partnerSignupConfiguration(t, resolver))
	seedPartnerOffer(t, ctx, inst)
	promotionID := "promo_original"
	if offerChange == "changed" {
		promotionID = "promo_changed"
	}
	changePartnerOffer(t, ctx, inst, promotionID, "tailscale_partner_gateway", offerChange != "disabled")
	require.NoError(t, inst.createTestUser(ctx, user))
	org := user.Organizations[0]
	require.NoError(t, inst.createTestOrganization(ctx, org, user.UserID))
	// Model the already-committed attribution before session storage failed.
	count, err := orgRepo.New(inst.conn).AttributePartnerSignup(ctx, orgRepo.AttributePartnerSignupParams{OrganizationID: conv.ToPGText(org.ID), StripePromotionCodeID: "promo_original"})
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	if redeemed {
		rows, err := usageRepo.New(inst.conn).RecordOrganizationPromotionRedemption(ctx, usageRepo.RecordOrganizationPromotionRedemptionParams{
			OrganizationID: conv.ToPGText(org.ID), StripePromotionCodeID: "promo_original", StripeSubscriptionID: conv.ToPGText("sub_fixture"),
		})
		require.NoError(t, err)
		require.EqualValues(t, 1, rows)
	}
	// Neither replay nor a different signup code may replace any attribution,
	// including a redeemed application with no pending application left.
	for _, code := range []string{"promo_original", "promo_other"} {
		count, err := orgRepo.New(inst.conn).AttributePartnerSignup(ctx, orgRepo.AttributePartnerSignupParams{
			OrganizationID: conv.ToPGText(org.ID), StripePromotionCodeID: code,
		})
		require.NoError(t, err)
		require.Zero(t, count)
	}
	require.NoError(t, orgRepo.New(inst.conn).SetOrganizationDefaultHostForTest(ctx, orgRepo.SetOrganizationDefaultHostForTestParams{ID: org.ID, DefaultHost: conv.ToPGText("https://" + testExtraPlatformHost)}))
	ctx = requestorigin.WithContext(ctx, originAt(requestorigin.SurfacePlatform, testServerURL.String()))
	ctx, state := partnerSignupLogin(t, ctx, inst, "Fixture Company", " FiXtUrE ", "")
	result, err := inst.service.Callback(ctx, &gen.CallbackPayload{Code: new("mock_code"), State: &state})
	require.NoError(t, err)
	require.Equal(t, extraHostTransfer("/"+org.Slug+"/billing"), result.Location)
	persisted, err := orgRepo.New(inst.conn).GetOrganizationPromotionApplication(ctx, org.ID)
	require.NoError(t, err)
	require.Equal(t, "promo_original", persisted.StripePromotionCodeID)
	require.Equal(t, redeemed, persisted.RedeemedAt.Valid)
	require.Equal(t, redeemed, persisted.StripeSubscriptionID.Valid)
	_, err = trialsRepo.New(inst.conn).GetTrial(ctx, org.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.Empty(t, inst.trialNotifier.trialStarted)
	resolver.AssertNumberOfCalls(t, "ResolveGatewayPromotionCode", 0)
}

func TestPartnerSignupInvalidCodeRedirectsAfterLogin(t *testing.T) {
	t.Parallel()
	user := defaultMockUserInfo()
	user.Organizations = nil
	resolver := newSignupPromotionCodeResolver(t)
	resolver.On("ResolveGatewayPromotionCode", mock.Anything, "invalid").Return("", errors.New("promotion code not found")).Once()
	ctx, inst := newTestAuthServiceForOrganizationProvisioning(t, user, partnerSignupConfiguration(t, resolver))
	ctx, state := partnerSignupLogin(t, ctx, inst, "Fixture Company", " INVALID ", "/destination")
	result, err := inst.service.Callback(ctx, &gen.CallbackPayload{Code: new("mock_code"), State: &state})
	require.NoError(t, err)
	location, err := url.Parse(result.Location)
	require.NoError(t, err)
	require.NotEmpty(t, location.Query().Get("signin_error"))
	require.Equal(t, "invalid", location.Query().Get("partner_code"))
	require.Equal(t, "Fixture Company", location.Query().Get("org_name"))
	require.Equal(t, "/destination", location.Query().Get("redirect"))
	require.Empty(t, result.SessionToken)
}

func TestPartnerSignupRegisterConcealsProviderError(t *testing.T) {
	t.Parallel()
	user := defaultMockUserInfo()
	user.Organizations = nil
	resolver := newSignupPromotionCodeResolver(t)
	resolver.On("ResolveGatewayPromotionCode", mock.Anything, "fixture").Return("", errors.New("private provider detail")).Once()
	ctx, inst := newTestAuthServiceForOrganizationProvisioning(t, user, partnerSignupConfiguration(t, resolver))
	callback, err := inst.callbackWithNonce(ctx, t)
	require.NoError(t, err)
	ctx, err = inst.sessionManager.Authenticate(ctx, callback.SessionToken)
	require.NoError(t, err)
	err = inst.service.Register(ctx, &gen.RegisterPayload{OrgName: "Fixture Company", PartnerCode: conv.PtrEmpty("fixture")})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private provider detail")
	session, err := inst.sessionManager.GetSession(ctx, callback.SessionToken)
	require.NoError(t, err)
	require.Empty(t, session.ActiveOrganizationID)
}

func TestPartnerSignupInvalidCodeHTTPRedirect(t *testing.T) {
	t.Parallel()
	resolver := newSignupPromotionCodeResolver(t)
	resolver.On("ResolveGatewayPromotionCode", mock.Anything, "invalid").Return("", errors.New("promotion code not found")).Once()
	testPartnerSignupHTTPRedirect(t, "invalid", resolver, func(*auth.AuthConfigurations) {})
}

func TestPartnerSignupDisabledHTTPRedirect(t *testing.T) {
	t.Parallel()
	testPartnerSignupHTTPRedirect(t, "fixture", newSignupPromotionCodeResolver(t), func(config *auth.AuthConfigurations) { config.PartnerPromotionCodeResolver = nil })
}

func TestPartnerSignupUnavailableHTTPRedirect(t *testing.T) {
	t.Parallel()
	resolver := newSignupPromotionCodeResolver(t)
	resolver.On("ResolveGatewayPromotionCode", mock.Anything, "fixture").Return("", errors.New("private provider detail")).Once()
	testPartnerSignupHTTPRedirect(t, "fixture", resolver, func(*auth.AuthConfigurations) {})
}

func testPartnerSignupHTTPRedirect(t *testing.T, code string, resolver *signupPromotionCodeResolver, configure func(*auth.AuthConfigurations)) {
	t.Helper()
	user := defaultMockUserInfo()
	user.Organizations = nil
	ctx, inst := newTestAuthServiceForOrganizationProvisioning(t, user, partnerSignupConfiguration(t, resolver), configure)
	mux := goahttp.NewMuxer()
	auth.Attach(mux, inst.service)
	query := url.Values{"org_name": {"Fixture Company"}, "partner_code": {code}, "redirect": {"/destination"}}
	login := httptest.NewRecorder()
	mux.ServeHTTP(login, httptest.NewRequestWithContext(ctx, http.MethodGet, "/rpc/auth.login?"+query.Encode(), nil))
	require.Equal(t, http.StatusTemporaryRedirect, login.Code, login.Body.String())
	state := nonceStateFromLocation(t, login.Header().Get("Location"))
	query = url.Values{"code": {"mock_code"}, "state": {state}}
	request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/rpc/auth.callback?"+query.Encode(), nil)
	response := login.Result()
	require.NoError(t, response.Body.Close())
	for _, cookie := range response.Cookies() {
		request.AddCookie(cookie)
	}
	callback := httptest.NewRecorder()
	mux.ServeHTTP(callback, request)
	require.Equal(t, http.StatusTemporaryRedirect, callback.Code, callback.Body.String())
	location, err := url.Parse(callback.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, inst.authConfigs.SignInRedirectURL+"/sign-up", location.Scheme+"://"+location.Host+location.Path)
	require.NotEmpty(t, location.Query().Get("signin_error"))
	require.Equal(t, code, location.Query().Get("partner_code"))
	require.Equal(t, "Fixture Company", location.Query().Get("org_name"))
	require.Equal(t, "/destination", location.Query().Get("redirect"))
	require.NotContains(t, callback.Header().Get("Location"), "private provider detail")
	_, err = orgRepo.New(inst.conn).GetOrganizationMetadataBySlug(ctx, "fixture-company")
	require.ErrorIs(t, err, pgx.ErrNoRows)
	count, err := audittest.AuditLogCountByAction(ctx, inst.conn, audit.ActionOrganizationPartnerSignupAttributed)
	require.NoError(t, err)
	require.Zero(t, count)
	require.Empty(t, inst.trialNotifier.trialStarted)
}

func TestPartnerSignupLoginCodeLengthBound(t *testing.T) {
	t.Parallel()
	ctx, inst := newTestAuthServiceWithWorkOSClient(t, defaultMockUserInfo(), nil)
	_, err := inst.service.Login(ctx, &gen.LoginPayload{PartnerCode: conv.PtrEmpty(strings.Repeat("a", 81))})
	require.Error(t, err)
	_, err = inst.service.Login(ctx, &gen.LoginPayload{PartnerCode: conv.PtrEmpty(strings.Repeat("a", 80))})
	require.NoError(t, err)
}

func TestPartnerSignupPendingLaterLoginDoesNotArmTrial(t *testing.T) {
	t.Parallel()
	testPartnerSignupLaterLoginDoesNotArmTrial(t, false)
}

func TestPartnerSignupRedeemedLaterLoginDoesNotArmTrial(t *testing.T) {
	t.Parallel()
	testPartnerSignupLaterLoginDoesNotArmTrial(t, true)
}

func testPartnerSignupLaterLoginDoesNotArmTrial(t *testing.T, redeemed bool) {
	t.Helper()

	const (
		email          = "active-signup@example.com"
		organizationID = "org-active-signup"
		workosOrgID    = "org_01ACTIVE_SIGNUP"
	)
	userInfo := &MockUserInfo{
		UserID:        "user_01ACTIVE_SIGNUP",
		Email:         email,
		Organizations: nil,
	}
	ctx, instance := newTestAuthServiceWithWorkOSClient(t, userInfo, &mockWorkOSFetcher{
		members: map[string][]workos.Member{
			userInfo.UserID: {{
				ID:             "om_active_signup",
				UserID:         userInfo.UserID,
				OrganizationID: workosOrgID,
				Organization:   "Gated Org",
				RoleSlugs:      []string{"admin"},
			}},
		},
		orgs: map[string]*workos.Organization{
			workosOrgID: {ID: workosOrgID, Name: "Gated Org"},
		},
	})
	require.NoError(t, instance.createTestUser(ctx, userInfo))
	require.NoError(t, instance.createTestOrganization(ctx, MockOrganizationEntry{
		ID:                 organizationID,
		Name:               "Gated Org",
		Slug:               organizationID,
		WorkosID:           new(workosOrgID),
		UserWorkspaceSlugs: nil,
	}, userInfo.UserID))

	count, err := orgRepo.New(instance.conn).AttributePartnerSignup(ctx, orgRepo.AttributePartnerSignupParams{
		OrganizationID: conv.ToPGText(organizationID), StripePromotionCodeID: "promo_fixture",
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	if redeemed {
		count, err := usageRepo.New(instance.conn).RecordOrganizationPromotionRedemption(ctx, usageRepo.RecordOrganizationPromotionRedemptionParams{
			OrganizationID: conv.ToPGText(organizationID), StripePromotionCodeID: "promo_fixture", StripeSubscriptionID: conv.ToPGText("sub_fixture"),
		})
		require.NoError(t, err)
		require.EqualValues(t, 1, count)
	}

	// Ordinary login carries no partner intent and has no partner signup configuration.
	result, err := instance.callbackWithNonce(ctx, t)
	require.NoError(t, err)
	require.NotContains(t, result.Location, "signin_error=")

	ctx, err = instance.sessionManager.Authenticate(ctx, result.SessionToken)
	require.NoError(t, err)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.Equal(t, organizationID, authCtx.ActiveOrganizationID)
	require.NotEqual(t, "enterprise", authCtx.AccountType)
	require.Empty(t, instance.trialNotifier.trialStarted)
	_, err = trialsRepo.New(instance.conn).GetTrial(ctx, organizationID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestPartnerSignupRejectsUnmappedOffer(t *testing.T) {
	t.Parallel()
	testPartnerSignupRejectsIneligibleOffer(t, "", "", false)
}

func TestPartnerSignupRejectsDisabledOffer(t *testing.T) {
	t.Parallel()
	testPartnerSignupRejectsIneligibleOffer(t, "promo_fixture", "tailscale_partner_gateway", false)
}

func TestPartnerSignupRejectsDifferentExactIDOffer(t *testing.T) {
	t.Parallel()
	testPartnerSignupRejectsIneligibleOffer(t, "promo_FIXTURE", "tailscale_partner_gateway", true)
}

func TestPartnerSignupRejectsUnsupportedFlowOffer(t *testing.T) {
	t.Parallel()
	testPartnerSignupRejectsIneligibleOffer(t, "promo_fixture", "unsupported", true)
}

func testPartnerSignupRejectsIneligibleOffer(t *testing.T, promotionID, flow string, enabled bool) {
	t.Helper()
	user := defaultMockUserInfo()
	user.Organizations = nil
	resolver := newSignupPromotionCodeResolver(t)
	resolver.On("ResolveGatewayPromotionCode", mock.Anything, "fixture").Return("promo_fixture", nil).Once()
	ctx, inst := newTestAuthServiceForOrganizationProvisioning(t, user, partnerSignupConfiguration(t, resolver))
	if promotionID != "" {
		seedPartnerOffer(t, ctx, inst)
		changePartnerOffer(t, ctx, inst, promotionID, flow, enabled)
	}
	ctx, state := partnerSignupLogin(t, ctx, inst, "Fixture Company", "fixture", "")
	result, err := inst.service.Callback(ctx, &gen.CallbackPayload{Code: new("mock_code"), State: &state})
	require.NoError(t, err)
	require.Contains(t, result.Location, "signin_error=")
	_, err = orgRepo.New(inst.conn).GetOrganizationMetadataBySlug(ctx, "fixture-company")
	require.ErrorIs(t, err, pgx.ErrNoRows)
	count, err := audittest.AuditLogCountByAction(ctx, inst.conn, audit.ActionOrganizationPartnerSignupAttributed)
	require.NoError(t, err)
	require.Zero(t, count)
	require.Empty(t, inst.trialNotifier.trialStarted)
}
