package remotesessions_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestRegisterRemoteSessionIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	const upstreamClientID = "register-endpoint-client"
	const upstreamSecret = "register-endpoint-secret"

	var (
		seenMethod  string
		seenPayload map[string]any
	)
	dcr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&seenPayload)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"client_id":                upstreamClientID,
			"client_secret":            upstreamSecret,
			"client_id_issued_at":      time.Now().Unix(),
			"client_secret_expires_at": time.Now().Add(time.Hour).Unix(),
		})
	}))
	t.Cleanup(dcr.Close)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsi-register", dcr.URL+"/register")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-register").String()

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientCreate)
	require.NoError(t, err)

	clientName := "registration-client-name"
	redirectURIs := []string{"https://app.example.com/callback"}
	result, err := ti.service.RegisterRemoteSessionIssuer(ctx, &gen.RegisterRemoteSessionIssuerPayload{
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerID:   userIssuerID,
		ClientName:            &clientName,
		RedirectUris:          redirectURIs,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, upstreamClientID, result.ClientID)
	require.Equal(t, issuerID, result.RemoteSessionIssuerID)
	require.Equal(t, userIssuerID, result.UserSessionIssuerID)
	require.NotNil(t, result.ClientSecretExpiresAt)
	require.Equal(t, http.MethodPost, seenMethod)
	require.Equal(t, clientName, seenPayload["client_name"])
	require.ElementsMatch(t, []any{"https://app.example.com/callback"}, seenPayload["redirect_uris"])

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestRegisterRemoteSessionIssuer_NoRegistrationEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// Issuer with no registration_endpoint configured.
	issuerID := createRemoteIssuer(t, ctx, ti, "rsi-register-no-endpoint", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-register-no-endpoint").String()

	_, err := ti.service.RegisterRemoteSessionIssuer(ctx, &gen.RegisterRemoteSessionIssuerPayload{
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerID:   userIssuerID,
		ClientName:            nil,
		RedirectUris:          nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestRegisterRemoteSessionIssuer_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsi-register-rbac", "https://idp.example.com/register")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-register-rbac").String()

	// Hand the caller only read scope; register should be denied.
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeProjectRead,
		Selector: authz.NewSelector(authz.ScopeProjectRead, authCtx.ProjectID.String()),
	})

	_, err := ti.service.RegisterRemoteSessionIssuer(ctx, &gen.RegisterRemoteSessionIssuerPayload{
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerID:   userIssuerID,
		ClientName:            nil,
		RedirectUris:          nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestRegisterRemoteSessionIssuer_IssuerNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-register-missing").String()

	_, err := ti.service.RegisterRemoteSessionIssuer(ctx, &gen.RegisterRemoteSessionIssuerPayload{
		RemoteSessionIssuerID: uuid.NewString(),
		UserSessionIssuerID:   userIssuerID,
		ClientName:            nil,
		RedirectUris:          nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}
