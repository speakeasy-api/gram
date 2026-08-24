package usage

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

func TestCreateStripeCheckoutUsesLocalWildcardFlag(t *testing.T) {
	t.Parallel()

	ti := newLocalStripeCheckoutTestInstance(t)
	ti.flags = &feature.InMemory{}
	ti.flags.SetFlag(feature.FlagPaygSelfServeBilling, feature.LocalWildcardDistinctID, true)
	ti.service.featureFlags = ti.flags

	checkoutURL, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	parsed, err := url.Parse(checkoutURL)
	require.NoError(t, err)
	require.Equal(t, stripeclient.LocalCheckoutPath, parsed.Path)
	require.NotEmpty(t, parsed.Query().Get("session"))
}

func TestLocalStripeCheckoutStartsPayg(t *testing.T) {
	t.Parallel()

	ti := newLocalStripeCheckoutTestInstance(t)
	baseline, err := audittest.AuditLogCountByAction(t.Context(), ti.db, audit.ActionOrganizationPaygActivated)
	require.NoError(t, err)

	checkoutURL, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	parsed, err := url.Parse(checkoutURL)
	require.NoError(t, err)

	mux := goahttp.NewMuxer()
	Attach(mux, ti.service)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, parsed.RequestURI(), nil))
	require.Equal(t, http.StatusSeeOther, recorder.Code, recorder.Body.String())
	require.Equal(t, "https://app.example.test/"+ti.orgSlug+"/billing", recorder.Header().Get("Location"))

	metadata, err := repo.New(ti.db).GetBillingMetadata(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.True(t, metadata.StripeCustomerID.Valid)
	require.True(t, metadata.StripeSubscriptionID.Valid)
	require.True(t, metadata.StripeBillingCycleAnchor.Valid)

	organization, err := orgrepo.New(ti.db).GetOrganizationMetadata(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.Equal(t, "payg", organization.GramAccountType)
	require.True(t, organization.Whitelisted)

	subscription, err := ti.service.GetStripeSubscription(ti.adminContext(t), &gen.GetStripeSubscriptionPayload{})
	require.NoError(t, err)
	require.Contains(t, []string{"active", "trialing"}, subscription.Status)

	after, err := audittest.AuditLogCountByAction(t.Context(), ti.db, audit.ActionOrganizationPaygActivated)
	require.NoError(t, err)
	require.Equal(t, baseline+1, after)
}

func TestAttachLocalStripeCheckoutRoute(t *testing.T) {
	t.Parallel()

	ti := newLocalStripeCheckoutTestInstance(t)
	mux := goahttp.NewMuxer()
	Attach(mux, ti.service)

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, stripeclient.LocalCheckoutPath, nil))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestAttachOmitsLocalStripeCheckoutRouteWithoutCompleter(t *testing.T) {
	t.Parallel()

	service := &Service{
		logger:       testenv.NewLogger(t),
		stripeClient: &fakeStripeWebhookClient{},
	}
	mux := goahttp.NewMuxer()
	Attach(mux, service)

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, stripeclient.LocalCheckoutPath, nil))
	require.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestLocalStripeCheckoutRejectsInvalidSuccessURL(t *testing.T) {
	t.Parallel()

	_, err := localCheckoutRedirectURL("/relative")
	require.Error(t, err)
	_, err = localCheckoutRedirectURL("javascript:alert(1)")
	require.Error(t, err)
}

func newLocalStripeCheckoutTestInstance(t *testing.T) *stripeCheckoutTestInstance {
	t.Helper()

	ti := newStripeCheckoutTestInstance(t)
	require.NoError(t, authz.SeedSystemRoleGrantsTx(t.Context(), ti.db, ti.orgID))
	publicURL, err := url.Parse("https://localhost:8000")
	require.NoError(t, err)
	stripe := stripeclient.NewStubClient(testenv.NewLogger(t), publicURL)
	ti.service.stripeClient = stripe
	ti.service.stripeHandler = ti.service.serviceStripeWebhookHandler
	ti.service.auditLogger = audit.NewLogger()
	ti.stripe = nil
	return ti
}

func TestCreateStripeCheckoutStillFailsClosedForExplicitDisable(t *testing.T) {
	t.Parallel()

	ti := newLocalStripeCheckoutTestInstance(t)
	ti.flags.SetFlag(feature.FlagPaygSelfServeBilling, ti.orgID, false)

	_, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}
