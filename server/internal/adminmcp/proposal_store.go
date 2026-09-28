package adminmcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	proposalLifetime         = 10 * time.Minute
	maxPendingProposals      = 5
	maxProposalArgumentBytes = MaxBodyBytes
	maxProposalPreviewBytes  = 64 << 10
	maxProposalResultBytes   = 64 << 10
	maxIdempotencyKeyLength  = 128
	proposalDigestVersion    = 1
)

type ProposalStatus string

const (
	ProposalPendingApproval ProposalStatus = "pending_approval"
	ProposalApproved        ProposalStatus = "approved"
	ProposalSucceeded       ProposalStatus = "succeeded"
	ProposalRejected        ProposalStatus = "rejected"
	ProposalExpired         ProposalStatus = "expired"
	ProposalInvalidated     ProposalStatus = "invalidated"
)

// Bounded reason codes. These are the only values written to the staff event
// trail; free text, arguments and results never are.
const (
	reasonConnectionChanged = "connection_changed"
	reasonDigestMismatch    = "digest_mismatch"
	reasonStaleState        = "stale_state"
	reasonExpired           = "expired"
	reasonOperationFailed   = "operation_failed"
	reasonNotApproved       = "not_approved"
)

var (
	ErrProposalNotFound     = errors.New("write proposal not found")
	ErrProposalConflict     = errors.New("this idempotency key was already used for a different target or change")
	ErrTooManyPending       = errors.New("too many pending write proposals; approve, reject or let some expire first")
	ErrProposalNotApproved  = errors.New("write proposal has not been approved in the admin site")
	ErrProposalClosed       = errors.New("write proposal is no longer executable")
	ErrProposalExpired      = errors.New("write proposal expired; prepare a new one")
	ErrProposalInvalidated  = errors.New("write proposal was invalidated; prepare a new one")
	ErrProposalChanged      = errors.New("the approval page is out of date; reload it")
	ErrConnectionChanged    = errors.New("the staff connection changed since this proposal was prepared")
	ErrStaleState           = errors.New("target state changed since the proposal was prepared")
	ErrOutcomeUnknown       = errors.New("write outcome unknown: the commit was not confirmed; check the proposal status before preparing another change")
	errInvalidProposalInput = errors.New("invalid write proposal")
)

// ProposalTarget is the canonical target resolved by the server. Tenant
// operations name exactly one organisation; global operations name none.
type ProposalTarget struct {
	OrganizationID string
	ProjectID      uuid.NullUUID
	ResourceKind   string
	ResourceID     string
}

// NewProposal is a server-built proposal. Arguments, expected state and the
// preview are produced by the operation from exact reads, never copied from a
// model without normalisation.
type NewProposal struct {
	Operation      WriteOperation
	SchemaVersion  int
	Target         ProposalTarget
	IdempotencyKey string
	Arguments      json.RawMessage
	ExpectedState  json.RawMessage
	Preview        json.RawMessage
}

type Proposal struct {
	ID                  uuid.UUID
	SubjectURN          string
	OAuthClientID       uuid.UUID
	ConnectionID        uuid.UUID
	Generation          uuid.UUID
	Operation           WriteOperation
	SchemaVersion       int
	PlatformGlobal      bool
	Target              ProposalTarget
	IdempotencyKey      string
	Arguments           json.RawMessage
	ExpectedStateDigest string
	ProposalDigest      string
	Preview             json.RawMessage
	Status              ProposalStatus
	ExpiresAt           time.Time
	ApprovedBy          string
	ApprovedAt          *time.Time
	RejectedAt          *time.Time
	InvalidatedAt       *time.Time
	InvalidationReason  string
	ExecutedAt          *time.Time
	ResultCode          string
	ResultPayload       json.RawMessage
	CreatedAt           time.Time
}

// proposalOwner is the identity a proposal is bound to, derived only from the
// authenticated principal.
type proposalOwner struct {
	SubjectURN   string
	ClientRowID  uuid.UUID
	ConnectionID uuid.UUID
	Generation   uuid.UUID
}

func ownerFromAuthority(authority writeAuthority) (proposalOwner, error) {
	clientRowID, err := uuid.Parse(authority.Principal.ClientRowID)
	if err != nil {
		return proposalOwner{}, ErrWriteIdentity
	}
	connectionID, err := uuid.Parse(authority.Principal.ConnectionID)
	if err != nil {
		return proposalOwner{}, ErrWriteIdentity
	}
	generation, err := uuid.Parse(authority.Principal.Generation)
	if err != nil {
		return proposalOwner{}, ErrWriteIdentity
	}
	return proposalOwner{SubjectURN: authority.Principal.Subject, ClientRowID: clientRowID, ConnectionID: connectionID, Generation: generation}, nil
}

// ProposalRunner performs the business change inside the execution
// transaction. It must revalidate the target and expected state under row
// locks, return ErrStaleState on drift, and return a bounded result.
type ProposalRunner func(ctx context.Context, tx pgx.Tx, proposal Proposal) (resultCode string, result json.RawMessage, err error)

// ProposalRevalidator checks target and expected state before approval.
type ProposalRevalidator func(ctx context.Context, tx pgx.Tx, proposal Proposal) error

// ProposalExecution describes how one stored proposal is executed.
//
// Lock, when set, acquires connection-scoped locks (such as feature cache
// advisory locks) before the execution transaction begins. The transaction
// runs on the returned connection, and release is called only after
// AfterCommit. Lock sees an unlocked read of the proposal; Execute refuses the
// run if the locked row's digest differs from what Lock saw.
//
// AfterCommit runs once after a confirmed commit, while any locks are still
// held. It must not fail the operation: the change is already durable.
type ProposalExecution struct {
	Lock        func(ctx context.Context, proposal Proposal) (*pgxpool.Conn, func(), error)
	Run         ProposalRunner
	AfterCommit func(ctx context.Context, receipt Proposal)
}

type proposalStore struct {
	db     *pgxpool.Pool
	logger *slog.Logger
}

func newProposalStore(db *pgxpool.Pool, logger *slog.Logger) *proposalStore {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &proposalStore{db: db, logger: logger}
}

const proposalColumns = `id, subject_urn, oauth_client_id, connection_id, connection_generation, operation,
 operation_schema_version, platform_global, organization_id, project_id, resource_kind, resource_id,
 idempotency_key, arguments, expected_state_digest, proposal_digest, preview, status, expires_at,
 approved_by_subject_urn, approved_at, rejected_at, invalidated_at, invalidation_reason, executed_at,
 result_code, result_payload, created_at`

func scanProposal(row pgx.Row) (Proposal, error) {
	var p Proposal
	var operation, status string
	var organizationID, resourceKind, resourceID, approvedBy, invalidationReason, resultCode *string
	var arguments, preview, result []byte
	err := row.Scan(&p.ID, &p.SubjectURN, &p.OAuthClientID, &p.ConnectionID, &p.Generation, &operation,
		&p.SchemaVersion, &p.PlatformGlobal, &organizationID, &p.Target.ProjectID, &resourceKind, &resourceID,
		&p.IdempotencyKey, &arguments, &p.ExpectedStateDigest, &p.ProposalDigest, &preview, &status, &p.ExpiresAt,
		&approvedBy, &p.ApprovedAt, &p.RejectedAt, &p.InvalidatedAt, &invalidationReason, &p.ExecutedAt,
		&resultCode, &result, &p.CreatedAt)
	if err != nil {
		return Proposal{}, fmt.Errorf("scan write proposal: %w", err)
	}
	p.Operation = WriteOperation(operation)
	p.Status = ProposalStatus(status)
	p.Target.OrganizationID = deref(organizationID)
	p.Target.ResourceKind = deref(resourceKind)
	p.Target.ResourceID = deref(resourceID)
	p.ApprovedBy = deref(approvedBy)
	p.InvalidationReason = deref(invalidationReason)
	p.ResultCode = deref(resultCode)
	p.Arguments = arguments
	p.Preview = preview
	if result != nil {
		p.ResultPayload = result
	}
	return p, nil
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func nullableText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// canonicalJSON produces a stable encoding so digests survive the JSONB round
// trip, which reorders keys and drops insignificant whitespace.
func canonicalJSON(raw []byte, limit int) ([]byte, error) {
	if len(raw) == 0 || len(raw) > limit {
		return nil, errInvalidProposalInput
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, errInvalidProposalInput
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errInvalidProposalInput
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode canonical JSON: %w", err)
	}
	return encoded, nil
}

func stateDigest(raw json.RawMessage) (string, error) {
	canonical, err := canonicalJSON(raw, maxProposalArgumentBytes)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func computeProposalDigest(p Proposal) (string, error) {
	arguments, err := canonicalJSON(p.Arguments, maxProposalArgumentBytes)
	if err != nil {
		return "", err
	}
	preview, err := canonicalJSON(p.Preview, maxProposalPreviewBytes)
	if err != nil {
		return "", err
	}
	projectID := ""
	if p.Target.ProjectID.Valid {
		projectID = p.Target.ProjectID.UUID.String()
	}
	encoded, err := json.Marshal(struct {
		Version        int             `json:"v"`
		Subject        string          `json:"subject"`
		Client         string          `json:"client"`
		Connection     string          `json:"connection"`
		Generation     string          `json:"generation"`
		Operation      string          `json:"operation"`
		SchemaVersion  int             `json:"schema_version"`
		PlatformGlobal bool            `json:"platform_global"`
		Organization   string          `json:"organization"`
		Project        string          `json:"project"`
		ResourceKind   string          `json:"resource_kind"`
		ResourceID     string          `json:"resource_id"`
		Arguments      json.RawMessage `json:"arguments"`
		ExpectedState  string          `json:"expected_state"`
		Preview        json.RawMessage `json:"preview"`
	}{
		Version: proposalDigestVersion, Subject: p.SubjectURN, Client: p.OAuthClientID.String(),
		Connection: p.ConnectionID.String(), Generation: p.Generation.String(), Operation: string(p.Operation),
		SchemaVersion: p.SchemaVersion, PlatformGlobal: p.PlatformGlobal, Organization: p.Target.OrganizationID,
		Project: projectID, ResourceKind: p.Target.ResourceKind, ResourceID: p.Target.ResourceID,
		Arguments: arguments, ExpectedState: p.ExpectedStateDigest, Preview: preview,
	})
	if err != nil {
		return "", fmt.Errorf("encode proposal digest input: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func digestsEqual(a, b string) bool {
	return a != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func (input NewProposal) validate() error {
	if !input.Operation.known() || input.SchemaVersion <= 0 {
		return errInvalidProposalInput
	}
	key := strings.TrimSpace(input.IdempotencyKey)
	if key == "" || key != input.IdempotencyKey || len(key) > maxIdempotencyKeyLength || !utf8.ValidString(key) {
		return errInvalidProposalInput
	}
	target := input.Target
	if (target.ResourceKind == "") != (target.ResourceID == "") {
		return errInvalidProposalInput
	}
	if input.Operation.PlatformGlobal() {
		if target.OrganizationID != "" || target.ProjectID.Valid {
			return errInvalidProposalInput
		}
	} else if target.OrganizationID == "" {
		return errInvalidProposalInput
	}
	return nil
}

func sameProposalRequest(existing, requested Proposal) bool {
	existingArgs, err := canonicalJSON(existing.Arguments, maxProposalArgumentBytes)
	if err != nil {
		return false
	}
	requestedArgs, err := canonicalJSON(requested.Arguments, maxProposalArgumentBytes)
	if err != nil {
		return false
	}
	return existing.Operation == requested.Operation && existing.SchemaVersion == requested.SchemaVersion &&
		existing.PlatformGlobal == requested.PlatformGlobal && existing.Target == requested.Target &&
		bytes.Equal(existingArgs, requestedArgs)
}

type pgExecer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

func recordWriteEvent(ctx context.Context, db pgExecer, proposalID uuid.NullUUID, subjectURN string, clientRowID uuid.NullUUID, event, reason string) error {
	_, err := db.Exec(ctx, `
INSERT INTO admin_mcp_write_events (proposal_id, subject_urn, oauth_client_id, event, reason_code)
VALUES ($1, $2, $3, $4, $5)
`, proposalID, subjectURN, clientRowID, event, nullableText(reason))
	if err != nil {
		return fmt.Errorf("record admin MCP write event: %w", err)
	}
	return nil
}

func proposalEventIDs(p Proposal) (uuid.NullUUID, uuid.NullUUID) {
	return uuid.NullUUID{UUID: p.ID, Valid: true}, uuid.NullUUID{UUID: p.OAuthClientID, Valid: true}
}

// lockActiveConnection serialises proposal work with reconsent and revocation,
// which update the same connection row. Share mode lets concurrent executes on
// one connection proceed while still blocking a generation change.
func lockActiveConnection(ctx context.Context, tx pgx.Tx, subjectURN string, clientRowID, connectionID, generation uuid.UUID, exclusive bool) error {
	lock := "FOR SHARE"
	if exclusive {
		lock = "FOR UPDATE"
	}
	// Consent takes the client row before the connection row. Use the same
	// order, and never take an exclusive client lock shared by other staff.
	var clientID uuid.UUID
	err := tx.QueryRow(ctx, `
SELECT id FROM admin_mcp_oauth_clients
WHERE id = $1 AND revoked_at IS NULL
  AND (client_secret_expires_at IS NULL OR client_secret_expires_at > clock_timestamp())
FOR SHARE`, clientRowID).Scan(&clientID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrConnectionChanged
	}
	if err != nil {
		return fmt.Errorf("lock admin MCP client: %w", err)
	}
	var active uuid.UUID
	err = tx.QueryRow(ctx, `
SELECT active_generation FROM admin_mcp_connections
WHERE id = $1 AND oauth_client_id = $2 AND subject_urn = $3
  AND revoked_at IS NULL AND reauthorization_required_at IS NULL
  AND authorization_expires_at > clock_timestamp()
  AND scopes @> ARRAY['admin:read', 'admin:write']::text[]
`+lock, connectionID, clientRowID, subjectURN).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrConnectionChanged
	}
	if err != nil {
		return fmt.Errorf("lock admin MCP connection: %w", err)
	}
	if active != generation {
		return ErrConnectionChanged
	}
	return nil
}

// Create stores a pending proposal, or returns the existing proposal for the
// same retry key when the target and change match.
func (s *proposalStore) Create(ctx context.Context, owner proposalOwner, input NewProposal, now time.Time) (Proposal, bool, error) {
	if err := input.validate(); err != nil {
		return Proposal{}, false, err
	}
	arguments, err := canonicalJSON(input.Arguments, maxProposalArgumentBytes)
	if err != nil {
		return Proposal{}, false, err
	}
	preview, err := canonicalJSON(input.Preview, maxProposalPreviewBytes)
	if err != nil {
		return Proposal{}, false, err
	}
	expected, err := stateDigest(input.ExpectedState)
	if err != nil {
		return Proposal{}, false, err
	}
	requested := Proposal{ //nolint:exhaustruct // Persistence assigns ID and terminal receipt fields.
		SubjectURN: owner.SubjectURN, OAuthClientID: owner.ClientRowID, ConnectionID: owner.ConnectionID,
		Generation: owner.Generation, Operation: input.Operation, SchemaVersion: input.SchemaVersion,
		PlatformGlobal: input.Operation.PlatformGlobal(), Target: input.Target, IdempotencyKey: input.IdempotencyKey,
		Arguments: arguments, ExpectedStateDigest: expected, Preview: preview, Status: ProposalPendingApproval,
		ExpiresAt: now.Add(proposalLifetime),
	}
	requested.ProposalDigest, err = computeProposalDigest(requested)
	if err != nil {
		return Proposal{}, false, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Proposal{}, false, fmt.Errorf("begin write proposal: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if err := lockActiveConnection(ctx, tx, owner.SubjectURN, owner.ClientRowID, owner.ConnectionID, owner.Generation, true); err != nil {
		return Proposal{}, false, err
	}
	existing, found, err := s.findByKey(ctx, tx, requested)
	if err != nil {
		return Proposal{}, false, err
	}
	if found {
		if !sameProposalRequest(existing, requested) {
			return Proposal{}, false, ErrProposalConflict
		}
		return existing, true, nil
	}
	var pending int
	if err := tx.QueryRow(ctx, `
SELECT count(*) FROM admin_mcp_write_proposals
WHERE subject_urn = $1 AND oauth_client_id = $2 AND status = 'pending_approval' AND expires_at > $3
`, owner.SubjectURN, owner.ClientRowID, now).Scan(&pending); err != nil {
		return Proposal{}, false, fmt.Errorf("count pending write proposals: %w", err)
	}
	if pending >= maxPendingProposals {
		return Proposal{}, false, ErrTooManyPending
	}
	created, err := scanProposal(tx.QueryRow(ctx, `
INSERT INTO admin_mcp_write_proposals
 (subject_urn, oauth_client_id, connection_id, connection_generation, operation, operation_schema_version,
  platform_global, organization_id, project_id, resource_kind, resource_id, idempotency_key, arguments,
  expected_state_digest, proposal_digest, preview, status, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, 'pending_approval', $17)
ON CONFLICT (subject_urn, oauth_client_id, operation, idempotency_key) DO NOTHING
RETURNING `+proposalColumns,
		requested.SubjectURN, requested.OAuthClientID, requested.ConnectionID, requested.Generation,
		string(requested.Operation), requested.SchemaVersion, requested.PlatformGlobal,
		nullableText(requested.Target.OrganizationID), requested.Target.ProjectID,
		nullableText(requested.Target.ResourceKind), nullableText(requested.Target.ResourceID),
		requested.IdempotencyKey, []byte(requested.Arguments), requested.ExpectedStateDigest,
		requested.ProposalDigest, []byte(requested.Preview), requested.ExpiresAt))
	if errors.Is(err, pgx.ErrNoRows) {
		// A concurrent request won the retry key; answer from its row.
		existing, found, err := s.findByKey(ctx, tx, requested)
		if err != nil || !found {
			return Proposal{}, false, errors.Join(ErrProposalConflict, err)
		}
		if !sameProposalRequest(existing, requested) {
			return Proposal{}, false, ErrProposalConflict
		}
		return existing, true, nil
	}
	if err != nil {
		return Proposal{}, false, err
	}
	proposalID, clientID := proposalEventIDs(created)
	if err := recordWriteEvent(ctx, tx, proposalID, created.SubjectURN, clientID, "prepared", ""); err != nil {
		return Proposal{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Proposal{}, false, fmt.Errorf("commit write proposal: %w", err)
	}
	return created, false, nil
}

func (s *proposalStore) findByKey(ctx context.Context, tx pgx.Tx, p Proposal) (Proposal, bool, error) {
	existing, err := scanProposal(tx.QueryRow(ctx, `SELECT `+proposalColumns+` FROM admin_mcp_write_proposals
WHERE subject_urn = $1 AND oauth_client_id = $2 AND operation = $3 AND idempotency_key = $4`,
		p.SubjectURN, p.OAuthClientID, string(p.Operation), p.IdempotencyKey))
	if errors.Is(err, pgx.ErrNoRows) {
		return Proposal{}, false, nil //nolint:exhaustruct // A missing row has no proposal.
	}
	if err != nil {
		return Proposal{}, false, err
	}
	return existing, true, nil
}

// GetForSubject returns a proposal only to the staff subject that prepared it.
// Another subject's proposal is indistinguishable from a missing one.
func (s *proposalStore) GetForSubject(ctx context.Context, id uuid.UUID, subjectURN string) (Proposal, error) {
	p, err := scanProposal(s.db.QueryRow(ctx, `SELECT `+proposalColumns+` FROM admin_mcp_write_proposals WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && p.SubjectURN != subjectURN) {
		return Proposal{}, ErrProposalNotFound
	}
	if err != nil {
		return Proposal{}, err
	}
	return p, nil
}

// GetForOwner requires the live owning connection and generation for MCP reads.
func (s *proposalStore) GetForOwner(ctx context.Context, id uuid.UUID, owner proposalOwner) (Proposal, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Proposal{}, fmt.Errorf("begin proposal lookup: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	p, err := scanProposal(tx.QueryRow(ctx, `SELECT `+proposalColumns+` FROM admin_mcp_write_proposals WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Proposal{}, ErrProposalNotFound
	}
	if err != nil {
		return Proposal{}, err
	}
	if p.SubjectURN != owner.SubjectURN || p.OAuthClientID != owner.ClientRowID || p.ConnectionID != owner.ConnectionID || p.Generation != owner.Generation {
		return Proposal{}, ErrProposalNotFound
	}
	if err := lockActiveConnection(ctx, tx, owner.SubjectURN, owner.ClientRowID, owner.ConnectionID, owner.Generation, false); err != nil {
		return Proposal{}, err
	}
	return p, nil
}

func lockProposal(ctx context.Context, tx pgx.Tx, id uuid.UUID) (Proposal, error) {
	p, err := scanProposal(tx.QueryRow(ctx, `SELECT `+proposalColumns+` FROM admin_mcp_write_proposals WHERE id = $1 FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Proposal{}, ErrProposalNotFound
	}
	return p, err
}

func closedError(status ProposalStatus) error {
	switch status {
	case ProposalExpired:
		return ErrProposalExpired
	case ProposalInvalidated:
		return ErrProposalInvalidated
	case ProposalPendingApproval:
		return ErrProposalNotApproved
	default:
		return ErrProposalClosed
	}
}

// closeProposal records a terminal state in its own transaction, so the
// refusal survives the rollback of the transaction that discovered it.
func (s *proposalStore) closeProposal(ctx context.Context, p Proposal, status ProposalStatus, reason string) {
	ctx = context.WithoutCancel(ctx)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "begin admin MCP proposal close", attr.SlogError(err))
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
UPDATE admin_mcp_write_proposals
SET status = $2,
    invalidated_at = CASE WHEN $2 = 'invalidated' THEN clock_timestamp() ELSE invalidated_at END,
    invalidation_reason = CASE WHEN $2 = 'invalidated' THEN $3 ELSE invalidation_reason END,
    updated_at = clock_timestamp()
WHERE id = $1 AND status IN ('pending_approval', 'approved')
`, p.ID, string(status), reason)
	if err != nil {
		s.logger.ErrorContext(ctx, "close admin MCP proposal", attr.SlogError(err))
		return
	}
	if tag.RowsAffected() == 1 {
		proposalID, clientID := proposalEventIDs(p)
		if err := recordWriteEvent(ctx, tx, proposalID, p.SubjectURN, clientID, string(status), reason); err != nil {
			s.logger.ErrorContext(ctx, "record admin MCP proposal close", attr.SlogError(err))
			return
		}
	}
	if err := tx.Commit(ctx); err != nil {
		s.logger.ErrorContext(ctx, "commit admin MCP proposal close", attr.SlogError(err))
	}
}

func (s *proposalStore) recordRefusal(ctx context.Context, p Proposal, reason string) {
	proposalID, clientID := proposalEventIDs(p)
	if err := recordWriteEvent(context.WithoutCancel(ctx), s.db, proposalID, p.SubjectURN, clientID, "execute_refused", reason); err != nil {
		s.logger.ErrorContext(ctx, "record admin MCP refusal", attr.SlogError(err))
	}
}

// Approve records the same staff subject's approval of the exact stored
// proposal the browser displayed. displayedDigest comes from the approval form
// and binds the decision to what was rendered. Browser verification must also
// match the linked, currently scoped connection inside this transaction.
func (s *proposalStore) Approve(ctx context.Context, id uuid.UUID, approverSubjectURN, displayedDigest string, now time.Time, verifyBrowser ProposalRevalidator, revalidate ProposalRevalidator) (Proposal, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Proposal{}, fmt.Errorf("begin proposal approval: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	p, err := lockProposal(ctx, tx, id)
	if err != nil {
		return Proposal{}, err
	}
	if p.SubjectURN != approverSubjectURN {
		return Proposal{}, ErrProposalNotFound
	}
	if p.Status != ProposalPendingApproval {
		if p.Status == ProposalApproved && digestsEqual(p.ProposalDigest, displayedDigest) {
			return p, nil
		}
		return Proposal{}, closedError(p.Status)
	}
	if !digestsEqual(p.ProposalDigest, displayedDigest) {
		return Proposal{}, ErrProposalChanged
	}
	fail := func(status ProposalStatus, reason string, cause error) (Proposal, error) {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		s.closeProposal(ctx, p, status, reason)
		return Proposal{}, cause
	}
	if !now.Before(p.ExpiresAt) {
		return fail(ProposalExpired, reasonExpired, ErrProposalExpired)
	}
	if recomputed, err := computeProposalDigest(p); err != nil || !digestsEqual(recomputed, p.ProposalDigest) {
		return fail(ProposalInvalidated, reasonDigestMismatch, ErrProposalInvalidated)
	}
	if err := lockActiveConnection(ctx, tx, p.SubjectURN, p.OAuthClientID, p.ConnectionID, p.Generation, false); err != nil {
		if errors.Is(err, ErrConnectionChanged) {
			return fail(ProposalInvalidated, reasonConnectionChanged, ErrConnectionChanged)
		}
		return Proposal{}, err
	}
	if verifyBrowser == nil {
		return Proposal{}, ErrWriteIdentity
	}
	if err := verifyBrowser(ctx, tx, p); err != nil {
		return Proposal{}, err
	}
	if revalidate == nil {
		return Proposal{}, ErrWriteDisabled
	}
	if err := revalidate(ctx, tx, p); err != nil {
		if errors.Is(err, ErrStaleState) {
			return fail(ProposalInvalidated, reasonStaleState, ErrStaleState)
		}
		return Proposal{}, err
	}
	approved, err := scanProposal(tx.QueryRow(ctx, `
UPDATE admin_mcp_write_proposals
SET status = 'approved', approved_by_subject_urn = $2, approved_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE id = $1
RETURNING `+proposalColumns, p.ID, approverSubjectURN))
	if err != nil {
		return Proposal{}, err
	}
	proposalID, clientID := proposalEventIDs(p)
	if err := recordWriteEvent(ctx, tx, proposalID, p.SubjectURN, clientID, "approved", ""); err != nil {
		return Proposal{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Proposal{}, fmt.Errorf("commit proposal approval: %w", err)
	}
	return approved, nil
}

// Reject cancels a pending or approved, unexecuted proposal.
func (s *proposalStore) Reject(ctx context.Context, id uuid.UUID, subjectURN string) (Proposal, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Proposal{}, fmt.Errorf("begin proposal rejection: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	p, err := lockProposal(ctx, tx, id)
	if err != nil {
		return Proposal{}, err
	}
	if p.SubjectURN != subjectURN {
		return Proposal{}, ErrProposalNotFound
	}
	if p.Status == ProposalRejected {
		return p, nil
	}
	if p.Status != ProposalPendingApproval && p.Status != ProposalApproved {
		return Proposal{}, closedError(p.Status)
	}
	rejected, err := scanProposal(tx.QueryRow(ctx, `
UPDATE admin_mcp_write_proposals
SET status = 'rejected', rejected_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE id = $1
RETURNING `+proposalColumns, p.ID))
	if err != nil {
		return Proposal{}, err
	}
	proposalID, clientID := proposalEventIDs(p)
	if err := recordWriteEvent(ctx, tx, proposalID, p.SubjectURN, clientID, "rejected", ""); err != nil {
		return Proposal{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Proposal{}, fmt.Errorf("commit proposal rejection: %w", err)
	}
	return rejected, nil
}

// replay returns a committed receipt only while the owning connection is still active.
func (s *proposalStore) replay(ctx context.Context, owner proposalOwner, id uuid.UUID) (Proposal, bool, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Proposal{}, false, fmt.Errorf("begin proposal replay: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	p, err := lockProposal(ctx, tx, id)
	if err != nil {
		return Proposal{}, false, err
	}
	if p.SubjectURN != owner.SubjectURN || p.OAuthClientID != owner.ClientRowID || p.ConnectionID != owner.ConnectionID || p.Generation != owner.Generation {
		return Proposal{}, false, ErrProposalNotFound
	}
	if p.Status != ProposalSucceeded {
		return Proposal{}, false, ErrProposalChanged
	}
	if err := lockActiveConnection(ctx, tx, owner.SubjectURN, owner.ClientRowID, owner.ConnectionID, owner.Generation, false); err != nil {
		return Proposal{}, false, err
	}
	return p, true, nil
}

// Execute runs only the stored proposal. The approval is consumed, the
// business change is made and the receipt is written in one transaction. A
// completed proposal replays its receipt and never writes again.
func (s *proposalStore) Execute(ctx context.Context, owner proposalOwner, id uuid.UUID, now time.Time, execution ProposalExecution) (Proposal, bool, error) {
	if execution.Run == nil {
		return Proposal{}, false, errInvalidProposalInput
	}
	var lockedDigest string
	var tx pgx.Tx
	var err error
	if execution.Lock != nil {
		pre, err := s.GetForOwner(ctx, id, owner)
		if err != nil {
			return Proposal{}, false, err
		}
		if pre.Status == ProposalSucceeded {
			return s.replay(ctx, owner, id)
		}
		conn, release, err := execution.Lock(ctx, pre)
		if err != nil {
			return Proposal{}, false, fmt.Errorf("acquire proposal execution locks: %w", err)
		}
		// Deferred first so it runs last: after rollback and AfterCommit.
		defer release()
		lockedDigest = pre.ProposalDigest
		tx, err = conn.Begin(ctx)
		if err != nil {
			return Proposal{}, false, fmt.Errorf("begin proposal execution: %w", err)
		}
	} else {
		tx, err = s.db.Begin(ctx)
		if err != nil {
			return Proposal{}, false, fmt.Errorf("begin proposal execution: %w", err)
		}
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	p, err := lockProposal(ctx, tx, id)
	if err != nil {
		return Proposal{}, false, err
	}
	if p.SubjectURN != owner.SubjectURN || p.OAuthClientID != owner.ClientRowID {
		return Proposal{}, false, ErrProposalNotFound
	}
	if p.Status == ProposalSucceeded {
		if p.Generation != owner.Generation || p.ConnectionID != owner.ConnectionID {
			return Proposal{}, false, ErrConnectionChanged
		}
		if err := lockActiveConnection(ctx, tx, owner.SubjectURN, owner.ClientRowID, owner.ConnectionID, owner.Generation, false); err != nil {
			return Proposal{}, false, err
		}
		return p, true, nil
	}
	refuse := func(reason string, cause error) (Proposal, bool, error) {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		s.recordRefusal(ctx, p, reason)
		return Proposal{}, false, cause
	}
	fail := func(status ProposalStatus, reason string, cause error) (Proposal, bool, error) {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		s.closeProposal(ctx, p, status, reason)
		return Proposal{}, false, cause
	}
	if p.Status != ProposalApproved {
		if p.Status == ProposalPendingApproval {
			if !now.Before(p.ExpiresAt) {
				return fail(ProposalExpired, reasonExpired, ErrProposalExpired)
			}
			return refuse(reasonNotApproved, ErrProposalNotApproved)
		}
		return Proposal{}, false, closedError(p.Status)
	}
	if !now.Before(p.ExpiresAt) {
		return fail(ProposalExpired, reasonExpired, ErrProposalExpired)
	}
	if p.Generation != owner.Generation || p.ConnectionID != owner.ConnectionID {
		return fail(ProposalInvalidated, reasonConnectionChanged, ErrConnectionChanged)
	}
	if recomputed, err := computeProposalDigest(p); err != nil || !digestsEqual(recomputed, p.ProposalDigest) {
		return fail(ProposalInvalidated, reasonDigestMismatch, ErrProposalInvalidated)
	}
	if lockedDigest != "" && !digestsEqual(lockedDigest, p.ProposalDigest) {
		return fail(ProposalInvalidated, reasonDigestMismatch, ErrProposalInvalidated)
	}
	if err := lockActiveConnection(ctx, tx, owner.SubjectURN, owner.ClientRowID, owner.ConnectionID, owner.Generation, false); err != nil {
		if errors.Is(err, ErrConnectionChanged) {
			return fail(ProposalInvalidated, reasonConnectionChanged, ErrConnectionChanged)
		}
		return Proposal{}, false, err
	}
	resultCode, result, runErr := execution.Run(ctx, tx, p)
	if runErr != nil {
		if errors.Is(runErr, ErrStaleState) {
			return fail(ProposalInvalidated, reasonStaleState, runErr)
		}
		refused, replay, err := refuse(reasonOperationFailed, runErr)
		return refused, replay, err
	}
	if resultCode == "" {
		resultCode = "succeeded"
	}
	var resultBytes []byte
	if len(result) > 0 {
		resultBytes, err = canonicalJSON(result, maxProposalResultBytes)
		if err != nil {
			return refuse(reasonOperationFailed, fmt.Errorf("operation result is not bounded JSON: %w", err))
		}
	}
	succeeded, err := scanProposal(tx.QueryRow(ctx, `
UPDATE admin_mcp_write_proposals
SET status = 'succeeded', executed_at = clock_timestamp(), result_code = $2, result_payload = $3, updated_at = clock_timestamp()
WHERE id = $1 AND status = 'approved'
RETURNING `+proposalColumns, p.ID, resultCode, resultBytes))
	if err != nil {
		return Proposal{}, false, fmt.Errorf("record proposal receipt: %w", err)
	}
	proposalID, clientID := proposalEventIDs(p)
	if err := recordWriteEvent(ctx, tx, proposalID, p.SubjectURN, clientID, "executed", ""); err != nil {
		return Proposal{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Proposal{}, false, errors.Join(ErrOutcomeUnknown, err)
	}
	if execution.AfterCommit != nil {
		execution.AfterCommit(context.WithoutCancel(ctx), succeeded)
	}
	return succeeded, false, nil
}
