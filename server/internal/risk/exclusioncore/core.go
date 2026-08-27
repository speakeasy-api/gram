package exclusioncore

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	RegexMaxLength      = 512
	MaxRegexPerScope    = 50
	displayNameMaxRunes = 80
)

var (
	ErrPolicyNotFound    = errors.New("risk policy not found")
	ErrExclusionNotFound = errors.New("risk exclusion not found")
)

// Exclusion is the transport-neutral representation of a persisted exclusion.
type Exclusion struct {
	ID             uuid.UUID
	ProjectID      uuid.UUID
	OrganizationID string
	RiskPolicyID   uuid.NullUUID
	MatchType      string
	MatchValue     string
	RuleIDFilter   string
	SourceFilter   string
	Enabled        bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Actor is the real user attributed to an exclusion mutation.
type Actor struct {
	Principal   urn.Principal
	DisplayName *string
	Slug        *string
}

// Transactor is the transaction capability required by exclusion mutations.
type Transactor interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// MutationAuditor records exclusion writes and their outbox event atomically.
type MutationAuditor interface {
	LogExclusionCreate(ctx context.Context, db repo.DBTX, event CreateAuditEvent) error
	LogExclusionUpdate(ctx context.Context, db repo.DBTX, event UpdateAuditEvent) error
	LogExclusionDelete(ctx context.Context, db repo.DBTX, event DeleteAuditEvent) error
}

// AfterCommit triggers best-effort historical reconciliation after commit.
type AfterCommit func(ctx context.Context, projectID, exclusionID uuid.UUID)

// MutationDependencies are optional for read-only Core users and required by writes.
type MutationDependencies struct {
	Transactor  Transactor
	Auditor     MutationAuditor
	AfterCommit AfterCommit
	Redactor    Redactor
}

type Core struct {
	db        repo.DBTX
	queries   *repo.Queries
	mutations *MutationDependencies
}

func New(db repo.DBTX, mutations ...MutationDependencies) *Core {
	core := &Core{db: db, queries: repo.New(db), mutations: nil}
	if len(mutations) > 0 {
		core.mutations = &mutations[0]
	}
	return core
}

// PageCursor identifies one exclusion in deterministic keyset order.
type PageCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// List returns a project's exclusions, optionally filtered to one policy.
func (c *Core) List(ctx context.Context, projectID uuid.UUID, policyID uuid.NullUUID) ([]Exclusion, error) {
	rows, err := c.queries.ListRiskExclusionsByProject(ctx, repo.ListRiskExclusionsByProjectParams{
		ProjectID:    projectID,
		RiskPolicyID: policyID,
	})
	if err != nil {
		return nil, fmt.Errorf("list risk exclusions: %w", err)
	}
	out := make([]Exclusion, 0, len(rows))
	for _, row := range rows {
		out = append(out, Project(row))
	}
	return out, nil
}

// ListPage returns at most limit+1 exclusions in deterministic keyset order.
func (c *Core) ListPage(ctx context.Context, projectID uuid.UUID, policyID uuid.NullUUID, cursor *PageCursor, limit int32) ([]Exclusion, error) {
	params := repo.ListRiskExclusionsByProjectPageParams{
		ProjectID:       projectID,
		RiskPolicyID:    policyID,
		CursorCreatedAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		CursorID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		PageLimit:       limit + 1,
	}
	if cursor != nil {
		params.CursorCreatedAt = pgtype.Timestamptz{Time: cursor.CreatedAt, InfinityModifier: pgtype.Finite, Valid: true}
		params.CursorID = uuid.NullUUID{UUID: cursor.ID, Valid: true}
	}
	rows, err := c.queries.ListRiskExclusionsByProjectPage(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list risk exclusions page: %w", err)
	}
	out := make([]Exclusion, 0, len(rows))
	for _, row := range rows {
		out = append(out, Project(row))
	}
	return out, nil
}

func Project(row repo.RiskExclusion) Exclusion {
	return Exclusion{
		ID:             row.ID,
		ProjectID:      row.ProjectID,
		OrganizationID: row.OrganizationID,
		RiskPolicyID:   row.RiskPolicyID,
		MatchType:      row.MatchType,
		MatchValue:     row.MatchValue,
		RuleIDFilter:   valueOrEmpty(row.RuleIDFilter),
		SourceFilter:   valueOrEmpty(row.SourceFilter),
		Enabled:        row.Enabled,
		CreatedAt:      row.CreatedAt.Time,
		UpdatedAt:      row.UpdatedAt.Time,
	}
}

// ValidationError separates a stable client message from its technical cause.
type ValidationError struct {
	Message string
	Cause   error
}

func (e *ValidationError) Error() string { return e.Message }
func (e *ValidationError) Unwrap() error { return e.Cause }

func ValidateMatchValue(matchType, matchValue string) error {
	if matchValue == "" {
		return &ValidationError{Message: "match_value must not be empty", Cause: nil}
	}
	if matchType != "regex" {
		return nil
	}
	if utf8.RuneCountInString(matchValue) > RegexMaxLength {
		return &ValidationError{Message: fmt.Sprintf("regex pattern too long (max %d characters)", RegexMaxLength), Cause: nil}
	}
	if _, err := regexp.Compile(matchValue); err != nil {
		return &ValidationError{Message: "invalid regex pattern", Cause: err}
	}
	return nil
}

func DisplayName(redactor Redactor, exclusion Exclusion) string {
	name := exclusion.MatchType + ":" + redactor.Redact(exclusion.ProjectID.String(), "match_value", exclusion.MatchValue)
	if len([]rune(name)) > displayNameMaxRunes {
		return string([]rune(name)[:displayNameMaxRunes])
	}
	return name
}

func AuditSnapshot(redactor Redactor, exclusion Exclusion) Exclusion {
	projectID := exclusion.ProjectID.String()
	exclusion.MatchValue = redactor.Redact(projectID, "match_value", exclusion.MatchValue)
	exclusion.RuleIDFilter = redactor.Redact(projectID, "rule_id_filter", exclusion.RuleIDFilter)
	exclusion.SourceFilter = redactor.Redact(projectID, "source_filter", exclusion.SourceFilter)
	return exclusion
}

// MutationError identifies the failed transaction step.
type MutationError struct {
	Message string
	Cause   error
}

func (e *MutationError) Error() string { return e.Message }
func (e *MutationError) Unwrap() error { return e.Cause }

// RegexLimitError reports an enabled regex scope at capacity.
type RegexLimitError struct{}

func (*RegexLimitError) Error() string {
	return fmt.Sprintf("too many regex exclusions in scope (max %d)", MaxRegexPerScope)
}

// CreateMutation is a fully parsed exclusion create command.
type CreateMutation struct {
	Params repo.CreateRiskExclusionParams
	Actor  Actor
}

// ToggleMutation changes only an exclusion's enabled state. It is the mutation
// contract for transports that do not permit editing exclusion definitions.
type ToggleMutation struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	Enabled   bool
	Actor     Actor
}

// UpdateMutation is a fully parsed exclusion replacement. Nil Enabled preserves
// the authoritative locked row's current value for Goa compatibility.
type UpdateMutation struct {
	ID           uuid.UUID
	ProjectID    uuid.UUID
	RiskPolicyID uuid.NullUUID
	MatchType    string
	MatchValue   string
	RuleIDFilter pgtype.Text
	SourceFilter pgtype.Text
	Enabled      *bool
	Actor        Actor
}

type DeleteMutation struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	Actor     Actor
}

type CreateAuditEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID
	Actor          Actor
	Exclusion      Exclusion
	DisplayName    string
}

type UpdateAuditEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID
	Actor          Actor
	Before         Exclusion
	After          Exclusion
	DisplayName    string
}

type DeleteAuditEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID
	Actor          Actor
	Exclusion      Exclusion
	DisplayName    string
}

func (c *Core) Create(ctx context.Context, input CreateMutation) (Exclusion, error) {
	if err := ValidateMatchValue(input.Params.MatchType, input.Params.MatchValue); err != nil {
		return Exclusion{}, err
	}
	deps, err := c.requireMutationDependencies()
	if err != nil {
		return Exclusion{}, err
	}
	tx, err := deps.Transactor.Begin(ctx)
	if err != nil {
		return Exclusion{}, mutationError("begin transaction", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	exclusion, err := c.createInTransaction(ctx, tx, input, deps)
	if err != nil {
		return Exclusion{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Exclusion{}, mutationError("commit risk exclusion create", err)
	}
	c.AfterCommit(ctx, exclusion)
	return exclusion, nil
}

// CreateInTransaction applies the exclusion row and audit to a caller-owned
// transaction. The caller owns commit and must invoke AfterCommit afterward.
func (c *Core) CreateInTransaction(ctx context.Context, tx pgx.Tx, input CreateMutation) (Exclusion, error) {
	deps, err := c.requireMutationDependencies()
	if err != nil {
		return Exclusion{}, err
	}
	if tx == nil {
		return Exclusion{}, mutationError("risk exclusion mutation transaction is not configured", nil)
	}
	return c.createInTransaction(ctx, tx, input, deps)
}

func (c *Core) createInTransaction(ctx context.Context, tx pgx.Tx, input CreateMutation, deps *MutationDependencies) (Exclusion, error) {
	if err := ValidateMatchValue(input.Params.MatchType, input.Params.MatchValue); err != nil {
		return Exclusion{}, err
	}
	queries := repo.New(tx)
	if err := queries.LockRiskExclusionMutations(ctx, input.Params.ProjectID.String()); err != nil {
		return Exclusion{}, mutationError("lock risk exclusion mutations", err)
	}
	if err := ensurePolicy(ctx, queries, input.Params.ProjectID, input.Params.RiskPolicyID); err != nil {
		return Exclusion{}, err
	}
	if err := enforceRegexLimit(ctx, queries, input.Params.ProjectID, input.Params.RiskPolicyID, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, input.Params.MatchType, input.Params.Enabled); err != nil {
		return Exclusion{}, err
	}
	row, err := queries.CreateRiskExclusion(ctx, input.Params)
	if err != nil {
		return Exclusion{}, mutationError("create risk exclusion", err)
	}
	exclusion := Project(row)
	if err := deps.Auditor.LogExclusionCreate(ctx, tx, CreateAuditEvent{
		OrganizationID: row.OrganizationID,
		ProjectID:      row.ProjectID,
		Actor:          input.Actor,
		Exclusion:      AuditSnapshot(deps.Redactor, exclusion),
		DisplayName:    DisplayName(deps.Redactor, exclusion),
	}); err != nil {
		return Exclusion{}, mutationError("log risk exclusion create", err)
	}
	return exclusion, nil
}

func (c *Core) Toggle(ctx context.Context, input ToggleMutation) (Exclusion, error) {
	deps, err := c.requireMutationDependencies()
	if err != nil {
		return Exclusion{}, err
	}
	tx, err := deps.Transactor.Begin(ctx)
	if err != nil {
		return Exclusion{}, mutationError("begin transaction", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	exclusion, err := c.toggleInTransaction(ctx, tx, input, deps)
	if err != nil {
		return Exclusion{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Exclusion{}, mutationError("commit risk exclusion toggle", err)
	}
	c.AfterCommit(ctx, exclusion)
	return exclusion, nil
}

// ToggleInTransaction applies an enabled-only update and audit to a caller-owned
// transaction. validate runs after the row is locked and before it is changed.
func (c *Core) ToggleInTransaction(ctx context.Context, tx pgx.Tx, input ToggleMutation, validate func(Exclusion) error) (Exclusion, error) {
	deps, err := c.requireMutationDependencies()
	if err != nil {
		return Exclusion{}, err
	}
	if tx == nil {
		return Exclusion{}, mutationError("risk exclusion mutation transaction is not configured", nil)
	}
	return c.toggleInTransaction(ctx, tx, input, deps, validate)
}

func (c *Core) toggleInTransaction(ctx context.Context, tx pgx.Tx, input ToggleMutation, deps *MutationDependencies, validators ...func(Exclusion) error) (Exclusion, error) {
	queries := repo.New(tx)
	if err := queries.LockRiskExclusionMutations(ctx, input.ProjectID.String()); err != nil {
		return Exclusion{}, mutationError("lock risk exclusion mutations", err)
	}
	before, err := queries.GetRiskExclusionForUpdate(ctx, repo.GetRiskExclusionForUpdateParams{ID: input.ID, ProjectID: input.ProjectID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Exclusion{}, fmt.Errorf("%w: %w", ErrExclusionNotFound, err)
		}
		return Exclusion{}, mutationError("lock risk exclusion", err)
	}
	beforeExclusion := Project(before)
	for _, validate := range validators {
		if validate != nil {
			if err := validate(beforeExclusion); err != nil {
				return Exclusion{}, err
			}
		}
	}
	if err := enforceRegexLimit(ctx, queries, input.ProjectID, before.RiskPolicyID, uuid.NullUUID{UUID: input.ID, Valid: true}, before.MatchType, input.Enabled); err != nil {
		return Exclusion{}, err
	}
	row, err := queries.UpdateRiskExclusion(ctx, toggleUpdateParams(before, input.Enabled))
	if err != nil {
		return Exclusion{}, mutationError("toggle risk exclusion", err)
	}
	afterExclusion := Project(row)
	if err := deps.Auditor.LogExclusionUpdate(ctx, tx, UpdateAuditEvent{
		OrganizationID: row.OrganizationID,
		ProjectID:      row.ProjectID,
		Actor:          input.Actor,
		Before:         AuditSnapshot(deps.Redactor, beforeExclusion),
		After:          AuditSnapshot(deps.Redactor, afterExclusion),
		DisplayName:    DisplayName(deps.Redactor, afterExclusion),
	}); err != nil {
		return Exclusion{}, mutationError("log risk exclusion toggle", err)
	}
	return afterExclusion, nil
}

// AfterCommit starts best-effort reconciliation after the transaction containing
// the exclusion, audit, and any outer receipt has committed.
func (c *Core) AfterCommit(ctx context.Context, exclusion Exclusion) {
	deps, err := c.requireMutationDependencies()
	if err != nil || deps.AfterCommit == nil {
		return
	}
	deps.AfterCommit(ctx, exclusion.ProjectID, exclusion.ID)
}

func toggleUpdateParams(before repo.RiskExclusion, enabled bool) repo.UpdateRiskExclusionParams {
	return repo.UpdateRiskExclusionParams{
		ID:           before.ID,
		ProjectID:    before.ProjectID,
		RiskPolicyID: before.RiskPolicyID,
		MatchType:    before.MatchType,
		MatchValue:   before.MatchValue,
		RuleIDFilter: before.RuleIDFilter,
		SourceFilter: before.SourceFilter,
		Enabled:      enabled,
	}
}

func (c *Core) Update(ctx context.Context, input UpdateMutation) (Exclusion, error) {
	if err := ValidateMatchValue(input.MatchType, input.MatchValue); err != nil {
		return Exclusion{}, err
	}
	deps, err := c.requireMutationDependencies()
	if err != nil {
		return Exclusion{}, err
	}
	tx, err := deps.Transactor.Begin(ctx)
	if err != nil {
		return Exclusion{}, mutationError("begin transaction", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := repo.New(tx)
	if err := queries.LockRiskExclusionMutations(ctx, input.ProjectID.String()); err != nil {
		return Exclusion{}, mutationError("lock risk exclusion mutations", err)
	}
	if err := ensurePolicy(ctx, queries, input.ProjectID, input.RiskPolicyID); err != nil {
		return Exclusion{}, err
	}
	before, err := queries.GetRiskExclusionForUpdate(ctx, repo.GetRiskExclusionForUpdateParams{ID: input.ID, ProjectID: input.ProjectID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Exclusion{}, fmt.Errorf("%w: %w", ErrExclusionNotFound, err)
		}
		return Exclusion{}, mutationError("lock risk exclusion", err)
	}
	enabled := before.Enabled
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	if err := enforceRegexLimit(ctx, queries, input.ProjectID, input.RiskPolicyID, uuid.NullUUID{UUID: input.ID, Valid: true}, input.MatchType, enabled); err != nil {
		return Exclusion{}, err
	}
	row, err := queries.UpdateRiskExclusion(ctx, repo.UpdateRiskExclusionParams{
		ID:           input.ID,
		ProjectID:    input.ProjectID,
		RiskPolicyID: input.RiskPolicyID,
		MatchType:    input.MatchType,
		MatchValue:   input.MatchValue,
		RuleIDFilter: input.RuleIDFilter,
		SourceFilter: input.SourceFilter,
		Enabled:      enabled,
	})
	if err != nil {
		return Exclusion{}, mutationError("update risk exclusion", err)
	}
	beforeExclusion := Project(before)
	afterExclusion := Project(row)
	if err := deps.Auditor.LogExclusionUpdate(ctx, tx, UpdateAuditEvent{
		OrganizationID: row.OrganizationID,
		ProjectID:      row.ProjectID,
		Actor:          input.Actor,
		Before:         AuditSnapshot(deps.Redactor, beforeExclusion),
		After:          AuditSnapshot(deps.Redactor, afterExclusion),
		DisplayName:    DisplayName(deps.Redactor, afterExclusion),
	}); err != nil {
		return Exclusion{}, mutationError("log risk exclusion update", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Exclusion{}, mutationError("commit risk exclusion update", err)
	}
	if deps.AfterCommit != nil {
		deps.AfterCommit(ctx, row.ProjectID, row.ID)
	}
	return afterExclusion, nil
}

func (c *Core) Delete(ctx context.Context, input DeleteMutation) error {
	deps, err := c.requireMutationDependencies()
	if err != nil {
		return err
	}
	tx, err := deps.Transactor.Begin(ctx)
	if err != nil {
		return mutationError("begin transaction", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := repo.New(tx)
	if err := queries.LockRiskExclusionMutations(ctx, input.ProjectID.String()); err != nil {
		return mutationError("lock risk exclusion mutations", err)
	}
	before, err := queries.GetRiskExclusionForUpdate(ctx, repo.GetRiskExclusionForUpdateParams{ID: input.ID, ProjectID: input.ProjectID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %w", ErrExclusionNotFound, err)
		}
		return mutationError("lock risk exclusion", err)
	}
	if err := queries.DeleteRiskExclusion(ctx, repo.DeleteRiskExclusionParams{ID: input.ID, ProjectID: input.ProjectID}); err != nil {
		return mutationError("delete risk exclusion", err)
	}
	exclusion := Project(before)
	if err := deps.Auditor.LogExclusionDelete(ctx, tx, DeleteAuditEvent{
		OrganizationID: before.OrganizationID,
		ProjectID:      before.ProjectID,
		Actor:          input.Actor,
		Exclusion:      AuditSnapshot(deps.Redactor, exclusion),
		DisplayName:    DisplayName(deps.Redactor, exclusion),
	}); err != nil {
		return mutationError("log risk exclusion delete", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return mutationError("commit risk exclusion delete", err)
	}
	if deps.AfterCommit != nil {
		deps.AfterCommit(ctx, before.ProjectID, before.ID)
	}
	return nil
}

func (c *Core) requireMutationDependencies() (*MutationDependencies, error) {
	if c.mutations == nil || c.mutations.Transactor == nil || c.mutations.Auditor == nil || !c.mutations.Redactor.Configured() {
		return nil, mutationError("risk exclusion mutation dependencies are not configured", nil)
	}
	return c.mutations, nil
}

func ensurePolicy(ctx context.Context, queries *repo.Queries, projectID uuid.UUID, policyID uuid.NullUUID) error {
	if !policyID.Valid {
		return nil
	}
	if _, err := queries.GetRiskPolicyForUpdate(ctx, repo.GetRiskPolicyForUpdateParams{ID: policyID.UUID, ProjectID: projectID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %w", ErrPolicyNotFound, err)
		}
		return mutationError("load risk policy", err)
	}
	return nil
}

func enforceRegexLimit(ctx context.Context, queries *repo.Queries, projectID uuid.UUID, policyID, excludeID uuid.NullUUID, matchType string, enabled bool) error {
	if matchType != "regex" || !enabled {
		return nil
	}
	count, err := queries.CountEnabledRegexExclusionsInScope(ctx, repo.CountEnabledRegexExclusionsInScopeParams{
		ProjectID: projectID, RiskPolicyID: policyID, ExcludeID: excludeID,
	})
	if err != nil {
		return mutationError("count regex exclusions", err)
	}
	if count >= MaxRegexPerScope {
		return &RegexLimitError{}
	}
	return nil
}

func valueOrEmpty(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func mutationError(message string, cause error) error {
	return &MutationError{Message: message, Cause: cause}
}
