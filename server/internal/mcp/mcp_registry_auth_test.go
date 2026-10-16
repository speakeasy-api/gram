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

const testMCPRegistryAuthRecord = "v=MCPv1; k=ed25519; p=AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="

func TestHandleMCPRegistryAuthServesRecordOnPlatformHost(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	require.NoError(t, ti.service.SetMCPRegistryAuth(testMCPRegistryAuthRecord))
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
	require.NoError(t, ti.service.SetMCPRegistryAuth(testMCPRegistryAuthRecord))
	req := httptest.NewRequest(http.MethodGet, "/.well-known/mcp-registry-auth", nil)
	err := ti.service.HandleMCPRegistryAuth(httptest.NewRecorder(), req)

	requireMCPRegistryAuthNotFound(t, err)
}

func TestHandleMCPRegistryAuthReturnsNotFoundOnCustomDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	require.NoError(t, ti.service.SetMCPRegistryAuth(testMCPRegistryAuthRecord))
	req := httptest.NewRequest(http.MethodGet, "/.well-known/mcp-registry-auth", nil)
	ctx = customdomains.WithContext(ctx, &customdomains.Context{
		OrganizationID: "registry-auth-organization",
		Domain:         "registry-auth.example.com",
		DomainID:       uuid.New(),
	})
	req = req.WithContext(requestorigin.WithContext(ctx, requestorigin.Origin{
		Surface:          requestorigin.SurfaceCustomDomain,
		BaseURL:          "https://registry-auth.example.com",
		OrganizationID:   "registry-auth-organization",
		NetworkIngressID: uuid.Nil,
		NetworkIdentity:  nil,
	}))
	err := ti.service.HandleMCPRegistryAuth(httptest.NewRecorder(), req)

	requireMCPRegistryAuthNotFound(t, err)
}

func TestSetMCPRegistryAuthRejectsMalformedRecords(t *testing.T) {
	t.Parallel()

	_, ti := newTestMCPService(t)
	for name, record := range map[string]string{
		"hex private key":   "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60",
		"wrong version":     "v=MCPv2; k=ed25519; p=AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
		"unknown key type":  "v=MCPv1; k=rsa; p=AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
		"short key":         "v=MCPv1; k=ed25519; p=dGVzdA==",
		"not base64":        "v=MCPv1; k=ed25519; p=not-base64!",
		"missing key field": "v=MCPv1; k=ed25519",
	} {
		require.Error(t, ti.service.SetMCPRegistryAuth(record), name)
	}
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
