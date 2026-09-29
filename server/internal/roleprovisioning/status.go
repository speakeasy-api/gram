package roleprovisioning

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/roleprovisioning/repo"
)

// Status is a consistent snapshot of intent and observed outcomes. It never
// performs provisioning, admission checks, publication, or feature flag writes.
type Status struct {
	Enabled   bool
	Version   int64
	ProjectID uuid.NullUUID
	Roles     []RoleStatus
	Projects  []ProjectOption
}
type ProjectOption struct {
	ID   uuid.UUID
	Name string
}
type RoleStatus struct {
	RoleURN string
	Name    string
	// Configured distinguishes saved intent from the first-selection default.
	Configured       bool
	Enabled          bool
	ProjectID        uuid.NullUUID
	AppliedProjectID uuid.NullUUID
	PluginID         uuid.NullUUID
	OriginAudience   string
	// published_before only means a fingerprint exists, not that current contents
	// have been published. Full publication freshness remains on plugin status.
	PublicationStatus string
	PendingReason     string
}

func (s *SettingsService) Status(ctx context.Context, organizationID string) (Status, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly, DeferrableMode: "", BeginQuery: "", CommitQuery: ""})
	if err != nil {
		return Status{}, fmt.Errorf("begin provisioning status: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	q := repo.New(tx)
	saved, err := q.GetSettings(ctx, organizationID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Status{}, fmt.Errorf("read provisioning settings: %w", err)
	}
	result := Status{Enabled: saved.Enabled, Version: saved.Version, ProjectID: saved.ProjectID, Roles: []RoleStatus{}, Projects: []ProjectOption{}}
	projects, err := q.ListStatusProjects(ctx, organizationID)
	if err != nil {
		return Status{}, fmt.Errorf("read provisioning projects: %w", err)
	}
	eligible := make(map[uuid.UUID]bool, len(projects))
	for _, p := range projects {
		result.Projects = append(result.Projects, ProjectOption{ID: p.ID, Name: p.Name})
		eligible[p.ID] = true
	}
	roles, err := accessrepo.New(tx).ListActiveOrganizationRoles(ctx, organizationID)
	if err != nil {
		return Status{}, fmt.Errorf("read provisioning roles: %w", err)
	}
	rows, err := q.ListRoleStatus(ctx, text(organizationID))
	if err != nil {
		return Status{}, fmt.Errorf("read provisioning outcomes: %w", err)
	}
	byRole := make(map[string]repo.ListRoleStatusRow, len(rows))
	for _, row := range rows {
		byRole[row.RoleUrn] = row
	}
	untouched := saved.Version == 0 && !saved.Enabled && !saved.ProjectID.Valid
	for _, role := range roles {
		r := RoleStatus{RoleURN: role.RoleUrn, Name: role.WorkosName, Configured: false, AppliedProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, PluginID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, PendingReason: "", Enabled: untouched || saved.Enabled, ProjectID: saved.ProjectID, OriginAudience: "not_provisioned", PublicationStatus: "not_provisioned"}
		if row, ok := byRole[role.RoleUrn]; ok {
			r.Configured = true
			r.Enabled, r.ProjectID = row.Enabled, row.ProjectID
			r.AppliedProjectID, r.PluginID = row.AppliedProjectID, row.PluginID
			r.PublicationStatus = row.PublicationStatus
			if r.PluginID.Valid {
				r.OriginAudience = "not_assigned"
				if row.OriginAssigned {
					r.OriginAudience = "assigned"
				}
			}
			if result.Enabled && r.Enabled {
				r.PendingReason = row.LastErrorCode
			}
		}
		if result.Enabled && r.Enabled {
			if !r.ProjectID.Valid || !eligible[r.ProjectID.UUID] {
				r.PendingReason = "choose_project"
			} else if r.PendingReason == "" && (!r.PluginID.Valid || r.AppliedProjectID != r.ProjectID || r.OriginAudience != "assigned") {
				r.PendingReason = "reconciliation_pending"
			}
		}
		result.Roles = append(result.Roles, r)
	}
	if err := tx.Commit(ctx); err != nil {
		return Status{}, fmt.Errorf("complete provisioning status: %w", err)
	}
	return result, nil
}
