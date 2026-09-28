// Package roleprovisioning owns saved role-provisioning intent and short,
// transaction-owned plugin transitions. Callers provide authorization and schedule
// work separately; no request, IdP sync, or publisher runs inside these locks.
package roleprovisioning

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/roleprovisioning/hints"
	"github.com/speakeasy-api/gram/server/internal/roleprovisioning/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var (
	ErrConflict = errors.New("role-provisioning settings version conflict")
	ErrInvalid  = errors.New("invalid role-provisioning configuration")
)

// Actor is supplied by the authenticated entry point or lifecycle consumer.
// PublicationUserID is independent of the audit principal and may be absent.
type Actor struct {
	Principal         urn.Principal
	PublicationUserID string
}

type Service struct {
	db          *pgxpool.Pool
	audit       *audit.Logger
	guard       *admission.Guard
	publication plugins.PublicationRequests
}

func New(db *pgxpool.Pool, logger *audit.Logger, guard *admission.Guard, publication plugins.PublicationRequests) *Service {
	return &Service{db: db, audit: logger, guard: guard, publication: publication}
}

type Selection struct {
	RoleURN string
	Enabled bool
	// Nil leaves an existing destination unchanged; a pointer to uuid.Nil clears it.
	ProjectID *uuid.UUID
}

type ConfigureInput struct {
	OrganizationID  string
	ExpectedVersion int64
	Enabled         bool
	// Nil preserves the saved default. On first configuration, it selects the
	// best eligible project. A pointer to uuid.Nil explicitly leaves it pending.
	ProjectID *uuid.UUID
	Roles     []Selection
}

type RoleSettings struct {
	RoleURN   string
	Enabled   bool
	ProjectID uuid.NullUUID
}

type Settings struct {
	Roles     []RoleSettings
	Enabled   bool
	ProjectID uuid.NullUUID
	Version   int64
}

func (s *Service) Settings(ctx context.Context, organizationID string) (Settings, error) {
	// The version and role selections are one logical configuration snapshot.
	// Repeatable read prevents a concurrent Configure commit between these reads
	// from pairing an old conflict token with new selections.
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Settings{}, fmt.Errorf("begin role-provisioning settings snapshot: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	q := repo.New(tx)
	row, err := q.GetSettings(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Settings{}, nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("read role-provisioning settings: %w", err)
	}
	roles, err := q.ListRoleSettings(ctx, text(organizationID))
	if err != nil {
		return Settings{}, fmt.Errorf("read role settings: %w", err)
	}
	result := Settings{Enabled: row.Enabled, ProjectID: row.ProjectID, Version: row.Version, Roles: make([]RoleSettings, 0, len(roles))}
	for _, role := range roles {
		result.Roles = append(result.Roles, RoleSettings{RoleURN: role.RoleUrn, Enabled: role.Enabled, ProjectID: role.ProjectID})
	}
	if err := tx.Commit(ctx); err != nil {
		return Settings{}, fmt.Errorf("complete role-provisioning settings snapshot: %w", err)
	}
	return result, nil
}

// Configure commits intent and its durable hint, never downstream provisioning.
// The expected version covers every explicit organization and per-role change.
func (s *Service) Configure(ctx context.Context, in ConfigureInput) (int64, error) {
	if in.OrganizationID == "" || in.ExpectedVersion < 0 {
		return 0, ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	q := repo.New(tx)
	if _, err = q.LockOrganization(ctx, in.OrganizationID); err != nil {
		return 0, fmt.Errorf("lock organization: %w", err)
	}
	saved, err := q.GetSettings(ctx, in.OrganizationID)
	initial := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !initial {
		return 0, err
	}
	if saved.Version != in.ExpectedVersion {
		return 0, ErrConflict
	}
	projects, err := q.ListProjects(ctx, in.OrganizationID)
	if err != nil {
		return 0, err
	}
	// Seeded and legacy inert rows are still untouched configuration. Every
	// explicit save increments version, including a deliberately pending NULL
	// destination, so re-enable must not choose again once version is nonzero.
	untouched := initial || (!saved.Enabled && !saved.ProjectID.Valid && saved.Version == 0)
	destination := saved.ProjectID
	if untouched && len(projects) > 0 {
		destination = uuid.NullUUID{UUID: projects[0], Valid: true}
	}
	if in.ProjectID != nil {
		destination = nullableID(*in.ProjectID)
	}
	if in.ProjectID != nil && destination.Valid && !slices.Contains(projects, destination.UUID) {
		return 0, fmt.Errorf("%w: destination project", ErrInvalid)
	}
	roles, err := accessrepo.New(tx).ListActiveOrganizationRoles(ctx, in.OrganizationID)
	if err != nil {
		return 0, err
	}
	selected := make(map[string]Selection, len(in.Roles))
	for _, selection := range in.Roles {
		if _, ok := selected[selection.RoleURN]; ok {
			return 0, fmt.Errorf("%w: duplicate role", ErrInvalid)
		}
		if !slices.ContainsFunc(roles, func(r accessrepo.ListActiveOrganizationRolesRow) bool { return r.RoleUrn == selection.RoleURN }) {
			return 0, fmt.Errorf("%w: role", ErrInvalid)
		}
		if selection.ProjectID != nil && *selection.ProjectID != uuid.Nil && !slices.Contains(projects, *selection.ProjectID) {
			return 0, fmt.Errorf("%w: role destination", ErrInvalid)
		}
		selected[selection.RoleURN] = selection
	}
	for _, role := range roles {
		if _, err = lockRole(ctx, q, in.OrganizationID, role.RoleUrn); err != nil {
			return 0, err
		}
		// First confirmation starts all roles selected; later calls preserve saved
		// exclusions. New roles inherit the sticky organization default.
		if err = q.EnsureRoleSetting(ctx, repo.EnsureRoleSettingParams{OrganizationID: text(in.OrganizationID), RoleUrn: role.RoleUrn, Enabled: true, ProjectID: destination}); err != nil {
			return 0, err
		}
		selection, ok := selected[role.RoleUrn]
		if !ok {
			continue
		}
		setting, err := q.GetRoleSetting(ctx, repo.GetRoleSettingParams{OrganizationID: text(in.OrganizationID), RoleUrn: role.RoleUrn})
		if err != nil {
			return 0, err
		}
		target := setting.ProjectID
		if selection.ProjectID != nil {
			target = nullableID(*selection.ProjectID)
		}
		if err = q.SaveRoleSetting(ctx, repo.SaveRoleSettingParams{OrganizationID: text(in.OrganizationID), RoleUrn: role.RoleUrn, Enabled: selection.Enabled, ProjectID: target}); err != nil {
			return 0, err
		}
	}
	version, err := q.SaveSettings(ctx, repo.SaveSettingsParams{OrganizationID: in.OrganizationID, Enabled: pgtype.Bool{Bool: in.Enabled, Valid: true}, ProjectID: destination, Version: pgtype.Int8{Int64: saved.Version + 1, Valid: true}})
	if err != nil {
		return 0, err
	}
	if err = hints.Emit(ctx, tx, hints.Hint{OrganizationID: in.OrganizationID}); err != nil {
		return 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return version, nil
}

func nullableID(id uuid.UUID) uuid.NullUUID { return uuid.NullUUID{UUID: id, Valid: id != uuid.Nil} }
func text(s string) pgtype.Text             { return pgtype.Text{String: s, Valid: true} }

func lockRole(ctx context.Context, q *repo.Queries, org, role string) (string, error) {
	parts := strings.Split(role, ":")
	if len(parts) != 3 || parts[0] != "role" {
		return "", ErrInvalid
	}
	id, err := uuid.Parse(parts[2])
	if err != nil || id.String() != parts[2] {
		return "", ErrInvalid
	}
	switch parts[1] {
	case "organization":
		return q.LockOrganizationRole(ctx, repo.LockOrganizationRoleParams{ID: id, OrganizationID: org})
	case "global":
		return q.LockGlobalRole(ctx, id)
	default:
		return "", ErrInvalid
	}
}
