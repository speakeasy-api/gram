package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

func TestHandleOpenAIAppsChallengeServesConfiguredTokenWithoutRootMapping(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	const (
		organizationID = "challenge-handler-organization"
		domainName     = "challenge-handler.example.com"
		token          = "openai-domain-challenge"
	)
	repository := customdomainsrepo.New(ti.conn)
	domain, err := repository.CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID:  organizationID,
		Domain:          domainName,
		IngressName:     pgtype.Text{},
		CertSecretName:  pgtype.Text{},
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)
	_, err = repository.UpdateCustomDomainSettings(ctx, customdomainsrepo.UpdateCustomDomainSettingsParams{
		UpdateIpAllowlist:              false,
		IpAllowlist:                    []string{},
		UpdateOpenaiAppsChallengeToken: true,
		OpenaiAppsChallengeToken:       pgtype.Text{String: token, Valid: true},
		OrganizationID:                 organizationID,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/openai-apps-challenge", nil)
	req = req.WithContext(customdomains.WithContext(ctx, &customdomains.Context{
		OrganizationID: organizationID,
		Domain:         domainName,
		DomainID:       domain.ID,
	}))
	recorder := httptest.NewRecorder()

	require.NoError(t, ti.service.HandleOpenAIAppsChallenge(recorder, req))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "text/plain", recorder.Header().Get("Content-Type"))
	require.Equal(t, token, recorder.Body.String())
}

func TestHandleOpenAIAppsChallengeReturnsNotFoundWhenUnset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	const (
		organizationID = "challenge-unset-organization"
		domainName     = "challenge-unset.example.com"
	)
	domain, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID:  organizationID,
		Domain:          domainName,
		IngressName:     pgtype.Text{},
		CertSecretName:  pgtype.Text{},
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/openai-apps-challenge", nil)
	req = req.WithContext(customdomains.WithContext(ctx, &customdomains.Context{
		OrganizationID: organizationID,
		Domain:         domainName,
		DomainID:       domain.ID,
	}))
	err = ti.service.HandleOpenAIAppsChallenge(httptest.NewRecorder(), req)

	requireOpenAIAppsChallengeNotFound(t, err)
}

func TestHandleOpenAIAppsChallengeReturnsNotFoundOnPlatformHostWhenUnset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	err := ti.service.HandleOpenAIAppsChallenge(httptest.NewRecorder(), newPlatformHostChallengeRequest(ctx))

	requireOpenAIAppsChallengeNotFound(t, err)
}

func TestHandleOpenAIAppsChallengeServesPlatformTokenOnPlatformHost(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	const token = "openai-platform-challenge"
	ti.service.SetOpenAIAppsChallengeToken(token)
	recorder := httptest.NewRecorder()

	require.NoError(t, ti.service.HandleOpenAIAppsChallenge(recorder, newPlatformHostChallengeRequest(ctx)))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "text/plain", recorder.Header().Get("Content-Type"))
	require.Equal(t, token, recorder.Body.String())
}

func TestHandleOpenAIAppsChallengeReturnsNotFoundWithoutRequestOrigin(t *testing.T) {
	t.Parallel()

	_, ti := newTestMCPService(t)
	ti.service.SetOpenAIAppsChallengeToken("openai-platform-challenge")
	req := httptest.NewRequest(http.MethodGet, "/.well-known/openai-apps-challenge", nil)
	err := ti.service.HandleOpenAIAppsChallenge(httptest.NewRecorder(), req)

	requireOpenAIAppsChallengeNotFound(t, err)
}

func TestHandleOpenAIAppsChallengeIgnoresPlatformTokenOnCustomDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	ti.service.SetOpenAIAppsChallengeToken("openai-platform-challenge")
	const (
		organizationID = "challenge-platform-token-organization"
		domainName     = "challenge-platform-token.example.com"
	)
	domain, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID:  organizationID,
		Domain:          domainName,
		IngressName:     pgtype.Text{},
		CertSecretName:  pgtype.Text{},
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/openai-apps-challenge", nil)
	req = req.WithContext(customdomains.WithContext(ctx, &customdomains.Context{
		OrganizationID: organizationID,
		Domain:         domainName,
		DomainID:       domain.ID,
	}))
	err = ti.service.HandleOpenAIAppsChallenge(httptest.NewRecorder(), req)

	requireOpenAIAppsChallengeNotFound(t, err)
}

func newPlatformHostChallengeRequest(ctx context.Context) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/.well-known/openai-apps-challenge", nil)
	return req.WithContext(requestorigin.WithContext(ctx, requestorigin.Origin{
		Surface:          requestorigin.SurfacePlatform,
		BaseURL:          "https://app.example.com",
		OrganizationID:   "",
		NetworkIngressID: uuid.Nil,
		NetworkIdentity:  nil,
	}))
}

func requireOpenAIAppsChallengeNotFound(t *testing.T, err error) {
	t.Helper()

	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeNotFound, shareable.Code)
}
