package slackdirectoryconnections_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
	slackapi "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/api"
	"github.com/stretchr/testify/require"
)

func protocolProvider(t *testing.T, change func(map[string]any), verifiedTeam string, beforeVerify ...func()) *slackdirectoryconnections.OAuthProvider {
	t.Helper()
	type requestCheck struct {
		ClientID      string
		Secret        string
		BasicAuth     bool
		ParseError    error
		RedirectURI   string
		Authorization string
		EncodeError   error
		Method        string
	}
	checks := make(chan requestCheck, 4)
	t.Cleanup(func() {
		close(checks)
		for check := range checks {
			require.NoError(t, check.ParseError)
			require.NoError(t, check.EncodeError)
			if check.Method == "oauth" {
				require.True(t, check.BasicAuth)
				require.Equal(t, "synthetic-client", check.ClientID)
				require.Equal(t, "synthetic-secret", check.Secret)
				require.Equal(t, "https://dashboard.example/slack-directory/callback", check.RedirectURI)
			} else {
				require.Equal(t, "Bearer synthetic-bot-token", check.Authorization)
			}
		}
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth.v2.access", func(w http.ResponseWriter, r *http.Request) {
		id, secret, ok := r.BasicAuth()
		parseErr := r.ParseForm()
		body := map[string]any{"ok": true, "access_token": "synthetic-bot-token", "refresh_token": "synthetic-refresh", "expires_in": 43200, "token_type": "bot", "scope": "users:read,users:read.email", "team": map[string]any{"id": "TEXAMPLE01", "name": "Example workspace"}, "enterprise": map[string]any{"id": "EEXAMPLE01"}, "is_enterprise_install": false}
		if change != nil {
			change(body)
		}
		encodeErr := json.NewEncoder(w).Encode(body)
		checks <- requestCheck{ClientID: id, Secret: secret, BasicAuth: ok, ParseError: parseErr, RedirectURI: r.Form.Get("redirect_uri"), EncodeError: encodeErr, Method: "oauth"}
	})
	mux.HandleFunc("/auth.test", func(w http.ResponseWriter, r *http.Request) {
		for _, hook := range beforeVerify {
			hook()
		}
		encodeErr := json.NewEncoder(w).Encode(map[string]any{"ok": true, "team_id": verifiedTeam, "team": "Example workspace"})
		checks <- requestCheck{Authorization: r.Header.Get("Authorization"), EncodeError: encodeErr, Method: "verify"}
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return slackdirectoryconnections.NewOAuthProvider(slackapi.NewClient(server.URL, server.Client()), "synthetic-client", "synthetic-secret", "https://dashboard.example/slack-directory/callback")
}

func TestProviderExpiryStartsBeforeVerification(t *testing.T) {
	t.Parallel()
	verificationStarted := make(chan time.Time, 1)
	p := protocolProvider(t, nil, "TEXAMPLE01", func() { verificationStarted <- time.Now() })
	result, err := p.Exchange(t.Context(), "synthetic-code")
	require.NoError(t, err)
	require.NotNil(t, result.Tokens.ExpiresAt)
	require.True(t, result.Tokens.ExpiresAt.Before((<-verificationStarted).Add(12*time.Hour)))
}

func TestProviderWorkspaceInstallAndRotation(t *testing.T) {
	t.Parallel()
	p := protocolProvider(t, nil, "TEXAMPLE01")
	result, err := p.Exchange(t.Context(), "synthetic-code")
	require.NoError(t, err)
	require.Equal(t, "TEXAMPLE01", result.WorkspaceID)
	require.Equal(t, "synthetic-refresh", result.Tokens.RefreshToken)
	require.NotNil(t, result.Tokens.ExpiresAt)
	require.WithinDuration(t, time.Now().Add(12*time.Hour), *result.Tokens.ExpiresAt, time.Second)
	require.Contains(t, p.AuthorizationURL("synthetic-state", "TEXAMPLE01"), "team=TEXAMPLE01")
}

func TestProviderTeamMismatch(t *testing.T) {
	t.Parallel()
	p := protocolProvider(t, nil, "TEXAMPLE02")
	_, err := p.Exchange(t.Context(), "synthetic-code")
	require.ErrorContains(t, err, "team_mismatch")
}

func TestProviderEnterpriseInstall(t *testing.T) {
	t.Parallel()
	p := protocolProvider(t, func(b map[string]any) { b["is_enterprise_install"] = true }, "TEXAMPLE01")
	_, err := p.Exchange(t.Context(), "synthetic-code")
	require.ErrorContains(t, err, "enterprise_install")
	require.NotContains(t, err.Error(), "synthetic-bot-token")
}

func TestProviderUserToken(t *testing.T) {
	t.Parallel()
	p := protocolProvider(t, func(b map[string]any) { b["token_type"] = "user" }, "TEXAMPLE01")
	_, err := p.Exchange(t.Context(), "synthetic-code")
	require.ErrorContains(t, err, "workspace_required")
	require.NotContains(t, err.Error(), "synthetic-bot-token")
}

func TestProviderMissingScope(t *testing.T) {
	t.Parallel()
	p := protocolProvider(t, func(b map[string]any) { b["scope"] = "users:read" }, "TEXAMPLE01")
	_, err := p.Exchange(t.Context(), "synthetic-code")
	require.ErrorContains(t, err, "scope_missing")
	require.NotContains(t, err.Error(), "synthetic-bot-token")
}

func TestProviderMissingRefresh(t *testing.T) {
	t.Parallel()
	p := protocolProvider(t, func(b map[string]any) { b["refresh_token"] = "" }, "TEXAMPLE01")
	_, err := p.Exchange(t.Context(), "synthetic-code")
	require.ErrorContains(t, err, "rotation_invalid")
	require.NotContains(t, err.Error(), "synthetic-bot-token")
}

func TestProviderBadClient(t *testing.T) {
	t.Parallel()
	p := protocolProvider(t, func(b map[string]any) { b["error"] = "invalid_client_id"; b["ok"] = false }, "TEXAMPLE01")
	_, err := p.Exchange(t.Context(), "synthetic-code")
	require.ErrorContains(t, err, "invalid_client_id")
	require.NotContains(t, err.Error(), "synthetic-bot-token")
}

func TestProviderSensitiveProviderError(t *testing.T) {
	t.Parallel()
	p := protocolProvider(t, func(b map[string]any) { b["error"] = "synthetic-bot-token"; b["ok"] = false }, "TEXAMPLE01")
	_, err := p.Exchange(t.Context(), "synthetic-code")
	require.ErrorContains(t, err, "provider_rejected")
	require.NotContains(t, err.Error(), "synthetic-bot-token")
}
