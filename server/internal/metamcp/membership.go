package metamcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// The refusals a gateway write can return. Each caller maps them to its own
// error surface: the management API to HTTP codes, the Platform MCP to tool
// refusals.
var (
	ErrGatewayNotFound               = errors.New("meta mcp server not found")
	ErrMembershipNotFound            = errors.New("meta mcp membership not found")
	ErrIssuerNotInProject            = errors.New("user_session_issuer_id does not reference a live issuer in this project")
	ErrMemberServerNotInProject      = errors.New("mcp_server_id does not reference a live server in this project")
	ErrMemberServerHasNoSlug         = errors.New("mcp server has no slug; update the server to generate one before attaching it")
	ErrMemberServerUnproxied         = errors.New("unproxied mcp servers cannot be meta mcp members")
	ErrMemberServerIsHostedToolset   = errors.New("a hosted toolset's own mcp server cannot be a meta mcp member; add a separate server for the toolset")
	ErrMemberSharesBackend           = errors.New("another member of this meta mcp server already fronts the same backend")
	ErrMemberAlreadyPresent          = errors.New("mcp server is already a member of this meta mcp server")
	ErrMemberNeedsShadowApproval     = errors.New("gateway member requires Shadow MCP approval")
	ErrMemberAdmissionUnverifiable   = errors.New("gateway member distribution approval could not be verified safely")
	ErrMemberServerNotFoundForUpdate = errors.New("member mcp server not found")
)

// Actor is the project and user a gateway write is made in and attributed to.
type Actor struct {
	OrganizationID string
	ProjectID      uuid.UUID
	UserID         string
	Email          *string
}

func (a Actor) principal() urn.Principal {
	return urn.NewPrincipal(urn.PrincipalTypeUser, a.UserID)
}

// GatewayWriter holds the gateway and membership rules every caller shares,
// so the dashboard and the Platform MCP create and change exactly the same
// gateways. Each method runs in the caller's transaction; the caller owns the
// commit and the post-commit follow-up in AfterMembershipCommit.
type GatewayWriter struct {
	Logger    *slog.Logger
	Audit     *audit.Logger
	Authz     *authz.Engine
	Admission *admission.Guard
}

// CreateGatewayInput describes a new gateway.
type CreateGatewayInput struct {
	Name                string
	UserSessionIssuerID uuid.NullUUID
	Visibility          string
	NetworkAccessMode   networkaccess.Mode
}

// CreateGateway creates a gateway with no members. The caller has already
// admitted its network access mode.
func (w GatewayWriter) CreateGateway(ctx context.Context, tx pgx.Tx, actor Actor, input CreateGatewayInput) (repo.MetaMcpServer, error) {
	txRepo := repo.New(tx)
	issuerID := input.UserSessionIssuerID
	if issuerID.Valid {
		// Locked to commit so a concurrent issuer delete cannot race the attach.
		if _, err := txRepo.LockUserSessionIssuerForMetaMCP(ctx, repo.LockUserSessionIssuerForMetaMCPParams{
			UserSessionIssuerID: issuerID.UUID,
			ProjectID:           actor.ProjectID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return repo.MetaMcpServer{}, ErrIssuerNotInProject
			}
			return repo.MetaMcpServer{}, fmt.Errorf("lock user session issuer: %w", err)
		}
	} else {
		// A gateway without sign-in serves everyone anonymously, which hides
		// every private member and can hold no member credentials — a trap,
		// not a use case. Mint a dedicated issuer when the caller supplies none.
		minted, err := mcpservers.MintServerUserSessionIssuer(ctx, tx, actor.OrganizationID, actor.ProjectID, input.Name)
		if err != nil {
			return repo.MetaMcpServer{}, fmt.Errorf("mint meta mcp issuer: %w", err)
		}
		issuerID = minted
	}

	created, err := txRepo.CreateMetaMCPServer(ctx, repo.CreateMetaMCPServerParams{
		OrganizationID:      actor.OrganizationID,
		ProjectID:           actor.ProjectID,
		Name:                input.Name,
		UserSessionIssuerID: issuerID,
		Visibility:          input.Visibility,
		NetworkAccessMode:   networkaccess.Storage(input.NetworkAccessMode),
	})
	if err != nil {
		return repo.MetaMcpServer{}, fmt.Errorf("create meta mcp server: %w", err)
	}

	if err := w.Audit.LogMetaMcpServerCreate(ctx, tx, audit.LogMetaMcpServerCreateEvent{
		OrganizationID:   actor.OrganizationID,
		ProjectID:        actor.ProjectID,
		Actor:            actor.principal(),
		ActorDisplayName: actor.Email,
		ActorSlug:        nil,
		MetaMcpServerURN: urn.NewMetaMcpServer(created.ID),
		Name:             created.Name,
	}); err != nil {
		return repo.MetaMcpServer{}, fmt.Errorf("log meta mcp server creation: %w", err)
	}
	return created, nil
}

// LockGateway takes the locks a membership change holds to commit: the
// project's distribution admission, then the gateway row, which serializes
// concurrent member changes so each sees every committed member.
func (w GatewayWriter) LockGateway(ctx context.Context, tx pgx.Tx, actor Actor, gatewayID uuid.UUID) (repo.MetaMcpServer, error) {
	if err := admission.LockProject(ctx, tx, actor.ProjectID); err != nil {
		return repo.MetaMcpServer{}, fmt.Errorf("lock distribution admission: %w", err)
	}
	meta, err := repo.New(tx).LockMetaMCPServer(ctx, repo.LockMetaMCPServerParams{
		ID:             gatewayID,
		OrganizationID: actor.OrganizationID,
		ProjectID:      actor.ProjectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return repo.MetaMcpServer{}, ErrGatewayNotFound
	}
	if err != nil {
		return repo.MetaMcpServer{}, fmt.Errorf("lock meta mcp server: %w", err)
	}
	return meta, nil
}

// AddedMember is one committed-to-be member and the server it fronts.
type AddedMember struct {
	Member repo.MetaMcpServerMember
	Server mcpserversrepo.McpServer
}

// AddMember adds one server to a gateway the caller locked with LockGateway.
// A refusal is one of the ErrMember* errors and is returned before anything is
// written.
func (w GatewayWriter) AddMember(ctx context.Context, tx pgx.Tx, actor Actor, meta repo.MetaMcpServer, mcpServerID uuid.UUID, sortOrder int32) (AddedMember, error) {
	txRepo := repo.New(tx)
	server, err := mcpserversrepo.New(tx).LockMCPServerByIDAndProjectID(ctx, mcpserversrepo.LockMCPServerByIDAndProjectIDParams{
		ID:        mcpServerID,
		ProjectID: actor.ProjectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return AddedMember{}, ErrMemberServerNotInProject
	}
	if err != nil {
		return AddedMember{}, fmt.Errorf("lock mcp server: %w", err)
	}

	// The gateway addresses members by qualified serverslug--toolname, so a
	// slugless server (legacy pre-2026-05 rows never updated since) can never
	// be reached; updating the server generates a slug. Unproxied backends
	// have no gateway-side dispatch path.
	if !server.Slug.Valid {
		return AddedMember{}, ErrMemberServerHasNoSlug
	}
	if server.UnproxiedMcpServerID.Valid {
		return AddedMember{}, ErrMemberServerUnproxied
	}
	// A toolset's own hosted server keeps its identity; members are separate servers.
	if server.ToolsetID.Valid && server.ID == server.ToolsetID.UUID {
		return AddedMember{}, ErrMemberServerIsHostedToolset
	}

	// The gateway lock serializes concurrent adds, so this sees every
	// committed member, and every member this transaction already added.
	sharing, err := txRepo.CountMetaMCPMembersSharingBackend(ctx, repo.CountMetaMCPMembersSharingBackendParams{
		MetaMcpServerID:      meta.ID,
		ProjectID:            actor.ProjectID,
		McpServerID:          mcpServerID,
		RemoteMcpServerID:    server.RemoteMcpServerID,
		TunneledMcpServerID:  server.TunneledMcpServerID,
		ToolsetID:            server.ToolsetID,
		UnproxiedMcpServerID: server.UnproxiedMcpServerID,
	})
	if err != nil {
		return AddedMember{}, fmt.Errorf("count meta mcp members sharing a backend: %w", err)
	}
	if sharing > 0 {
		return AddedMember{}, ErrMemberSharesBackend
	}

	admissionCtx, err := admission.AdministratorWriteContext(ctx, w.Authz, actor.OrganizationID)
	if err != nil {
		return AddedMember{}, errors.Join(ErrMemberAdmissionUnverifiable, err)
	}
	if err := w.Admission.CheckGatewayMemberAddition(admissionCtx, tx, actor.OrganizationID, actor.ProjectID, meta.ID, mcpServerID); err != nil {
		if errors.Is(err, admission.ErrApprovalRequired) {
			return AddedMember{}, errors.Join(ErrMemberNeedsShadowApproval, err)
		}
		return AddedMember{}, errors.Join(ErrMemberAdmissionUnverifiable, err)
	}

	member, err := txRepo.CreateMetaMCPMember(ctx, repo.CreateMetaMCPMemberParams{
		ProjectID:       actor.ProjectID,
		MetaMcpServerID: meta.ID,
		McpServerID:     mcpServerID,
		SortOrder:       sortOrder,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return AddedMember{}, errors.Join(ErrMemberAlreadyPresent, err)
		}
		return AddedMember{}, fmt.Errorf("add meta mcp member: %w", err)
	}

	// Best-effort consent wiring: when both sides have the identity pieces —
	// the gateway an issuer, the member a stamped upstream AS with a client —
	// bind that client to the gateway's issuer so consent offers the member's
	// provider without a manual attach ceremony.
	if meta.UserSessionIssuerID.Valid && server.RemoteSessionIssuerID.Valid && server.UserSessionIssuerID.Valid {
		// No DB constraint enforces one client per (issuer, upstream); every
		// client-binding writer serializes on this advisory lock instead.
		if err := remotesessionsrepo.New(tx).LockRemoteSessionIssuerForClientBinding(ctx, server.RemoteSessionIssuerID.UUID); err != nil {
			return AddedMember{}, fmt.Errorf("lock remote session issuer for client binding: %w", err)
		}
		attached, err := txRepo.AutoAttachMemberProviderClient(ctx, repo.AutoAttachMemberProviderClientParams{
			GatewayIssuerID: meta.UserSessionIssuerID.UUID,
			ProjectID:       actor.ProjectID,
			MemberIssuerID:  server.UserSessionIssuerID.UUID,
			RemoteIssuerID:  server.RemoteSessionIssuerID.UUID,
		})
		if err != nil {
			return AddedMember{}, fmt.Errorf("attach member provider client: %w", err)
		}
		if attached > 0 {
			w.Logger.InfoContext(ctx, "attached member provider client to meta mcp issuer",
				attr.SlogMetaMcpServerID(meta.ID.String()),
				attr.SlogMcpServerID(server.ID.String()))
		} else {
			// No bindable client, or the upstream is already bound: either
			// way the skip should be visible, matching autoConfigureAuth's
			// every-skip-has-a-reason posture.
			w.Logger.InfoContext(ctx, "no member provider client attached to meta mcp issuer",
				attr.SlogMetaMcpServerID(meta.ID.String()),
				attr.SlogMcpServerID(server.ID.String()))
		}
	}

	if err := w.Audit.LogMetaMcpMemberAdd(ctx, tx, memberAuditEvent(actor, meta, member)); err != nil {
		return AddedMember{}, fmt.Errorf("log meta mcp member addition: %w", err)
	}
	return AddedMember{Member: member, Server: server}, nil
}

// RemoveMember removes one membership of a gateway.
func (w GatewayWriter) RemoveMember(ctx context.Context, tx pgx.Tx, actor Actor, meta repo.MetaMcpServer, memberID uuid.UUID) (repo.MetaMcpServerMember, error) {
	txRepo := repo.New(tx)
	existing, err := txRepo.LockMetaMCPMember(ctx, repo.LockMetaMCPMemberParams{ID: memberID, ProjectID: actor.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) || err == nil && existing.MetaMcpServerID != meta.ID {
		return repo.MetaMcpServerMember{}, ErrMembershipNotFound
	}
	if err != nil {
		return repo.MetaMcpServerMember{}, fmt.Errorf("lock meta mcp membership: %w", err)
	}

	deleted, err := txRepo.DeleteMetaMCPMember(ctx, repo.DeleteMetaMCPMemberParams{ID: memberID, ProjectID: actor.ProjectID})
	if err != nil {
		return repo.MetaMcpServerMember{}, fmt.Errorf("remove meta mcp member: %w", err)
	}

	// Reverse the add-side auto-attach: when no other live member of this
	// gateway still fronts the removed member's upstream, unbind that provider
	// client from the gateway issuer so it stops appearing on the consent
	// screen. The member row is already soft-deleted above, so the detach's
	// "last member" guard sees the post-removal set.
	if meta.UserSessionIssuerID.Valid {
		server, err := mcpserversrepo.New(tx).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
			ID:        deleted.McpServerID,
			ProjectID: actor.ProjectID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// The member's server is itself gone (e.g. removed as part of a
			// server deletion); its binding is cleaned up on that path.
		case err != nil:
			return repo.MetaMcpServerMember{}, fmt.Errorf("load removed member server: %w", err)
		case server.RemoteSessionIssuerID.Valid:
			if err := remotesessionsrepo.New(tx).LockRemoteSessionIssuerForClientBinding(ctx, server.RemoteSessionIssuerID.UUID); err != nil {
				return repo.MetaMcpServerMember{}, fmt.Errorf("lock remote session issuer for client binding: %w", err)
			}
			detached, err := txRepo.AutoDetachMemberProviderClient(ctx, repo.AutoDetachMemberProviderClientParams{
				GatewayIssuerID: meta.UserSessionIssuerID.UUID,
				RemoteIssuerID:  server.RemoteSessionIssuerID.UUID,
				ProjectID:       actor.ProjectID,
			})
			if err != nil {
				return repo.MetaMcpServerMember{}, fmt.Errorf("detach member provider client: %w", err)
			}
			if detached > 0 {
				w.Logger.InfoContext(ctx, "detached member provider client from meta mcp issuer",
					attr.SlogMetaMcpServerID(meta.ID.String()),
					attr.SlogMcpServerID(server.ID.String()))
			}
		}
	}

	if err := w.Audit.LogMetaMcpMemberRemove(ctx, tx, memberAuditEvent(actor, meta, deleted)); err != nil {
		return repo.MetaMcpServerMember{}, fmt.Errorf("log meta mcp member removal: %w", err)
	}
	return deleted, nil
}

// SetMemberSortOrder moves one membership of a gateway to a new position.
func (w GatewayWriter) SetMemberSortOrder(ctx context.Context, tx pgx.Tx, actor Actor, meta repo.MetaMcpServer, memberID uuid.UUID, sortOrder int32) (repo.MetaMcpServerMember, mcpserversrepo.McpServer, error) {
	txRepo := repo.New(tx)
	existing, err := txRepo.LockMetaMCPMember(ctx, repo.LockMetaMCPMemberParams{ID: memberID, ProjectID: actor.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) || err == nil && existing.MetaMcpServerID != meta.ID {
		return repo.MetaMcpServerMember{}, mcpserversrepo.McpServer{}, ErrMembershipNotFound
	}
	if err != nil {
		return repo.MetaMcpServerMember{}, mcpserversrepo.McpServer{}, fmt.Errorf("lock meta mcp membership: %w", err)
	}

	updated, err := txRepo.UpdateMetaMCPMemberSortOrder(ctx, repo.UpdateMetaMCPMemberSortOrderParams{
		SortOrder: sortOrder,
		ID:        memberID,
		ProjectID: actor.ProjectID,
	})
	if err != nil {
		return repo.MetaMcpServerMember{}, mcpserversrepo.McpServer{}, fmt.Errorf("update meta mcp membership: %w", err)
	}

	server, err := mcpserversrepo.New(tx).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        updated.McpServerID,
		ProjectID: actor.ProjectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return repo.MetaMcpServerMember{}, mcpserversrepo.McpServer{}, ErrMemberServerNotFoundForUpdate
	}
	if err != nil {
		return repo.MetaMcpServerMember{}, mcpserversrepo.McpServer{}, fmt.Errorf("get member mcp server: %w", err)
	}

	if err := w.Audit.LogMetaMcpMemberUpdate(ctx, tx, memberAuditEvent(actor, meta, updated)); err != nil {
		return repo.MetaMcpServerMember{}, mcpserversrepo.McpServer{}, fmt.Errorf("log meta mcp member update: %w", err)
	}
	return updated, server, nil
}

func memberAuditEvent(actor Actor, meta repo.MetaMcpServer, member repo.MetaMcpServerMember) audit.LogMetaMcpMemberEvent {
	return audit.LogMetaMcpMemberEvent{
		OrganizationID:   actor.OrganizationID,
		ProjectID:        actor.ProjectID,
		Actor:            actor.principal(),
		ActorDisplayName: actor.Email,
		ActorSlug:        nil,
		MetaMcpServerURN: urn.NewMetaMcpServer(meta.ID),
		Name:             meta.Name,
		MembershipURN:    urn.NewMetaMcpServerMember(member.ID),
		McpServerURN:     urn.NewMcpServer(member.McpServerID),
		SortOrder:        member.SortOrder,
	}
}

// AfterMembershipCommit is the best-effort follow-up every committed
// membership change owes: resync the denormalized issuer column and the
// members' upstream client associations, then ask plugins carrying the
// gateway to republish.
func AfterMembershipCommit(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, publisher plugins.PluginPublishSignaler, actor Actor, meta repo.MetaMcpServer) {
	// Every client-binding writer follows its commit with this resync so the
	// denormalized mcp_servers.remote_session_issuer_id cannot go stale when
	// the gateway issuer is shared with a server row. A member joining or
	// leaving also gains or loses its association with a client the gateway's
	// issuer binds, whether or not a binding changed.
	if meta.UserSessionIssuerID.Valid {
		remotesessions.BestEffortResyncMCPServerRemoteSessionIssuers(ctx, logger, db, actor.OrganizationID, actor.ProjectID, []uuid.UUID{meta.UserSessionIssuerID.UUID})
	}
	SignalGatewayPluginPublish(ctx, logger, db, publisher, actor.ProjectID, actor.UserID, meta.ID, false)
}

// SignalGatewayPluginPublish asks for a plugin republish after a gateway
// change. Unless detached is set, it is skipped when no plugin carries the
// gateway, since no package then changed.
func SignalGatewayPluginPublish(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, publisher plugins.PluginPublishSignaler, projectID uuid.UUID, userID string, gatewayID uuid.UUID, detached bool) {
	if publisher == nil {
		return
	}
	if !detached {
		attached, err := pluginsrepo.New(db).HasPluginMembershipForGateway(ctx, pluginsrepo.HasPluginMembershipForGatewayParams{ProjectID: projectID, GatewayID: gatewayID})
		if err != nil {
			logger.WarnContext(ctx, "check gateway plugin membership", attr.SlogError(err))
			return
		}
		if !attached {
			return
		}
	}
	connected, err := pluginsrepo.New(db).HasPluginGithubConnectionForProject(ctx, projectID)
	if err != nil {
		logger.WarnContext(ctx, "check plugin connection after gateway mutation", attr.SlogError(err))
		return
	}
	if connected {
		if err := publisher.SignalPluginPublish(context.WithoutCancel(ctx), projectID, userID); err != nil {
			logger.WarnContext(ctx, "signal plugin publish after gateway mutation", attr.SlogError(err))
		}
	}
}
