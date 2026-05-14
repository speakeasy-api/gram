package oauthtest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// IssuerGatedToolsetOpts configures CreateIssuerGatedToolset.
type IssuerGatedToolsetOpts struct {
	// Slug prefix for the toolset. A UUID suffix is appended automatically.
	Slug string
	// IsPublic sets McpIsPublic on the toolset. Default false (private).
	IsPublic bool
	// UpstreamMetadata is RFC 8414 JSON describing the remote authorization
	// server (e.g. devidptest.Instance.OAuth21Metadata(t)). The helper reads
	// issuer / authorization_endpoint / token_endpoint / registration_endpoint
	// out of this document and DCR-registers a remote_session_client.
	UpstreamMetadata []byte
	// RemoteSessionCallbackBaseURL, when set, registers the Gram
	// /remote_login_callback URL for the generated MCP slug. Tests that drive
	// a real upstream authorize flow should set this to the Gram server URL.
	RemoteSessionCallbackBaseURL string
	// AuthnChallengeMode is "chain" or "interactive". Default "interactive".
	AuthnChallengeMode string
}

// IssuerGatedToolsetResult holds the rows created by CreateIssuerGatedToolset.
type IssuerGatedToolsetResult struct {
	Toolset             toolsets_repo.Toolset
	UserSessionIssuer   usersessions_repo.UserSessionIssuer
	RemoteSessionIssuer remotesessions_repo.RemoteSessionIssuer
	RemoteSessionClient remotesessions_repo.RemoteSessionClient
}

// CreateIssuerGatedToolset provisions a user_session_issuer + one
// remote_session_issuer + one DCR-registered remote_session_client wired to
// the provided upstream AS metadata, then creates a toolset bound to the
// user_session_issuer. Used by integration tests that exercise the
// /mcp/{slug}/connect + /mcp/{slug}/remote_login_callback handlers against
// a live dev-idp instance.
func CreateIssuerGatedToolset(
	t *testing.T,
	ctx context.Context,
	conn *pgxpool.Pool,
	enc *encryption.Client,
	authCtx *contextvalues.AuthContext,
	opts IssuerGatedToolsetOpts,
) IssuerGatedToolsetResult {
	t.Helper()

	require.NotNil(t, opts.UpstreamMetadata, "UpstreamMetadata is required")

	var meta struct {
		Issuer                string `json:"issuer"`
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint         string `json:"token_endpoint"`
		RegistrationEndpoint  string `json:"registration_endpoint"`
		JwksURI               string `json:"jwks_uri"`
	}
	require.NoError(t, json.Unmarshal(opts.UpstreamMetadata, &meta))
	require.NotEmpty(t, meta.RegistrationEndpoint, "upstream must support DCR for issuer-gated tests")

	suffix := uuid.New().String()[:8]
	if opts.Slug == "" {
		opts.Slug = "issuer-gated"
	}
	slug := opts.Slug + "-" + suffix
	mode := opts.AuthnChallengeMode
	if mode == "" {
		mode = "interactive"
	}

	usersRepo := usersessions_repo.New(conn)
	remoteRepo := remotesessions_repo.New(conn)
	toolsetsRepo := toolsets_repo.New(conn)

	usi, err := usersRepo.CreateUserSessionIssuer(ctx, usersessions_repo.CreateUserSessionIssuerParams{
		ProjectID:          *authCtx.ProjectID,
		Slug:               "usi-" + suffix,
		AuthnChallengeMode: mode,
		SessionDuration: pgtype.Interval{
			Microseconds: int64(time.Hour / time.Microsecond),
			Days:         0,
			Months:       0,
			Valid:        true,
		},
	})
	require.NoError(t, err)

	rsi, err := remoteRepo.CreateRemoteSessionIssuer(ctx, remotesessions_repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         *authCtx.ProjectID,
		Slug:                              "rsi-" + suffix,
		Issuer:                            meta.Issuer,
		AuthorizationEndpoint:             conv.ToPGText(meta.AuthorizationEndpoint),
		TokenEndpoint:                     conv.ToPGText(meta.TokenEndpoint),
		RegistrationEndpoint:              conv.ToPGText(meta.RegistrationEndpoint),
		JwksUri:                           conv.PtrToPGTextEmpty(conv.PtrEmpty(meta.JwksURI)),
		ScopesSupported:                   []string{},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
		Oidc:                              false,
		Passthrough:                       false,
	})
	require.NoError(t, err)

	redirectURIs := []string{"http://localhost/unused"}
	if opts.RemoteSessionCallbackBaseURL != "" {
		redirectURIs = []string{strings.TrimRight(opts.RemoteSessionCallbackBaseURL, "/") + "/mcp/" + slug + "/remote_login_callback"}
	}
	clientID, clientSecret := dcrRegister(t, ctx, meta.RegistrationEndpoint, "test-issuer-gated-"+suffix, redirectURIs)
	encSecret, err := enc.Encrypt([]byte(clientSecret))
	require.NoError(t, err)

	rsc, err := remoteRepo.CreateRemoteSessionClient(ctx, remotesessions_repo.CreateRemoteSessionClientParams{
		ProjectID:             *authCtx.ProjectID,
		RemoteSessionIssuerID: rsi.ID,
		UserSessionIssuerID:   usi.ID,
		ClientID:              clientID,
		ClientSecretEncrypted: conv.ToPGText(encSecret),
		ClientIDIssuedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
		ClientSecretExpiresAt: pgtype.Timestamptz{Valid: false},
	})
	require.NoError(t, err)

	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Issuer-Gated MCP " + suffix,
		Slug:                   slug,
		Description:            conv.ToPGText("Test toolset bound to user_session_issuer"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText(slug),
		McpEnabled:             true,
	})
	require.NoError(t, err)

	if opts.IsPublic {
		toolset, err = toolsetsRepo.UpdateToolset(ctx, toolsets_repo.UpdateToolsetParams{
			Name:                   toolset.Name,
			Description:            toolset.Description,
			DefaultEnvironmentSlug: toolset.DefaultEnvironmentSlug,
			McpSlug:                toolset.McpSlug,
			McpIsPublic:            true,
			McpEnabled:             toolset.McpEnabled,
			CustomDomainID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			ToolSelectionMode:      "",
			Slug:                   toolset.Slug,
			ProjectID:              toolset.ProjectID,
		})
		require.NoError(t, err)
	}

	toolset, err = toolsetsRepo.UpdateToolsetUserSessionIssuer(ctx, toolsets_repo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: usi.ID, Valid: true},
		Slug:                toolset.Slug,
		ProjectID:           toolset.ProjectID,
	})
	require.NoError(t, err)

	return IssuerGatedToolsetResult{
		Toolset:             toolset,
		UserSessionIssuer:   usi,
		RemoteSessionIssuer: rsi,
		RemoteSessionClient: rsc,
	}
}

// dcrRegister POSTs an RFC 7591 client registration to the given endpoint
// and returns the issued (client_id, client_secret).
func dcrRegister(t *testing.T, ctx context.Context, endpoint, clientName string, redirectURIs []string) (string, string) {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"client_name":                clientName,
		"redirect_uris":              redirectURIs,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "client_secret_basic",
	})
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, 2, resp.StatusCode/100, "DCR %s: %s", resp.Status, strings.TrimSpace(string(raw)))

	var out struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	require.NoError(t, json.Unmarshal(raw, &out))
	require.NotEmpty(t, out.ClientID)
	require.NotEmpty(t, out.ClientSecret)
	return out.ClientID, out.ClientSecret
}
