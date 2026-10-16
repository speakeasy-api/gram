package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

const testMCPRegistryAuthRecord = "v=MCPv1; k=ed25519; p=dGVzdC1wdWJsaWMta2V5"

func TestHandleMCPRegistryAuthServesRecordOnPlatformHost(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	ti.service.SetMCPRegistryAuth(testMCPRegistryAuthRecord)
	recorder := httptest.NewRecorder()

	require.NoError(t, ti.service.HandleMCPRegistryAuth(recorder, newPlatformHostRegistryAuthRequest(ctx)))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "text/plain", recorder.Header().Get("Content-Type"))
	require.Equal(t, testMCPRegistryAuthRecord, recorder.Body.String())
}

func TestHandleMCPRegistryAuthReturnsNotFoundWhenUnset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	err := ti.service.HandleMCPRegistryAuth(httptest.NewRecorder(), newPlatformHostRegistryAuthRequest(ctx))

	requireMCPRegistryAuthNotFound(t, err)
}

func TestHandleMCPRegistryAuthReturnsNotFoundWithoutRequestOrigin(t *testing.T) {
	t.Parallel()

	_, ti := newTestMCPService(t)
	ti.service.SetMCPRegistryAuth(testMCPRegistryAuthRecord)
	req := httptest.NewRequest(http.MethodGet, "/.well-known/mcp-registry-auth", nil)
	err := ti.service.HandleMCPRegistryAuth(httptest.NewRecorder(), req)

	requireMCPRegistryAuthNotFound(t, err)
}

func TestHandleMCPRegistryAuthReturnsNotFoundOnCustomDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	ti.service.SetMCPRegistryAuth(testMCPRegistryAuthRecord)
	req := httptest.NewRequest(http.MethodGet, "/.well-known/mcp-registry-auth", nil)
	req = req.WithContext(customdomains.WithContext(ctx, &customdomains.Context{
		OrganizationID: "registry-auth-organization",
		Domain:         "registry-auth.example.com",
		DomainID:       uuid.New(),
	}))
	err := ti.service.HandleMCPRegistryAuth(httptest.NewRecorder(), req)

	requireMCPRegistryAuthNotFound(t, err)
}

func newPlatformHostRegistryAuthRequest(ctx context.Context) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/.well-known/mcp-registry-auth", nil)
	return req.WithContext(requestorigin.WithContext(ctx, requestorigin.Origin{
		Surface:          requestorigin.SurfacePlatform,
		BaseURL:          "https://app.example.com",
		OrganizationID:   "",
		NetworkIngressID: uuid.Nil,
		NetworkIdentity:  nil,
	}))
}

func requireMCPRegistryAuthNotFound(t *testing.T, err error) {
	t.Helper()

	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeNotFound, shareable.Code)
}
