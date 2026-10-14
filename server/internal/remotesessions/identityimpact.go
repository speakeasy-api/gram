// identityimpact.go previews which MCP servers and gateways a client binding
// change on a user session issuer would affect, so the dashboard can name them
// before the change is made. It mirrors ResyncMCPServerRemoteSessionIssuers: a
// server's upstream is the single live provider among the clients its project
// can see.

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

	identityImpactRepoint       = "repoint"
	identityImpactClear         = "clear"
	identityImpactResignin      = "resignin"
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

// impactOf classifies what the change does to a server in projectID, or ""
// when nothing.
func (c identityChange) impactOf(projectID uuid.UUID) string {
	var current, next []uuid.UUID
	clientRemoved := false
	for _, client := range c.bound {
		if !client.visibleTo(projectID) || client.providerDeleted {
			continue
		}
		current = append(current, client.providerID)
		if c.removed[client.id] {
			clientRemoved = true
			continue
		}
		next = append(next, client.providerID)
	}
	if c.added != nil && c.added.visibleTo(projectID) {
		next = append(next, c.added.providerID)
	}
	before, after := derivedProvider(current), derivedProvider(next)
	switch {
	case before != after && after == uuid.Nil:
		return identityImpactClear
	case before != after:
		return identityImpactRepoint
	case clientRemoved:
		return identityImpactResignin
	default:
		return ""
	}
}

// gatewayImpactOf is what the change does to a gateway in projectID. A gateway
// binds one client per member provider, so only an unbound client affects it.
func (c identityChange) gatewayImpactOf(projectID uuid.UUID) string {
	for _, client := range c.bound {
		if c.removed[client.id] && client.visibleTo(projectID) && !client.providerDeleted {
			return identityImpactClientRemoved
		}
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

// derivedProvider is the single distinct provider, else uuid.Nil.
func derivedProvider(providers []uuid.UUID) uuid.UUID {
	derived := uuid.Nil
	for _, id := range providers {
		if derived != uuid.Nil && id != derived {
			return uuid.Nil
		}
		derived = id
	}
	return derived
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
	if payload.Change == identityChangeDetach && (providerID.Valid || clientID.Valid) {
		return nil, oops.E(oops.CodeBadRequest, nil, "detach takes no provider_id or client_id").LogError(ctx, logger)
	}

	if err := s.authorizeServerIdentityImpact(ctx, logger, projectID, userIssuerID, targetID); err != nil {
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

	if err := s.planIdentityChange(ctx, q, projectID, orgID, !userIssuer.ProjectID.Valid, payload.Change, providerID, clientID, &change); err != nil {
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
		impact := change.impactOf(row.ProjectID)
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
// without one.
func (s *Service) authorizeServerIdentityImpact(ctx context.Context, logger *slog.Logger, projectID, userIssuerID uuid.UUID, targetID uuid.NullUUID) error {
	if !targetID.Valid {
		return s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: projectID.String(), Dimensions: nil})
	}
	target, err := mcpserversrepo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        targetID.UUID,
		ProjectID: projectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeNotFound, err, "MCP server not found").LogError(ctx, logger)
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get MCP server").LogError(ctx, logger)
	}
	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, mcpGrantResourceID(target.ID, target.ToolsetID), projectID.String())); err != nil {
		return err
	}
	if !target.UserSessionIssuerID.Valid || target.UserSessionIssuerID.UUID != userIssuerID {
		return oops.E(oops.CodeBadRequest, nil, "MCP server does not use this user session issuer").LogError(ctx, logger)
	}
	return nil
}

// planIdentityChange fills change.removed and change.added the way the
// matching write would: the commit's ReplaceBound, a plain attach, or detaching
// every client the caller's project sees. It applies the writes' org-wide
// binding refusal.
func (s *Service) planIdentityChange(ctx context.Context, q *repo.Queries, projectID uuid.UUID, orgID string, userIssuerOrgLevel bool, kind string, providerID, clientID uuid.NullUUID, change *identityChange) error {
	var visible []repo.RemoteSessionClient
	for _, client := range change.bound {
		if client.visibleTo(projectID) {
			visible = append(visible, repo.RemoteSessionClient{ID: client.id, ProjectID: client.projectID, RemoteSessionIssuerID: client.providerID}) //nolint:exhaustruct // Only the fields replacedClients reads.
		}
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
		replaced, err := replacedClients(visible, conv.Ternary(providerID.Valid || clientID.Valid, added.providerID, uuid.Nil), conv.Ternary(clientID.Valid, added.id, uuid.Nil))
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

func mcpGrantResourceID(id uuid.UUID, toolsetID uuid.NullUUID) string {
	if toolsetID.Valid {
		return toolsetID.UUID.String()
	}
	return id.String()
}
