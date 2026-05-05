package toolsets

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	environmentsRepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oauth"
	oauthRepo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func (s *Service) UpdateOAuthProxyServer(ctx context.Context, payload *gen.UpdateOAuthProxyServerPayload) (*types.Toolset, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	form := payload.OauthProxyServer

	toolsetDetails, err := mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(payload.Slug), new(s.toolsetCache.SkipCache()))
	if err != nil {
		return nil, err
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: toolsetDetails.ID, Dimensions: nil}); err != nil {
		return nil, err
	}

	// No-op short-circuit: if the caller sent nothing, return the current state
	// without opening a transaction or emitting an audit event.
	if form == nil ||
		(form.Audience == nil &&
			form.AuthorizationEndpoint == nil &&
			form.TokenEndpoint == nil &&
			form.ScopesSupported == nil &&
			form.TokenEndpointAuthMethodsSupported == nil &&
			form.EnvironmentSlug == nil) {
		return toolsetDetails, nil
	}

	// Validate token_endpoint_auth_methods_supported values if provided.
	if form.TokenEndpointAuthMethodsSupported != nil {
		for _, method := range form.TokenEndpointAuthMethodsSupported {
			if !validOAuthProxyAuthMethods[method] {
				return nil, oops.E(oops.CodeBadRequest, nil, "invalid token_endpoint_auth_methods_supported value: %s (must be client_secret_basic, client_secret_post, or none)", method).Log(ctx, s.logger)
			}
		}
	}

	// Reject empty-string endpoints. Without this guard an API caller could
	// send authorization_endpoint: "" or token_endpoint: "" which would flow
	// through conv.PtrToPGText as pgtype.Text{Valid: true, String: ""} and
	// COALESCE would overwrite the existing endpoint with empty string,
	// breaking OAuth for all users of the toolset. Mirrors AddOAuthProxyServer's
	// validation at impl.go:1008-1012.
	if form.AuthorizationEndpoint != nil && *form.AuthorizationEndpoint == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "authorization_endpoint cannot be empty").Log(ctx, s.logger)
	}
	if form.TokenEndpoint != nil && *form.TokenEndpoint == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "token_endpoint cannot be empty").Log(ctx, s.logger)
	}

	// Reject empty arrays for auth methods on update. The create path requires
	// it to be non-empty for custom providers, and clearing it on update would
	// produce a proxy that can't function. (scopes_supported is allowed to be
	// empty — many MCP servers don't advertise scopes in their well-known doc.)
	if form.TokenEndpointAuthMethodsSupported != nil && len(form.TokenEndpointAuthMethodsSupported) == 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "token_endpoint_auth_methods_supported cannot be empty").Log(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing OAuth proxy servers").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	// Load the toolset inside the transaction so the returned view reflects a consistent snapshot.
	toolsetDetails, err = mv.DescribeToolset(ctx, s.logger, dbtx, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(payload.Slug), new(s.toolsetCache.SkipCache()))
	if err != nil {
		return nil, err
	}

	if toolsetDetails.OauthProxyServer == nil {
		return nil, oops.E(oops.CodeNotFound, nil, "no OAuth proxy server attached to this toolset").Log(ctx, s.logger)
	}

	// Capture the pre-update state for the audit log. mv.DescribeToolset returns a fresh
	// allocation each call, so this pointer remains stable through the re-fetch below.
	toolsetSnapshotBefore := toolsetDetails

	toolsetID, err := uuid.Parse(toolsetDetails.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "invalid toolset ID").Log(ctx, s.logger)
	}

	serverID, err := uuid.Parse(toolsetDetails.OauthProxyServer.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "invalid OAuth proxy server ID").Log(ctx, s.logger)
	}

	// Load the provider row so we can (a) check provider_type and (b) read existing
	// secrets for the environment_slug read-modify-write path.
	provider, err := s.oauthRepo.WithTx(dbtx).GetOAuthProxyProviderByServer(ctx, oauthRepo.GetOAuthProxyProviderByServerParams{
		OauthProxyServerID: serverID,
		ProjectID:          *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "OAuth proxy provider not found").Log(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to load OAuth proxy provider").Log(ctx, s.logger)
	}

	// Gram-managed servers cannot be edited via this endpoint.
	if oauth.OAuthProxyProviderType(provider.ProviderType) == oauth.OAuthProxyProviderTypeGram {
		return nil, oops.E(oops.CodeBadRequest, nil, "gram-managed OAuth proxy servers cannot be edited via this endpoint").Log(ctx, s.logger)
	}

	// Validate environment_slug if provided.
	if form.EnvironmentSlug != nil {
		if string(*form.EnvironmentSlug) == "" {
			return nil, oops.E(oops.CodeBadRequest, nil, "environment_slug cannot be empty").Log(ctx, s.logger)
		}
		_, err = s.environmentRepo.WithTx(dbtx).GetEnvironmentBySlug(ctx, environmentsRepo.GetEnvironmentBySlugParams{
			Slug:      string(*form.EnvironmentSlug),
			ProjectID: *authCtx.ProjectID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, err, "environment not found").Log(ctx, s.logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "failed to get environment").Log(ctx, s.logger)
		}
	}

	// Apply server-row update (audience) if provided.
	if form.Audience != nil {
		_, err = s.oauthRepo.WithTx(dbtx).UpdateOAuthProxyServerAudience(ctx, oauthRepo.UpdateOAuthProxyServerAudienceParams{
			ID:        serverID,
			ProjectID: *authCtx.ProjectID,
			Audience:  conv.PtrToPGText(form.Audience),
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, err, "OAuth proxy server not found").Log(ctx, s.logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "failed to update OAuth proxy server audience").Log(ctx, s.logger)
		}
	}

	// Apply provider-row update if any provider field was provided.
	providerFieldChanged := form.AuthorizationEndpoint != nil ||
		form.TokenEndpoint != nil ||
		form.ScopesSupported != nil ||
		form.TokenEndpointAuthMethodsSupported != nil ||
		form.EnvironmentSlug != nil

	if providerFieldChanged {
		// Compute secrets: read-modify-write for environment_slug.
		var secretsBytes []byte
		if form.EnvironmentSlug != nil {
			// Unmarshal existing secrets, set environment_slug, marshal back.
			existingSecrets := map[string]string{}
			if provider.Secrets != nil {
				if err := json.Unmarshal(provider.Secrets, &existingSecrets); err != nil {
					return nil, oops.E(oops.CodeUnexpected, err, "failed to unmarshal provider secrets").Log(ctx, s.logger)
				}
			}
			existingSecrets["environment_slug"] = string(*form.EnvironmentSlug)
			secretsBytes, err = json.Marshal(existingSecrets)
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "failed to marshal provider secrets").Log(ctx, s.logger)
			}
		}
		// secretsBytes == nil when environment_slug was not provided → COALESCE keeps existing value.

		_, err = s.oauthRepo.WithTx(dbtx).UpdateOAuthProxyProviderFields(ctx, oauthRepo.UpdateOAuthProxyProviderFieldsParams{
			ID:                                provider.ID,
			OauthProxyServerID:                serverID,
			ProjectID:                         *authCtx.ProjectID,
			AuthorizationEndpoint:             conv.PtrToPGText(form.AuthorizationEndpoint),
			TokenEndpoint:                     conv.PtrToPGText(form.TokenEndpoint),
			ScopesSupported:                   form.ScopesSupported,
			TokenEndpointAuthMethodsSupported: form.TokenEndpointAuthMethodsSupported,
			Secrets:                           secretsBytes,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, err, "OAuth proxy provider not found").Log(ctx, s.logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "failed to update OAuth proxy provider fields").Log(ctx, s.logger)
		}
	}

	// Re-fetch the toolset so the returned value reflects all updates.
	toolsetDetails, err = mv.DescribeToolset(ctx, s.logger, dbtx, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(payload.Slug), new(s.toolsetCache.SkipCache()))
	if err != nil {
		return nil, err
	}

	// Emit audit event inside the transaction (before commit). Pass before/after
	// toolset snapshots so the audit log captures what actually changed, not just
	// that an update occurred. Mirrors LogToolsetUpdate's snapshot pattern.
	if err := s.audit.LogToolsetUpdateOAuthProxy(ctx, dbtx, audit.LogToolsetUpdateOAuthProxyEvent{
		OrganizationID:        authCtx.ActiveOrganizationID,
		ProjectID:             *authCtx.ProjectID,
		Actor:                 urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:      authCtx.Email,
		ActorSlug:             nil,
		ToolsetURN:            urn.NewToolset(toolsetID),
		ToolsetName:           toolsetDetails.Name,
		ToolsetSlug:           string(toolsetDetails.Slug),
		ToolsetVersionAfter:   toolsetDetails.ToolsetVersion,
		OAuthProxyServerID:    toolsetDetails.OauthProxyServer.ID,
		OAuthProxyServerSlug:  string(toolsetDetails.OauthProxyServer.Slug),
		ToolsetSnapshotBefore: toolsetSnapshotBefore,
		ToolsetSnapshotAfter:  toolsetDetails,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to log toolset update").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating OAuth proxy server").Log(ctx, s.logger)
	}

	return toolsetDetails, nil
}
