// Package selectionbackfill records, for every live user session credential,
// the upstream OAuth client it uses for each MCP server it can reach, and
// reports the pairs existing configuration does not decide. It records
// through the same statement issuance uses, so a run is idempotent and agrees
// with what a new sign-in would record. It runs after the association
// backfill, since a choice is only recorded for an associated client.
package selectionbackfill

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// defaultPageSize bounds each page of credentials read. Each credential is
// decided and written on its own, so the page size only trades memory for
// round trips.
const defaultPageSize = 200

// Outcome classifies one credential's choice for one reachable server.
type Outcome string

const (
	// OutcomeSelected is a pair that already has a usable choice.
	OutcomeSelected Outcome = "selected"

	// OutcomeUnusableClient is a pair whose chosen client is no longer usable:
	// detached from the server, unbound from the credential's issuer, or
	// deleted. A client choice is never replaced by another client, so the
	// credential must reconnect.
	OutcomeUnusableClient Outcome = "unusable_client"

	// OutcomeClient is a pair whose server has exactly one associated client
	// the credential's issuer also binds; that client is chosen.
	OutcomeClient Outcome = "client"

	// OutcomeNoAuth is a pair whose server has no associated client; an
	// explicit no-auth choice is recorded.
	OutcomeNoAuth Outcome = "no_auth"

	// OutcomeAmbiguous is a pair with several eligible clients. Only the
	// consent page records which one a person uses, so the credential must
	// reconnect to choose one.
	OutcomeAmbiguous Outcome = "ambiguous"

	// OutcomeUnreachableClient is a pair whose server's associated clients
	// are none of them bound to the credential's issuer, so the credential
	// cannot use any of them. The credential must reconnect.
	OutcomeUnreachableClient Outcome = "unreachable_client"
)

// Decided reports whether the outcome records or already has a choice.
func (o Outcome) Decided() bool {
	switch o {
	case OutcomeSelected, OutcomeClient, OutcomeNoAuth:
		return true
	default:
		return false
	}
}

// Classify predicts what RecordUpstreamClientSelections records for one pair.
func Classify(row ListUpstreamClientSelectionCoverageRow) Outcome {
	switch {
	case row.Selected:
		return OutcomeSelected
	case row.ChoseClient:
		return OutcomeUnusableClient
	case row.EligibleClients == 1:
		return OutcomeClient
	case row.EligibleClients > 1:
		return OutcomeAmbiguous
	case row.AssociatedClients > 0:
		return OutcomeUnreachableClient
	default:
		return OutcomeNoAuth
	}
}

// Options controls one run.
type Options struct {
	// OrganizationID limits trusted offline maintenance to one organization
	// when non-empty. Empty is the existing all-organization maintenance mode;
	// request-driven lifecycle reconciliation requires a non-empty tenant.
	OrganizationID string

	// Apply records the decided choices; otherwise the run only predicts.
	Apply bool

	// PageSize overrides defaultPageSize when positive.
	PageSize int
}

// Gap is one credential and server pair left without a choice.
type Gap struct {
	// CredentialID is the user session credential.
	CredentialID uuid.UUID `json:"credential_id"`

	// MCPServerID is the reachable server.
	MCPServerID uuid.UUID `json:"mcp_server_id"`

	// Outcome is why no choice is recorded.
	Outcome Outcome `json:"outcome"`
}

// Report summarizes a run by credential class. Gaps carry ids only.
type Report struct {
	// Mode is "apply" or "dry_run".
	Mode string `json:"mode"`

	// CredentialKind is the class of credential covered.
	CredentialKind string `json:"credential_kind"`

	// Credentials counts live credentials scanned.
	Credentials int `json:"credentials"`

	// FullyCovered counts credentials with a choice for every reachable
	// server after the run, or predicted to have one in a dry run.
	FullyCovered int `json:"fully_covered"`

	// Pairs counts credential and server pairs per outcome, as decided before
	// any write.
	Pairs map[Outcome]int `json:"pairs"`

	// Written counts choices this run recorded.
	Written int64 `json:"written"`

	// Gaps lists every pair without a choice: after the run with Apply,
	// otherwise as predicted. Cutover is blocked until it is empty.
	Gaps []Gap `json:"gaps"`
}

// Run scans live user session credentials and, with Apply, records their
// choices through the same statement issuance uses.
func Run(ctx context.Context, db DBTX, opts Options) (Report, error) {
	pageSize := opts.PageSize
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	report := Report{
		Mode:           "dry_run",
		CredentialKind: upstreamclients.CredentialKindUserSession,
		Credentials:    0,
		FullyCovered:   0,
		Pairs:          make(map[Outcome]int),
		Written:        0,
		Gaps:           []Gap{},
	}
	if opts.Apply {
		report.Mode = "apply"
	}
	q := New(db)
	sessions := usersessionsrepo.New(db)
	cursor := uuid.Nil
	for {
		credentials, err := q.ListLiveUserSessionCredentials(ctx, ListLiveUserSessionCredentialsParams{
			Cursor:         cursor,
			OrganizationID: opts.OrganizationID,
			IssuerID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			PageSize:       int32(pageSize), // #nosec G115 -- bounded by defaultPageSize or an operator flag
		})
		if err != nil {
			return report, fmt.Errorf("list live credentials after %s: %w", cursor, err)
		}
		if len(credentials) == 0 {
			return report, nil
		}
		for _, credential := range credentials {
			coverageParams := ListUpstreamClientSelectionCoverageParams{
				UserSessionIssuerID: credential.UserSessionIssuerID,
				CredentialKind:      upstreamclients.CredentialKindUserSession,
				CredentialID:        credential.CredentialID,
			}
			coverage, err := q.ListUpstreamClientSelectionCoverage(ctx, coverageParams)
			if err != nil {
				return report, fmt.Errorf("read coverage for credential %s: %w", credential.CredentialID, err)
			}
			covered := true
			for _, row := range coverage {
				outcome := Classify(row)
				report.Pairs[outcome]++
				if !opts.Apply && !outcome.Decided() {
					covered = false
					report.Gaps = append(report.Gaps, Gap{CredentialID: credential.CredentialID, MCPServerID: row.McpServerID, Outcome: outcome})
				}
			}
			if opts.Apply {
				written, err := recordCredentialSelections(ctx, sessions, credential.UserSessionIssuerID, []uuid.UUID{credential.CredentialID}, nil)
				if err != nil {
					return report, fmt.Errorf("record selections for credential %s: %w", credential.CredentialID, err)
				}
				report.Written += written
				// Coverage after a write is what was stored, not what was
				// predicted: evidence may change between the read and the
				// write, which then records nothing for the pair.
				persisted, err := q.ListUpstreamClientSelectionCoverage(ctx, coverageParams)
				if err != nil {
					return report, fmt.Errorf("reread coverage for credential %s: %w", credential.CredentialID, err)
				}
				for _, row := range persisted {
					if !row.Selected {
						covered = false
						report.Gaps = append(report.Gaps, Gap{CredentialID: credential.CredentialID, MCPServerID: row.McpServerID, Outcome: Classify(row)})
					}
				}
			}
			report.Credentials++
			if covered {
				report.FullyCovered++
			}
			cursor = credential.CredentialID
		}
		if err := ctx.Err(); err != nil {
			return report, fmt.Errorf("backfill interrupted: %w", err)
		}
	}
}

// ReconcileIssuerInLockedTransaction records missing choices and names typed
// clients on existing choices for one issuer's live credential lineages. The
// caller holds the complete server/source writer scope and registration gates.
// It uses migration selection policy without granting permissions.
func ReconcileIssuerInLockedTransaction(ctx context.Context, tx pgx.Tx, organizationID string, issuerID uuid.UUID) error {
	_, err := ReconcileIssuerScopeInLockedTransaction(ctx, tx, organizationID, issuerID, nil, nil)
	return err
}

// ReconcileIssuerScopeInLockedTransaction limits existing credentials to the
// maintenance servers. Moved lineages instead record full target reach: moving
// their issuer changes their audience even on servers needing no typed sync.
// A nil server scope retains full reach; an empty scope records/maps nothing
// for existing credentials. The returned count reports guarded bulk rows whose
// legacy evidence changed before their mapping write.
func ReconcileIssuerScopeInLockedTransaction(ctx context.Context, tx pgx.Tx, organizationID string, issuerID uuid.UUID, serverIDs, fullReachCredentialIDs []uuid.UUID) (int64, error) {
	if tx == nil || organizationID == "" || issuerID == uuid.Nil {
		return 0, errors.New("reconcile issuer credentials: no transaction or tenant scope")
	}
	if serverIDs != nil && len(serverIDs) == 0 && len(fullReachCredentialIDs) == 0 {
		return 0, nil
	}
	fullReach := make(map[uuid.UUID]bool, len(fullReachCredentialIDs))
	for _, id := range fullReachCredentialIDs {
		fullReach[id] = true
	}
	q, sessions := New(tx), usersessionsrepo.New(tx)
	cursor := uuid.Nil
	for {
		credentials, err := q.ListLiveUserSessionCredentials(ctx, ListLiveUserSessionCredentialsParams{
			Cursor: cursor, OrganizationID: organizationID, PageSize: defaultPageSize, IssuerID: uuid.NullUUID{UUID: issuerID, Valid: true},
		})
		if err != nil {
			return 0, fmt.Errorf("list issuer credentials: %w", err)
		}
		var existingIDs, movedIDs []uuid.UUID
		for _, credential := range credentials {
			if fullReach[credential.CredentialID] {
				movedIDs = append(movedIDs, credential.CredentialID)
			} else {
				existingIDs = append(existingIDs, credential.CredentialID)
			}
			cursor = credential.CredentialID
		}
		if _, err := recordCredentialSelections(ctx, sessions, issuerID, existingIDs, serverIDs); err != nil {
			return 0, fmt.Errorf("record existing issuer credential choices: %w", err)
		}
		if _, err := recordCredentialSelections(ctx, sessions, issuerID, movedIDs, nil); err != nil {
			return 0, fmt.Errorf("record moved issuer credential choices: %w", err)
		}
		if len(credentials) < defaultPageSize {
			break
		}
	}
	skipped, err := authclientbackfill.ReconcileUserSessionSelectionsForServersInLockedTransaction(ctx, tx, organizationID, issuerID, serverIDs)
	if err != nil {
		return skipped, fmt.Errorf("map issuer credential choices: %w", err)
	}
	return skipped, nil
}

func recordCredentialSelections(ctx context.Context, q *usersessionsrepo.Queries, issuerID uuid.UUID, credentialIDs, serverIDs []uuid.UUID) (int64, error) {
	if len(credentialIDs) == 0 || (serverIDs != nil && len(serverIDs) == 0) {
		return 0, nil
	}
	written, err := q.RecordUpstreamClientSelections(ctx, usersessionsrepo.RecordUpstreamClientSelectionsParams{CredentialIds: credentialIDs,
		UserSessionIssuerID: issuerID,
		CredentialKind:      upstreamclients.CredentialKindUserSession,
		CredentialID:        uuid.Nil,
		// These choices are inferred for existing credentials, not explicit
		// administrator choices; consolidation uses the same backfill source.
		Source:             upstreamclients.SelectionSourceBackfill,
		PickedMcpServerIds: []uuid.UUID{}, PickedClientIds: []uuid.UUID{},
		PickedAuthClientIds: nil, ScopeMcpServerIds: serverIDs,
	})
	if err != nil {
		return 0, fmt.Errorf("record credential selections: %w", err)
	}
	return written, nil
}
