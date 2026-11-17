package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ListAssistantOrganizations lists only organizations with live assistants in live projects.
func (s *Service) ListAssistantOrganizations(ctx context.Context, _ *gen.ListAssistantOrganizationsPayload) (*gen.AdminAssistantOrganizations, error) {
	rows, err := repo.New(s.db).AdminListAssistantOrganizations(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list assistant organizations").LogError(ctx, s.logger)
	}
	result := &gen.AdminAssistantOrganizations{Organizations: make([]*gen.AdminAssistantOrganization, 0, len(rows))}
	for _, row := range rows {
		result.Organizations = append(result.Organizations, &gen.AdminAssistantOrganization{ID: row.ID, Name: row.Name, Slug: row.Slug, Assistants: row.Assistants})
	}
	return result, nil
}

// RepairOrganizationAssistantAgentNames repairs one exact organization's legacy names.
func (s *Service) RepairOrganizationAssistantAgentNames(ctx context.Context, p *gen.RepairOrganizationAssistantAgentNamesPayload) (*gen.AdminAssistantAgentNameRepairRun, error) {
	auth, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || auth == nil || auth.OIDCSubject == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	organizationID, err := s.canonicalAdminOrganizationID(ctx, p.OrganizationID)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin assistant agent name repair").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	result, err := RepairAssistantAgentNamesTx(ctx, tx, s.logger, s.audit, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "repair assistant agent names").LogError(ctx, s.logger)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit assistant agent name repair").LogError(ctx, s.logger)
	}
	s.logger.InfoContext(ctx, "assistant agent name repair ran", attr.SlogOrganizationID(organizationID), attr.SlogAdminOIDCSubject(auth.OIDCSubject), attr.SlogAuditAction("assistant_agent_names:repair"))
	return result, nil
}

// RepairAssistantAgentNamesTx is shared by the staff API and Admin MCP. Each
// rename and its customer audit event share a savepoint. The unique name index
// handles concurrent name reservations; a failed rename never aborts its peers.
func RepairAssistantAgentNamesTx(ctx context.Context, tx pgx.Tx, logger *slog.Logger, auditLogger *audit.Logger, organizationID string) (*gen.AdminAssistantAgentNameRepairRun, error) {
	q := repo.New(tx)
	if _, err := q.AdminGetOrganization(ctx, repo.AdminGetOrganizationParams{ID: organizationID, AllowSlug: false}); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAssistantMigrationOrganizationNotFound
	} else if err != nil {
		return nil, fmt.Errorf("read organization: %w", err)
	}
	if err := q.AdminLockAssistantAgentNames(ctx, organizationID); err != nil {
		return nil, fmt.Errorf("lock assistant names: %w", err)
	}
	rows, err := q.AdminListAssistantAgentNames(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("read assistant names: %w", err)
	}
	result := &gen.AdminAssistantAgentNameRepairRun{OrganizationID: organizationID, Repaired: 0, Conflicts: 0, Failed: 0, Results: make([]*gen.AdminAssistantAgentNameRepairResult, 0, len(rows))}
	for _, row := range rows {
		if row.RepairState == "not_needed" {
			continue
		}
		outcome := "conflict"
		if row.RepairState == "pending" {
			err := repairAssistantAgentName(ctx, tx, auditLogger, organizationID, row)
			var pgerr *pgconn.PgError
			switch {
			case err == nil:
				outcome = "repaired"
			case errors.As(err, &pgerr) && pgerr.Code == pgerrcode.UniqueViolation && pgerr.ConstraintName == "agents_organization_name_key":
				outcome = "conflict"
			default:
				outcome = "failed"
				logger.ErrorContext(ctx, "repair assistant agent name", attr.SlogError(err), attr.SlogAssistantID(row.AssistantID.String()))
			}
		}
		switch outcome {
		case "repaired":
			result.Repaired++
		case "conflict":
			result.Conflicts++
		default:
			result.Failed++
		}
		result.Results = append(result.Results, &gen.AdminAssistantAgentNameRepairResult{AssistantID: row.AssistantID.String(), ProjectID: row.ProjectID.String(), Name: row.Name, Outcome: outcome})
	}
	return result, nil
}

func repairAssistantAgentName(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, organizationID string, row repo.AdminListAssistantAgentNamesRow) error {
	savepoint, err := tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin rename savepoint: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return savepoint.Rollback(ctx) })
	count, err := repo.New(savepoint).AdminRepairAssistantAgentName(ctx, repo.AdminRepairAssistantAgentNameParams{Name: row.Name, AgentID: row.AgentID, OrganizationID: organizationID, ProjectID: uuid.NullUUID{UUID: row.ProjectID, Valid: true}, OldName: row.AgentName})
	if err != nil {
		return fmt.Errorf("rename agent: %w", err)
	}
	if count != 1 {
		return errors.New("assistant agent changed during repair")
	}
	before := audit.AgentSnapshot{OwnerUserID: row.OwnerUserID, OwnerReassignmentRequiredAt: nil, OwnerReassignmentReason: conv.FromPGText[string](row.OwnerReassignmentReason), Name: row.AgentName, Lifecycle: row.Lifecycle}
	if row.OwnerReassignmentRequiredAt.Valid {
		before.OwnerReassignmentRequiredAt = new(row.OwnerReassignmentRequiredAt.Time.Format(time.RFC3339Nano))
	}
	after := before
	after.Name = row.Name
	actor, displayName, _ := adminActor(ctx)
	if err := auditLogger.LogAgent(ctx, savepoint, audit.LogAgentEvent{OrganizationID: organizationID, AgentURN: urn.NewAgentIdentity(row.AgentID.String()), Actor: actor, ActorDisplayName: displayName, Action: audit.ActionAgentRename, Name: row.Name, Before: &before, After: &after}); err != nil {
		return fmt.Errorf("audit agent rename: %w", err)
	}
	if err := savepoint.Commit(ctx); err != nil {
		return fmt.Errorf("commit agent rename savepoint: %w", err)
	}
	return nil
}
