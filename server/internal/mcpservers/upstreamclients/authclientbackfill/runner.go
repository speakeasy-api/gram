// Package authclientbackfill records a typed auth client for every upstream
// OAuth client an MCP server is associated with, and links the server to it.
// A subject-owned client becomes a remote_session auth client and a
// self-owned one a service_account auth client. A dry run decides without
// writing. Every write is conditional on the evidence it was decided from, so
// a run is idempotent and a concurrent change is skipped rather than
// overwritten; rerunning picks it up. Each run also retires backfilled
// clients and server links whose association or upstream client is gone, so
// the records track the existing configuration, and marks each server it links
// a client to as needing upstream authentication where that is still
// undecided. Admin-created records are never read or retired here. Nothing reads these records yet: the existing
// resolution stays authoritative.
package authclientbackfill

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// defaultPageSize bounds each page of associations read.
const defaultPageSize = 200

// Client types recorded by this backfill.
const (
	TypeRemoteSession  = "remote_session"
	TypeServiceAccount = "service_account"
)

// SourceBackfill marks clients and links this backfill created; only those
// are found, linked or retired here.
const SourceBackfill = "backfill"

// validType reports whether a client type is one this backfill records.
func validType(clientType string) bool {
	return clientType == TypeRemoteSession || clientType == TypeServiceAccount
}

// Outcome classifies one association.
type Outcome string

const (
	// OutcomeCreated is an association whose auth client this run recorded.
	OutcomeCreated Outcome = "created"

	// OutcomeExisting is an association whose auth client and server link
	// were already recorded.
	OutcomeExisting Outcome = "existing"

	// OutcomeAssociated is an association whose auth client was already
	// recorded and whose server link this run recorded.
	OutcomeAssociated Outcome = "associated"

	// OutcomeAmbiguous is an association whose client's owner is unknown,
	// whose recorded auth client has another type than the client's owner now
	// implies, or whose recorded auth client belongs to another project. It
	// is never guessed.
	OutcomeAmbiguous Outcome = "ambiguous"

	// OutcomeSkippedRace is an association whose evidence changed before a
	// write; rerunning picks it up.
	OutcomeSkippedRace Outcome = "skipped_race"
)

// Covered reports whether an association with this outcome has its auth
// client and server link after the run.
func (o Outcome) Covered() bool {
	return o != OutcomeAmbiguous && o != OutcomeSkippedRace
}

// Options controls one run.
type Options struct {
	// OrganizationID is the one organization the run covers. Required.
	OrganizationID string

	// Apply writes the decided records; otherwise the run only decides.
	Apply bool

	// PageSize overrides defaultPageSize when positive.
	PageSize int
}

// Result is one association's decision.
type Result struct {
	// AssociationID is the server and client association decided.
	AssociationID uuid.UUID `json:"association_id"`

	// MCPServerID is the associated server.
	MCPServerID uuid.UUID `json:"mcp_server_id"`

	// ProjectID is the server's project.
	ProjectID uuid.UUID `json:"project_id"`

	// RemoteSessionClientID is the associated upstream OAuth client.
	RemoteSessionClientID uuid.UUID `json:"remote_session_client_id"`

	// AuthClientID is the auth client recorded for the client, when known.
	AuthClientID uuid.NullUUID `json:"auth_client_id"`

	// Outcome classifies the association.
	Outcome Outcome `json:"outcome"`
}

// Report summarizes a run. Results carry ids only, never names or URLs.
type Report struct {
	// Mode is "apply" or "dry_run".
	Mode string `json:"mode"`

	// Associations counts associations decided.
	Associations int `json:"associations"`

	// Outcomes counts associations per outcome.
	Outcomes map[Outcome]int `json:"outcomes"`

	// PendingClients counts auth clients to record, written or not.
	PendingClients int `json:"pending_clients"`

	// PendingLinks counts server links to record, written or not.
	PendingLinks int `json:"pending_links"`

	// ClientsWritten counts auth clients recorded.
	ClientsWritten int `json:"clients_written"`

	// LinksWritten counts server links recorded.
	LinksWritten int `json:"links_written"`

	// Gaps counts associations left without an auth client or link.
	Gaps int `json:"gaps"`

	// PendingRetiredClients counts backfilled auth clients no live
	// association reaches any more, retired or not.
	PendingRetiredClients int `json:"pending_retired_clients"`

	// PendingRetiredLinks counts backfilled server links whose association or
	// auth client is gone, retired or not.
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

	// Results lists every decided association.
	Results []Result `json:"results"`
}

// clientKey identifies one upstream client within one organization, which is
// what one auth client covers.
type clientKey struct {
	organizationID string
	clientID       uuid.UUID
}

// Run decides every association and, with Apply, writes the decisions.
func Run(ctx context.Context, db DBTX, opts Options) (Report, error) {
	if opts.OrganizationID == "" {
		return Report{}, errors.New("organization id is required")
	}
	pageSize := opts.PageSize
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	report := Report{
		Mode:                  "dry_run",
		Associations:          0,
		Outcomes:              map[Outcome]int{},
		PendingClients:        0,
		PendingLinks:          0,
		ClientsWritten:        0,
		LinksWritten:          0,
		Gaps:                  0,
		PendingRetiredClients: 0,
		PendingRetiredLinks:   0,
		ClientsRetired:        0,
		LinksRetired:          0,
		PendingMarkedServers:  0,
		ServersMarked:         0,
		Results:               []Result{},
	}
	if opts.Apply {
		report.Mode = "apply"
	}
	q := New(db)
	// Auth clients decided earlier in this run, so a client shared by several
	// servers is recorded once. A nil id marks one decided but not written.
	decided := map[clientKey]uuid.UUID{}
	cursor := uuid.Nil
	for {
		rows, err := q.ListAuthClientBackfillCandidates(ctx, ListAuthClientBackfillCandidatesParams{
			Cursor:         cursor,
			OrganizationID: opts.OrganizationID,
			PageSize:       int32(pageSize), // #nosec G115 -- bounded by defaultPageSize or an operator flag
		})
		if err != nil {
			return report, fmt.Errorf("list auth client backfill candidates after %s: %w", cursor, err)
		}
		if len(rows) == 0 {
			if err := reconcile(ctx, q, opts, &report); err != nil {
				return report, err
			}
			if err := markRequiringUpstreamAuth(ctx, q, opts, &report); err != nil {
				return report, err
			}
			return report, nil
		}
		for _, row := range rows {
			result, err := decideAndWrite(ctx, q, opts, row, decided, &report)
			if err != nil {
				return report, err
			}
			report.Associations++
			report.Outcomes[result.Outcome]++
			if !result.Outcome.Covered() {
				report.Gaps++
			}
			report.Results = append(report.Results, result)
			cursor = row.AssociationID
		}
		if err := ctx.Err(); err != nil {
			return report, fmt.Errorf("backfill interrupted: %w", err)
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

func decideAndWrite(ctx context.Context, q *Queries, opts Options, row ListAuthClientBackfillCandidatesRow, decided map[clientKey]uuid.UUID, report *Report) (Result, error) {
	result := Result{
		AssociationID:         row.AssociationID,
		MCPServerID:           row.McpServerID,
		ProjectID:             row.ProjectID,
		RemoteSessionClientID: row.RemoteSessionClientID,
		AuthClientID:          uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Outcome:               OutcomeExisting,
	}
	clientType, ok := Type(row.CredentialOwner)
	if !ok {
		result.Outcome = OutcomeAmbiguous
		return result, nil
	}

	key := clientKey{organizationID: row.OrganizationID, clientID: row.RemoteSessionClientID}
	authClientID := row.AuthClientID
	switch {
	case authClientID != uuid.Nil && (row.AuthClientType != clientType ||
		row.AuthClientProjectID.Valid && row.AuthClientProjectID.UUID != row.ProjectID):
		result.AuthClientID = uuid.NullUUID{UUID: authClientID, Valid: true}
		result.Outcome = OutcomeAmbiguous
		return result, nil
	case authClientID != uuid.Nil:
		// Recorded before this run.
	default:
		id, seen := decided[key]
		switch {
		case seen && id == uuid.Nil:
			// Decided earlier in this dry run; only the link is new.
			report.PendingLinks++
			result.Outcome = OutcomeAssociated
			return result, nil
		case seen:
			authClientID = id
			result.Outcome = OutcomeAssociated
			report.PendingLinks++
		default:
			report.PendingClients++
			report.PendingLinks++
			result.Outcome = OutcomeCreated
			if !opts.Apply {
				decided[key] = uuid.Nil
				return result, nil
			}
			id, err := recordAuthClient(ctx, q, row, clientType)
			switch {
			case errors.Is(err, errRace):
				result.Outcome = OutcomeSkippedRace
				return result, nil
			case err != nil:
				return result, err
			}
			report.ClientsWritten++
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
		report.PendingLinks++
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
	report.LinksWritten++
	return result, nil
}

// errRace reports a decided write whose evidence changed before it.
var errRace = errors.New("evidence changed")

// recordAuthClient records the auth client for one association's client. It
// is scoped like the client: a project client's auth client belongs to that
// project, and an organization or platform-global client's to the
// association's organization.
func recordAuthClient(ctx context.Context, q *Queries, row ListAuthClientBackfillCandidatesRow, clientType string) (uuid.UUID, error) {
	if !validType(clientType) {
		return uuid.Nil, fmt.Errorf("unknown auth client type %q", clientType)
	}
	name := DefaultName(clientType)
	if row.IssuerName.Valid && row.IssuerName.String != "" {
		name = row.IssuerName.String
	}
	id, err := q.CreateAuthClient(ctx, CreateAuthClientParams{
		OrganizationID:        row.OrganizationID,
		ProjectID:             row.ClientProjectID,
		ClientType:            clientType,
		Name:                  name,
		RemoteSessionClientID: row.RemoteSessionClientID,
	})
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return uuid.Nil, errRace
	case errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation:
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

// reconcile counts, and with Apply retires, backfilled auth clients no live
// association reaches and backfilled links whose association or auth client
// is gone. Clients go first so their links are caught in the same run.
func reconcile(ctx context.Context, q *Queries, opts Options, report *Report) error {
	staleClients, err := q.ListStaleBackfillAuthClients(ctx, opts.OrganizationID)
	if err != nil {
		return fmt.Errorf("list stale auth clients: %w", err)
	}
	staleLinks, err := q.ListStaleBackfillLinks(ctx, opts.OrganizationID)
	if err != nil {
		return fmt.Errorf("list stale server links: %w", err)
	}
	report.PendingRetiredClients = len(staleClients)
	report.PendingRetiredLinks = len(staleLinks)
	if !opts.Apply {
		return nil
	}
	retiredClients, err := q.RetireStaleBackfillAuthClients(ctx, opts.OrganizationID)
	if err != nil {
		return fmt.Errorf("retire stale auth clients: %w", err)
	}
	report.ClientsRetired = len(retiredClients)
	retiredLinks, err := q.RetireStaleBackfillLinks(ctx, opts.OrganizationID)
	if err != nil {
		return fmt.Errorf("retire stale server links: %w", err)
	}
	report.LinksRetired = len(retiredLinks)
	// A link may go stale only once its client is retired in this run.
	report.PendingRetiredLinks = max(report.PendingRetiredLinks, report.LinksRetired)
	return nil
}

// markRequiringUpstreamAuth counts, and with Apply records, that servers
// offering a backfilled auth client need upstream authentication. Only
// undecided servers change; a decision either way is never overwritten.
func markRequiringUpstreamAuth(ctx context.Context, q *Queries, opts Options, report *Report) error {
	seen := map[uuid.UUID]bool{}
	serverIDs := []uuid.UUID{}
	for _, result := range report.Results {
		if result.Outcome.Covered() && !seen[result.MCPServerID] {
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
