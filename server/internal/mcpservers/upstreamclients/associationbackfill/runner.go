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
	// It uses no client.
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

	// OutcomeGrant is a server on a shared issuer for whose upstream exactly
	// one of the issuer's clients holds grants.
	OutcomeGrant Outcome = "grant"

	// OutcomeDerived is a server on a shared issuer whose derived
	// authorization server is exactly one of the issuer's clients', with no
	// grants for its upstream.
	OutcomeDerived Outcome = "derived"

	// OutcomeDetached is a server on a shared issuer whose proven client an
	// administrator detached. The detach stands: the server uses no client.
	OutcomeDetached Outcome = "detached"

	// OutcomeNoClient is a server on a shared issuer with no evidence for any
	// of the issuer's clients: no metadata, grant or derived authorization
	// server names one. It uses no client.
	OutcomeNoClient Outcome = "no_client"

	// OutcomeAmbiguous is a server on a shared issuer whose evidence names
	// several clients, or whose grants and derived authorization server name
	// different ones, and that has no association. It needs explicit
	// configuration before cutover.
	OutcomeAmbiguous Outcome = "ambiguous"

	// OutcomeUnproxied is a hosted or unproxied server on a shared issuer
	// whose configuration proves no single client. Serving keeps such servers
	// on the issuer's bindings, so nothing needs deciding.
	OutcomeUnproxied Outcome = "unproxied"
)

// Covered reports whether a server with this outcome has its associations
// after the run.
func (o Outcome) Covered() bool {
	return o != OutcomeAmbiguous
}

// GatewayOutcome classifies one server a gateway issuer fronts.
type GatewayOutcome string

const (
	// GatewayOutcomeGrant is a server one of the issuer's clients holds
	// grants for: their recorded resource is the server's upstream, which is
	// how requests route to it today.
	GatewayOutcomeGrant GatewayOutcome = "grant"

	// GatewayOutcomeDerived is a server nobody has a grant for yet whose
	// derived authorization server is that of exactly one of the issuer's
	// clients, which is how consent claimed it.
	GatewayOutcomeDerived GatewayOutcome = "derived"

	// GatewayOutcomeConfigured is a server already associated with the
	// decided client.
	GatewayOutcomeConfigured GatewayOutcome = "configured"

	// GatewayOutcomeDetached is a server whose decided client was explicitly
	// detached from it; the detach is kept.
	GatewayOutcomeDetached GatewayOutcome = "detached"

	// GatewayOutcomeUnserved is a server none of the issuer's clients serve:
	// it needs no upstream authentication, or one the gateway has no client
	// for. The gateway serves it no credential today either.
	GatewayOutcomeUnserved GatewayOutcome = "unserved"

	// GatewayOutcomeAmbiguous is a server several of the issuer's clients
	// match by the same evidence. It needs explicit configuration.
	GatewayOutcomeAmbiguous GatewayOutcome = "ambiguous"

	// GatewayOutcomeConflict is a server whose grants name one client while
	// its derived authorization server names another. It needs explicit
	// configuration.
	GatewayOutcomeConflict GatewayOutcome = "conflict"
)

// Covered reports whether a server with this outcome needs nothing more.
func (o GatewayOutcome) Covered() bool {
	return o != GatewayOutcomeAmbiguous && o != GatewayOutcomeConflict
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

// GatewayResult is one decision for a server a gateway issuer fronts.
type GatewayResult struct {
	// UserSessionIssuerID is the gateway issuer.
	UserSessionIssuerID uuid.UUID `json:"user_session_issuer_id"`

	// MCPServerID is the fronted server.
	MCPServerID uuid.UUID `json:"mcp_server_id"`

	// ProjectID is the server's project.
	ProjectID uuid.UUID `json:"project_id"`

	// Outcome classifies the pair.
	Outcome GatewayOutcome `json:"outcome"`

	// Associate is the decided client the server is not yet associated with.
	Associate uuid.NullUUID `json:"associate"`

	// Written reports that this run wrote the association.
	Written bool `json:"written"`

	// Skipped reports a decided write whose evidence changed before it.
	Skipped bool `json:"skipped"`
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

	// GatewayOutcomes counts gateway issuer and server pairs per outcome.
	GatewayOutcomes map[GatewayOutcome]int `json:"gateway_outcomes"`

	// GatewayResults lists every decided gateway issuer and server pair.
	GatewayResults []GatewayResult `json:"gateway_results"`
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
		Outcomes: make(map[Outcome]int),
		Decided:  0,
		Written:  0,
		Skipped:  0,
		Gaps:     0,
		Results:  []ServerResult{},

		GatewayOutcomes: make(map[GatewayOutcome]int),
		GatewayResults:  []GatewayResult{},
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
			if err := runGateways(ctx, q, opts, &report); err != nil {
				return report, err
			}
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
						RequireMetadataMatch:  result.Outcome == OutcomeMetadataMatch,
						RequireEvidenceMatch:  result.Outcome == OutcomeGrant || result.Outcome == OutcomeDerived,
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

	var bound, matched, granted, derived []ListUpstreamClientBackfillCandidatesRow
	for _, row := range rows {
		if !row.RemoteSessionClientID.Valid {
			continue
		}
		bound = append(bound, row)
		if row.MetadataMatch {
			matched = append(matched, row)
		}
		if row.GrantMatch {
			granted = append(granted, row)
		}
		if row.DerivedMatch {
			derived = append(derived, row)
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
	case !first.Proxied:
		result.Outcome = OutcomeUnproxied
	case len(matched) > 1:
		result.Outcome = OutcomeAmbiguous
	case len(matched) == 1:
		result.Outcome = OutcomeDetached
	default:
		result.Outcome, decided = decideByEvidence(granted, derived)
	}
	for _, row := range decided {
		if !row.PairLive {
			result.Associate = append(result.Associate, row.RemoteSessionClientID.UUID)
		}
	}
	return result
}

// runGateways decides every server a gateway issuer fronts and, with Apply,
// writes the decisions.
func runGateways(ctx context.Context, q *Queries, opts Options, report *Report) error {
	rows, err := q.ListGatewayAssociationCandidates(ctx, opts.OrganizationID)
	if err != nil {
		return fmt.Errorf("list gateway association candidates: %w", err)
	}
	for start := 0; start < len(rows); {
		end := start + 1
		for end < len(rows) && rows[end].IssuerID == rows[start].IssuerID && rows[end].McpServerID == rows[start].McpServerID {
			end++
		}
		result := DecideGateway(rows[start:end])
		if result.Associate.Valid {
			report.Decided++
			if opts.Apply {
				n, err := q.BackfillGatewayAssociation(ctx, BackfillGatewayAssociationParams{
					UserSessionIssuerID:   result.UserSessionIssuerID,
					RemoteSessionClientID: result.Associate.UUID,
					McpServerID:           result.MCPServerID,
				})
				if err != nil {
					return fmt.Errorf("write gateway association for server %s: %w", result.MCPServerID, err)
				}
				result.Written, result.Skipped = n > 0, n == 0
				if result.Written {
					report.Written++
				} else {
					report.Skipped++
				}
			}
		}
		report.GatewayOutcomes[result.Outcome]++
		if !result.Outcome.Covered() {
			report.Gaps++
		}
		report.GatewayResults = append(report.GatewayResults, result)
		start = end
	}
	return nil
}

// decideByEvidence decides a proxied server on a shared issuer from its
// clients' grants, then their derived authorization servers, as
// DecideGateway does. A proven client whose pair an administrator detached
// stays detached.
func decideByEvidence(granted, derived []ListUpstreamClientBackfillCandidatesRow) (Outcome, []ListUpstreamClientBackfillCandidatesRow) {
	var decided ListUpstreamClientBackfillCandidatesRow
	var outcome Outcome
	switch {
	case len(granted) > 1:
		return OutcomeAmbiguous, nil
	case len(granted) == 1:
		decided, outcome = granted[0], OutcomeGrant
		for _, row := range derived {
			if row.RemoteSessionClientID != decided.RemoteSessionClientID {
				return OutcomeAmbiguous, nil
			}
		}
	case len(derived) > 1:
		return OutcomeAmbiguous, nil
	case len(derived) == 1:
		decided, outcome = derived[0], OutcomeDerived
	default:
		return OutcomeNoClient, nil
	}
	if decided.PairRecorded {
		return OutcomeDetached, nil
	}
	return outcome, []ListUpstreamClientBackfillCandidatesRow{decided}
}

// DecideGateway classifies one gateway issuer and server pair from its
// candidate rows, which all belong to that pair. Grants decide first, since
// requests route by them today; the derived authorization server decides
// only a server nobody has a grant for. More than one match, or the two
// disagreeing, is left for explicit configuration.
func DecideGateway(rows []ListGatewayAssociationCandidatesRow) GatewayResult {
	first := rows[0]
	result := GatewayResult{
		UserSessionIssuerID: first.IssuerID,
		MCPServerID:         first.McpServerID,
		ProjectID:           first.ProjectID,
		Outcome:             GatewayOutcomeUnserved,
		Associate:           uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Written:             false,
		Skipped:             false,
	}
	var granted, derived []ListGatewayAssociationCandidatesRow
	for _, row := range rows {
		if row.GrantMatch {
			granted = append(granted, row)
		}
		if row.DerivedMatch {
			derived = append(derived, row)
		}
	}
	var decided ListGatewayAssociationCandidatesRow
	switch {
	case len(granted) > 1:
		result.Outcome = GatewayOutcomeAmbiguous
		return result
	case len(granted) == 1:
		decided = granted[0]
		for _, row := range derived {
			if row.RemoteSessionClientID != decided.RemoteSessionClientID {
				result.Outcome = GatewayOutcomeConflict
				return result
			}
		}
		result.Outcome = GatewayOutcomeGrant
	case len(derived) > 1:
		result.Outcome = GatewayOutcomeAmbiguous
		return result
	case len(derived) == 1:
		decided = derived[0]
		result.Outcome = GatewayOutcomeDerived
	default:
		return result
	}
	switch {
	case decided.PairLive:
		result.Outcome = GatewayOutcomeConfigured
	case decided.PairRecorded:
		result.Outcome = GatewayOutcomeDetached
	default:
		result.Associate = uuid.NullUUID{UUID: decided.RemoteSessionClientID, Valid: true}
	}
	return result
}
