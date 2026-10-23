package metamcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	gen "github.com/speakeasy-api/gram/server/gen/meta_mcp"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/builtinmcp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// SetBuiltinMember changes only membership/configuration, never authorization.
// The gateway row is locked by the caller, serializing concurrent membership edits.
func (w GatewayWriter) SetBuiltinMember(ctx context.Context, tx pgx.Tx, actor Actor, meta repo.MetaMcpServer, key string, environmentID uuid.NullUUID, sortOrder int32) (repo.BuiltinMcpServerMember, error) {
	if err := builtinmcp.RequireAdmin(ctx, tx, actor.OrganizationID); err != nil {
		return repo.BuiltinMcpServerMember{}, fmt.Errorf("authorize built-in membership configuration: %w", err)
	}
	if _, ok := builtinmcp.Lookup(key); !ok {
		return repo.BuiltinMcpServerMember{}, oops.E(oops.CodeInvalid, nil, "unknown built-in MCP server")
	}
	if meta.ProjectID != actor.ProjectID || meta.OrganizationID != actor.OrganizationID {
		return repo.BuiltinMcpServerMember{}, ErrGatewayNotFound
	}
	r := repo.New(tx)
	if environmentID.Valid {
		if _, err := r.LockBuiltinMemberEnvironment(ctx, repo.LockBuiltinMemberEnvironmentParams{ID: environmentID.UUID, ProjectID: actor.ProjectID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return repo.BuiltinMcpServerMember{}, oops.E(oops.CodeInvalid, nil, "environment must belong to this project")
			}
			return repo.BuiltinMcpServerMember{}, fmt.Errorf("lock built-in member environment: %w", err)
		}
	}
	existing, err := r.ListBuiltinMetaMCPMembers(ctx, repo.ListBuiltinMetaMCPMembersParams{ProjectID: uuid.NullUUID{UUID: actor.ProjectID, Valid: true}, MetaMcpServerID: uuid.NullUUID{UUID: meta.ID, Valid: true}})
	if err != nil {
		return repo.BuiltinMcpServerMember{}, fmt.Errorf("read built-in gateway membership: %w", err)
	}
	wasMember := false
	for _, member := range existing {
		if member.BuiltinKey == key {
			wasMember = true
			break
		}
	}
	member, err := r.SetBuiltinMetaMCPMember(ctx, repo.SetBuiltinMetaMCPMemberParams{ProjectID: uuid.NullUUID{UUID: actor.ProjectID, Valid: true}, MetaMcpServerID: uuid.NullUUID{UUID: meta.ID, Valid: true}, BuiltinKey: key, EnvironmentID: environmentID, SortOrder: sortOrder})
	if err != nil {
		return repo.BuiltinMcpServerMember{}, fmt.Errorf("set built-in gateway member: %w", err)
	}
	if wasMember {
		err = w.Audit.LogMetaMcpMemberUpdate(ctx, tx, builtinMemberAuditEvent(actor, meta, member))
	} else {
		err = w.Audit.LogMetaMcpMemberAdd(ctx, tx, builtinMemberAuditEvent(actor, meta, member))
	}
	if err != nil {
		return repo.BuiltinMcpServerMember{}, fmt.Errorf("audit built-in membership: %w", err)
	}
	return member, nil
}

func (w GatewayWriter) RemoveBuiltinMember(ctx context.Context, tx pgx.Tx, actor Actor, meta repo.MetaMcpServer, memberID uuid.UUID) error {
	if err := builtinmcp.RequireAdmin(ctx, tx, actor.OrganizationID); err != nil {
		return fmt.Errorf("authorize built-in membership removal: %w", err)
	}
	member, err := repo.New(tx).DeleteBuiltinMetaMCPMember(ctx, repo.DeleteBuiltinMetaMCPMemberParams{ID: memberID, ProjectID: uuid.NullUUID{UUID: actor.ProjectID, Valid: true}, MetaMcpServerID: uuid.NullUUID{UUID: meta.ID, Valid: true}})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrMembershipNotFound
	}
	if err != nil {
		return fmt.Errorf("remove built-in gateway member: %w", err)
	}
	if err := w.Audit.LogMetaMcpMemberRemove(ctx, tx, builtinMemberAuditEvent(actor, meta, member)); err != nil {
		return fmt.Errorf("audit built-in membership removal: %w", err)
	}
	return nil
}

func builtinMemberAuditEvent(actor Actor, meta repo.MetaMcpServer, member repo.BuiltinMcpServerMember) audit.LogMetaMcpMemberEvent {
	return audit.LogMetaMcpMemberEvent{OrganizationID: actor.OrganizationID, ProjectID: actor.ProjectID, Actor: actor.principal(), ActorDisplayName: actor.Email, ActorSlug: nil, MetaMcpServerURN: urn.NewMetaMcpServer(meta.ID), Name: meta.Name, MembershipURN: urn.NewMetaMcpServerMember(member.ID), McpServerURN: urn.NewMcpServer(builtinmcp.ID(actor.ProjectID, member.BuiltinKey)), SortOrder: member.SortOrder}
}

func builtinMemberView(projectID uuid.UUID, id uuid.UUID, key string, environmentID uuid.NullUUID, sortOrder int32) *types.MetaMcpMember {
	definition, _ := builtinmcp.Lookup(key)
	var environment *string
	if environmentID.Valid {
		environment = conv.PtrEmpty(environmentID.UUID.String())
	}
	return &types.MetaMcpMember{ID: id.String(), McpServerID: builtinmcp.ID(projectID, key).String(), McpServerName: conv.PtrEmpty(definition.Name), McpServerSlug: conv.PtrEmpty("builtin_" + key), BuiltinKey: conv.PtrEmpty(key), EnvironmentID: environment, SortOrder: int(sortOrder)}
}

func (s *Service) SetBuiltinMetaMcpMember(ctx context.Context, payload *gen.SetBuiltinMetaMcpMemberPayload) (*types.MetaMcpMember, error) {
	ac, ok := contextvalues.GetAuthContext(ctx)
	if !ok || ac == nil || ac.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := builtinmcp.RequireAdmin(ctx, s.db, ac.ActiveOrganizationID); err != nil {
		return nil, fmt.Errorf("authorize built-in membership configuration: %w", err)
	}
	metaID, err := uuid.Parse(payload.MetaMcpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid gateway id")
	}
	var environment uuid.NullUUID
	if payload.EnvironmentID != nil {
		id, err := uuid.Parse(*payload.EnvironmentID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid environment id")
		}
		environment = uuid.NullUUID{UUID: id, Valid: true}
	}
	sortOrder, err := sortOrderValue(payload.SortOrder)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid sort order")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin built-in membership transaction")
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	actor := actorFromAuthContext(ac)
	writer := s.writer()
	meta, err := writer.LockGateway(ctx, tx, actor, metaID)
	if err != nil {
		return nil, gatewayWriteError(ctx, s.logger, err, "lock gateway")
	}
	member, err := writer.SetBuiltinMember(ctx, tx, actor, meta, payload.BuiltinKey, environment, sortOrder)
	if err != nil {
		return nil, err
	}
	if err := s.publicationRequests.Project(ctx, tx, actor.OrganizationID, actor.ProjectID, actor.UserID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "enqueue gateway publication")
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit built-in membership transaction")
	}
	s.signalPluginPublish(ctx, actor.ProjectID, actor.UserID, meta.ID, false)
	return builtinMemberView(actor.ProjectID, member.ID, member.BuiltinKey, member.EnvironmentID, member.SortOrder), nil
}

func (s *Service) removeBuiltinMetaMcpMember(ctx context.Context, ac *contextvalues.AuthContext, member repo.BuiltinMcpServerMember) error {
	if err := builtinmcp.RequireAdmin(ctx, s.db, ac.ActiveOrganizationID); err != nil {
		return fmt.Errorf("authorize built-in membership removal: %w", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin built-in membership transaction")
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	actor := actorFromAuthContext(ac)
	writer := s.writer()
	meta, err := writer.LockGateway(ctx, tx, actor, member.MetaMcpServerID.UUID)
	if err != nil {
		return gatewayWriteError(ctx, s.logger, err, "lock gateway")
	}
	if err := writer.RemoveBuiltinMember(ctx, tx, actor, meta, member.ID); err != nil {
		return err
	}
	if err := s.publicationRequests.Project(ctx, tx, actor.OrganizationID, actor.ProjectID, actor.UserID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "enqueue gateway publication")
	}
	if err := tx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit built-in membership transaction")
	}
	AfterMembershipCommit(ctx, s.logger, s.db, s.publisher, actor, meta)
	return nil
}

// Reordering preserves the environment read under the gateway lock, so it cannot
// overwrite a concurrent configuration change with a stale pre-lock value.
func (s *Service) reorderBuiltinMetaMcpMember(ctx context.Context, ac *contextvalues.AuthContext, member repo.BuiltinMcpServerMember, sortOrder int32) (*types.MetaMcpMember, error) {
	if err := builtinmcp.RequireAdmin(ctx, s.db, ac.ActiveOrganizationID); err != nil {
		return nil, fmt.Errorf("authorize built-in membership configuration: %w", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin built-in membership transaction")
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	actor := actorFromAuthContext(ac)
	writer := s.writer()
	meta, err := writer.LockGateway(ctx, tx, actor, member.MetaMcpServerID.UUID)
	if err != nil {
		return nil, gatewayWriteError(ctx, s.logger, err, "lock gateway")
	}
	current, err := repo.New(tx).GetBuiltinMetaMCPMember(ctx, repo.GetBuiltinMetaMCPMemberParams{ID: member.ID, ProjectID: conv.ToNullUUID(actor.ProjectID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load built-in gateway membership")
	}
	updated, err := writer.SetBuiltinMember(ctx, tx, actor, meta, current.BuiltinKey, current.EnvironmentID, sortOrder)
	if err != nil {
		return nil, err
	}
	if err := s.publicationRequests.Project(ctx, tx, actor.OrganizationID, actor.ProjectID, actor.UserID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "enqueue gateway publication")
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit built-in membership transaction")
	}
	s.signalPluginPublish(ctx, actor.ProjectID, actor.UserID, meta.ID, false)
	return builtinMemberView(actor.ProjectID, updated.ID, updated.BuiltinKey, updated.EnvironmentID, updated.SortOrder), nil
}
