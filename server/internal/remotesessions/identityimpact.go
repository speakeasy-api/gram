// identityimpact.go previews which MCP servers and gateways a client binding
// change on a user session issuer would affect, so the dashboard can name them
// before the change is made. A server's upstream is the provider that server
// itself names, so a change reaches it only by removing that provider's
// client: the issuer's other clients belong to the other servers sharing it,
// and no change moves a server from one provider to another behind its back.

package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

const (
	identityChangeReplace = "replace"
	identityChangeAttach  = "attach"
	identityChangeDetach  = "detach"
	identityChangeDisable = "disable"

	identityImpactClear         = "clear"
	identityImpactClientRemoved = "client_removed"

	identityImpactKindServer  = "mcp_server"
	identityImpactKindGateway = "gateway"
)

// impactClient is a client bound to the user session issuer. A nil project is
// an organization-level client, visible to every project.
type impactClient struct {
	id              uuid.UUID
	projectID       uuid.NullUUID
	providerID      uuid.UUID
	providerDeleted bool
}

func (c impactClient) visibleTo(projectID uuid.UUID) bool {
	return !c.projectID.Valid || c.projectID.UUID == projectID
}

// identityChange is the bindings before and after a proposed change.
type identityChange struct {
	bound   []impactClient
	removed map[uuid.UUID]bool
	added   *impactClient
}

// impactOf classifies what the change does to a server in projectID that signs
// in through serverProvider, or "" when nothing. Only that provider's client
// matters: the server's upstream is the one it names, so clients of other
// providers on the same user session issuer leave it alone. A server with no
// provider has no upstream to lose.
//
// Losing the client is the only outcome left. A change that would put another
// client in its place is a replacement of a client this server signs in
// through, which the commit refuses rather than carrying out from a page that
// authorizes only its own server.
func (c identityChange) impactOf(projectID uuid.UUID, serverProvider uuid.NullUUID) string {
	if !serverProvider.Valid {
		return ""
	}
	removed := false
	for _, client := range c.bound {
		if client.providerID != serverProvider.UUID || !client.visibleTo(projectID) || client.providerDeleted {
			continue
		}
		if c.removed[client.id] {
			removed = true
		}
	}
	if !removed {
		return ""
	}
	return identityImpactClear
}

// gatewayImpactOf is what the change does to a gateway in projectID. A gateway
// offers one client per member provider, so it loses a member's sign-in only
// when a client is unbound and nothing takes its place for that provider.
func (c identityChange) gatewayImpactOf(projectID uuid.UUID) string {
	for _, client := range c.bound {
		if !c.removed[client.id] || !client.visibleTo(projectID) || client.providerDeleted {
			continue
		}
		if c.added != nil && c.added.visibleTo(projectID) && c.added.providerID == client.providerID {
			continue
		}
		return identityImpactClientRemoved
	}
	return ""
}

// touchesOrgClient reports whether the change binds or unbinds an
// organization-level client, which every project on the issuer can see.
func (c identityChange) touchesOrgClient() bool {
	if c.added != nil && !c.added.projectID.Valid {
		return true
	}
	for _, client := range c.bound {
		if c.removed[client.id] && !client.projectID.Valid {
			return true
		}
	}
	return false
}

func (s *Service) GetServerIdentityImpact(ctx context.Context, payload *gen.GetServerIdentityImpactPayload) (*gen.ServerIdentityImpactResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID
	logger := s.logger.With(attr.SlogProjectID(projectID.String()))

	userIssuerID, err := uuid.Parse(payload.UserSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer_id").LogError(ctx, logger)
	}
	targetID, err := conv.PtrToNullUUID(payload.McpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp_server_id").LogError(ctx, logger)
	}
	providerID, err := conv.PtrToNullUUID(payload.ProviderID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid provider_id").LogError(ctx, logger)
	}
	clientID, err := conv.PtrToNullUUID(payload.ClientID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid client_id").LogError(ctx, logger)
	}
	if (payload.Change == identityChangeDetach || payload.Change == identityChangeDisable) && (providerID.Valid || clientID.Valid) {
		return nil, oops.E(oops.CodeBadRequest, nil, "detach and disable take no provider_id or client_id").LogError(ctx, logger)
	}
	if payload.Change == identityChangeDisable && !targetID.Valid {
		return nil, oops.E(oops.CodeBadRequest, nil, "disable requires mcp_server_id").LogError(ctx, logger)
	}

	target, err := s.authorizeServerIdentityImpact(ctx, logger, projectID, userIssuerID, targetID)
	if err != nil {
		return nil, err
	}

	q := repo.New(s.db)
	userIssuer, err := q.GetUserSessionIssuerForProject(ctx, repo.GetUserSessionIssuerForProjectParams{
		ID:             userIssuerID,
		ProjectID:      projectID,
		OrganizationID: orgID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get user session issuer").LogError(ctx, logger)
	}

	boundRows, err := q.ListOrganizationClientsForUserSessionIssuer(ctx, repo.ListOrganizationClientsForUserSessionIssuerParams{
		UserSessionIssuerID: userIssuerID,
		OrganizationID:      orgID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list clients bound to user session issuer").LogError(ctx, logger)
	}
	change := identityChange{bound: make([]impactClient, 0, len(boundRows)), removed: map[uuid.UUID]bool{}, added: nil}
	for _, row := range boundRows {
		change.bound = append(change.bound, impactClient{id: row.ID, projectID: row.ProjectID, providerID: row.RemoteSessionIssuerID, providerDeleted: row.ProviderDeleted})
	}

	clientMode, err := impactClientChoice(payload.ClientMode, clientID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid client_mode").LogError(ctx, logger)
	}

	if err := s.planIdentityChange(ctx, q, projectID, orgID, !userIssuer.ProjectID.Valid, payload.Change, providerID, clientID, clientMode, impactTarget{id: targetID, provider: target.RemoteSessionIssuerID}, userIssuerID, &change); err != nil {
		return nil, identityOopsError(err, "preview identity change").LogError(ctx, logger)
	}

	servers, err := q.ListOrganizationMCPServersForUserSessionIssuer(ctx, repo.ListOrganizationMCPServersForUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: userIssuerID, Valid: true},
		OrganizationID:      orgID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list MCP servers sharing the user session issuer").LogError(ctx, logger)
	}
	gateways, err := q.ListOrganizationGatewaysForUserSessionIssuer(ctx, repo.ListOrganizationGatewaysForUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: userIssuerID, Valid: true},
		OrganizationID:      orgID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list gateways sharing the user session issuer").LogError(ctx, logger)
	}

	// Touching an organization-level client counts every unreadable server on
	// the issuer, so the count does not reveal which hidden projects it affects.
	countAllHidden := change.touchesOrgClient()
	var candidates []*gen.ServerIdentityImpactServer
	checks := []authz.Check{}
	for _, row := range servers {
		if targetID.Valid && row.ID == targetID.UUID {
			continue
		}
		impact := change.impactOf(row.ProjectID, row.RemoteSessionIssuerID)
		if impact == "" && !countAllHidden {
			continue
		}
		candidates = append(candidates, &gen.ServerIdentityImpactServer{
			ID:          row.ID.String(),
			Kind:        identityImpactKindServer,
			Name:        conv.FromPGText[string](row.Name),
			Slug:        conv.FromPGText[string](row.Slug),
			ProjectID:   row.ProjectID.String(),
			ProjectName: row.ProjectName,
			Impact:      impact,
		})
		checks = append(checks, authz.MCPCheck(authz.ScopeMCPRead, mcpGrantResourceID(row.ID, row.ToolsetID), row.ProjectID.String()))
	}
	for _, row := range gateways {
		impact := change.gatewayImpactOf(row.ProjectID)
		if impact == "" && !countAllHidden {
			continue
		}
		candidates = append(candidates, &gen.ServerIdentityImpactServer{
			ID:          row.ID.String(),
			Kind:        identityImpactKindGateway,
			Name:        conv.PtrEmpty(row.Name),
			Slug:        nil,
			ProjectID:   row.ProjectID.String(),
			ProjectName: row.ProjectName,
			Impact:      impact,
		})
		checks = append(checks, authz.Check{Scope: authz.ScopeMCPRead, ResourceKind: "", ResourceID: row.ProjectID.String(), Dimensions: nil})
	}

	readable, err := s.authz.FindMatched(ctx, checks)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "check MCP server read access").LogError(ctx, logger)
	}
	result := &gen.ServerIdentityImpactResult{Servers: []*gen.ServerIdentityImpactServer{}, HiddenServerCount: 0}
	for i, candidate := range candidates {
		switch {
		case !readable[i]:
			result.HiddenServerCount++
		case candidate.Impact != "":
			result.Servers = append(result.Servers, candidate)
		}
	}
	return result, nil
}

// authorizeServerIdentityImpact requires what making the change requires: write
// access to the server it is made from, or to the project for a change made
// without one. It returns the loaded server, whose own provider is what the
// change is previewed against.
func (s *Service) authorizeServerIdentityImpact(ctx context.Context, logger *slog.Logger, projectID, userIssuerID uuid.UUID, targetID uuid.NullUUID) (mcpserversrepo.McpServer, error) {
	var none mcpserversrepo.McpServer
	if !targetID.Valid {
		if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: projectID.String(), Dimensions: nil}); err != nil {
			return none, err
		}
		return none, nil
	}
	target, err := mcpserversrepo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        targetID.UUID,
		ProjectID: projectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return none, oops.E(oops.CodeNotFound, err, "MCP server not found").LogError(ctx, logger)
	}
	if err != nil {
		return none, oops.E(oops.CodeUnexpected, err, "get MCP server").LogError(ctx, logger)
	}
	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, mcpGrantResourceID(target.ID, target.ToolsetID), projectID.String())); err != nil {
		return none, err
	}
	if !target.UserSessionIssuerID.Valid || target.UserSessionIssuerID.UUID != userIssuerID {
		return none, oops.E(oops.CodeBadRequest, nil, "MCP server does not use this user session issuer").LogError(ctx, logger)
	}
	return target, nil
}

// impactTarget is the server a change is made from: the one the preview
// excludes, and whose own provider decides what the change replaces.
type impactTarget struct {
	id       uuid.NullUUID
	provider uuid.NullUUID
}

// planIdentityChange fills change.removed and change.added the way the
// matching write would: the commit's ReplaceServerBound, a plain attach, the
// per-server disable, or detaching every client the caller's project sees. It
// applies the writes' org-wide binding refusal.
func (s *Service) planIdentityChange(ctx context.Context, q *repo.Queries, projectID uuid.UUID, orgID string, userIssuerOrgLevel bool, kind string, providerID, clientID uuid.NullUUID, clientMode clientChoiceKind, target impactTarget, userIssuerID uuid.UUID, change *identityChange) error {
	var visible []repo.RemoteSessionClient
	for _, client := range change.bound {
		if client.visibleTo(projectID) {
			visible = append(visible, repo.RemoteSessionClient{ID: client.id, ProjectID: client.projectID, RemoteSessionIssuerID: client.providerID}) //nolint:exhaustruct // Only the fields decideServerBound reads.
		}
	}
	if kind == identityChangeDisable {
		// What the per-server disable unbinds: the clients of the provider the
		// target signs in to, and only when no other server in the
		// organization still signs in through it. Nothing is bound in its
		// place, so the preview names only servers losing their sign-in — and
		// an unrelated client of another provider cannot refuse it.
		if !target.provider.Valid {
			return nil
		}
		shared, err := s.providerServedByAnotherServer(ctx, q, orgID, userIssuerID, target, target.provider.UUID)
		if err != nil {
			return err
		}
		if shared {
			return nil
		}
		for _, client := range visible {
			if client.RemoteSessionIssuerID != target.provider.UUID {
				continue
			}
			if err := refuseOrgWideBinding(userIssuerOrgLevel, client.ProjectID); err != nil {
				return err
			}
			change.removed[client.ID] = true
		}
		return nil
	}
	if kind == identityChangeDetach {
		for _, client := range visible {
			if err := refuseOrgWideBinding(userIssuerOrgLevel, client.ProjectID); err != nil {
				return err
			}
			change.removed[client.ID] = true
		}
		return nil
	}
	if kind != identityChangeReplace && kind != identityChangeAttach {
		return identityRefusal(ErrIdentityInvalid, nil, fmt.Sprintf("unsupported change %q", kind))
	}

	// A new client is the caller project's; a new provider matches no stored one.
	added := impactClient{id: uuid.New(), projectID: uuid.NullUUID{UUID: projectID, Valid: true}, providerID: uuid.New(), providerDeleted: false}
	if providerID.Valid {
		added.providerID = providerID.UUID
	}
	if clientID.Valid {
		existing, err := q.GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
			ID:             clientID.UUID,
			ProjectID:      projectID,
			OrganizationID: orgID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return identityRefusal(ErrIdentityNotFound, err, "remote session client not found")
		}
		if err != nil {
			return fmt.Errorf("get remote session client: %w", err)
		}
		if providerID.Valid && existing.RemoteSessionClient.RemoteSessionIssuerID != providerID.UUID {
			return identityRefusal(ErrIdentityInvalid, nil, "existing client does not belong to the selected provider")
		}
		added = impactClient{id: existing.RemoteSessionClient.ID, projectID: existing.RemoteSessionClient.ProjectID, providerID: existing.RemoteSessionClient.RemoteSessionIssuerID, providerDeleted: false}
	}
	alreadyBound := false
	for _, client := range change.bound {
		if client.id == added.id {
			alreadyBound = true
		}
	}
	if !alreadyBound {
		if err := refuseOrgWideBinding(userIssuerOrgLevel, added.projectID); err != nil {
			return err
		}
		change.added = &added
	}

	if kind == identityChangeReplace {
		replaced, err := s.replacedServerClients(ctx, q, orgID, userIssuerID, target, visible, clientMode, conv.Ternary(providerID.Valid || clientID.Valid, added.providerID, uuid.Nil), conv.Ternary(clientID.Valid, added.id, uuid.Nil))
		if err != nil {
			return err
		}
		for _, client := range replaced {
			if err := refuseOrgWideBinding(userIssuerOrgLevel, client.ProjectID); err != nil {
				return err
			}
			change.removed[client.ID] = true
		}
	}
	return nil
}

// replacedServerClients is what the matching per-server commit would unbind,
// decided by the same rules: the clients of the provider the target server is
// moving off, and only when no other server in the organization still signs in
// through them. Without a target the change belongs to the Remote Identity
// Provider settings, which replace nothing.
//
// clientMode is the mode the matching commit would run in, which is what
// decides whether a client already bound for the chosen provider is reused or
// replaced. Inferring it from the absence of a client id instead would read
// hand-entered credentials as an automatic registration and report no impact
// for a replacement that signs out every server using that provider.
func (s *Service) replacedServerClients(ctx context.Context, q *repo.Queries, orgID string, userIssuerID uuid.UUID, target impactTarget, visible []repo.RemoteSessionClient, clientMode clientChoiceKind, providerID, keep uuid.UUID) ([]repo.RemoteSessionClient, error) {
	if !target.id.Valid {
		return nil, nil
	}
	decision, err := decideServerBound(visible, clientMode, providerID, keep, target.provider)
	if err != nil {
		return nil, err
	}
	// The superseded client always goes, and only from a commit the provider's
	// other servers do not block — the same refusal unboundClients applies.
	unbound := decision.superseded
	if len(unbound) > 0 {
		shared, err := s.providerServedByAnotherServer(ctx, q, orgID, userIssuerID, target, providerID)
		if err != nil {
			return nil, err
		}
		if shared {
			return nil, identityRefusal(ErrIdentityConflict, nil, sharedProviderClientMessage)
		}
	}
	if len(decision.release) == 0 {
		return unbound, nil
	}
	shared, err := s.providerServedByAnotherServer(ctx, q, orgID, userIssuerID, target, target.provider.UUID)
	if err != nil {
		return nil, err
	}
	if shared {
		return unbound, nil
	}
	return append(unbound, decision.release...), nil
}

// providerServedByAnotherServer reports whether a live MCP server other than
// the target signs in to providerID through this user session issuer, the
// condition the matching commit reads under its locks.
func (s *Service) providerServedByAnotherServer(ctx context.Context, q *repo.Queries, orgID string, userIssuerID uuid.UUID, target impactTarget, providerID uuid.UUID) (bool, error) {
	if providerID == uuid.Nil {
		return false, nil
	}
	others, err := q.CountOrganizationMCPServersWithRemoteSessionIssuer(ctx, repo.CountOrganizationMCPServersWithRemoteSessionIssuerParams{
		UserSessionIssuerID:   uuid.NullUUID{UUID: userIssuerID, Valid: true},
		RemoteSessionIssuerID: uuid.NullUUID{UUID: providerID, Valid: true},
		ExcludedMcpServerID:   target.id.UUID,
		OrganizationID:        orgID,
	})
	if err != nil {
		return false, fmt.Errorf("count MCP servers sharing the provider: %w", err)
	}
	return others > 0, nil
}

// impactClientChoice maps the previewed client mode onto the commit's own
// client choice. An absent mode falls back to what naming a client implies,
// since commitServerIdentityConfiguration accepts existing_client_id in
// existing mode alone: a caller that sent one before this field existed is
// previewing that mode, not the automatic registration that would reuse the
// bound client instead of replacing it.
func impactClientChoice(mode *string, clientID uuid.NullUUID) (clientChoiceKind, error) {
	fallback := serverIdentityClientModeAuto
	if clientID.Valid {
		fallback = serverIdentityClientModeExisting
	}
	switch conv.PtrValOr(mode, fallback) {
	case serverIdentityClientModeAuto:
		return clientRegister, nil
	case serverIdentityClientModeExisting:
		return clientLink, nil
	case serverIdentityClientModeManual:
		return clientManual, nil
	default:
		return 0, fmt.Errorf("unsupported client_mode")
	}
}

func mcpGrantResourceID(id uuid.UUID, toolsetID uuid.NullUUID) string {
	if toolsetID.Valid {
		return toolsetID.UUID.String()
	}
	return id.String()
}
