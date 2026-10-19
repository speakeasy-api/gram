package migration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/plugins"
)

// Status is the outcome of one server in a migration run.
type Status string

const (
	// StatusMigrated means the server was switched and its toolset disabled.
	StatusMigrated Status = "migrated"

	// StatusDeleted means the toolset and its server were deleted.
	StatusDeleted Status = "deleted"

	// StatusPreviewed means a dry run completed every step and rolled back.
	StatusPreviewed Status = "previewed"

	// StatusFailed means the server was left unchanged; Reason says why.
	StatusFailed Status = "failed"

	// StatusNotAttempted means an earlier server failed and the run stopped.
	StatusNotAttempted Status = "not_attempted"
)

// Request selects one server to migrate.
type Request struct {
	// ToolsetID is the candidate's toolset.
	ToolsetID uuid.UUID
}

// Actor is the staff member a migration is audited under.
type Actor struct {
	// UserID is the staff principal id recorded on audit rows.
	UserID string

	// Email is the staff member's display name on audit rows.
	Email *string
}

// Outcome reports what happened to one server.
type Outcome struct {
	// ToolsetID is the candidate's toolset.
	ToolsetID uuid.UUID

	// Action is what the run did, or would do, with the candidate; empty
	// when it was not attempted.
	Action Action

	// Status is the server's result.
	Status Status

	// Reason explains a failure.
	Reason string

	// McpSlug is the server's address, which does not change.
	McpSlug string

	// McpServerID is the server, which keeps its id.
	McpServerID uuid.UUID

	// RemoteMcpServerID is the remote source the server now proxies.
	RemoteMcpServerID uuid.UUID

	// RemoteURL is the upstream URL the server now dials.
	RemoteURL string

	// Actions describes each change, in order.
	Actions []string

	// Warnings describe what the migration could not carry over.
	Warnings []string
}

// ToolCallCounter counts each candidate's recent tool calls by toolset id.
type ToolCallCounter interface {
	CountToolCalls(ctx context.Context, candidates []Candidate) (map[uuid.UUID]uint64, error)
}

// Migrator lists and migrates external-MCP-backed servers.
type Migrator struct {
	logger         *slog.Logger
	tracerProvider trace.TracerProvider
	db             *pgxpool.Pool
	audit          *audit.Logger
	policy         *guardian.Policy
	encryption     *encryption.Client
	publication    plugins.PublicationRequests
	toolCalls      ToolCallCounter
}

// NewMigrator builds a migrator. enc decrypts environment values copied into
// headers and encrypts them again. toolCalls may be nil when telemetry is
// unavailable.
func NewMigrator(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, auditLogger *audit.Logger, policy *guardian.Policy, enc *encryption.Client, publication plugins.PublicationRequests, toolCalls ToolCallCounter) *Migrator {
	return &Migrator{
		logger:         logger.With(attr.SlogComponent("external_mcp_migration")),
		tracerProvider: tracerProvider,
		db:             db,
		audit:          auditLogger,
		policy:         policy,
		encryption:     enc,
		publication:    publication,
		toolCalls:      toolCalls,
	}
}

// errNotCandidate marks a server that stopped being a candidate, for example
// because it was already migrated.
var errNotCandidate = errors.New("server is no longer backed by an external MCP attachment")

// failure is a reason a server cannot be migrated that is safe to show staff.
type failure struct {
	reason string
}

func (f failure) Error() string { return f.reason }

func fail(format string, args ...any) error {
	return failure{reason: fmt.Sprintf(format, args...)}
}

// Run migrates each request in order, one transaction per server, and stops
// at the first failure. With dryRun every transaction rolls back, so the
// outcomes preview the run without changing anything.
func (m *Migrator) Run(ctx context.Context, actor Actor, requests []Request, dryRun bool) []Outcome {
	outcomes := make([]Outcome, 0, len(requests))
	stopped := false
	for _, request := range requests {
		if stopped {
			outcomes = append(outcomes, Outcome{ToolsetID: request.ToolsetID, Action: "", Status: StatusNotAttempted, Reason: "an earlier server failed", McpSlug: "", McpServerID: uuid.Nil, RemoteMcpServerID: uuid.Nil, RemoteURL: "", Actions: nil, Warnings: nil})
			continue
		}
		outcome := m.runOne(ctx, actor, request, dryRun)
		if outcome.Status == StatusFailed && !dryRun {
			stopped = true
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes
}

func (m *Migrator) runOne(ctx context.Context, actor Actor, request Request, dryRun bool) Outcome {
	logger := m.logger.With(attr.SlogToolsetID(request.ToolsetID.String()))
	outcome, err := m.migrate(ctx, actor, request, dryRun)
	if err == nil {
		return outcome
	}

	outcome.ToolsetID = request.ToolsetID
	outcome.Status = StatusFailed
	// The transaction rolled back, so nothing it created exists.
	outcome.RemoteMcpServerID = uuid.Nil
	outcome.RemoteURL = ""
	outcome.Actions = nil
	var reason failure
	switch {
	case errors.As(err, &reason):
		outcome.Reason = reason.reason
	case errors.Is(err, errNotCandidate):
		outcome.Reason = errNotCandidate.Error()
	default:
		outcome.Reason = "unexpected error; see server logs"
		logger.ErrorContext(ctx, "migrate external MCP server", attr.SlogError(err))
	}
	return outcome
}

func (m *Migrator) migrate(ctx context.Context, actor Actor, request Request, dryRun bool) (Outcome, error) {
	var outcome Outcome
	if actor.UserID == "" {
		return outcome, fail("missing staff actor")
	}

	tx, err := m.db.Begin(ctx)
	if err != nil {
		return outcome, fmt.Errorf("begin migration: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	outcome, err = m.migrateInTx(ctx, tx, actor, request)
	if err != nil {
		return outcome, err
	}
	if dryRun {
		outcome.Status = StatusPreviewed
		return outcome, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return outcome, fmt.Errorf("commit migration: %w", err)
	}
	outcome.Status = StatusMigrated
	if outcome.Action == ActionDelete {
		outcome.Status = StatusDeleted
	}
	return outcome, nil
}

// lockScope takes the organization and project locks every hosted MCP write
// takes, in the same order, before the toolset itself.
func lockScope(ctx context.Context, tx pgx.Tx, organizationID string, projectID uuid.UUID) error {
	if err := lockNetworkIngress(ctx, tx, organizationID); err != nil {
		return err
	}
	return lockAdmission(ctx, tx, projectID)
}
