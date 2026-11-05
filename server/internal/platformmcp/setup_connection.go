package platformmcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urls"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var _ remotesessions.ServerConnectionTester = (*RemoteMCPReadinessProber)(nil)

// TestUserConnection reuses the catalog's bounded initialization and tool-list
// probe. Setup requires a direct human grant, so anonymous, service-account and
// identity-chaining readiness cannot stand in for a completed user sign-in.
func (p *RemoteMCPReadinessProber) TestUserConnection(ctx context.Context, organizationID, userID string, projectID, mcpServerID, remoteServerID, userSessionIssuerID uuid.UUID) (*gen.ServerConnectionTestResult, error) {
	if _, err := projectsrepo.New(p.db).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{ID: projectID, OrganizationID: organizationID}); err != nil {
		return nil, fmt.Errorf("validate connection project: %w", err)
	}
	remote, err := remotemcprepo.New(p.db).GetServerByID(ctx, remotemcprepo.GetServerByIDParams{ID: remoteServerID, ProjectID: projectID})
	if err != nil {
		return nil, fmt.Errorf("load connection source: %w", err)
	}
	if remote.TransportType != "streamable-http" || !urls.IsAbsoluteHTTPSOrLoopback(remote.Url) {
		return setupConnectionResult("configuration_required", "unsupported_transport"), nil
	}
	enforced, err := p.authEnforcement.Enforced(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("read upstream auth enforcement: %w", err)
	}
	var clients []remotesessions.Client
	if enforced {
		// Typed auth clients choose the server's upstream authentication, so
		// the user's client is the registration its personal auth client
		// offers, whatever the sign-in binds or the server is associated with.
		clients, err = p.offeredRegistrations(ctx, organizationID, projectID, []uuid.UUID{mcpServerID}, []string{authz.AuthClientTypeRemoteSession})
		if err != nil {
			return nil, err
		}
	} else {
		clients, err = p.sessions.ListClients(ctx, projectID, organizationID, userSessionIssuerID)
		if err != nil {
			return nil, fmt.Errorf("list connection clients: %w", err)
		}
		associated, err := upstreamclientsrepo.New(p.db).ListMCPServerRemoteSessionClients(ctx, upstreamclientsrepo.ListMCPServerRemoteSessionClientsParams{McpServerID: mcpServerID, ProjectID: projectID})
		if err != nil {
			return nil, fmt.Errorf("list connection server clients: %w", err)
		}
		// A shared sign-in issuer can bind clients for several upstreams. Only
		// the target server's explicit associations authorize this diagnostic.
		clients = slices.DeleteFunc(clients, func(client remotesessions.Client) bool {
			return !slices.ContainsFunc(associated, func(row upstreamclientsrepo.ListMCPServerRemoteSessionClientsRow) bool {
				return row.RemoteSessionClientID == client.ID && row.RemoteSessionIssuerID == client.RemoteSessionIssuerID
			})
		})
	}
	if len(clients) != 1 || clients[0].CredentialOwner == remotesessions.CredentialOwnerSelf {
		return setupConnectionResult("configuration_required", "user_identity_client_required"), nil
	}
	subject := urn.NewUserSubject(userID)
	registration := remotesessions.AuthClientTokenRequest{
		ProjectID: projectID, OrganizationID: organizationID, UserSessionIssuerID: userSessionIssuerID,
		Subject: subject, RegistrationID: clients[0].ID, SkipUnusable: false,
	}
	var reconnect bool
	if enforced {
		reconnect, err = p.sessions.AuthClientNeedsReconnect(ctx, registration)
	} else {
		reconnect, err = p.sessions.ClientNeedsReconnect(ctx, projectID, organizationID, userSessionIssuerID, subject, clients[0].ID)
	}
	if err != nil {
		return nil, fmt.Errorf("check user authorization: %w", err)
	}
	if reconnect {
		return setupConnectionResult("authorization_required", "upstream_authorization_required"), nil
	}
	var accessToken string
	if enforced {
		var tokens map[uuid.UUID]remotesessions.UpstreamToken
		tokens, err = p.sessions.ResolveAuthClientAccessTokens(ctx, registration)
		accessToken = tokens[clients[0].RemoteSessionIssuerID].Token
		if err == nil && accessToken == "" {
			err = remotesessions.ErrNoValidToken
		}
	} else {
		var authorization remotesessions.ResolvedAuthorization
		authorization, err = p.sessions.ResolveAuthorization(ctx, remotesessions.RefreshIn(mcpconnectionattempts.ProviderSessionScope(organizationID, projectID, "dashboard")), projectID, organizationID, userSessionIssuerID, clients[0].RemoteSessionIssuerID, subject, remote.Url)
		accessToken = authorization.AccessToken
	}
	switch {
	case errors.Is(err, remotesessions.ErrRemoteSessionMisconfigured), errors.Is(err, remotesessions.ErrNoRemoteSessionClientBinding), errors.Is(err, remotesessions.ErrChosenClientUnbound):
		return setupConnectionResult("configuration_required", "upstream_identity_provider_not_configured"), nil
	case errors.Is(err, remotesessions.ErrRemoteSessionUnavailable):
		return setupConnectionResult("failed", "probe_temporarily_unavailable"), nil
	case errors.Is(err, remotesessions.ErrNoValidToken), errors.Is(err, pgx.ErrNoRows):
		return setupConnectionResult("authorization_required", "upstream_authorization_required"), nil
	case err != nil:
		return nil, fmt.Errorf("resolve user authorization: %w", err)
	}
	headers, err := p.loadHeaders(ctx, remote.ID)
	if err != nil {
		return nil, err
	}
	for _, header := range headers {
		// Stored server credentials must not substitute for the user whose
		// authorization this setup test claims to verify.
		if strings.EqualFold(header.Name, "Authorization") && header.Value.Valid && header.Value.String != "" {
			return setupConnectionResult("configuration_required", "authorization_header_conflict"), nil
		}
		if header.IsRequired && (!header.Value.Valid || header.Value.String == "") {
			return setupConnectionResult("configuration_required", "required_header_missing"), nil
		}
		if header.ValueFromRequestHeader.Valid && header.ValueFromRequestHeader.String != "" {
			return setupConnectionResult("configuration_required", "request_header_not_supported"), nil
		}
	}
	state, evidence, count := p.probeTools(ctx, remote.Url, headers, accessToken)
	status := "failed"
	if state == ReadinessReady {
		status = "ready"
	}
	if state == ReadinessUnauthorized {
		status = "authorization_required"
	}
	result := setupConnectionResult(status, evidence)
	if status == "ready" {
		result.ToolCount = new(count)
	}
	return result, nil
}

func setupConnectionResult(status, code string) *gen.ServerConnectionTestResult {
	message := "Could not initialize the connection and list its tools. Check the provider permissions and try again."
	switch status {
	case "ready":
		message = "Connection initialized and tools discovered."
	case "authorization_required":
		message = "Sign in to the provider with this account, then test the connection again."
	case "configuration_required":
		message = "Check this server's User Identity client and connection settings, then try again."
	}
	if code == "authorization_header_conflict" {
		message = "Remove the stored Authorization header from this server before testing User Identity."
	}
	return &gen.ServerConnectionTestResult{Status: status, Code: code, Message: message, ToolCount: nil}
}

// offeredRegistrations returns the OAuth registrations within the project's
// reach that serverIDs offer through auth clients of clientTypes, in a stable
// order, so a repeated probe judges the same registration first.
func (p *RemoteMCPReadinessProber) offeredRegistrations(ctx context.Context, organizationID string, projectID uuid.UUID, serverIDs []uuid.UUID, clientTypes []string) ([]remotesessions.Client, error) {
	offered, err := upstreamclientsrepo.New(p.db).ListServerConsentAuthClients(ctx, upstreamclientsrepo.ListServerConsentAuthClientsParams{OrganizationID: organizationID, McpServerIds: serverIDs})
	if err != nil {
		return nil, fmt.Errorf("list server auth clients: %w", err)
	}
	var ids []uuid.UUID
	for _, client := range offered {
		if slices.Contains(clientTypes, client.ClientType.String) && client.RemoteSessionClientID.Valid && !slices.Contains(ids, client.RemoteSessionClientID.UUID) {
			ids = append(ids, client.RemoteSessionClientID.UUID)
		}
	}
	clients, err := p.sessions.ListAuthClientRegistrations(ctx, projectID, organizationID, ids)
	if err != nil {
		return nil, fmt.Errorf("list auth client registrations: %w", err)
	}
	slices.SortFunc(clients, func(a, b remotesessions.Client) int { return bytes.Compare(a.ID[:], b.ID[:]) })
	return clients, nil
}
