// Migration of legacy OAuth proxy client registrations into
// user_session_clients, run as part of CloneClientFromOAuthProxyProvider.
//
// Legacy registrations live in Redis only, written by
// oauth.ClientRegistrationService (server/internal/oauth/client_registration.go)
// and keyed by whichever public MCP URL the registering client hit. The oauth
// package imports remotesessions, so this file re-declares the narrow slice
// of that wire format it reads — the cache key layout and the registration
// fields the migration needs — rather than importing it back. The clone tests
// seed Redis through the real oauth typed cache, pinning the two declarations
// together; this whole file goes away with the legacy OAuth proxy.

package remotesessions

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// LegacyRegistrationStore is the slice of the Redis cache the clone handler
// scans for legacy OAuth proxy client registrations. Satisfied by
// *cache.RedisCacheAdapter.
type LegacyRegistrationStore interface {
	Get(ctx context.Context, key string, value any) error
	ScanKeys(ctx context.Context, prefix string) ([]string, error)
}

// legacyClientInfoCacheKey mirrors oauth.ClientInfoCacheKey. Stored keys
// carry one extra trailing ":" (appended by cache.TypedCacheObject with
// cache.SuffixNone), which the prefix scan tolerates and raw Gets receive
// verbatim from ScanKeys.
func legacyClientInfoCacheKey(mcpURL string, clientID string) string {
	return "oauthClientInfo:" + mcpURL + ":" + clientID
}

// legacyProxyClientInfo mirrors the subset of oauth.OauthProxyClientInfo
// fields the migration reads. Field names must match the source struct
// exactly: values are decoded by the same codec that wrote them, which keys
// struct fields by name.
type legacyProxyClientInfo struct {
	ClientID                string
	ClientSecret            string
	ClientName              string
	RedirectUris            []string
	TokenEndpointAuthMethod string
}

// migrateLegacyClientRegistrations finds every MCP server attached to the
// oauth_proxy_server being cloned, scans Redis for dynamic client
// registrations under each server's public URL variants, and ensures a
// matching user_session_clients row exists on the target user_session_issuer
// — preserving each client_id so migrated MCP clients skip re-registration
// and re-auth. Runs on the clone's transaction: any failure aborts the whole
// clone so a partial migration never commits. Returns the number of rows
// inserted (already-migrated registrations count as zero).
func (s *Service) migrateLegacyClientRegistrations(ctx context.Context, txRepo *repo.Queries, projectID, oauthProxyServerID, userSessionIssuerID uuid.UUID) (int64, error) {
	endpoints, err := txRepo.ListToolsetMCPEndpointsForOAuthProxyServer(ctx, repo.ListToolsetMCPEndpointsForOAuthProxyServerParams{
		OauthProxyServerID: uuid.NullUUID{UUID: oauthProxyServerID, Valid: true},
		ProjectID:          projectID,
	})
	if err != nil {
		return 0, fmt.Errorf("list mcp endpoints for oauth proxy server: %w", err)
	}

	// Registrations are keyed by whichever URL the MCP client originally hit,
	// so collect every variant: the default-domain URL always, plus the
	// custom-domain URL when the toolset has one.
	baseURL := strings.TrimRight(s.serverURL.String(), "/")
	mcpURLs := make([]string, 0, len(endpoints)*2)
	for _, endpoint := range endpoints {
		mcpURLs = append(mcpURLs, baseURL+"/mcp/"+endpoint.McpSlug.String)
		if endpoint.CustomDomain.Valid && endpoint.CustomDomain.String != "" {
			mcpURLs = append(mcpURLs, "https://"+endpoint.CustomDomain.String+"/mcp/"+endpoint.McpSlug.String)
		}
	}

	seen := make(map[string]bool)
	var migrated int64
	for _, mcpURL := range mcpURLs {
		keys, err := s.legacyRegistrations.ScanKeys(ctx, legacyClientInfoCacheKey(mcpURL, ""))
		if err != nil {
			return 0, fmt.Errorf("scan legacy client registrations for %s: %w", mcpURL, err)
		}
		for _, key := range keys {
			var info legacyProxyClientInfo
			if err := s.legacyRegistrations.Get(ctx, key, &info); err != nil {
				return 0, fmt.Errorf("read legacy client registration %s: %w", key, err)
			}
			if info.ClientID == "" || seen[info.ClientID] {
				continue
			}
			seen[info.ClientID] = true

			// Public clients (token_endpoint_auth_method=none) carry no
			// secret and rely on PKCE; confidential clients keep a bcrypt
			// hash of their existing plaintext secret, matching how the
			// user-session AS stores secrets it mints itself.
			var secretHash pgtype.Text
			if info.TokenEndpointAuthMethod != "none" && info.ClientSecret != "" {
				hashed, hashErr := bcrypt.GenerateFromPassword([]byte(info.ClientSecret), bcrypt.DefaultCost)
				if hashErr != nil {
					return 0, fmt.Errorf("hash client secret for %s: %w", info.ClientID, hashErr)
				}
				secretHash = conv.ToPGText(string(hashed))
			}

			// A nil slice would encode as SQL NULL and trip the NOT NULL
			// constraint on redirect_uris.
			redirectURIs := info.RedirectUris
			if redirectURIs == nil {
				redirectURIs = []string{}
			}

			rows, err := txRepo.MigrateLegacyUserSessionClient(ctx, repo.MigrateLegacyUserSessionClientParams{
				ClientID:            info.ClientID,
				ClientSecretHash:    secretHash,
				ClientName:          info.ClientName,
				RedirectUris:        redirectURIs,
				UserSessionIssuerID: userSessionIssuerID,
				ProjectID:           projectID,
			})
			if err != nil {
				return 0, fmt.Errorf("insert migrated user session client %s: %w", info.ClientID, err)
			}
			migrated += rows
		}
	}

	return migrated, nil
}
