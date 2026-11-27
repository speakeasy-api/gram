package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"

	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	platformtoolscore "github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	assistantBuiltinGrantsGranted        = "granted"
	assistantBuiltinGrantsAlreadyGranted = "already_granted"
	assistantBuiltinGrantsFailed         = "failed"
)

// BackfillOrganizationAssistantBuiltinGrants grants every live agent-backed
// assistant in one exact organization explicit mcp:connect policy grants for
// its built-in toolsets, attributed to the staff member running it.
func (s *Service) BackfillOrganizationAssistantBuiltinGrants(ctx context.Context, p *gen.BackfillOrganizationAssistantBuiltinGrantsPayload) (*gen.AdminAssistantBuiltinGrantBackfillRun, error) {
	auth, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || auth == nil || auth.OIDCSubject == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	organizationID, err := s.canonicalAdminOrganizationID(ctx, p.OrganizationID)
	if err != nil {
		return nil, err
	}
	logger := s.logger.With(attr.SlogOrganizationID(organizationID))

	// The run outlives the request, so a closed tab does not abandon it.
	detached := context.WithoutCancel(ctx)
	runCtx, cancel := context.WithTimeout(detached, assistantIdentityMigrationTimeout)
	defer cancel()

	tx, err := s.db.Begin(runCtx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin assistant builtin grant backfill").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(detached) })
	run, err := BackfillAssistantBuiltinGrantsTx(runCtx, tx, logger, s.audit, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "backfill assistant builtin grants").LogError(ctx, logger)
	}
	if err := tx.Commit(runCtx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit assistant builtin grant backfill").LogError(ctx, logger)
	}

	logger.InfoContext(detached, "assistant builtin grant backfill ran",
		attr.SlogAuditAction("assistant_builtin_grants:backfill"),
		attr.SlogAdminOIDCSubject(auth.OIDCSubject),
	)
	return run, nil
}

// BackfillAssistantBuiltinGrantsTx is shared by the staff API and Admin MCP.
// Each assistant runs under its own savepoint, so one failure never aborts its
// peers. Existing project-wide wildcard mcp:connect grants do not suppress
// writing the explicit built-in grants, so every assistant agent holds visible,
// editable built-in policy rows.
func BackfillAssistantBuiltinGrantsTx(ctx context.Context, tx pgx.Tx, logger *slog.Logger, auditLogger *audit.Logger, organizationID string) (*gen.AdminAssistantBuiltinGrantBackfillRun, error) {
	q := repo.New(tx)
	if _, err := q.AdminGetOrganization(ctx, repo.AdminGetOrganizationParams{ID: organizationID, AllowSlug: false}); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAssistantMigrationOrganizationNotFound
	} else if err != nil {
		return nil, fmt.Errorf("read organization: %w", err)
	}
	rows, err := q.AdminListAssistantBuiltinGrantTargets(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list assistant builtin grant targets: %w", err)
	}
	run := &gen.AdminAssistantBuiltinGrantBackfillRun{
		OrganizationID: organizationID,
		Granted:        0,
		AlreadyGranted: 0,
		Failed:         0,
		Results:        make([]*gen.AdminAssistantBuiltinGrantBackfillResult, 0, len(rows)),
	}
	for _, row := range rows {
		added, err := backfillAssistantBuiltinGrants(ctx, tx, auditLogger, organizationID, row)
		outcome := assistantBuiltinGrantsAlreadyGranted
		switch {
		case err != nil:
			outcome = assistantBuiltinGrantsFailed
			added = 0
			run.Failed++
			logger.ErrorContext(ctx, "backfill assistant builtin grants", attr.SlogError(err), attr.SlogAssistantID(row.AssistantID.String()))
		case added > 0:
			outcome = assistantBuiltinGrantsGranted
			run.Granted++
		default:
			run.AlreadyGranted++
		}
		run.Results = append(run.Results, &gen.AdminAssistantBuiltinGrantBackfillResult{
			AssistantID: row.AssistantID.String(),
			ProjectID:   row.ProjectID.String(),
			Name:        row.Name,
			Outcome:     outcome,
			GrantsAdded: added,
		})
	}
	return run, nil
}

func backfillAssistantBuiltinGrants(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, organizationID string, row repo.AdminListAssistantBuiltinGrantTargetsRow) (int64, error) {
	savepoint, err := tx.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin builtin grant backfill savepoint: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return savepoint.Rollback(ctx) })

	agents := agentrepo.New(savepoint)
	existing, err := agents.ListAgentPolicyGrants(ctx, agentrepo.ListAgentPolicyGrantsParams{
		OrganizationID: organizationID,
		AgentID:        row.AgentID,
	})
	if err != nil {
		return 0, fmt.Errorf("list agent policy grants: %w", err)
	}

	desired := assistantidentity.BuiltinConnectGrants(row.ProjectID, row.IsManaged)
	if len(row.SlackAuthClientIds) > 0 {
		desired = append(desired, assistantidentity.BuiltinConnectGrant(row.ProjectID, platformtoolscore.AssistantSlackPlatformToolsetSlug))
		for _, clientID := range row.SlackAuthClientIds {
			desired = append(desired, assistantidentity.AssistantSlackAuthGrant(clientID))
		}
	}
	actor, displayName, _ := adminActor(ctx)
	agentURN := urn.NewAgentIdentity(row.AgentID.String())
	var added int64
	for _, grant := range desired {
		prior, found := findExactGrant(existing, grant)
		if found && !prior.ExpiresAt.Valid {
			continue
		}
		encoded, err := json.Marshal(grant.Selector)
		if err != nil {
			return 0, fmt.Errorf("encode builtin grant selector: %w", err)
		}
		written, err := agents.EnsurePermanentAgentPolicyGrant(ctx, agentrepo.EnsurePermanentAgentPolicyGrantParams{
			OrganizationID: organizationID,
			AgentID:        row.AgentID,
			Scope:          string(grant.Scope),
			Selectors:      encoded,
		})
		if err != nil {
			return 0, fmt.Errorf("ensure builtin grant: %w", err)
		}
		action := audit.ActionAgentPolicyGrantCreate
		var before *audit.AgentPolicyGrantSnapshot
		if found {
			action = audit.ActionAgentPolicyGrantUpdate
			before = &audit.AgentPolicyGrantSnapshot{
				ID:       prior.ID,
				Scope:    prior.Scope,
				Effect:   "allow",
				Selector: grant.Selector,
			}
		}
		if err := auditLogger.LogAgentPolicyGrant(ctx, savepoint, audit.LogAgentPolicyGrantEvent{
			OrganizationID:   organizationID,
			AgentURN:         agentURN,
			Actor:            actor,
			ActorDisplayName: displayName,
			Action:           action,
			Name:             row.AgentName,
			Before:           before,
			After: &audit.AgentPolicyGrantSnapshot{
				ID:       written.ID,
				Scope:    written.Scope,
				Effect:   "allow",
				Selector: grant.Selector,
			},
		}); err != nil {
			return 0, fmt.Errorf("audit builtin grant: %w", err)
		}
		added++
	}
	if err := savepoint.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit builtin grant backfill savepoint: %w", err)
	}
	return added, nil
}

// findExactGrant looks up an existing grant row matching desired's scope and
// selector. Wildcard grants such as mcp:connect * do not match, so explicit
// per-built-in policy rows are still written.
func findExactGrant(rows []agentrepo.ListAgentPolicyGrantsRow, desired authz.Grant) (agentrepo.ListAgentPolicyGrantsRow, bool) {
	for _, row := range rows {
		if row.Scope != string(desired.Scope) {
			continue
		}
		stored, err := authz.SelectorFromRow(row.Selectors)
		if err != nil {
			continue
		}
		if maps.Equal(stored, desired.Selector) {
			return row, true
		}
	}
	var zero agentrepo.ListAgentPolicyGrantsRow
	return zero, false
}
