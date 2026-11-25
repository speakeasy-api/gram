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
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
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
	if err := validateRemoteOfferTarget(ctx, p.db, projectID, mcpServerID, remoteServerID, userSessionIssuerID); err != nil {
		return nil, err
	}
	clients, incomplete, err := p.offeredRegistrations(ctx, organizationID, projectID, mcpServerID, []string{authz.AuthClientTypeRemoteSession})
	if err != nil {
		return nil, err
	}
	if incomplete || len(clients) != 1 || clients[0].CredentialOwner == remotesessions.CredentialOwnerSelf || clients[0].UpstreamRejectedAt != nil {
		return setupConnectionResult("configuration_required", "user_identity_client_required"), nil
	}
	subject := urn.NewUserSubject(userID)
	registration := remotesessions.AuthClientTokenRequest{
		Attribution: remotesessions.RefreshIn(mcpconnectionattempts.ServerScope(organizationID, projectID, mcpServerID, mcpconnectionattempts.AppFirstParty)),
		ProjectID:   projectID, OrganizationID: organizationID, UserSessionIssuerID: userSessionIssuerID,
		Subject: subject, RegistrationID: clients[0].ID, SkipUnusable: false,
	}
	reconnect, err := p.sessions.AuthClientNeedsReconnect(ctx, registration)
	if err != nil {
		return nil, fmt.Errorf("check user authorization: %w", err)
	}
	if reconnect {
		return setupConnectionResult("authorization_required", "upstream_authorization_required"), nil
	}
	tokens, err := p.sessions.ResolveAuthClientAccessTokens(ctx, registration)
	accessToken := tokens[clients[0].RemoteSessionIssuerID].Token
	if err == nil && accessToken == "" {
		err = remotesessions.ErrNoValidToken
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
// reach that one server offers through auth clients of clientTypes, in a stable
// order, so a repeated probe judges the same registration first.
func (p *RemoteMCPReadinessProber) offeredRegistrations(ctx context.Context, organizationID string, projectID uuid.UUID, serverID uuid.UUID, clientTypes []string) (clients []remotesessions.Client, incomplete bool, err error) {
	offered, err := upstreamclientsrepo.New(p.db).ListServerConsentAuthClients(ctx, upstreamclientsrepo.ListServerConsentAuthClientsParams{OrganizationID: organizationID, McpServerIds: []uuid.UUID{serverID}})
	if err != nil {
		return nil, false, fmt.Errorf("list server auth clients: %w", err)
	}
	if len(offered) == 0 {
		return nil, false, ErrReadinessInvalid
	}
	var ids []uuid.UUID
	for _, offer := range offered {
		incomplete = incomplete || offer.UpstreamAuthPending
		if !slices.Contains(clientTypes, offer.ClientType.String) {
			continue
		}
		if !offer.RemoteSessionClientID.Valid {
			incomplete = true
		} else if !slices.Contains(ids, offer.RemoteSessionClientID.UUID) {
			ids = append(ids, offer.RemoteSessionClientID.UUID)
		}
	}
	clients, err = p.sessions.ListAuthClientRegistrations(ctx, projectID, organizationID, ids)
	if err != nil {
		return nil, false, fmt.Errorf("list auth client registrations: %w", err)
	}
	slices.SortFunc(clients, func(a, b remotesessions.Client) int { return bytes.Compare(a.ID[:], b.ID[:]) })
	return clients, incomplete || len(clients) != len(ids), nil
}

// validateRemoteOfferTarget prevents stale registrations from probing a source
// with credentials offered to a different server or sign-in.
func validateRemoteOfferTarget(ctx context.Context, db mcpserversrepo.DBTX, projectID, serverID, sourceID, userIssuerID uuid.UUID) error {
	server, err := mcpserversrepo.New(db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrReadinessInvalid
	}
	if err != nil {
		return fmt.Errorf("load remote offer target: %w", err)
	}
	if server.Visibility == "disabled" || !server.RemoteMcpServerID.Valid || server.RemoteMcpServerID.UUID != sourceID || !server.UserSessionIssuerID.Valid || server.UserSessionIssuerID.UUID != userIssuerID {
		return ErrReadinessInvalid
	}
	return nil
}
