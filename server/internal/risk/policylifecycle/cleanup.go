package policylifecycle

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	shadowadmission "github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// OrphanRepairSeedName identifies the one-time startup repair.
	OrphanRepairSeedName = "orphaned-mcp-risk-policies"

	// OrphanRepairSeedVersion changes only when the repair semantics change.
	OrphanRepairSeedVersion = "2026-10-06-v1"

	// OrphanRepairActorDisplayName is the name the audit feed shows for the repair.
	OrphanRepairActorDisplayName = "Orphaned policy repair"
)

// Actor identifies who caused lifecycle cleanup.
type Actor struct {
	Principal   urn.Principal
	DisplayName *string
	Slug        *string
}

// Cleaner soft-deletes policies whose sole MCP target no longer exists.
type Cleaner struct {
	tracer trace.Tracer
	audit  *audit.Logger
}

// NewCleaner creates a lifecycle cleaner.
func NewCleaner(tracerProvider trace.TracerProvider, auditLogger *audit.Logger) *Cleaner {
	return &Cleaner{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/risk/policylifecycle"),
		audit:  auditLogger,
	}
}

// ScopeEdit is a surviving policy a server delete removed the server from.
type ScopeEdit struct {
	PolicyID    uuid.UUID
	ScopeBefore []byte
	ScopeAfter  []byte
}

// ServerCleanup is what a server delete did to the project's risk policies.
type ServerCleanup struct {
	// Deleted are policies left with no target, whose results the caller cleans post-commit.
	Deleted    []uuid.UUID
	ScopeEdits []ScopeEdit
}

// SoftDeleteForMCPServer tombstones policies owned exclusively by one server
// and removes the server from every other policy's scope, so no policy keeps
// an id that later saves reject. The caller must invoke it after tombstoning
// the server in the same transaction, and must have taken the Shadow MCP
// admission lock before the server row lock, as tombstone.Lock does.
func (c *Cleaner) SoftDeleteForMCPServer(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID uuid.UUID,
	mcpServerID uuid.UUID,
	actor Actor,
) (_ ServerCleanup, retErr error) {
	ctx, span := c.tracer.Start(ctx, "policylifecycle.softDeleteForMCPServer", trace.WithAttributes(
		attr.ProjectID(projectID.String()),
		attr.McpServerID(mcpServerID.String()),
	))
	defer func() {
		if retErr != nil {
			span.SetStatus(codes.Error, retErr.Error())
		}
		span.End()
	}()

	if err := lockPolicyCleanup(ctx, tx, projectID); err != nil {
		return ServerCleanup{}, err
	}

	queries := repo.New(tx)
	policies, err := queries.ListLifecycleBoundRiskPoliciesByMCPServer(ctx, repo.ListLifecycleBoundRiskPoliciesByMCPServerParams{
		ProjectID:   projectID,
		McpServerID: mcpServerID.String(),
	})
	if err != nil {
		return ServerCleanup{}, fmt.Errorf("list lifecycle-bound risk policies: %w", err)
	}

	deleted, err := c.softDeletePolicies(ctx, tx, organizationID, projectID, policies, actor)
	if err != nil {
		return ServerCleanup{}, err
	}

	scoping, err := queries.ListRiskPoliciesScopingMCPServerForUpdate(ctx, repo.ListRiskPoliciesScopingMCPServerForUpdateParams{
		ProjectID:   projectID,
		McpServerID: mcpServerID.String(),
	})
	if err != nil {
		return ServerCleanup{}, fmt.Errorf("list risk policies scoping the server: %w", err)
	}
	edits := make([]ScopeEdit, 0, len(scoping))
	var emptied []repo.RiskPolicy
	for _, policy := range scoping {
		scope, remaining, allServers, err := withoutServer(policy.McpScope, mcpServerID)
		if err != nil {
			return ServerCleanup{}, fmt.Errorf("remove server from risk policy %s scope: %w", policy.ID, err)
		}
		if remaining == 0 && !allServers {
			emptied = append(emptied, policy)
			continue
		}
		updated, err := queries.SetRiskPolicyMCPScope(ctx, repo.SetRiskPolicyMCPScopeParams{
			McpScope:  scope,
			ID:        policy.ID,
			ProjectID: projectID,
		})
		if err != nil {
			return ServerCleanup{}, fmt.Errorf("update risk policy scope: %w", err)
		}
		audience, err := policycore.AudiencePrincipalURNs(ctx, tx, organizationID, policy.ID.String())
		if err != nil {
			return ServerCleanup{}, fmt.Errorf("load risk policy audience: %w", err)
		}
		if err := c.audit.LogRiskPolicyUpdate(ctx, tx, audit.LogRiskPolicyUpdateEvent{
			OrganizationID:   organizationID,
			ProjectID:        projectID,
			Actor:            actor.Principal,
			ActorDisplayName: actor.DisplayName,
			ActorSlug:        actor.Slug,
			RiskPolicyID:     policy.ID,
			RiskPolicyName:   policy.Name,
			SnapshotBefore:   policycore.ToGoa(policycore.AuditSnapshot(policycore.Project(policy, audience))),
			SnapshotAfter:    policycore.ToGoa(policycore.AuditSnapshot(policycore.Project(updated, audience))),
		}); err != nil {
			return ServerCleanup{}, fmt.Errorf("log risk policy scope update: %w", err)
		}
		edits = append(edits, ScopeEdit{PolicyID: policy.ID, ScopeBefore: policy.McpScope, ScopeAfter: updated.McpScope})
	}

	// A scope that named only this server, repeated, now targets nothing.
	emptiedDeleted, err := c.softDeletePolicies(ctx, tx, organizationID, projectID, emptied, actor)
	if err != nil {
		return ServerCleanup{}, err
	}
	deleted = append(deleted, emptiedDeleted...)

	span.SetAttributes(attr.DBDeletedRowsCount(int64(len(deleted))))
	return ServerCleanup{Deleted: deleted, ScopeEdits: edits}, nil
}

// withoutServer drops every entry for serverID from a scope's servers and
// keeps the other keys. allServers follows the lifecycle query: only a missing
// or false all_servers leaves the policy bound to its listed servers.
func withoutServer(raw []byte, serverID uuid.UUID) (_ []byte, remaining int, allServers bool, _ error) {
	var scope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &scope); err != nil {
		return nil, 0, false, fmt.Errorf("decode scope: %w", err)
	}
	var servers []json.RawMessage
	if err := json.Unmarshal(scope["servers"], &servers); err != nil {
		return nil, 0, false, fmt.Errorf("decode scope servers: %w", err)
	}
	kept := make([]json.RawMessage, 0, len(servers))
	for _, entry := range servers {
		var server struct {
			MCPServerID string `json:"mcp_server_id"`
		}
		if err := json.Unmarshal(entry, &server); err == nil && server.MCPServerID == serverID.String() {
			continue
		}
		kept = append(kept, entry)
	}
	encodedServers, err := json.Marshal(kept)
	if err != nil {
		return nil, 0, false, fmt.Errorf("encode scope servers: %w", err)
	}
	scope["servers"] = encodedServers
	encoded, err := json.Marshal(scope)
	if err != nil {
		return nil, 0, false, fmt.Errorf("encode scope: %w", err)
	}
	flag, ok := scope["all_servers"]
	allServers = ok && string(flag) != "false"
	return encoded, len(kept), allServers, nil
}

// RepairOrphans tombstones lifecycle-bound policies left by server deletions
// that predate transactional cleanup. It is idempotent and commits per project.
func (c *Cleaner) RepairOrphans(ctx context.Context, db *pgxpool.Pool) (_ []uuid.UUID, retErr error) {
	ctx, span := c.tracer.Start(ctx, "policylifecycle.repairOrphans")
	defer func() {
		if retErr != nil {
			span.SetStatus(codes.Error, retErr.Error())
		}
		span.End()
	}()

	projectIDs, err := repo.New(db).ListProjectIDsWithOrphanedLifecycleBoundRiskPolicies(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects with orphaned risk policies: %w", err)
	}

	var deleted []uuid.UUID
	for _, projectID := range projectIDs {
		projectDeleted, err := c.repairProject(ctx, db, projectID)
		if err != nil {
			return nil, err
		}
		deleted = append(deleted, projectDeleted...)
	}

	span.SetAttributes(attr.DBDeletedRowsCount(int64(len(deleted))))
	return deleted, nil
}

func (c *Cleaner) repairProject(ctx context.Context, db *pgxpool.Pool, projectID uuid.UUID) (_ []uuid.UUID, retErr error) {
	ctx, span := c.tracer.Start(ctx, "policylifecycle.repairProject", trace.WithAttributes(
		attr.ProjectID(projectID.String()),
	))
	defer func() {
		if retErr != nil {
			span.SetStatus(codes.Error, retErr.Error())
		}
		span.End()
	}()

	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin orphaned risk policy repair: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	if err := lockPolicyCleanup(ctx, tx, projectID); err != nil {
		return nil, err
	}

	policies, err := repo.New(tx).ListOrphanedLifecycleBoundRiskPoliciesByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list orphaned risk policies: %w", err)
	}
	if len(policies) == 0 {
		return nil, nil
	}

	deleted, err := c.softDeletePolicies(
		ctx,
		tx,
		policies[0].OrganizationID,
		projectID,
		policies,
		Actor{
			Principal:   urn.NewSystemPrincipal(OrphanRepairSeedName),
			DisplayName: conv.PtrEmpty(OrphanRepairActorDisplayName),
			Slug:        nil,
		},
	)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit orphaned risk policy repair: %w", err)
	}
	span.SetAttributes(attr.DBDeletedRowsCount(int64(len(deleted))))
	return deleted, nil
}

// lockPolicyCleanup takes the project locks every risk policy writer takes,
// in the shared order: Shadow MCP admission, then policy mutations, then
// exclusion mutations. A server delete must take the admission lock before it
// locks the server row, because UpdateMcpServer takes them in that order.
// Taking the advisory lock again in the same transaction does not block.
func lockPolicyCleanup(ctx context.Context, tx pgx.Tx, projectID uuid.UUID) error {
	if err := shadowadmission.LockProject(ctx, tx, projectID); err != nil {
		return fmt.Errorf("lock shadow MCP admission: %w", err)
	}
	queries := repo.New(tx)
	if err := queries.LockRiskPolicyMutations(ctx, projectID.String()); err != nil {
		return fmt.Errorf("lock risk policy mutations: %w", err)
	}
	if err := queries.LockRiskExclusionMutations(ctx, projectID.String()); err != nil {
		return fmt.Errorf("lock risk exclusion mutations: %w", err)
	}
	return nil
}

func (c *Cleaner) softDeletePolicies(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID uuid.UUID,
	policies []repo.RiskPolicy,
	actor Actor,
) ([]uuid.UUID, error) {
	queries := repo.New(tx)
	deleted := make([]uuid.UUID, 0, len(policies))
	for _, policy := range policies {
		if err := queries.DeleteRiskPolicy(ctx, repo.DeleteRiskPolicyParams{
			ID:        policy.ID,
			ProjectID: projectID,
		}); err != nil {
			return nil, fmt.Errorf("soft-delete risk policy: %w", err)
		}
		if err := queries.DeleteRiskPolicyBypassRequestsByPolicy(ctx, repo.DeleteRiskPolicyBypassRequestsByPolicyParams{
			RiskPolicyID: policy.ID,
			ProjectID:    projectID,
		}); err != nil {
			return nil, fmt.Errorf("soft-delete risk policy bypass requests: %w", err)
		}
		if _, err := queries.DeleteRiskExclusionsByPolicy(ctx, repo.DeleteRiskExclusionsByPolicyParams{
			RiskPolicyID: uuid.NullUUID{UUID: policy.ID, Valid: true},
			ProjectID:    projectID,
		}); err != nil {
			return nil, fmt.Errorf("delete risk policy exclusions: %w", err)
		}
		if err := authz.ReplaceGrantAudience(ctx, tx, authz.ResourceGrant{
			Resource: authz.Resource{
				OrganizationID: organizationID,
				Scope:          authz.ScopeRiskPolicyEvaluate,
				ResourceID:     policy.ID.String(),
			},
			Principals: nil,
			Selector:   authz.NewSelector(authz.ScopeRiskPolicyEvaluate, policy.ID.String()),
		}); err != nil {
			return nil, fmt.Errorf("clear risk policy audience: %w", err)
		}
		if err := c.audit.LogRiskPolicyDelete(ctx, tx, audit.LogRiskPolicyDeleteEvent{
			OrganizationID:   organizationID,
			ProjectID:        projectID,
			Actor:            actor.Principal,
			ActorDisplayName: actor.DisplayName,
			ActorSlug:        actor.Slug,
			RiskPolicyID:     policy.ID,
			RiskPolicyName:   policy.Name,
		}); err != nil {
			return nil, fmt.Errorf("log risk policy deletion: %w", err)
		}
		deleted = append(deleted, policy.ID)
	}

	return deleted, nil
}
