// Package clientscopes clears the scope copied onto remote session clients
// created before #7199, so their logins discover what to request.
package clientscopes

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const defaultPageSize = 200

// Actor is the system principal the audit entries name.
const Actor = "client-scopes-backfill"

type Outcome string

const (
	OutcomeCleared       Outcome = "cleared"
	OutcomeWouldClear    Outcome = "would_clear"
	OutcomeSkipManaged   Outcome = "skip_managed"
	OutcomeSkipKind      Outcome = "skip_kind"
	OutcomeSkipEdited    Outcome = "skip_edited"
	OutcomeSkipUnmatched Outcome = "skip_unmatched"
	// OutcomeSkipChanged: the row moved between the scan and the locked recheck.
	OutcomeSkipChanged Outcome = "skip_changed"
)

type Options struct {
	CreatedBefore time.Time
	PageSize      int
	Apply         bool
}

// RowReport is one candidate; it goes to the report file, never stdout.
type RowReport struct {
	ClientID       uuid.UUID `json:"remote_session_client_id"`
	ProjectID      string    `json:"project_id"`
	OrganizationID string    `json:"organization_id"`
	Outcome        Outcome   `json:"outcome"`
	Match          string    `json:"match,omitempty"`
	Scope          []string  `json:"scope"`
}

type Report struct {
	Mode     string          `json:"mode"`
	Scanned  int             `json:"scanned"`
	Outcomes map[Outcome]int `json:"outcomes"`
	Rows     []RowReport     `json:"rows"`
}

type Runner struct {
	pool    *pgxpool.Pool
	audit   *audit.Logger
	options Options
}

func NewRunner(pool *pgxpool.Pool, options Options) *Runner {
	if options.PageSize <= 0 {
		options.PageSize = defaultPageSize
	}
	return &Runner{pool: pool, audit: audit.NewLogger(audit.WithoutOutbox()), options: options}
}

// Classify decides a candidate from its evidence alone.
func Classify(row ListClientScopeCandidatesRow) (Outcome, string) {
	switch {
	case row.Managed:
		return OutcomeSkipManaged, ""
	case row.ExcludedKind || row.OrganizationID == "":
		return OutcomeSkipKind, ""
	case row.ScopeEdited:
		return OutcomeSkipEdited, ""
	case row.MatchesResource:
		return OutcomeWouldClear, "resource"
	case row.MatchesIssuer:
		return OutcomeWouldClear, "issuer"
	default:
		return OutcomeSkipUnmatched, ""
	}
}

// Run scans every candidate; on error the report holds the rows processed so far.
func (r *Runner) Run(ctx context.Context) (Report, error) {
	report := Report{Mode: conv.Ternary(r.options.Apply, "apply", "dry-run"), Scanned: 0, Outcomes: map[Outcome]int{}, Rows: []RowReport{}}
	cursor := uuid.Nil
	for {
		rows, err := New(r.pool).ListClientScopeCandidates(ctx, r.listParams(cursor, uuid.NullUUID{UUID: uuid.Nil, Valid: false}))
		if err != nil {
			return report, fmt.Errorf("list client scope candidates: %w", err)
		}
		for _, row := range rows {
			outcome, match := Classify(row)
			if outcome == OutcomeWouldClear && r.options.Apply {
				outcome, match, err = r.clear(ctx, row.ID)
				if err != nil {
					return report, fmt.Errorf("clear client %s scope: %w", row.ID, err)
				}
			}
			report.Scanned++
			report.Outcomes[outcome]++
			report.Rows = append(report.Rows, RowReport{
				ClientID:       row.ID,
				ProjectID:      conv.Ternary(row.ProjectID.Valid, row.ProjectID.UUID.String(), ""),
				OrganizationID: row.OrganizationID,
				Outcome:        outcome,
				Match:          match,
				Scope:          row.Scope,
			})
			cursor = row.ID
		}
		if len(rows) < r.options.PageSize {
			return report, nil
		}
	}
}

func (r *Runner) listParams(after uuid.UUID, clientID uuid.NullUUID) ListClientScopeCandidatesParams {
	return ListClientScopeCandidatesParams{
		CreatedBefore: pgtype.Timestamptz{Time: r.options.CreatedBefore, InfinityModifier: pgtype.Finite, Valid: true},
		AfterID:       after,
		ClientID:      clientID,
		PageSize:      int32(r.options.PageSize), //nolint:gosec // bounded by flag parsing
	}
}

// clear rechecks the evidence under the row lock, then clears and audits in one transaction.
func (r *Runner) clear(ctx context.Context, clientID uuid.UUID) (Outcome, string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", "", fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := New(tx)

	before, err := q.LockClientForScopeClear(ctx, clientID)
	if errors.Is(err, pgx.ErrNoRows) {
		return OutcomeSkipChanged, "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("lock client: %w", err)
	}
	rechecked, err := q.ListClientScopeCandidates(ctx, r.listParams(uuid.Nil, uuid.NullUUID{UUID: clientID, Valid: true}))
	if err != nil {
		return "", "", fmt.Errorf("recheck client: %w", err)
	}
	if len(rechecked) != 1 {
		return OutcomeSkipChanged, "", nil
	}
	outcome, match := Classify(rechecked[0])
	if outcome != OutcomeWouldClear {
		return outcome, "", nil
	}

	after, err := q.ClearClientScope(ctx, ClearClientScopeParams{ID: clientID, ExpectedUpdatedAt: before.UpdatedAt})
	if errors.Is(err, pgx.ErrNoRows) {
		return OutcomeSkipChanged, "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("clear scope: %w", err)
	}
	issuerIDs, err := q.ListUserSessionIssuerIDsForClient(ctx, clientID)
	if err != nil {
		return "", "", fmt.Errorf("list user session issuers: %w", err)
	}
	beforeView, err := mv.BuildRemoteSessionClientView(remotesessionsrepo.RemoteSessionClient(before), issuerIDs)
	if err != nil {
		return "", "", fmt.Errorf("build client view: %w", err)
	}
	afterView, err := mv.BuildRemoteSessionClientView(remotesessionsrepo.RemoteSessionClient(after), issuerIDs)
	if err != nil {
		return "", "", fmt.Errorf("build client view: %w", err)
	}
	if err := r.audit.LogRemoteSessionClientUpdate(ctx, tx, audit.LogRemoteSessionClientUpdateEvent{
		OrganizationID:         rechecked[0].OrganizationID,
		ProjectID:              conv.Ternary(after.ProjectID.Valid, after.ProjectID.UUID, uuid.Nil),
		Actor:                  urn.NewSystemPrincipal(Actor),
		ActorDisplayName:       nil,
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(after.ID),
		ClientID:               after.ClientID,
		SnapshotBefore:         beforeView,
		SnapshotAfter:          afterView,
	}); err != nil {
		return "", "", fmt.Errorf("audit scope clear: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", "", fmt.Errorf("commit: %w", err)
	}
	return OutcomeCleared, match, nil
}
