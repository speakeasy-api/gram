package platformmcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/projects"
)

const (
	operationCreateProject = "create_project"
	operationRenameProject = "rename_project"

	projectLifecycleFeature = "project_lifecycle"

	// maxProjectReceiptBytes bounds a stored create or rename result. The
	// payload is an id, two names of at most 40 characters and a slug, so
	// anything near this size is corrupt rather than large.
	maxProjectReceiptBytes = 4 << 10 // 4 KiB

	// maxIdempotencyKeyLength matches the bound every other Platform MCP
	// mutation puts on its idempotency key.
	maxIdempotencyKeyLength = 128

	projectOutcomeCreated   = "created"
	projectOutcomeRenamed   = "renamed"
	projectOutcomeUnchanged = "unchanged"
)

var (
	ErrProjectLifecycleInvalid  = errors.New("invalid project lifecycle request")
	ErrProjectLifecycleConflict = errors.New("project lifecycle conflict")
	ErrProjectLifecycleMissing  = errors.New("project lifecycle target not found")
)

// ProjectLifecycleError is safe to map into a tool refusal: Message is written
// for the administrator and never carries database detail.
type ProjectLifecycleError struct {
	Code    string
	Message string
	Cause   error
}

func (e *ProjectLifecycleError) Error() string { return e.Message }
func (e *ProjectLifecycleError) Unwrap() error { return e.Cause }

// ProjectCreatePreviewError is the confirmation_required refusal of an unconfirmed
// create. It carries the exact name and slug the confirmed call would create,
// so the user confirms the real address rather than an agent's guess at the
// derivation. Nothing is charged, written, or recorded to produce it.
type ProjectCreatePreviewError struct {
	// Name is the display name exactly as it would be stored.
	Name string

	// Slug is the slug the project would get, from the same derivation the
	// confirmed call uses.
	Slug string
}

func (e *ProjectCreatePreviewError) Error() string {
	return fmt.Sprintf("Nothing was created. Show the user the name %q and the slug %q it would get; the slug addresses the project in dashboard links and never changes. Call again with confirmed: true only after the user confirms both.", e.Name, e.Slug)
}

func (e *ProjectCreatePreviewError) Unwrap() error { return ErrProjectLifecycleInvalid }

type CreateProjectInput struct {
	Name           string `json:"name" jsonschema:"display name for the new project, 1 to 40 characters with at least one letter or digit; its slug is derived from it"`
	IdempotencyKey string `json:"idempotency_key" jsonschema:"caller-chosen key, at most 128 characters, chosen once per create and passed on both the preview and the confirmed call; a retry of the confirmed call with it returns the same project instead of making a second one"`
	Confirmed      bool   `json:"confirmed" jsonschema:"false to preview the exact slug without creating anything; true only after the user confirmed this exact name and the slug the preview returned"`
}

type RenameProjectInput struct {
	ProjectID      string `json:"project_id" jsonschema:"ID of the project to rename, from list_projects"`
	Name           string `json:"name" jsonschema:"new display name, 1 to 40 characters after trimming"`
	IdempotencyKey string `json:"idempotency_key" jsonschema:"caller-chosen key, at most 128 characters, that makes a retry of this exact rename safe"`
	Confirmed      bool   `json:"confirmed" jsonschema:"true only after the user confirmed this exact project and new name"`
}

// ProjectMutationOutput reports one committed create or rename. A refusal is
// returned as an error result and never as this struct.
type ProjectMutationOutput struct {
	// Outcome is "created", "renamed", or "unchanged" (a rename to the name the
	// project already had).
	Outcome string `json:"outcome"`

	// Project is a fresh read taken after the commit when SnapshotScope is
	// "fresh_read_after_commit", and the stored result otherwise.
	Project Project `json:"project"`

	// PreviousName is the name a rename replaced. Empty on a create.
	PreviousName string `json:"previous_name,omitempty"`

	// SnapshotScope is "fresh_read_after_commit" when Project was re-read
	// after the write, or "verification_unavailable" when that read failed —
	// for instance because the project was deleted after a replayed create.
	SnapshotScope string `json:"snapshot_scope"`

	Receipt RiskMutationToolReceipt `json:"receipt"`
}

// projectReceipt is the stored replay result. It is decoded strictly, so an
// unknown field marks the receipt as corrupt rather than being ignored.
type projectReceipt struct {
	Outcome      string `json:"outcome"`
	ProjectID    string `json:"project_id"`
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	PreviousName string `json:"previous_name,omitempty"`
}

// ProjectLifecycleService creates and renames projects for the Platform MCP
// through the same core the projects management API uses, so an agent can
// make exactly the projects the dashboard can and no others.
type ProjectLifecycleService struct {
	db      *pgxpool.Pool
	queries *platformrepo.Queries
	logger  *slog.Logger
	core    *projects.Core
	// engine checks the resource-scoped project:write grant a rename needs,
	// exactly as the dashboard's own project update does; a rename needs
	// nothing more.
	engine *authz.Engine
	// admin re-checks org:admin live for a create, as the dashboard's project
	// creation requires, so a denial keeps the challenge and audit behavior
	// every other admin-gated Platform MCP path records.
	admin Authorizer
	// changes meters create and rename together, so alternating between
	// the two cannot multiply the write rate.
	changes OperationBudget
	now     func() time.Time
}

func NewProjectLifecycleService(logger *slog.Logger, db *pgxpool.Pool, core *projects.Core, engine *authz.Engine, admin Authorizer, changes OperationBudget) (*ProjectLifecycleService, error) {
	if logger == nil || db == nil || core == nil || engine == nil || admin == nil || !changes.valid() {
		return nil, ErrProjectLifecycleInvalid
	}
	return &ProjectLifecycleService{
		db: db, queries: platformrepo.New(db), logger: logger, core: core,
		engine: engine, admin: admin, changes: changes, now: time.Now,
	}, nil
}

func (s *ProjectLifecycleService) valid() bool {
	return s != nil && s.db != nil && s.queries != nil && s.logger != nil && s.core != nil && s.engine != nil && s.admin != nil && s.changes.valid() && s.now != nil
}

// CreateProject creates an empty project in the caller's organization. The
// receipt that makes a retry replay is written in the same transaction as the
// project, against the project it created, because there is no project to key
// it on beforehand.
func (s *ProjectLifecycleService) CreateProject(ctx context.Context, principal Principal, input CreateProjectInput) (ProjectMutationOutput, error) {
	if !s.valid() {
		return ProjectMutationOutput{}, projectLifecycleUnavailable(errors.New("project lifecycle service is not composed"))
	}
	slug, err := projects.ProjectSlug(input.Name)
	if err != nil {
		return ProjectMutationOutput{}, projectNameRefusal(err)
	}
	if err := s.requireAdmin(ctx, principal); err != nil {
		return ProjectMutationOutput{}, err
	}
	// Validated before the preview, so the preview and the confirmed call,
	// which take the same key, refuse the same input.
	key, err := validIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return ProjectMutationOutput{}, err
	}
	if !input.Confirmed {
		// The preview: the slug comes from the same derivation the confirmed
		// call uses, and nothing is charged, written, or recorded.
		return ProjectMutationOutput{}, &ProjectCreatePreviewError{Name: input.Name, Slug: slug}
	}
	inputHash, err := projectInputHash(operationCreateProject, struct {
		Name string `json:"name"`
	}{Name: input.Name})
	if err != nil {
		return ProjectMutationOutput{}, err
	}

	// A completed creation under this key replays before anything is charged
	// or locked, and the budget is charged outside any transaction: the
	// limiter is a network call, and holding a PostgreSQL connection and the
	// receipt lock across it would let a slow limiter pin connections. The
	// locked re-check inside the receipt transaction still replays a request
	// that completed concurrently; both racers are then charged, which errs
	// on the conservative side.
	if replay, ok := s.completedCreateReceipt(ctx, principal, key, inputHash); ok {
		return s.finish(ctx, principal, replay.result, replay.operation), nil
	}
	if err := s.changes.AllowConnectionOrOrganization(ctx, principal); err != nil {
		return ProjectMutationOutput{}, projectBudgetError(err)
	}

	stored, receipt, created, err := s.createInReceiptTransaction(ctx, principal, input.Name, slug, key, inputHash)
	if err != nil {
		return ProjectMutationOutput{}, err
	}
	if created {
		// After the commit, exactly as the dashboard does: the follow-up is
		// best-effort and must never be able to undo a created project.
		s.core.AfterCreateCommitted(ctx, receipt.projectID, projects.ProjectActor{UserID: principal.UserID, DisplayName: nil})
	}
	return s.finish(ctx, principal, stored, receipt.operation), nil
}

type createdReceipt struct {
	projectID uuid.UUID
	operation OperationReceipt
}

// storedReplay is a completed receipt found by the pre-check, ready to return
// without opening a transaction.
type storedReplay struct {
	result    projectReceipt
	operation OperationReceipt
}

// noReplay is the empty result the pre-checks return beside false.
var noReplay storedReplay

// completedCreateReceipt is the read-only pre-check for a create replay. It
// reports only a completed, unexpired receipt whose input matches; anything
// else — a miss, a different input, a read failure — falls through to the
// receipt transaction, which decides it authoritatively under the lock.
func (s *ProjectLifecycleService) completedCreateReceipt(ctx context.Context, principal Principal, key, inputHash string) (storedReplay, bool) {
	row, err := s.queries.GetPlatformMCPProjectCreationReceipt(ctx, platformrepo.GetPlatformMCPProjectCreationReceiptParams{
		OrganizationID: principal.OrganizationID, UserID: conv.ToPGText(principal.UserID),
		Operation: operationCreateProject, IdempotencyKey: key,
	})
	if err != nil {
		return noReplay, false
	}
	return replayableReceipt(row, inputHash)
}

// completedRenameReceipt is the same pre-check for a rename, keyed on the
// exact project the rename targets.
func (s *ProjectLifecycleService) completedRenameReceipt(ctx context.Context, principal Principal, projectID uuid.UUID, key, inputHash string) (storedReplay, bool) {
	row, err := s.queries.GetPlatformMCPOperationReceipt(ctx, platformrepo.GetPlatformMCPOperationReceiptParams{
		OrganizationID: principal.OrganizationID, ProjectID: projectID, Operation: operationRenameProject, IdempotencyKey: key,
		UserID: conv.ToPGText(principal.UserID), SubjectUrn: userSubjectURN(principal.UserID),
	})
	if err != nil {
		return noReplay, false
	}
	return replayableReceipt(row, inputHash)
}

// replayableReceipt judges expiry against the wall clock rather than the
// service's injectable clock, because the authoritative path decides it with
// the database's clock_timestamp(); an injected clock would let the pre-check
// replay a receipt the locked path has already treated as expired.
func replayableReceipt(row platformrepo.PlatformMcpOperationReceipt, inputHash string) (storedReplay, bool) {
	if row.Status != receiptStatusSucceeded || row.InputHash != inputHash || !row.ExpiresAt.Time.After(time.Now()) {
		return noReplay, false
	}
	result, err := decodeProjectReceipt(row.ResultPayload)
	if err != nil {
		return noReplay, false
	}
	return storedReplay{result: result, operation: operationReceiptFromRow(row, true)}, true
}

func (s *ProjectLifecycleService) createInReceiptTransaction(ctx context.Context, principal Principal, name, slug, key, inputHash string) (projectReceipt, createdReceipt, bool, error) {
	connectionID, generation, err := principalConnection(principal)
	if err != nil {
		return projectReceipt{}, createdReceipt{}, false, projectLifecycleInvalid("The project request is missing its caller identity.")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return projectReceipt{}, createdReceipt{}, false, s.unexpected(ctx, fmt.Errorf("begin project creation: %w", err))
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	q := s.queries.WithTx(tx)
	// The empty project id is deliberate: the lock has to cover every project
	// this key could create, so two concurrent retries serialize here and the
	// second finds the first one's receipt.
	if err := q.LockPlatformMCPOperationReceipt(ctx, platformrepo.LockPlatformMCPOperationReceiptParams{
		OrganizationID: principal.OrganizationID, SubjectUrn: userSubjectURN(principal.UserID), ProjectID: "",
		Operation: operationCreateProject, IdempotencyKey: key,
	}); err != nil {
		return projectReceipt{}, createdReceipt{}, false, s.unexpected(ctx, fmt.Errorf("lock project creation receipt: %w", err))
	}
	existing, err := q.GetPlatformMCPProjectCreationReceipt(ctx, platformrepo.GetPlatformMCPProjectCreationReceiptParams{
		OrganizationID: principal.OrganizationID, UserID: conv.ToPGText(principal.UserID),
		Operation: operationCreateProject, IdempotencyKey: key,
	})
	switch {
	case err == nil:
		if existing.InputHash != inputHash {
			return projectReceipt{}, createdReceipt{}, false, projectLifecycleConflict("That idempotency key was already used to create a project with a different name. Use a fresh key for a different project.")
		}
		stored, decodeErr := decodeProjectReceipt(existing.ResultPayload)
		if existing.Status != receiptStatusSucceeded || decodeErr != nil {
			return projectReceipt{}, createdReceipt{}, false, s.unexpected(ctx, errors.Join(errors.New("stored project creation receipt is invalid"), decodeErr))
		}
		if err := tx.Commit(ctx); err != nil {
			return projectReceipt{}, createdReceipt{}, false, s.unexpected(ctx, fmt.Errorf("commit project creation replay: %w", err))
		}
		return stored, createdReceipt{projectID: existing.ProjectID, operation: operationReceiptFromRow(existing, true)}, false, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return projectReceipt{}, createdReceipt{}, false, s.unexpected(ctx, fmt.Errorf("load project creation receipt: %w", err))
	}

	project, err := s.core.CreateInTransaction(ctx, tx, projects.CreateProjectMutation{
		OrganizationID: principal.OrganizationID,
		Name:           name,
		Actor:          projects.ProjectActor{UserID: principal.UserID, DisplayName: nil},
	})
	switch {
	case errors.Is(err, projects.ErrProjectSlugTaken):
		return projectReceipt{}, createdReceipt{}, false, &ProjectLifecycleError{
			Code:    "conflict",
			Message: fmt.Sprintf("A project with the address %q already exists in this organization, so nothing was created. Choose a name that differs by more than punctuation or capitalization, or use the existing project.", slug),
			Cause:   errors.Join(ErrProjectLifecycleConflict, err),
		}
	case errors.Is(err, projects.ErrProjectNameInvalid), errors.Is(err, projects.ErrProjectSlugEmpty):
		return projectReceipt{}, createdReceipt{}, false, projectNameRefusal(err)
	case err != nil:
		return projectReceipt{}, createdReceipt{}, false, s.unexpected(ctx, fmt.Errorf("create project: %w", err))
	}

	stored := projectReceipt{Outcome: projectOutcomeCreated, ProjectID: project.ID.String(), Name: project.Name, Slug: project.Slug, PreviousName: ""}
	payload, err := encodeProjectReceipt(stored)
	if err != nil {
		return projectReceipt{}, createdReceipt{}, false, s.unexpected(ctx, err)
	}
	row, err := q.CreatePlatformMCPOperationReceipt(ctx, platformrepo.CreatePlatformMCPOperationReceiptParams{
		OrganizationID: principal.OrganizationID, ProjectID: project.ID, RegistrationID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ConnectionID: connectionID, ConnectionGeneration: generation,
		UserID: conv.ToPGText(principal.UserID), ActingSurface: conv.ToPGText(string(principal.surface())),
		Operation: operationCreateProject, IdempotencyKey: key, InputHash: inputHash,
		Status: receiptStatusSucceeded, ResultCode: conv.ToPGText("succeeded"), ResultPayload: payload,
		ExpiresAt: timestamp(s.now().UTC().Add(receiptLifetime)),
	})
	if err != nil {
		return projectReceipt{}, createdReceipt{}, false, s.unexpected(ctx, fmt.Errorf("record project creation receipt: %w", err))
	}
	if err := tx.Commit(ctx); err != nil {
		return projectReceipt{}, createdReceipt{}, false, s.unexpected(ctx, fmt.Errorf("commit project creation: %w", err))
	}
	return stored, createdReceipt{projectID: project.ID, operation: operationReceiptFromRow(row, false)}, true, nil
}

// RenameProject changes one project's display name. Its slug, and so every
// dashboard link and API header that addresses the project, stays the same.
func (s *ProjectLifecycleService) RenameProject(ctx context.Context, principal Principal, input RenameProjectInput) (ProjectMutationOutput, error) {
	if !s.valid() {
		return ProjectMutationOutput{}, projectLifecycleUnavailable(errors.New("project lifecycle service is not composed"))
	}
	if !input.Confirmed {
		return ProjectMutationOutput{}, &ProjectLifecycleError{
			Code:    "confirmation_required",
			Message: "Nothing was renamed. Confirm the exact project and its new name with the user, then call again with confirmed: true.",
			Cause:   ErrProjectLifecycleInvalid,
		}
	}
	key, err := validIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return ProjectMutationOutput{}, err
	}
	projectID, err := uuid.Parse(strings.TrimSpace(input.ProjectID))
	if err != nil {
		return ProjectMutationOutput{}, projectLifecycleInvalid("Provide the exact project ID from list_projects.")
	}
	name, err := projects.ProjectRenameName(input.Name)
	if err != nil {
		return ProjectMutationOutput{}, projectNameRefusal(err)
	}
	if principal.OrganizationID == "" || principal.UserID == "" {
		return ProjectMutationOutput{}, projectLifecycleInvalid("The project request is missing its caller identity.")
	}
	// Authorized exactly as the dashboard's project update is: project:write
	// on this project, with no organization-admin requirement. Checked
	// against the id the caller named before anything confirms it exists, so
	// a real and an invented project get the same answer and this tool cannot
	// be used to probe for project ids.
	if err := s.engine.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: projectID.String(), Dimensions: nil}); err != nil {
		mapped := toolExposureAuthorizationError(err, authz.ScopeProjectWrite)
		if _, ok := errors.AsType[*ExternalAuthorizationError](mapped); !ok {
			return ProjectMutationOutput{}, s.unexpected(ctx, err)
		}
		return ProjectMutationOutput{}, mapped
	}
	resolved, err := s.queries.ResolvePlatformMCPProjectByID(ctx, platformrepo.ResolvePlatformMCPProjectByIDParams{OrganizationID: principal.OrganizationID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ProjectMutationOutput{}, projectLifecycleMissing()
	}
	if err != nil {
		return ProjectMutationOutput{}, s.unexpected(ctx, fmt.Errorf("resolve project to rename: %w", err))
	}
	project := ResolvedProject{ID: resolved.ID, Name: resolved.Name, Slug: resolved.Slug}
	inputHash, err := projectInputHash(operationRenameProject, struct {
		ProjectID string `json:"project_id"`
		Name      string `json:"name"`
	}{ProjectID: projectID.String(), Name: name})
	if err != nil {
		return ProjectMutationOutput{}, err
	}

	// As for a create: a completed rename replays before anything is charged
	// or locked, and the budget is charged outside any transaction.
	if replay, ok := s.completedRenameReceipt(ctx, principal, project.ID, key, inputHash); ok {
		return s.finish(ctx, principal, replay.result, replay.operation), nil
	}
	if err := s.changes.AllowConnectionOrOrganization(ctx, principal); err != nil {
		return ProjectMutationOutput{}, projectBudgetError(err)
	}

	receipt, err := executeMutationReceipt(ctx, mutationReceiptExecution[projectReceipt]{
		DB: s.db, Now: s.now, Principal: principal, Project: project, Operation: operationRenameProject,
		IdempotencyKey: key, InputHash: inputHash, Label: "project rename",
		Invalid: func(error) error { return projectLifecycleInvalid("The project rename request is invalid.") },
		Conflict: func(message string) error {
			return &ProjectLifecycleError{Code: "conflict", Message: message, Cause: ErrProjectLifecycleConflict}
		},
		Unavailable: projectLifecycleUnavailable,
		ValidateReplay: func(stored []byte) bool {
			_, err := decodeProjectReceipt(stored)
			return err == nil
		},
		EncodeResult: encodeProjectReceipt,
		Mutate: func(ctx context.Context, tx pgx.Tx) (projectReceipt, error) {
			renamed, err := s.core.RenameInTransaction(ctx, tx, projects.RenameProjectMutation{
				OrganizationID: principal.OrganizationID,
				ProjectID:      project.ID,
				Name:           name,
				Actor:          projects.ProjectActor{UserID: principal.UserID, DisplayName: nil},
			})
			switch {
			case errors.Is(err, projects.ErrProjectNotFound):
				return projectReceipt{}, projectLifecycleMissing()
			case errors.Is(err, projects.ErrProjectNameInvalid):
				return projectReceipt{}, projectNameRefusal(err)
			case err != nil:
				return projectReceipt{}, s.unexpected(ctx, fmt.Errorf("rename project: %w", err))
			}
			outcome := projectOutcomeRenamed
			if renamed.Before.Name == renamed.After.Name {
				outcome = projectOutcomeUnchanged
			}
			return projectReceipt{
				Outcome: outcome, ProjectID: renamed.After.ID.String(), Name: renamed.After.Name,
				Slug: renamed.After.Slug, PreviousName: renamed.Before.Name,
			}, nil
		},
	})
	if err != nil {
		// The executor's own failures (begin, lock, commit) are wrapped
		// database errors; only this package's refusals are safe to return.
		return ProjectMutationOutput{}, s.unexpected(ctx, err)
	}
	stored, err := decodeProjectReceipt(receipt.ResultPayload)
	if err != nil {
		return ProjectMutationOutput{}, s.unexpected(ctx, err)
	}
	return s.finish(ctx, principal, stored, receipt), nil
}

// finish reports the committed project from a fresh read rather than from the
// request, so the caller states what the platform now holds.
func (s *ProjectLifecycleService) finish(ctx context.Context, principal Principal, stored projectReceipt, receipt OperationReceipt) ProjectMutationOutput {
	output := ProjectMutationOutput{
		Outcome:       stored.Outcome,
		Project:       Project{ID: stored.ProjectID, Name: stored.Name, Slug: stored.Slug},
		PreviousName:  stored.PreviousName,
		SnapshotScope: "verification_unavailable",
		Receipt:       riskMutationToolReceipt(receipt),
	}
	projectID, err := uuid.Parse(stored.ProjectID)
	if err != nil {
		return output
	}
	live, err := s.queries.ResolvePlatformMCPProjectByID(ctx, platformrepo.ResolvePlatformMCPProjectByIDParams{OrganizationID: principal.OrganizationID, ProjectID: projectID})
	if err != nil {
		return output
	}
	output.Project = Project{ID: live.ID.String(), Name: live.Name, Slug: live.Slug}
	output.SnapshotScope = "fresh_read_after_commit"
	return output
}

func (s *ProjectLifecycleService) requireAdmin(ctx context.Context, principal Principal) error {
	if principal.OrganizationID == "" || principal.UserID == "" {
		return projectLifecycleInvalid("The project request is missing its caller identity.")
	}
	if err := s.admin.RequireLiveOrgAdmin(ctx, principal); err != nil {
		mapped := toolExposureAuthorizationError(err, authz.ScopeOrgAdmin)
		if _, ok := errors.AsType[*ExternalAuthorizationError](mapped); ok {
			return mapped
		}
		// Anything but a denial carries database detail and must not reach
		// an external caller as the tool's error text.
		return s.unexpected(ctx, err)
	}
	return nil
}

// unexpected logs the cause server-side and returns the generic refusal, so a
// database or composition failure never reaches the caller verbatim.
func (s *ProjectLifecycleService) unexpected(ctx context.Context, err error) error {
	if refusal, ok := errors.AsType[*ProjectLifecycleError](err); ok {
		return refusal
	}
	s.logger.ErrorContext(ctx, "platform mcp project lifecycle", attr.SlogError(err))
	return projectLifecycleUnavailable(err)
}

func validIdempotencyKey(value string) (string, error) {
	key := strings.TrimSpace(value)
	if key == "" || len(key) > maxIdempotencyKeyLength {
		return "", projectLifecycleInvalid("Provide a stable idempotency key of at most 128 characters so retrying this exact change is safe.")
	}
	return key, nil
}

func projectInputHash(operation string, normalized any) (string, error) {
	payload, err := json.Marshal(normalized)
	if err != nil {
		return "", projectLifecycleInvalid("The project request could not be normalized.")
	}
	digest := sha256.Sum256(append([]byte("platform-mcp-project-lifecycle-v1\x00"+operation+"\x00"), payload...))
	return hex.EncodeToString(digest[:]), nil
}

func encodeProjectReceipt(result projectReceipt) ([]byte, error) {
	if !validProjectReceipt(result) {
		return nil, projectLifecycleUnavailable(errors.New("unsafe project receipt result"))
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("encode project receipt: %w", err)
	}
	if len(payload) > maxProjectReceiptBytes {
		return nil, projectLifecycleUnavailable(errors.New("project receipt result is too large"))
	}
	return payload, nil
}

func decodeProjectReceipt(payload []byte) (projectReceipt, error) {
	if len(payload) == 0 || len(payload) > maxProjectReceiptBytes {
		return projectReceipt{}, errors.New("project receipt payload has an invalid size")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var result projectReceipt
	if err := decoder.Decode(&result); err != nil {
		return projectReceipt{}, fmt.Errorf("decode project receipt: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF || !validProjectReceipt(result) {
		return projectReceipt{}, errors.New("project receipt payload is invalid")
	}
	return result, nil
}

func validProjectReceipt(result projectReceipt) bool {
	switch result.Outcome {
	case projectOutcomeCreated, projectOutcomeRenamed, projectOutcomeUnchanged:
	default:
		return false
	}
	return uuid.Validate(result.ProjectID) == nil && strings.TrimSpace(result.Name) != "" && result.Slug != ""
}

func projectNameRefusal(err error) error {
	if errors.Is(err, projects.ErrProjectSlugEmpty) {
		return &ProjectLifecycleError{
			Code:    "invalid_request",
			Message: "That name has no letters or digits, so no project address can be made from it. Choose a name with at least one letter or number.",
			Cause:   errors.Join(ErrProjectLifecycleInvalid, err),
		}
	}
	return &ProjectLifecycleError{
		Code:    "invalid_request",
		Message: "A project name must be 1 to 40 characters and cannot be blank.",
		Cause:   errors.Join(ErrProjectLifecycleInvalid, err),
	}
}

func projectLifecycleInvalid(message string) error {
	return &ProjectLifecycleError{Code: "invalid_request", Message: message, Cause: ErrProjectLifecycleInvalid}
}

func projectLifecycleConflict(message string) error {
	return &ProjectLifecycleError{Code: "conflict", Message: message, Cause: ErrProjectLifecycleConflict}
}

func projectLifecycleMissing() error {
	return &ProjectLifecycleError{
		Code:    "not_found",
		Message: "That project does not exist in this organization. List the projects again and use an ID from that list.",
		Cause:   ErrProjectLifecycleMissing,
	}
}

func projectLifecycleUnavailable(cause error) error {
	return &ProjectLifecycleError{
		Code:    unavailableCode,
		Message: "Creating or renaming projects is temporarily unavailable.",
		Cause:   errors.Join(ErrUnavailable, cause),
	}
}

// projectBudgetError keeps a throttle readable and distinct from the generic
// unavailable refusal, so a caller waits rather than treating a spent
// allowance as a broken feature.
func projectBudgetError(err error) error {
	if errors.Is(err, ErrOperationRateLimited) {
		return &ProjectLifecycleError{Code: "rate_limited", Message: "Projects were created or renamed too often just now. Try again shortly.", Cause: err}
	}
	return projectLifecycleUnavailable(err)
}
