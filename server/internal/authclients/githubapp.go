package authclients

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/auth_clients"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authclients/githubapp"
	"github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type githubAppVerifier interface {
	Verify(context.Context, string, int64, int64, string) error
}

// WithGitHubApps configures encrypted customer-owned app credentials.
func WithGitHubApps(enc *encryption.Client, policy *guardian.Policy) func(*Service) {
	return func(s *Service) { s.enc = enc; s.githubApps = githubapp.New(s.db, enc, policy) }
}

func (s *Service) ConfigureGitHubAppAuthClient(ctx context.Context, payload *gen.ConfigureGitHubAppAuthClientPayload) (*types.AuthClient, error) {
	authCtx, logger, err := s.require(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}
	if s.githubApps == nil || s.enc == nil {
		return nil, oops.E(oops.CodeUnexpected, nil, "GitHub App credentials are unavailable")
	}
	serverID, err := uuid.Parse(payload.McpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid MCP server ID")
	}
	appID, err := strconv.ParseInt(payload.AppID, 10, 64)
	if err != nil || appID <= 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid GitHub App ID")
	}
	installationID, err := strconv.ParseInt(payload.InstallationID, 10, 64)
	if err != nil || installationID <= 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid installation ID")
	}
	instance, err := githubapp.Origin(payload.InstanceURL)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid GitHub instance URL")
	}
	origin, err := githubapp.Origin(payload.UpstreamOrigin)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid MCP origin")
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" || len(name) > 100 || len(payload.PrivateKey) > 32768 {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid name or private key")
	}
	// Check the target before any network request or secret processing.
	q := repo.New(s.db)
	target, err := q.GetGitHubAppServerURL(ctx, repo.GetGitHubAppServerURLParams{McpServerID: serverID, OrganizationID: authCtx.ActiveOrganizationID})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, nil, "remote MCP server not found")
	}
	u, err := url.Parse(target)
	if err != nil || u.User != nil || u.Scheme != "https" || "https://"+strings.TrimSuffix(strings.ToLower(u.Host), ":443") != origin {
		return nil, oops.E(oops.CodeBadRequest, nil, "allowed MCP origin must match the server's upstream URL")
	}
	if err := s.githubApps.Verify(ctx, instance, appID, installationID, payload.PrivateKey); err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "could not verify GitHub App installation; check the instance URL, app ID, installation ID, private key, and network access")
	}
	encrypted, err := s.enc.Encrypt([]byte(payload.PrivateKey))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "encrypt GitHub App key").LogError(ctx, logger)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	queries := repo.New(tx)
	server, err := queries.LockOrganizationMCPServer(ctx, repo.LockOrganizationMCPServerParams{ID: serverID, OrganizationID: authCtx.ActiveOrganizationID})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, nil, "MCP server not found")
	}
	lockedTarget, err := queries.LockGitHubAppServerURL(ctx, repo.LockGitHubAppServerURLParams{McpServerID: serverID, OrganizationID: authCtx.ActiveOrganizationID})
	if err != nil || lockedTarget != target {
		return nil, oops.E(oops.CodeBadRequest, nil, "MCP destination changed during verification; retry configuration")
	}
	var client repo.McpAuthClient
	var before *types.AuthClient
	if payload.ID != nil {
		id, err := uuid.Parse(*payload.ID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, nil, "invalid auth client ID")
		}
		client, err = queries.LockAuthClient(ctx, repo.LockAuthClientParams{ID: id, OrganizationID: authCtx.ActiveOrganizationID})
		if err != nil {
			return nil, oops.E(oops.CodeNotFound, nil, "auth client not found")
		}
		if client.ClientType != "service_account" || client.RemoteSessionClientID.Valid || (client.ProjectID.Valid && client.ProjectID.UUID != server.ProjectID) {
			return nil, oops.E(oops.CodeBadRequest, nil, "auth client is not a GitHub App in this project")
		}
		existing, err := queries.GetGitHubAppConfiguration(ctx, repo.GetGitHubAppConfigurationParams{AuthClientID: id, OrganizationID: authCtx.ActiveOrganizationID})
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, nil, "auth client is not a GitHub App")
		}
		if existing.UpstreamOrigin != origin {
			return nil, oops.E(oops.CodeBadRequest, nil, "create a separate GitHub App service account for a different MCP origin")
		}
		before, err = s.view(ctx, queries, authCtx.ActiveOrganizationID, client, logger)
		if err != nil {
			return nil, err
		}
		client, err = queries.UpdateAuthClient(ctx, repo.UpdateAuthClientParams{ID: id, OrganizationID: authCtx.ActiveOrganizationID, Name: name, Enabled: client.Enabled})
		if err != nil {
			return nil, err
		}
	} else {
		client, err = queries.CreateGitHubAppAuthClient(ctx, repo.CreateGitHubAppAuthClientParams{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: uuid.NullUUID{UUID: server.ProjectID, Valid: true}, Name: name})
		if err != nil {
			return nil, err
		}
	}
	if err := queries.SetGitHubAppCredential(ctx, repo.SetGitHubAppCredentialParams{AuthClientID: client.ID, OrganizationID: authCtx.ActiveOrganizationID, InstanceUrl: instance, AppID: appID, InstallationID: installationID, PrivateKeyEncrypted: encrypted, UpstreamOrigin: origin}); err != nil {
		return nil, err
	}
	if err := queries.AttachGitHubAppAuthClient(ctx, repo.AttachGitHubAppAuthClientParams{AuthClientID: client.ID, OrganizationID: authCtx.ActiveOrganizationID, ProjectID: server.ProjectID, McpServerID: server.ID}); err != nil {
		return nil, err
	}
	if err := queries.ReplaceServerAuthWithGitHubApp(ctx, repo.ReplaceServerAuthWithGitHubAppParams{McpServerID: server.ID, OrganizationID: authCtx.ActiveOrganizationID, AuthClientID: client.ID}); err != nil {
		return nil, err
	}
	after, err := s.view(ctx, queries, authCtx.ActiveOrganizationID, client, logger)
	if err != nil {
		return nil, err
	}
	if s.audit != nil {
		err = s.audit.LogAuthClientUpdate(ctx, tx, audit.LogAuthClientUpdateEvent{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: server.ProjectID, Actor: urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), ActorDisplayName: authCtx.Email, AuthClientURN: urn.NewAuthClient(client.ID), AuthClientName: client.Name, AuthClientSnapshotBefore: before, AuthClientSnapshotAfter: after})
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("save GitHub App: %w", err)
	}
	return after, nil
}
