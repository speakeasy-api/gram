package mcpmetadata_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featuresrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// offerOrganizationGateway gives an organization the one organization-level
// issuer the gateway signs people in through, and turns the gateway on.
func offerOrganizationGateway(t *testing.T, ctx context.Context, ti *testInstance, organizationID string) {
	t.Helper()

	_, err := usersessionsrepo.New(ti.conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:     conv.ToPGText(organizationID),
		Slug:               "org-gateway-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)

	features := featuresrepo.New(ti.conn)
	setting := featuresrepo.EnableFeatureParams{OrganizationID: organizationID, FeatureName: string(productfeatures.FeatureOrganizationGatewayDisabled)}
	_, err = features.EnableFeature(ctx, setting)
	require.NoError(t, err)
	_, err = features.DeleteFeature(ctx, featuresrepo.DeleteFeatureParams(setting))
	require.NoError(t, err)
}

func serveOrganizationGatewayPage(t *testing.T, ctx context.Context, ti *testInstance, organizationSlug string) *httptest.ResponseRecorder {
	t.Helper()

	ctx = requestorigin.WithContext(ctx, requestorigin.Origin{Surface: requestorigin.SurfacePlatform, BaseURL: ti.serverURL.String()})
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/mcp/gateway/"+organizationSlug, nil)
	req.Header.Set("Accept", "text/html")
	rr := httptest.NewRecorder()
	require.NoError(t, ti.service.ServeOrganizationGatewayPage(rr, req, organizationSlug))
	return rr
}

func TestServeOrganizationGatewayPage_MemberSeesSetupGuidance(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPMetadataService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	offerOrganizationGateway(t, ctx, ti, authCtx.ActiveOrganizationID)

	rr := serveOrganizationGatewayPage(t, ctx, ti, authCtx.OrganizationSlug)

	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Equal(t, "no-store", rr.Header().Get("Cache-Control"))
	require.Contains(t, rr.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'")
	body := rr.Body.String()
	gatewayURL := ti.serverURL.String() + "/mcp/gateway/" + authCtx.OrganizationSlug
	require.Contains(t, body, gatewayURL)
	require.Contains(t, body, "claude mcp add --transport http --scope user &#39;"+authCtx.OrganizationSlug+"&#39;")
	require.Contains(t, body, "codex mcp login")
	require.Contains(t, body, "cursor://anysphere.cursor-deeplink/mcp/install")
	require.NotContains(t, body, "isn't available")
}

func TestServeOrganizationGatewayPage_UnavailableTellsMemberToAskAnAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPMetadataService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	rr := serveOrganizationGatewayPage(t, ctx, ti, authCtx.OrganizationSlug)

	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	body := rr.Body.String()
	require.Contains(t, body, "isn't available")
	require.NotContains(t, body, "/mcp/gateway/"+authCtx.OrganizationSlug, "an unavailable gateway offers no URL")
}

func TestServeOrganizationGatewayPage_SignedOutVisitorIsSentToLogin(t *testing.T) {
	t.Parallel()
	_, ti := newTestMCPMetadataService(t)

	rr := serveOrganizationGatewayPage(t, context.Background(), ti, "any-organization")

	require.Equal(t, http.StatusFound, rr.Code)
	loginURL, err := url.Parse(rr.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "/login", loginURL.Path)
	require.Equal(t, "/mcp/gateway/any-organization", loginURL.Query().Get("redirect"))
}

func TestServeOrganizationGatewayPage_NonMemberGetsNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPMetadataService(t)

	otherSlug := "other-" + uuid.NewString()[:8]
	other, err := orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:             "org-" + uuid.NewString(),
		Name:           "Other Organization Name",
		Slug:           otherSlug,
		WorkosID:       pgtype.Text{String: "", Valid: false},
		Whitelisted:    pgtype.Bool{Bool: false, Valid: false},
		CreationSource: pgtype.Text{String: "", Valid: false},
		DefaultHost:    pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	offerOrganizationGateway(t, ctx, ti, other.ID)

	rr := serveOrganizationGatewayPage(t, ctx, ti, otherSlug)

	require.Equal(t, http.StatusNotFound, rr.Code)
	require.NotContains(t, rr.Body.String(), "Other Organization Name")
}

func TestServeOrganizationGatewayPage_UnknownOrganizationGetsNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPMetadataService(t)

	rr := serveOrganizationGatewayPage(t, ctx, ti, "missing-"+uuid.NewString()[:8])

	require.Equal(t, http.StatusNotFound, rr.Code)
}

func TestServeOrganizationGatewayPage_CustomDomainGetsNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPMetadataService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	offerOrganizationGateway(t, ctx, ti, authCtx.ActiveOrganizationID)

	domainCtx := customdomains.WithContext(ctx, &customdomains.Context{OrganizationID: authCtx.ActiveOrganizationID, Domain: "gateway.example.com", DomainID: uuid.New()})
	rr := serveOrganizationGatewayPage(t, domainCtx, ti, authCtx.OrganizationSlug)

	require.Equal(t, http.StatusNotFound, rr.Code)
}
