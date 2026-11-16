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
	"slices"

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
	credentialOwner string
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

	// scopedToServer reports a change decided for one server, which leaves every
	// client another server uses bound.
	scopedToServer bool

	// moved lists the servers an explicit client replacement moves to the new
	// client, whose people must sign in again.
	moved []uuid.UUID

	// detachClients lists the clients a server giving up User Identity unbinds.
	detachClients []uuid.UUID

	// releaseClients lists the clients that server stops using, which stay bound
	// for others.
	releaseClients []uuid.UUID
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
	change := identityChange{bound: make([]impactClient, 0, len(boundRows)), removed: map[uuid.UUID]bool{}, added: nil, scopedToServer: false, moved: nil, detachClients: nil, releaseClients: nil}
	for _, row := range boundRows {
		change.bound = append(change.bound, impactClient{id: row.ID, projectID: row.ProjectID, providerID: row.RemoteSessionIssuerID, credentialOwner: row.CredentialOwner, providerDeleted: row.ProviderDeleted})
	}

	orgAdmin, err := s.actorIsOrganizationAdmin(ctx, orgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "check organization admin access").LogError(ctx, logger)
	}
	replaceClient := conv.PtrValOr(payload.ReplaceClient, false)
	if replaceClient && (!targetID.Valid || payload.Change != identityChangeReplace) {
		return nil, oops.E(oops.CodeBadRequest, nil, "replace_client needs mcp_server_id and change replace").LogError(ctx, logger)
	}
	if err := s.planIdentityChange(ctx, q, projectID, orgID, userIssuerID, targetID, !userIssuer.ProjectID.Valid, orgAdmin, payload.Change, providerID, clientID, replaceClient, &change); err != nil {
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
	listed := make(map[uuid.UUID]bool, len(change.moved))
	var candidates []*gen.ServerIdentityImpactServer
	checks := []authz.Check{}
	for _, row := range servers {
		if targetID.Valid && row.ID == targetID.UUID {
			continue
		}
		impact := change.impactOf(row.ProjectID)
		// A change made from one server never takes a client from a directly
		// Remote MCP-backed sibling, which signs in through its own association
		// whatever the issuer's bindings derive.
		if change.scopedToServer && row.RemoteMcpServerID.Valid {
			impact = ""
		}
		if slices.Contains(change.moved, row.ID) {
			impact = identityImpactResignin
			listed[row.ID] = true
		}
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
		checks = append(checks, authz.MCPCheck(authz.ScopeMCPRead, row.ProjectID.String(), row.ProjectID.String()))
	}

	readable, err := s.authz.FindMatched(ctx, checks)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "check MCP server read access").LogError(ctx, logger)
	}
	result := &gen.ServerIdentityImpactResult{Servers: []*gen.ServerIdentityImpactServer{}, HiddenServerCount: 0, DetachClientIds: uuidStrings(change.detachClients), ReleaseClientIds: uuidStrings(change.releaseClients)}
	for i, candidate := range candidates {
		switch {
		case !readable[i]:
			result.HiddenServerCount++
		case candidate.Impact != "":
			result.Servers = append(result.Servers, candidate)
		}
	}
	// A server using the client through a gateway or another issuer is not on
	// this issuer's list, so it cannot be named. Counting it stops the change
	// being confirmed against a list the commit would refuse.
	for _, serverID := range change.moved {
		if !listed[serverID] {
			result.HiddenServerCount++
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
// clients. Made from a server, detaching unbinds only the clients that server
// alone uses; made without one, every client the caller's project sees. It
// applies the writes' org-wide binding refusal.
func (s *Service) planIdentityChange(ctx context.Context, q *repo.Queries, projectID uuid.UUID, orgID string, userIssuerID uuid.UUID, targetID uuid.NullUUID, userIssuerOrgLevel, orgAdmin bool, kind string, providerID, clientID uuid.NullUUID, replaceClient bool, change *identityChange) error {
	var visible []repo.RemoteSessionClient
	for _, client := range change.bound {
		if client.visibleTo(projectID) {
			visible = append(visible, repo.RemoteSessionClient{ID: client.id, ProjectID: client.projectID, RemoteSessionIssuerID: client.providerID, CredentialOwner: client.credentialOwner}) //nolint:exhaustruct // Only the fields decideBoundClients reads.
		}
	}
	change.scopedToServer = targetID.Valid && kind != identityChangeAttach
	if kind == identityChangeDetach {
		detached := visible
		if targetID.Valid {
			// Leaving User Identity is a move to no provider: nothing is asked for
			// and no provider's client is kept.
			decision, err := decideBoundClients(ctx, q, boundClientsRequest{
				OrganizationID:      orgID,
				UserSessionIssuerID: userIssuerID,
				MCPServerID:         targetID.UUID,
				Bound:               visible,
				ProviderID:          uuid.New(),
				Keep:                uuid.Nil,
				RegistersClient:     false,
				ReplaceClient:       false,
				Leaving:             true,
			})
			if err != nil {
				return err
			}
			detached = decision.detach
			for _, client := range decision.detach {
				change.detachClients = append(change.detachClients, client.ID)
			}
			for _, client := range decision.release {
				change.releaseClients = append(change.releaseClients, client.ID)
			}
		}
		for _, client := range detached {
			if err := refuseOrgWideBinding(userIssuerOrgLevel, orgAdmin, client.ProjectID); err != nil {
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
	added := impactClient{id: uuid.New(), projectID: uuid.NullUUID{UUID: projectID, Valid: true}, providerID: uuid.New(), credentialOwner: "", providerDeleted: false}
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
		added = impactClient{id: existing.RemoteSessionClient.ID, projectID: existing.RemoteSessionClient.ProjectID, providerID: existing.RemoteSessionClient.RemoteSessionIssuerID, credentialOwner: existing.RemoteSessionClient.CredentialOwner, providerDeleted: false}
	}
	alreadyBound := false
	for _, client := range change.bound {
		if client.id == added.id {
			alreadyBound = true
		}
	}
	if !alreadyBound {
		if err := refuseOrgWideBinding(userIssuerOrgLevel, orgAdmin, added.projectID); err != nil {
			return err
		}
		change.added = &added
	}

	if kind == identityChangeReplace {
		// The preview cannot tell automatic registration from typed credentials,
		// so naming no client reads as the automatic form the commit reuses for.
		decision, err := decideBoundClients(ctx, q, boundClientsRequest{
			OrganizationID:      orgID,
			UserSessionIssuerID: userIssuerID,
			MCPServerID:         targetID.UUID,
			Bound:               visible,
			ProviderID:          conv.Ternary(providerID.Valid || clientID.Valid, added.providerID, uuid.Nil),
			Keep:                conv.Ternary(clientID.Valid, added.id, uuid.Nil),
			RegistersClient:     !clientID.Valid,
			ReplaceClient:       replaceClient,
			Leaving:             false,
		})
		if err != nil {
			return err
		}
		if decision.reuse {
			change.added = nil
		}
		if err := refuseMovedServersOutsideProject(ctx, mcpserversrepo.New(s.db), projectID, decision.moved); err != nil {
			return err
		}
		change.moved = decision.moved
		for _, client := range decision.detach {
			if err := refuseOrgWideBinding(userIssuerOrgLevel, orgAdmin, client.ProjectID); err != nil {
				return err
			}
			change.removed[client.ID] = true
		}
	}
	return nil
}

func uuidStrings(ids []uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

func mcpGrantResourceID(id uuid.UUID, toolsetID uuid.NullUUID) string {
	if toolsetID.Valid {
		return toolsetID.UUID.String()
	}
	return id.String()
}
