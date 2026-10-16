// Package associationbackfill records the MCP server upstream OAuth client
// associations that existing configuration proves, and reports the servers
// whose configuration proves none. A dry run decides without writing. Every
// write is conditional on the evidence it was decided from, so a run is
// idempotent and a concurrent configuration change is skipped rather than
// overwritten; rerunning picks it up. An ambiguous server is never guessed:
// it stays a gap until an administrator configures it.
package associationbackfill

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// defaultPageSize bounds each page of servers read; writes are single
// conditional statements, so the page size only trades memory for round trips.
const defaultPageSize = 200

// Outcome classifies one server.
type Outcome string

const (
	// OutcomeUnbound is a server whose user session issuer binds no client.
	// Nothing is associated. This is not evidence that the server needs no
	// upstream authentication.
	OutcomeUnbound Outcome = "unbound"

	// OutcomeSoleConsumer is a server that is its issuer's only consumer, so
	// every client bound to the issuer is meant for it.
	OutcomeSoleConsumer Outcome = "sole_consumer"

	// OutcomeMetadataMatch is a server on a shared issuer whose own upstream
	// advertises exactly one of the issuer's clients' authorization servers.
	OutcomeMetadataMatch Outcome = "metadata_match"

	// OutcomeConfigured is a server on a shared issuer that already has an
	// explicitly configured association and no further proven one.
	OutcomeConfigured Outcome = "configured"

	// OutcomeAmbiguous is a server on a shared issuer whose configuration
	// proves no single client and that has no association. It needs explicit
	// configuration before cutover.
	OutcomeAmbiguous Outcome = "ambiguous"
)

// Covered reports whether a server with this outcome has its associations
// after the run.
func (o Outcome) Covered() bool {
	return o != OutcomeAmbiguous
}

// Options controls one run.
type Options struct {
	// OrganizationID limits the run to one organization when non-empty.
	OrganizationID string

	// Apply writes the decided associations; otherwise the run only decides.
	Apply bool

	// PageSize overrides defaultPageSize when positive.
	PageSize int
}

// ServerResult is one server's decision.
type ServerResult struct {
	// MCPServerID is the server decided.
	MCPServerID uuid.UUID `json:"mcp_server_id"`

	// ProjectID is the server's project.
	ProjectID uuid.UUID `json:"project_id"`

	// Outcome classifies the server.
	Outcome Outcome `json:"outcome"`

	// Associate lists the decided clients the server is not yet associated
	// with.
	Associate []uuid.UUID `json:"associate,omitempty"`

	// RespectRemoved makes each write yield to a removed association for the
	// pair. Only a binding on an issuer the server alone consumes overrides a
	// removal, since that binding is meant for the server whatever happened
	// before.
	RespectRemoved bool `json:"respect_removed"`

	// Written counts associations this run wrote; zero in a dry run.
	Written int `json:"written"`

	// Skipped counts decided writes whose evidence changed before the write.
	Skipped int `json:"skipped"`
}

// Report summarizes a run. Results carry ids only, never names or URLs.
type Report struct {
	// Mode is "apply" or "dry_run".
	Mode string `json:"mode"`

	// Servers counts servers decided.
	Servers int `json:"servers"`

	// Outcomes counts servers per outcome.
	Outcomes map[Outcome]int `json:"outcomes"`

	// Decided counts associations decided, written or not.
	Decided int `json:"decided"`

	// Written counts associations written.
	Written int `json:"written"`

	// Skipped counts decided writes that lost a race; rerun to pick them up.
	Skipped int `json:"skipped"`

	// Gaps counts servers left without a proven association; cutover is
	// blocked until it is zero.
	Gaps int `json:"gaps"`

	// Results lists every decided server.
	Results []ServerResult `json:"results"`
}

// Run decides every server and, with Apply, writes the decisions.
func Run(ctx context.Context, db DBTX, opts Options) (Report, error) {
	pageSize := opts.PageSize
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	report := Report{
		Mode:     "dry_run",
		Servers:  0,
		Outcomes: map[Outcome]int{},
		Decided:  0,
		Written:  0,
		Skipped:  0,
		Gaps:     0,
		Results:  []ServerResult{},
	}
	if opts.Apply {
		report.Mode = "apply"
	}
	q := New(db)
	cursor := uuid.Nil
	for {
		rows, err := q.ListUpstreamClientBackfillCandidates(ctx, ListUpstreamClientBackfillCandidatesParams{
			Cursor:         cursor,
			OrganizationID: opts.OrganizationID,
			PageSize:       int32(pageSize), // #nosec G115 -- bounded by defaultPageSize or an operator flag
		})
		if err != nil {
			return report, fmt.Errorf("list backfill candidates after %s: %w", cursor, err)
		}
		if len(rows) == 0 {
			return report, nil
		}
		for _, server := range groupByServer(rows) {
			result := Decide(server)
			report.Decided += len(result.Associate)
			if opts.Apply {
				for _, clientID := range result.Associate {
					n, err := q.BackfillUpstreamClientAssociation(ctx, BackfillUpstreamClientAssociationParams{
						RemoteSessionClientID: clientID,
						McpServerID:           result.MCPServerID,
						UserSessionIssuerID:   server[0].UserSessionIssuerID,
						RequireSoleConsumer:   result.Outcome == OutcomeSoleConsumer,
						RespectRemoved:        result.RespectRemoved,
					})
					if err != nil {
						return report, fmt.Errorf("write association for server %s: %w", result.MCPServerID, err)
					}
					if n == 0 {
						result.Skipped++
					} else {
						result.Written++
					}
				}
			}
			report.Servers++
			report.Outcomes[result.Outcome]++
			report.Written += result.Written
			report.Skipped += result.Skipped
			if !result.Outcome.Covered() {
				report.Gaps++
			}
			report.Results = append(report.Results, result)
			cursor = result.MCPServerID
		}
		if err := ctx.Err(); err != nil {
			return report, fmt.Errorf("backfill interrupted: %w", err)
		}
	}
}

// groupByServer splits a page into one slice per server, preserving order.
func groupByServer(rows []ListUpstreamClientBackfillCandidatesRow) [][]ListUpstreamClientBackfillCandidatesRow {
	var groups [][]ListUpstreamClientBackfillCandidatesRow
	for _, row := range rows {
		if n := len(groups); n > 0 && groups[n-1][0].McpServerID == row.McpServerID {
			groups[n-1] = append(groups[n-1], row)
			continue
		}
		groups = append(groups, []ListUpstreamClientBackfillCandidatesRow{row})
	}
	return groups
}

// Decide classifies one server from its candidate rows, which all belong to
// that server, and lists the clients to associate. It associates only what
// configuration proves: every bound client when the server is its issuer's
// only consumer, otherwise the single client its own upstream's metadata
// names. It never copies a shared issuer's clients onto every server on it,
// and never reads an unbound issuer as proof that no authentication is needed.
func Decide(rows []ListUpstreamClientBackfillCandidatesRow) ServerResult {
	first := rows[0]
	result := ServerResult{
		MCPServerID:    first.McpServerID,
		ProjectID:      first.ProjectID,
		Outcome:        OutcomeUnbound,
		Associate:      nil,
		RespectRemoved: true,
		Written:        0,
		Skipped:        0,
	}

	var bound, matched []ListUpstreamClientBackfillCandidatesRow
	for _, row := range rows {
		if !row.RemoteSessionClientID.Valid {
			continue
		}
		bound = append(bound, row)
		if row.MetadataMatch {
			matched = append(matched, row)
		}
	}

	var decided []ListUpstreamClientBackfillCandidatesRow
	switch {
	case len(bound) == 0:
		return result
	case first.IssuerServerCount <= 1 && !first.IssuerHasGateway:
		result.Outcome, decided, result.RespectRemoved = OutcomeSoleConsumer, bound, false
	case len(matched) == 1 && !matched[0].PairRecorded:
		result.Outcome, decided = OutcomeMetadataMatch, matched
	case first.LiveAssociations > 0:
		result.Outcome = OutcomeConfigured
	default:
		// Includes a metadata match an administrator has since detached: the
		// server is left for explicit configuration rather than re-associated.
		result.Outcome = OutcomeAmbiguous
	}
	for _, row := range decided {
		if !row.PairLive {
			result.Associate = append(result.Associate, row.RemoteSessionClientID.UUID)
		}
	}
	return result
}
