// Package authclientbackfill records a typed auth client for every upstream
// identity an MCP server is configured with today, and links the server to it.
// A subject-owned upstream OAuth client becomes a remote_session auth client
// and a self-owned one a service_account auth client; a source's credential
// headers a static_credential or pass_through client; and an identity
// chaining upstream an ema client. A dry run decides without writing. Every
// write is conditional on the evidence it was decided from, and a run holds
// the organization's advisory lock, so a run is idempotent and a concurrent
// change is skipped rather than overwritten; rerunning picks it up. Each run
// also retires backfilled clients, links and header copies the configuration
// no longer yields, so the records track it, and marks each server it links a
// client to as needing upstream authentication where that is still
// undecided. Admin-created records are never
// read or retired here. Its last phase grants the default auth permissions
// the mandatory auth client check needs (see grants.go); those grants are live
// authorization data. The client records themselves are not read yet: the
// existing resolution stays authoritative.
package authclientbackfill

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// defaultPageSize bounds each page read.
const defaultPageSize = 200

// Client types recorded by this backfill.
const (
	TypeRemoteSession    = "remote_session"
	TypeServiceAccount   = "service_account"
	TypeStaticCredential = "static_credential" // #nosec G101 -- a client type, not a credential
	TypePassThrough      = "pass_through"
	TypeEMA              = "ema"
)

// SourceBackfill marks clients and links this backfill created; only those
// are found, linked or retired here.
const SourceBackfill = "backfill"

// validType reports whether a client type is one this backfill records.
func validType(clientType string) bool {
	switch clientType {
	case TypeRemoteSession, TypeServiceAccount, TypeStaticCredential, TypePassThrough, TypeEMA:
		return true
	default:
		return false
	}
}

// Kind is the configuration a result was decided from.
type Kind string

const (
	// KindOAuth is a server to upstream OAuth client association, or the
	// credential headers copied onto an upstream OAuth client's auth client.
	KindOAuth Kind = "oauth"

	// KindHeader is a source's credential headers, or a server on that source.
	KindHeader Kind = "header"

	// KindEMA is an identity chaining upstream, or a server it reaches.
	KindEMA Kind = "ema"
)

// Kinds lists every kind in report order.
var Kinds = []Kind{KindOAuth, KindHeader, KindEMA}

// Phase names, as reported in PhaseErrors.
const (
	PhaseOAuth   = "oauth"
	PhaseHeaders = "headers"
	PhaseEMA     = "ema"
	// PhaseEMAWarnings reports same-origin servers no client covers.
	PhaseEMAWarnings = "ema_warnings"
	PhaseReconcile   = "reconcile"
	PhaseMark        = "mark"
	PhaseGrants      = "grants"
)

// Outcome classifies one decision.
type Outcome string

const (
	// OutcomeCreated is configuration whose auth client this run recorded.
	OutcomeCreated Outcome = "created"

	// OutcomeExisting is configuration whose records were already in place.
	OutcomeExisting Outcome = "existing"

	// OutcomeAssociated is configuration whose auth client was already
	// recorded and whose server link this run recorded.
	OutcomeAssociated Outcome = "associated"

	// OutcomeUpdated is configuration whose auth client was already recorded
	// and whose header copies or scopes this run brought up to date.
	OutcomeUpdated Outcome = "updated"

	// OutcomeAmbiguous is configuration that cannot be recorded without a
	// guess; Reason says why. It is never guessed.
	OutcomeAmbiguous Outcome = "ambiguous"

	// OutcomeSkippedRace is configuration whose evidence changed before a
	// write; rerunning picks it up.
	OutcomeSkippedRace Outcome = "skipped_race"

	// OutcomeWarning is configuration worth a look that needs no action and
	// never holds the run back; Reason says why.
	OutcomeWarning Outcome = "warning"
)

// Covered reports whether configuration with this outcome has its records
// after the run, or needs none: a warning is not a gap.
func (o Outcome) Covered() bool {
	return o != OutcomeAmbiguous && o != OutcomeSkippedRace
}

// Reason says why configuration is ambiguous.
type Reason string

const (
	// ReasonUnknownOwner is an upstream OAuth client whose credential owner
	// is neither subject nor self.
	ReasonUnknownOwner Reason = "unknown_owner"

	// ReasonTypeMismatch is an upstream OAuth client whose recorded auth
	// client has another type than its owner now implies.
	ReasonTypeMismatch Reason = "type_mismatch"

	// ReasonProjectMismatch is an upstream OAuth client whose recorded auth
	// client belongs to another project.
	ReasonProjectMismatch Reason = "project_mismatch"

	// ReasonMultipleAuthorizationHeaders is a source with more than one
	// Authorization header, differing in letter case.
	ReasonMultipleAuthorizationHeaders Reason = "multiple_authorization_headers"

	// ReasonDuplicateHeaderNames is a source with credential headers whose
	// names differ only in letter case.
	ReasonDuplicateHeaderNames Reason = "duplicate_header_names"

	// ReasonDeniedSource is a source with a header forwarded from a request
	// header the proxy refuses as a source, such as Authorization, which on
	// an issuer-gated endpoint carries the caller's Speakeasy session token.
	// The proxy drops or refuses that header at request time, so no client is
	// recorded for it.
	ReasonDeniedSource Reason = "denied_source"

	// ReasonMixedCredentialHeaders is a source with no Authorization header
	// whose credential headers are partly stored and partly forwarded.
	ReasonMixedCredentialHeaders Reason = "mixed_credential_headers" // #nosec G101 -- a reason code, not a credential

	// ReasonOAuthClientSharedAcrossSources is an upstream OAuth client used by
	// servers on more than one source while any of those sources carries
	// credential headers: copying them onto the shared client would send one
	// source's credentials from the others' servers.
	ReasonOAuthClientSharedAcrossSources Reason = "oauth_client_shared_across_sources"

	// ReasonEMAClientSharedAcrossSources is an ema client offered by servers
	// on more than one source with credential headers, so no one source's
	// headers can be copied onto it.
	ReasonEMAClientSharedAcrossSources Reason = "ema_client_shared_across_sources"

	// ReasonConflictingScopes is identity chaining bindings for one upstream
	// requesting different scopes.
	ReasonConflictingScopes Reason = "conflicting_scopes"

	// ReasonResourceVariants is identity chaining bindings for one upstream
	// naming its resource differently, beyond a trailing slash.
	ReasonResourceVariants Reason = "resource_variants"

	// ReasonResourceNotMatched is identity chaining bindings whose resource
	// names no server's upstream.
	ReasonResourceNotMatched Reason = "resource_not_matched"

	// ReasonIssuerNotAllowed is a server an identity chaining upstream's
	// resource matches whose protected resource metadata does not list the
	// upstream's issuer, so the runtime would refuse the exchange.
	ReasonIssuerNotAllowed Reason = "issuer_not_allowed"

	// ReasonRegistrationOutOfReach is an identity chaining binding whose
	// registration or issuer is deleted or belongs to no scope the binding's
	// project can use.
	ReasonRegistrationOutOfReach Reason = "registration_out_of_reach"

	// ReasonServerNotMatched is a warning: a server with no other backfilled
	// auth client at the same origin as an identity chaining upstream, whose
	// metadata may name that resource, which only live discovery can
	// confirm, so it is reported rather than linked.
	ReasonServerNotMatched Reason = "server_not_matched"
)

// Options controls one run.
type Options struct {
	// OrganizationID is the one organization the run covers. Required.
	OrganizationID string

	// Apply writes the decided records; otherwise the run only decides.
	Apply bool

	// PageSize overrides defaultPageSize when positive.
	PageSize int
}

// Result is one decision. Results carry ids only.
type Result struct {
	// Kind is the configuration decided.
	Kind Kind `json:"kind"`

	// SubjectID is what was decided: the association, source, upstream OAuth
	// client or first binding.
	SubjectID uuid.UUID `json:"subject_id"`

	// AssociationID is the server and client association decided, for
	// KindOAuth associations; the nil UUID otherwise.
	AssociationID uuid.UUID `json:"association_id"`

	// MCPServerID is the server concerned, when one is.
	MCPServerID uuid.UUID `json:"mcp_server_id"`

	// ProjectID is the project the configuration belongs to.
	ProjectID uuid.UUID `json:"project_id"`

	// RemoteSessionClientID is the upstream OAuth client concerned, when one
	// is.
	RemoteSessionClientID uuid.UUID `json:"remote_session_client_id"`

	// BindingIDs lists every identity chaining binding decided together, for
	// KindEMA.
	BindingIDs []uuid.UUID `json:"binding_ids,omitempty"`

	// SourceIDs lists the sources concerned, for an upstream OAuth client
	// shared across sources.
	SourceIDs []uuid.UUID `json:"source_ids,omitempty"`

	// ServerIDs lists the servers concerned, for an upstream OAuth client
	// shared across sources.
	ServerIDs []uuid.UUID `json:"server_ids,omitempty"`

	// AuthClientID is the auth client recorded, when known.
	AuthClientID uuid.NullUUID `json:"auth_client_id"`

	// Outcome classifies the decision.
	Outcome Outcome `json:"outcome"`

	// Reason says why an ambiguous decision is ambiguous.
	Reason Reason `json:"reason,omitempty"`
}

// KindCounts summarizes one kind of configuration.
type KindCounts struct {
	// Decided counts decisions.
	Decided int `json:"decided"`

	// PendingClients counts auth clients to record, written or not.
	PendingClients int `json:"pending_clients"`

	// PendingLinks counts server links to record, written or not.
	PendingLinks int `json:"pending_links"`

	// PendingHeaders counts header copies and scope sets to add, update or
	// retire, written or not.
	PendingHeaders int `json:"pending_headers"`

	// Written counts auth clients, links, header copies and scope sets
	// written.
	Written int `json:"written"`

	// Unresolved counts ambiguous decisions.
	Unresolved int `json:"unresolved"`

	// Warnings counts advisory decisions.
	Warnings int `json:"warnings"`

	// PendingRetirements counts backfilled clients and links to retire,
	// retired or not.
	PendingRetirements int `json:"pending_retirements"`

	// Retired counts clients and links retired.
	Retired int `json:"retired"`
}

// Report summarizes a run. Results carry ids only, never names, URLs or
// header values.
type Report struct {
	// Mode is "apply" or "dry_run".
	Mode string `json:"mode"`

	// Associations counts every decision across kinds.
	Associations int `json:"associations"`

	// Outcomes counts decisions per outcome.
	Outcomes map[Outcome]int `json:"outcomes"`

	// ByKind summarizes each kind.
	ByKind map[Kind]*KindCounts `json:"by_kind"`

	// PendingClients counts auth clients to record, written or not.
	PendingClients int `json:"pending_clients"`

	// PendingLinks counts server links to record, written or not.
	PendingLinks int `json:"pending_links"`

	// PendingHeaders counts header copies and scope sets to add, update or
	// retire, written or not.
	PendingHeaders int `json:"pending_headers"`

	// ClientsWritten counts auth clients recorded.
	ClientsWritten int `json:"clients_written"`

	// LinksWritten counts server links recorded.
	LinksWritten int `json:"links_written"`

	// HeadersWritten counts header copies and scope sets written.
	HeadersWritten int `json:"headers_written"`

	// Gaps counts decisions left without their records.
	Gaps int `json:"gaps"`

	// PendingRetiredClients counts backfilled auth clients the configuration
	// no longer yields, retired or not.
	PendingRetiredClients int `json:"pending_retired_clients"`

	// PendingRetiredLinks counts backfilled server links the configuration no
	// longer yields, retired or not.
	PendingRetiredLinks int `json:"pending_retired_links"`

	// ClientsRetired counts auth clients retired.
	ClientsRetired int `json:"clients_retired"`

	// LinksRetired counts server links retired.
	LinksRetired int `json:"links_retired"`

	// PendingMarkedServers counts servers offering a backfilled auth client
	// whose need for upstream authentication is still undecided, marked or
	// not.
	PendingMarkedServers int `json:"pending_marked_servers"`

	// ServersMarked counts servers marked as needing upstream authentication.
	ServersMarked int `json:"servers_marked"`

	// PendingGrants counts default grants the organization is missing, and
	// PendingGrantsByKind splits them by default.
	PendingGrants       int               `json:"pending_grants"`
	PendingGrantsByKind map[GrantKind]int `json:"pending_grants_by_kind"`

	// GrantsWritten counts default grants this run added.
	GrantsWritten int `json:"grants_written"`

	// APIKeysGranted lists the API keys this run added policy grants to, for
	// the run's log.
	APIKeysGranted []string `json:"api_keys_granted"`

	// APIKeysWaiting means key policies were not planned because some auth
	// clients are not recorded yet.
	APIKeysWaiting bool `json:"api_keys_waiting"`

	// PhaseErrors names each phase that failed and why. A failed phase's
	// writes are rolled back and its records are not retired; the other
	// phases still run.
	PhaseErrors map[string]string `json:"phase_errors"`

	// Results lists every decision.
	Results []Result `json:"results"`
}

func (r *Report) kind(k Kind) *KindCounts {
	counts, ok := r.ByKind[k]
	if !ok {
		counts = &KindCounts{Decided: 0, PendingClients: 0, PendingLinks: 0, PendingHeaders: 0, Written: 0, Unresolved: 0, Warnings: 0, PendingRetirements: 0, Retired: 0}
		r.ByKind[k] = counts
	}
	return counts
}

// add records one decision.
func (r *Report) add(result Result) {
	r.Associations++
	r.Outcomes[result.Outcome]++
	counts := r.kind(result.Kind)
	counts.Decided++
	switch result.Outcome {
	case OutcomeAmbiguous:
		counts.Unresolved++
	case OutcomeWarning:
		counts.Warnings++
	case OutcomeCreated, OutcomeExisting, OutcomeAssociated, OutcomeUpdated, OutcomeSkippedRace:
	}
	if !result.Outcome.Covered() {
		r.Gaps++
	}
	r.Results = append(r.Results, result)
}

func (r *Report) pendingClient(k Kind) { r.PendingClients++; r.kind(k).PendingClients++ }
func (r *Report) pendingLink(k Kind)   { r.PendingLinks++; r.kind(k).PendingLinks++ }
func (r *Report) pendingHeader(k Kind) { r.PendingHeaders++; r.kind(k).PendingHeaders++ }
func (r *Report) wroteClient(k Kind)   { r.ClientsWritten++; r.kind(k).Written++ }
func (r *Report) wroteLink(k Kind)     { r.LinksWritten++; r.kind(k).Written++ }
func (r *Report) wroteHeader(k Kind)   { r.HeadersWritten++; r.kind(k).Written++ }

// newReport returns an empty report in the given mode.
func newReport(apply bool) Report {
	report := Report{
		Mode:                  "dry_run",
		Associations:          0,
		Outcomes:              map[Outcome]int{},
		ByKind:                map[Kind]*KindCounts{},
		PendingClients:        0,
		PendingLinks:          0,
		PendingHeaders:        0,
		ClientsWritten:        0,
		LinksWritten:          0,
		HeadersWritten:        0,
		Gaps:                  0,
		PendingRetiredClients: 0,
		PendingRetiredLinks:   0,
		ClientsRetired:        0,
		LinksRetired:          0,
		PendingMarkedServers:  0,
		ServersMarked:         0,
		PendingGrants:         0,
		PendingGrantsByKind:   map[GrantKind]int{},
		GrantsWritten:         0,
		APIKeysGranted:        []string{},
		APIKeysWaiting:        false,
		PhaseErrors:           map[string]string{},
		Results:               []Result{},
	}
	if apply {
		report.Mode = "apply"
	}
	for _, k := range Kinds {
		report.kind(k)
	}
	return report
}

// merge folds a committed phase's report into r.
func (r *Report) merge(o Report) {
	r.Associations += o.Associations
	for outcome, n := range o.Outcomes {
		r.Outcomes[outcome] += n
	}
	for k, c := range o.ByKind {
		mine := r.kind(k)
		mine.Decided += c.Decided
		mine.PendingClients += c.PendingClients
		mine.PendingLinks += c.PendingLinks
		mine.PendingHeaders += c.PendingHeaders
		mine.Written += c.Written
		mine.Unresolved += c.Unresolved
		mine.Warnings += c.Warnings
		mine.PendingRetirements += c.PendingRetirements
		mine.Retired += c.Retired
	}
	r.PendingClients += o.PendingClients
	r.PendingLinks += o.PendingLinks
	r.PendingHeaders += o.PendingHeaders
	r.ClientsWritten += o.ClientsWritten
	r.LinksWritten += o.LinksWritten
	r.HeadersWritten += o.HeadersWritten
	r.Gaps += o.Gaps
	r.PendingRetiredClients += o.PendingRetiredClients
	r.PendingRetiredLinks += o.PendingRetiredLinks
	r.ClientsRetired += o.ClientsRetired
	r.LinksRetired += o.LinksRetired
	r.PendingMarkedServers += o.PendingMarkedServers
	r.ServersMarked += o.ServersMarked
	r.PendingGrants += o.PendingGrants
	for kind, n := range o.PendingGrantsByKind {
		r.PendingGrantsByKind[kind] += n
	}
	r.GrantsWritten += o.GrantsWritten
	r.APIKeysGranted = append(r.APIKeysGranted, o.APIKeysGranted...)
	r.APIKeysWaiting = r.APIKeysWaiting || o.APIKeysWaiting
	r.Results = append(r.Results, o.Results...)
}

// TxStarter begins the transactions a run's phases use.
type TxStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Acquirer hands out the one connection a run holds its lock on.
type Acquirer interface {
	Acquire(ctx context.Context) (*pgxpool.Conn, error)
}

// cleanupTimeout bounds the unlock, close and rollback a run does on its way
// out, so an unreachable database cannot hold it open.
const cleanupTimeout = 10 * time.Second

// ErrRunInProgress means another run holds the organization's backfill lock.
var ErrRunInProgress = errors.New("an auth client backfill is already running for this organization")

// clientKey identifies one upstream client within one organization, which is
// what one auth client covers.
type clientKey struct {
	organizationID string
	clientID       uuid.UUID
}

// beforePhase runs before each phase, and afterPhase after its work and
// before it commits; tests use them to fail one.
var (
	beforePhase = func(string) error { return nil }
	afterPhase  = func(string) error { return nil }
)

// phase is one independently committed step of a run.
type phase struct {
	name string
	kind Kind
	run  func(ctx context.Context, tx pgx.Tx, report *Report) error
}

// Run decides every configured upstream identity and, with Apply, writes the
// decisions. The run holds the organization's advisory lock on one connection
// for all its phases, taken without waiting: when another run holds it, Run
// returns ErrRunInProgress, dry runs included. Each phase runs in its own
// transaction on that connection. A phase's report is
// kept only when it commits; a failed phase is rolled back and named in
// PhaseErrors, its records are not retired, and the later phases still run.
// What earlier phases committed stays, so an interrupted apply keeps its
// progress. Run returns an error only when the run as a whole cannot proceed.
func Run(ctx context.Context, db Acquirer, opts Options) (Report, error) {
	return withOrganizationLock(ctx, db, opts, func(conn *pgxpool.Conn, startedAt pgtype.Timestamptz) (Report, error) {
		return runLocked(ctx, conn, opts, startedAt)
	})
}

// withOrganizationLock runs fn holding the organization's backfill lock on one
// connection, taken without waiting.
func withOrganizationLock(ctx context.Context, db Acquirer, opts Options, fn func(conn *pgxpool.Conn, startedAt pgtype.Timestamptz) (Report, error)) (Report, error) {
	if opts.OrganizationID == "" {
		return Report{}, errors.New("organization id is required")
	}
	conn, err := db.Acquire(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("acquire connection: %w", err)
	}
	// Unlocking is registered before the lock is tried, so a lock taken by a
	// try that then errored is still released. Unlocking a lock this session
	// does not hold is a no-op; a connection that cannot unlock is closed
	// rather than returned to the pool still holding the lock.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		if err := New(conn).UnlockOrganizationBackfill(cleanup, opts.OrganizationID); err != nil {
			_ = conn.Conn().Close(cleanup)
		}
		conn.Release()
	}()
	lock, err := New(conn).TryLockOrganizationBackfill(ctx, opts.OrganizationID)
	switch {
	case err != nil:
		return Report{}, fmt.Errorf("lock organization backfill: %w", err)
	case !lock.Locked:
		return Report{}, ErrRunInProgress
	}
	return fn(conn, lock.StartedAt)
}

// RunGrants runs only the grants phase, for organizations the rest of the
// backfill does not apply to. An apply holds the same organization lock as
// Run; a dry run writes nothing and takes no lock, so checking never blocks a
// run.
func RunGrants(ctx context.Context, db Acquirer, opts Options) (Report, error) {
	if !opts.Apply {
		if opts.OrganizationID == "" {
			return Report{}, errors.New("organization id is required")
		}
		conn, err := db.Acquire(ctx)
		if err != nil {
			return Report{}, fmt.Errorf("acquire connection: %w", err)
		}
		defer conn.Release()
		return runGrantsPhase(ctx, conn, opts)
	}
	return withOrganizationLock(ctx, db, opts, func(conn *pgxpool.Conn, _ pgtype.Timestamptz) (Report, error) {
		return runGrantsPhase(ctx, conn, opts)
	})
}

// runGrantsPhase runs the grants phase alone. Key policies are written once,
// so they wait until the organization's auth clients are all recorded.
func runGrantsPhase(ctx context.Context, conn *pgxpool.Conn, opts Options) (Report, error) {
	report := newReport(opts.Apply)
	local := newReport(opts.Apply)
	probe, err := clientsRecorded(ctx, conn, opts.OrganizationID)
	if err != nil {
		return report, fmt.Errorf("check recorded auth clients: %w", err)
	}
	ready := keysReady(probe)
	report.APIKeysWaiting = !ready
	// The probe's failures say why keys wait.
	maps.Copy(report.PhaseErrors, probe.PhaseErrors)
	p := phase{name: PhaseGrants, kind: "", run: func(ctx context.Context, tx pgx.Tx, local *Report) error {
		return runGrants(ctx, tx, opts, local, ready)
	}}
	if err := runPhase(ctx, conn, opts, p, &local); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return report, fmt.Errorf("grants interrupted: %w", ctxErr)
		}
		report.PhaseErrors[PhaseGrants] = err.Error()
		return report, nil
	}
	report.merge(local)
	return report, nil
}

func runLocked(ctx context.Context, db TxStarter, opts Options, startedAt pgtype.Timestamptz) (Report, error) {
	return runPhases(ctx, db, opts, startedAt, true)
}

// clientsRecorded dry-runs every phase but grants, to tell whether key
// policies written now would be complete.
func clientsRecorded(ctx context.Context, db TxStarter, organizationID string) (Report, error) {
	return runPhases(ctx, db, Options{OrganizationID: organizationID, Apply: false, PageSize: 0}, pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}, false)
}

// keysReady reports whether the phases before grants left every auth client,
// link and header copy recorded: none failed, no decision lacks its records,
// and a dry run has nothing left to write.
func keysReady(r Report) bool {
	if len(r.PhaseErrors) > 0 || r.Gaps > 0 {
		return false
	}
	return r.Mode == "apply" || (r.PendingClients == 0 && r.PendingLinks == 0 && r.PendingHeaders == 0)
}

func runPhases(ctx context.Context, db TxStarter, opts Options, startedAt pgtype.Timestamptz, withGrants bool) (Report, error) {
	pageSize := opts.PageSize
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	report := newReport(opts.Apply)
	size := int32(pageSize) // #nosec G115 -- bounded by defaultPageSize or an operator flag
	want := newExpected()
	failed := map[Kind]bool{}
	// Set by the EMA phase when it succeeds.
	var ema *emaPlan
	phases := []phase{
		{name: PhaseOAuth, kind: KindOAuth, run: func(ctx context.Context, tx pgx.Tx, local *Report) error {
			q := New(tx)
			if err := runOAuth(ctx, tx, q, opts, size, local); err != nil {
				return err
			}
			return reconcile(ctx, q, opts, local)
		}},
		// EMA before headers, so a server an ema client serves gets its
		// credential headers on that client.
		{name: PhaseEMA, kind: KindEMA, run: func(ctx context.Context, tx pgx.Tx, local *Report) error {
			plan := newEMAPlan()
			if err := runEMA(ctx, tx, opts, local, want, plan); err != nil {
				return err
			}
			ema = plan
			return nil
		}},
		{name: PhaseHeaders, kind: KindHeader, run: func(ctx context.Context, tx pgx.Tx, local *Report) error {
			return runHeaders(ctx, tx, opts, size, local, want, ema)
		}},
		{name: PhaseEMAWarnings, kind: "", run: func(ctx context.Context, tx pgx.Tx, local *Report) error {
			// After every phase that records clients, so a server any of them
			// covers draws no warning.
			if ema == nil {
				return nil
			}
			return reportEMAWarnings(ctx, New(tx), opts, ema, &report, local)
		}},
		{name: PhaseReconcile, kind: "", run: func(ctx context.Context, tx pgx.Tx, local *Report) error {
			return reconcileDerived(ctx, New(tx), opts, local, want, failed, startedAt)
		}},
		{name: PhaseMark, kind: "", run: func(ctx context.Context, tx pgx.Tx, local *Report) error {
			return markRequiringUpstreamAuth(ctx, New(tx), opts, &report, local)
		}},
	}
	if withGrants {
		phases = append(phases, phase{name: PhaseGrants, kind: "", run: func(ctx context.Context, tx pgx.Tx, local *Report) error {
			// Last, over every committed phase's auth clients.
			ready := keysReady(report)
			local.APIKeysWaiting = !ready
			return runGrants(ctx, tx, opts, local, ready)
		}})
	}
	for _, p := range phases {
		local := newReport(opts.Apply)
		err := runPhase(ctx, db, opts, p, &local)
		switch {
		case err != nil:
			if ctxErr := ctx.Err(); ctxErr != nil {
				return report, fmt.Errorf("backfill interrupted: %w", ctxErr)
			}
			report.PhaseErrors[p.name] = err.Error()
			if p.kind != "" {
				failed[p.kind] = true
			}
			if p.name == PhaseEMA {
				// Its decisions were rolled back with it.
				ema = nil
			}
		default:
			report.merge(local)
		}
	}
	return report, nil
}

// runPhase runs one phase in its own transaction, committing on apply and
// rolling back on a dry run.
func runPhase(ctx context.Context, db TxStarter, opts Options, p phase, local *Report) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin %s phase: %w", p.name, err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if err := beforePhase(p.name); err != nil {
		return err
	}
	if err := p.run(ctx, tx, local); err != nil {
		return err
	}
	if err := afterPhase(p.name); err != nil {
		return err
	}
	if !opts.Apply {
		return nil
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit %s phase: %w", p.name, err)
	}
	return nil
}

// inSavepoint runs fn in a savepoint of tx, rolling it back when fn fails.
func inSavepoint(ctx context.Context, tx pgx.Tx, fn func(sp pgx.Tx) error) error {
	sp, err := tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("open savepoint: %w", err)
	}
	if err := fn(sp); err != nil {
		if rbErr := sp.Rollback(ctx); rbErr != nil {
			return fmt.Errorf("%w (rollback: %w)", err, rbErr)
		}
		return err
	}
	if err := sp.Commit(ctx); err != nil {
		return fmt.Errorf("release savepoint: %w", err)
	}
	return nil
}

// isUniqueViolation reports a unique key conflict.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation
}

// runOAuth decides every server to upstream OAuth client association.
func runOAuth(ctx context.Context, tx pgx.Tx, q *Queries, opts Options, pageSize int32, report *Report) error {
	// Auth clients decided earlier in this run, so a client shared by several
	// servers is recorded once. A nil id marks one decided but not written.
	decided := map[clientKey]uuid.UUID{}
	cursor := uuid.Nil
	for {
		rows, err := q.ListAuthClientBackfillCandidates(ctx, ListAuthClientBackfillCandidatesParams{
			Cursor:         cursor,
			OrganizationID: opts.OrganizationID,
			PageSize:       pageSize,
		})
		if err != nil {
			return fmt.Errorf("list auth client backfill candidates after %s: %w", cursor, err)
		}
		if len(rows) == 0 {
			return nil
		}
		for _, row := range rows {
			result, err := decideAndWrite(ctx, tx, q, opts, row, decided, report)
			if err != nil {
				return err
			}
			report.add(result)
			cursor = row.AssociationID
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("backfill interrupted: %w", err)
		}
	}
}

// Type returns the auth client type a client's credential owner implies, or
// false when the owner is unknown.
func Type(credentialOwner string) (string, bool) {
	switch credentialOwner {
	case "subject":
		return TypeRemoteSession, true
	case "self":
		return TypeServiceAccount, true
	default:
		return "", false
	}
}

func decideAndWrite(ctx context.Context, tx pgx.Tx, q *Queries, opts Options, row ListAuthClientBackfillCandidatesRow, decided map[clientKey]uuid.UUID, report *Report) (Result, error) {
	result := Result{
		Kind:                  KindOAuth,
		SubjectID:             row.AssociationID,
		AssociationID:         row.AssociationID,
		MCPServerID:           row.McpServerID,
		ProjectID:             row.ProjectID,
		RemoteSessionClientID: row.RemoteSessionClientID,
		BindingIDs:            nil,
		SourceIDs:             nil,
		ServerIDs:             nil,
		AuthClientID:          uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Outcome:               OutcomeExisting,
		Reason:                "",
	}
	clientType, ok := Type(row.CredentialOwner)
	if !ok {
		result.Outcome = OutcomeAmbiguous
		result.Reason = ReasonUnknownOwner
		return result, nil
	}

	key := clientKey{organizationID: row.OrganizationID, clientID: row.RemoteSessionClientID}
	authClientID := row.AuthClientID
	switch {
	case authClientID != uuid.Nil && row.AuthClientType != clientType:
		result.AuthClientID = uuid.NullUUID{UUID: authClientID, Valid: true}
		result.Outcome = OutcomeAmbiguous
		result.Reason = ReasonTypeMismatch
		return result, nil
	case authClientID != uuid.Nil && row.AuthClientProjectID.Valid && row.AuthClientProjectID.UUID != row.ProjectID:
		result.AuthClientID = uuid.NullUUID{UUID: authClientID, Valid: true}
		result.Outcome = OutcomeAmbiguous
		result.Reason = ReasonProjectMismatch
		return result, nil
	case authClientID != uuid.Nil:
		// Recorded before this run.
	default:
		id, seen := decided[key]
		switch {
		case seen && id == uuid.Nil:
			// Decided earlier in this dry run; only the link is new.
			report.pendingLink(KindOAuth)
			result.Outcome = OutcomeAssociated
			return result, nil
		case seen:
			authClientID = id
			result.Outcome = OutcomeAssociated
			report.pendingLink(KindOAuth)
		default:
			report.pendingClient(KindOAuth)
			report.pendingLink(KindOAuth)
			result.Outcome = OutcomeCreated
			if !opts.Apply {
				decided[key] = uuid.Nil
				return result, nil
			}
			id, err := recordAuthClient(ctx, tx, row, clientType)
			switch {
			case errors.Is(err, errRace):
				result.Outcome = OutcomeSkippedRace
				return result, nil
			case err != nil:
				return result, err
			}
			report.wroteClient(KindOAuth)
			decided[key] = id
			authClientID = id
		}
	}
	result.AuthClientID = uuid.NullUUID{UUID: authClientID, Valid: true}

	if row.LinkLive {
		return result, nil
	}
	if result.Outcome == OutcomeExisting {
		result.Outcome = OutcomeAssociated
		report.pendingLink(KindOAuth)
	}
	if !opts.Apply {
		return result, nil
	}
	n, err := q.LinkAuthClientToServer(ctx, LinkAuthClientToServerParams{
		AuthClientID:   authClientID,
		AssociationID:  row.AssociationID,
		OrganizationID: row.OrganizationID,
	})
	if err != nil {
		return result, fmt.Errorf("link auth client for association %s: %w", row.AssociationID, err)
	}
	if n == 0 {
		result.Outcome = OutcomeSkippedRace
		return result, nil
	}
	report.wroteLink(KindOAuth)
	return result, nil
}

// errRace reports a decided write whose evidence changed before it.
var errRace = errors.New("evidence changed")

// recordAuthClient records the auth client for one association's client in a
// savepoint, so a conflict leaves the run usable. It is scoped like the
// client: a project client's auth client belongs to that project, and an
// organization or platform-global client's to the association's organization.
func recordAuthClient(ctx context.Context, tx pgx.Tx, row ListAuthClientBackfillCandidatesRow, clientType string) (uuid.UUID, error) {
	if !validType(clientType) {
		return uuid.Nil, fmt.Errorf("unknown auth client type %q", clientType)
	}
	name := DefaultName(clientType)
	if row.IssuerName.Valid && row.IssuerName.String != "" {
		name = row.IssuerName.String
	}
	var id uuid.UUID
	err := inSavepoint(ctx, tx, func(sp pgx.Tx) error {
		var err error
		id, err = New(sp).CreateAuthClient(ctx, CreateAuthClientParams{
			ID:                    OAuthClientID(row.OrganizationID, row.RemoteSessionClientID, clientType),
			OrganizationID:        row.OrganizationID,
			ProjectID:             row.ClientProjectID,
			ClientType:            clientType,
			Name:                  name,
			RemoteSessionClientID: row.RemoteSessionClientID,
		})
		return err
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows), isUniqueViolation(err):
		return uuid.Nil, errRace
	case err != nil:
		return uuid.Nil, fmt.Errorf("record auth client for client %s: %w", row.RemoteSessionClientID, err)
	}
	return id, nil
}

// DefaultName names an auth client whose upstream issuer has no name.
func DefaultName(clientType string) string {
	if clientType == TypeServiceAccount {
		return "Service account"
	}
	return "Personal connection"
}

// reconcile counts, and with Apply retires, backfilled OAuth auth clients no
// live association reaches and backfilled links whose association or auth
// client is gone. Clients go first so their links are caught in the same run.
func reconcile(ctx context.Context, q *Queries, opts Options, report *Report) error {
	staleClients, err := q.ListStaleBackfillAuthClients(ctx, opts.OrganizationID)
	if err != nil {
		return fmt.Errorf("list stale auth clients: %w", err)
	}
	staleLinks, err := q.ListStaleBackfillLinks(ctx, opts.OrganizationID)
	if err != nil {
		return fmt.Errorf("list stale server links: %w", err)
	}
	pendingLinks := len(staleLinks)
	counts := report.kind(KindOAuth)
	if !opts.Apply {
		report.PendingRetiredClients += len(staleClients)
		report.PendingRetiredLinks += pendingLinks
		counts.PendingRetirements += len(staleClients) + pendingLinks
		return nil
	}
	retiredClients, err := q.RetireStaleBackfillAuthClients(ctx, opts.OrganizationID)
	if err != nil {
		return fmt.Errorf("retire stale auth clients: %w", err)
	}
	retiredLinks, err := q.RetireStaleBackfillLinks(ctx, opts.OrganizationID)
	if err != nil {
		return fmt.Errorf("retire stale server links: %w", err)
	}
	// A link may go stale only once its client is retired in this run.
	pendingLinks = max(pendingLinks, len(retiredLinks))
	report.PendingRetiredClients += len(staleClients)
	report.PendingRetiredLinks += pendingLinks
	report.ClientsRetired += len(retiredClients)
	report.LinksRetired += len(retiredLinks)
	counts.PendingRetirements += len(staleClients) + pendingLinks
	counts.Retired += len(retiredClients) + len(retiredLinks)
	return nil
}

// markRequiringUpstreamAuth counts, and with Apply records, that servers
// offering a backfilled auth client of any kind need upstream authentication.
// Only undecided servers change; a decision either way is never overwritten.
func markRequiringUpstreamAuth(ctx context.Context, q *Queries, opts Options, committed *Report, report *Report) error {
	seen := map[uuid.UUID]bool{}
	serverIDs := []uuid.UUID{}
	for _, result := range committed.Results {
		if result.MCPServerID != uuid.Nil && result.Outcome.Covered() && result.Outcome != OutcomeWarning && !seen[result.MCPServerID] {
			seen[result.MCPServerID] = true
			serverIDs = append(serverIDs, result.MCPServerID)
		}
	}
	if len(serverIDs) == 0 {
		return nil
	}
	undecided, err := q.ListUndecidedUpstreamAuthServers(ctx, ListUndecidedUpstreamAuthServersParams{
		OrganizationID: opts.OrganizationID,
		McpServerIds:   serverIDs,
	})
	if err != nil {
		return fmt.Errorf("list servers with undecided upstream auth: %w", err)
	}
	report.PendingMarkedServers = len(undecided)
	if !opts.Apply || len(undecided) == 0 {
		return nil
	}
	marked, err := q.MarkServersRequiringUpstreamAuth(ctx, MarkServersRequiringUpstreamAuthParams{
		OrganizationID: opts.OrganizationID,
		McpServerIds:   undecided,
	})
	if err != nil {
		return fmt.Errorf("mark servers requiring upstream auth: %w", err)
	}
	report.ServersMarked = len(marked)
	return nil
}
