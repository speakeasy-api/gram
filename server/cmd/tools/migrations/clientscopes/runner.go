// Package clientscopes clears the scope copied onto remote session clients
// created before #7199, so their logins discover what to request.
package clientscopes

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	defaultPageSize = 200
	lockTimeout     = "5s"
)

// ActorComponent is the system principal the audit entries name.
const ActorComponent = "client-scopes-backfill"

type Outcome string

const (
	OutcomeCleared      Outcome = "cleared"
	OutcomeWouldClear   Outcome = "would_clear"
	OutcomeRestored     Outcome = "restored"
	OutcomeWouldRestore Outcome = "would_restore"

	OutcomeSkipExcluded Outcome = "skip_excluded"
	OutcomeSkipManaged  Outcome = "skip_managed"
	OutcomeSkipKind     Outcome = "skip_kind"
	// OutcomeSkipMetadataDocument: the client's published metadata document lists its scope.
	OutcomeSkipMetadataDocument Outcome = "skip_metadata_document"
	OutcomeSkipSignIn           Outcome = "skip_sign_in_client"
	OutcomeSkipIdentityChaining Outcome = "skip_identity_chaining"
	OutcomeSkipUnlinked         Outcome = "skip_unlinked"
	OutcomeSkipEdited           Outcome = "skip_edited"
	OutcomeSkipUnmatched        Outcome = "skip_unmatched"
	// OutcomeSkipRequestChanges: a copy, but a login after the clear would request a different set.
	OutcomeSkipRequestChanges Outcome = "skip_request_changes"
	// OutcomeSkipChanged: the row moved, or stayed locked, between the scan and the write.
	OutcomeSkipChanged Outcome = "skip_changed"
)

type Options struct {
	CreatedBefore time.Time
	PageSize      int
	Apply         bool
	// ExcludedOrganizations are never written to, so their audit logs gain no entries.
	ExcludedOrganizations map[string]bool
}

// RowReport is one client; it goes to the report file, never stdout.
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

// Login is what one sign-in context would request once the client stores no scope.
type Login struct {
	Request []string
	// ResourceCopy reports that the client's scope equals this context's resource scopes_supported.
	ResourceCopy bool
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
func Classify(row ListClientScopeCandidatesRow, logins []Login) (Outcome, string) {
	switch {
	case row.Managed:
		return OutcomeSkipManaged, ""
	case row.ExcludedKind || row.OrganizationID == "":
		return OutcomeSkipKind, ""
	case row.MetadataDocument:
		return OutcomeSkipMetadataDocument, ""
	case row.TrustedSignIn:
		return OutcomeSkipSignIn, ""
	case row.IdentityChaining:
		return OutcomeSkipIdentityChaining, ""
	case !row.Linked:
		return OutcomeSkipUnlinked, ""
	case row.ScopeEdited:
		return OutcomeSkipEdited, ""
	}
	match := ""
	if slices.ContainsFunc(logins, func(l Login) bool { return l.ResourceCopy }) {
		match = "resource"
	} else if sameSet(row.Scope, row.IssuerScopesSupported) {
		match = "issuer"
	}
	if match == "" {
		return OutcomeSkipUnmatched, ""
	}
	for _, login := range logins {
		if !sameSet(login.Request, row.Scope) {
			return OutcomeSkipRequestChanges, match
		}
	}
	return OutcomeWouldClear, match
}

// Run scans every candidate; on error the report holds the rows processed so far.
func (r *Runner) Run(ctx context.Context) (Report, error) {
	report := newReport(r.options.Apply)
	cursor := uuid.Nil
	for {
		rows, err := New(r.pool).ListClientScopeCandidates(ctx, r.listParams(cursor, uuid.NullUUID{UUID: uuid.Nil, Valid: false}))
		if err != nil {
			return report, fmt.Errorf("list client scope candidates: %w", err)
		}
		for _, row := range rows {
			outcome, match, err := r.classify(ctx, r.pool, row)
			if err != nil {
				return report, err
			}
			scope := row.Scope
			if outcome == OutcomeWouldClear && r.options.Apply {
				outcome, match, scope, err = r.clear(ctx, row)
				if err != nil {
					return report, fmt.Errorf("clear client %s scope: %w", row.ID, err)
				}
			}
			report.add(RowReport{
				ClientID:       row.ID,
				ProjectID:      conv.Ternary(row.ProjectID.Valid, row.ProjectID.UUID.String(), ""),
				OrganizationID: row.OrganizationID,
				Outcome:        outcome,
				Match:          match,
				Scope:          scope,
			})
			cursor = row.ID
		}
		if len(rows) < r.options.PageSize {
			return report, nil
		}
	}
}

// Restore puts back every scope this backfill cleared that is still unset.
func (r *Runner) Restore(ctx context.Context) (Report, error) {
	report := newReport(r.options.Apply)
	clears, err := New(r.pool).ListBackfillClears(ctx, ActorComponent)
	if err != nil {
		return report, fmt.Errorf("list backfill clears: %w", err)
	}
	for _, entry := range clears {
		outcome := OutcomeSkipExcluded
		if !r.options.ExcludedOrganizations[entry.OrganizationID] {
			if outcome, err = r.restore(ctx, entry); err != nil {
				return report, fmt.Errorf("restore client %s scope: %w", entry.ClientID, err)
			}
		}
		report.add(RowReport{
			ClientID:       entry.ClientID,
			ProjectID:      conv.Ternary(entry.ProjectID.Valid, entry.ProjectID.UUID.String(), ""),
			OrganizationID: entry.OrganizationID,
			Outcome:        outcome,
			Match:          "",
			Scope:          entry.Scope,
		})
	}
	return report, nil
}

func newReport(apply bool) Report {
	return Report{Mode: conv.Ternary(apply, "apply", "dry-run"), Scanned: 0, Outcomes: map[Outcome]int{}, Rows: []RowReport{}}
}

func (r *Report) add(row RowReport) {
	r.Scanned++
	r.Outcomes[row.Outcome]++
	r.Rows = append(r.Rows, row)
}

func (r *Runner) classify(ctx context.Context, db remotesessionsrepo.DBTX, row ListClientScopeCandidatesRow) (Outcome, string, error) {
	if r.options.ExcludedOrganizations[row.OrganizationID] {
		return OutcomeSkipExcluded, "", nil
	}
	logins, err := loginsAfterClear(ctx, db, row)
	if err != nil {
		return "", "", fmt.Errorf("resolve logins for client %s: %w", row.ID, err)
	}
	outcome, match := Classify(row, logins)
	return outcome, match, nil
}

func (r *Runner) listParams(after uuid.UUID, clientID uuid.NullUUID) ListClientScopeCandidatesParams {
	return ListClientScopeCandidatesParams{
		CreatedBefore: pgtype.Timestamptz{Time: r.options.CreatedBefore, InfinityModifier: pgtype.Finite, Valid: true},
		AfterID:       after,
		ClientID:      clientID,
		PageSize:      conv.SafeInt32(r.options.PageSize),
	}
}

// loginsAfterClear resolves, per server signing in through the client or
// reached through it as a gateway member, what a login would request with no
// client scope, the way the login itself does.
func loginsAfterClear(ctx context.Context, db remotesessionsrepo.DBTX, row ListClientScopeCandidatesRow) ([]Login, error) {
	input := remotesessions.ConnectionScopeInput{
		ClientID:                row.ID,
		ClientScope:             nil,
		IssuerScopesSupported:   row.IssuerScopesSupported,
		IssuerScopeOverride:     row.IssuerScopeOverride,
		IssuerOmitScopeFallback: row.IssuerOmitScopeFallback,
	}
	login := func(project uuid.UUID, url string, owns bool) (Login, error) {
		resource, advertised, err := cachedResource(ctx, db, project, url)
		if err != nil {
			return Login{}, err
		}
		return Login{Request: remotesessions.ClientConnectionScopes(input, resource, owns).Scopes, ResourceCopy: sameSet(row.Scope, advertised)}, nil
	}

	servers, err := New(db).ListClientLoginServers(ctx, ListClientLoginServersParams{ClientID: row.ID, OrganizationID: row.OrganizationID, ProjectID: row.ProjectID})
	if err != nil {
		return nil, fmt.Errorf("list login servers: %w", err)
	}
	members, err := New(db).ListClientMemberServers(ctx, ListClientMemberServersParams{ClientID: row.ID, OrganizationID: row.OrganizationID, ProjectID: row.ProjectID})
	if err != nil {
		return nil, fmt.Errorf("list member servers: %w", err)
	}
	logins := make([]Login, 0, len(servers)+len(members)+1)
	for _, server := range servers {
		owns := false
		if server.Url != "" {
			siblings, err := remotesessionsrepo.New(db).ListRemoteSessionClientIDsForUserSessionIssuer(ctx, remotesessionsrepo.ListRemoteSessionClientIDsForUserSessionIssuerParams{
				UserSessionIssuerID: server.UserSessionIssuerID,
				ProjectID:           server.ProjectID,
				OrganizationID:      row.OrganizationID,
			})
			if err != nil {
				return nil, fmt.Errorf("list sibling clients: %w", err)
			}
			if owns, err = remotesessions.ResourceAppliesAtUpstream(ctx, db, row.OrganizationID, row.ID, siblings, server.Url); err != nil {
				return nil, fmt.Errorf("decide resource ownership: %w", err)
			}
		}
		l, err := login(server.ProjectID, server.Url, owns)
		if err != nil {
			return nil, err
		}
		logins = append(logins, l)
	}
	// A member login reads the member's resource with the client it was qualified to.
	for _, member := range members {
		l, err := login(member.ProjectID, member.Url, member.Url != "")
		if err != nil {
			return nil, err
		}
		logins = append(logins, l)
	}
	if len(logins) == 0 {
		l, err := login(uuid.Nil, "", false)
		if err != nil {
			return nil, err
		}
		logins = append(logins, l)
	}
	return logins, nil
}

// cachedResource is the resource row a login reads without probing, plus the
// list the row last advertised. Only the former predicts the request: a login
// whose probe fails ignores a list past its last-good window.
func cachedResource(ctx context.Context, db remotesessionsrepo.DBTX, project uuid.UUID, url string) (remotesessions.ResourceScopes, []string, error) {
	none := remotesessions.ResourceScopesFromRow(nil, true)
	if url == "" {
		return none, nil, nil
	}
	row, err := remotemcprepo.New(db).GetRemoteProtectedResource(ctx, remotemcprepo.GetRemoteProtectedResourceParams{ProjectID: project, ResourceIdentifier: url})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return none, nil, nil
	case err != nil:
		return none, nil, fmt.Errorf("read protected resource: %w", err)
	}
	return remotesessions.ResourceScopesFromRow(&row, true), row.ScopesSupported, nil
}

// clear rechecks the evidence under the row lock, then clears and audits in one transaction.
func (r *Runner) clear(ctx context.Context, row ListClientScopeCandidatesRow) (Outcome, string, []string, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return "", "", nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	q := New(tx)

	before, err := q.LockClientForScopeWrite(ctx, LockClientForScopeWriteParams{ID: row.ID, ProjectID: row.ProjectID, OrganizationID: row.OrganizationID})
	if skipped(err) {
		return OutcomeSkipChanged, "", row.Scope, nil
	}
	if err != nil {
		return "", "", nil, fmt.Errorf("lock client: %w", err)
	}
	rechecked, err := q.ListClientScopeCandidates(ctx, r.listParams(uuid.Nil, uuid.NullUUID{UUID: row.ID, Valid: true}))
	if err != nil {
		return "", "", nil, fmt.Errorf("recheck client: %w", err)
	}
	if len(rechecked) != 1 {
		return OutcomeSkipChanged, "", before.Scope, nil
	}
	outcome, match, err := r.classify(ctx, tx, rechecked[0])
	if err != nil {
		return "", "", nil, err
	}
	if outcome != OutcomeWouldClear {
		return outcome, match, before.Scope, nil
	}

	after, err := q.ClearClientScope(ctx, ClearClientScopeParams{ID: row.ID, ExpectedUpdatedAt: before.UpdatedAt, ProjectID: row.ProjectID, OrganizationID: row.OrganizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return OutcomeSkipChanged, "", before.Scope, nil
	}
	if err != nil {
		return "", "", nil, fmt.Errorf("clear scope: %w", err)
	}
	if err := r.auditWrite(ctx, tx, rechecked[0].OrganizationID, before, after); err != nil {
		return "", "", nil, err
	}
	if err := tx.Commit(context.WithoutCancel(ctx)); err != nil {
		return "", "", nil, fmt.Errorf("commit: %w", err)
	}
	return OutcomeCleared, match, before.Scope, nil
}

func (r *Runner) restore(ctx context.Context, entry ListBackfillClearsRow) (Outcome, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	q := New(tx)

	before, err := q.LockClientForScopeWrite(ctx, LockClientForScopeWriteParams{ID: entry.ClientID, ProjectID: entry.ProjectID, OrganizationID: entry.OrganizationID})
	if skipped(err) {
		return OutcomeSkipChanged, nil
	}
	if err != nil {
		return "", fmt.Errorf("lock client: %w", err)
	}
	after, err := q.RestoreClientScope(ctx, RestoreClientScopeParams{Scope: entry.Scope, ID: entry.ClientID, ExpectedUpdatedAt: before.UpdatedAt, ProjectID: entry.ProjectID, OrganizationID: entry.OrganizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return OutcomeSkipChanged, nil
	}
	if err != nil {
		return "", fmt.Errorf("restore scope: %w", err)
	}
	if err := r.auditWrite(ctx, tx, entry.OrganizationID, before, after); err != nil {
		return "", err
	}
	// A dry run takes the same path and rolls it back, so it reports what apply would do.
	if !r.options.Apply {
		return OutcomeWouldRestore, nil
	}
	if err := tx.Commit(context.WithoutCancel(ctx)); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}
	return OutcomeRestored, nil
}

func (r *Runner) begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '"+lockTimeout+"'"); err != nil {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("set lock timeout: %w", err)
	}
	return tx, nil
}

func (r *Runner) auditWrite(ctx context.Context, tx pgx.Tx, organizationID string, before, after RemoteSessionClient) error {
	issuerIDs, err := remotesessionsrepo.New(tx).ListUserSessionIssuerIDsForRemoteSessionClient(ctx, after.ID)
	if err != nil {
		return fmt.Errorf("list user session issuers: %w", err)
	}
	views := make([]*types.RemoteSessionClient, 0, 2)
	for _, row := range []RemoteSessionClient{before, after} {
		view, err := mv.BuildRemoteSessionClientView(remotesessionsrepo.RemoteSessionClient(row), issuerIDs)
		if err != nil {
			return fmt.Errorf("build client view: %w", err)
		}
		views = append(views, view)
	}
	if err := r.audit.LogRemoteSessionClientUpdate(ctx, tx, audit.LogRemoteSessionClientUpdateEvent{
		OrganizationID:         organizationID,
		ProjectID:              conv.Ternary(after.ProjectID.Valid, after.ProjectID.UUID, uuid.Nil),
		Actor:                  urn.NewSystemPrincipal(ActorComponent),
		ActorDisplayName:       nil,
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(after.ID),
		ClientID:               after.ClientID,
		SnapshotBefore:         views[0],
		SnapshotAfter:          views[1],
	}); err != nil {
		return fmt.Errorf("audit scope write: %w", err)
	}
	return nil
}

// skipped reports a row that is gone or held past the lock timeout.
func skipped(err error) bool {
	if errors.Is(err, pgx.ErrNoRows) {
		return true
	}
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	return ok && pgErr.Code == pgerrcode.LockNotAvailable
}

func sameSet(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	return slices.Equal(dedupeSorted(a), dedupeSorted(b))
}

func dedupeSorted(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)
	return slices.Compact(out)
}
