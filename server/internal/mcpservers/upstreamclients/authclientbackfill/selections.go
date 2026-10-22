package authclientbackfill

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
)

// CredentialClass is the kind of inbound credential a choice belongs to, as
// coverage is reported.
type CredentialClass string

const (
	// ClassInteractive is a person's session from a browser sign-in.
	ClassInteractive CredentialClass = "interactive"

	// ClassIDJAG is a session issued for an inbound ID-JAG exchange.
	ClassIDJAG CredentialClass = "id_jag"

	// ClassDashboardMint is a session the dashboard minted.
	ClassDashboardMint CredentialClass = "dashboard_mint"

	// ClassWorkload is a workload's session.
	ClassWorkload CredentialClass = "workload"

	// ClassAgent is an agent's session, delegated by the person who approved
	// it.
	ClassAgent CredentialClass = "agent"
)

// SelectionOutcome classifies one choice's mapping onto a typed auth client.
type SelectionOutcome string

const (
	// SelectionMapped is a choice that already names a usable auth client.
	SelectionMapped SelectionOutcome = "mapped"

	// SelectionMap is a choice with exactly one auth client to name, named by
	// an apply.
	SelectionMap SelectionOutcome = "map"

	// SelectionNoAuth is an explicit no-auth choice on a server with no auth
	// client: it names none and needs none.
	SelectionNoAuth SelectionOutcome = "no_auth"

	// SelectionAmbiguous is a choice that several auth clients could serve.
	// Nothing recorded says which one the credential uses, so it is never
	// guessed: the server's configuration decides it, or the credential
	// reconnects and chooses.
	SelectionAmbiguous SelectionOutcome = "ambiguous"

	// SelectionNoClient is an OAuth choice whose registration no live, enabled
	// auth client linked to the server carries, so there is nothing to name
	// until the auth client backfill or an administrator records one.
	SelectionNoClient SelectionOutcome = "no_client"

	// SelectionStaleNoAuth is a no-auth choice on a server that now offers an
	// OAuth-backed client. It is already unusable; the credential reconnects.
	SelectionStaleNoAuth SelectionOutcome = "stale_no_auth"

	// SelectionUnusable is a choice naming an auth client that is no longer
	// usable for its server. A named client is never replaced by another, so
	// the credential reconnects.
	SelectionUnusable SelectionOutcome = "unusable"

	// SelectionSkippedRace is a choice whose evidence changed before the
	// write; rerunning picks it up.
	SelectionSkippedRace SelectionOutcome = "skipped_race"
)

// Covered reports whether a choice with this outcome serves through a typed
// auth client after the run, or needs none.
func (o SelectionOutcome) Covered() bool {
	switch o {
	case SelectionMapped, SelectionMap, SelectionNoAuth:
		return true
	default:
		return false
	}
}

// SelectionCounts summarizes the live choices of one credential class.
type SelectionCounts struct {
	// Selections counts live choices.
	Selections int `json:"selections"`

	// Outcomes counts choices per outcome, as decided before any write.
	Outcomes map[SelectionOutcome]int `json:"outcomes"`

	// Written counts choices this run named an auth client on.
	Written int `json:"written"`
}

// Gaps counts the class's choices left without a usable auth client.
func (c *SelectionCounts) Gaps() int {
	n := 0
	for outcome, count := range c.Outcomes {
		if !outcome.Covered() {
			n += count
		}
	}
	return n
}

// SelectionGap is one choice left without a usable auth client. It carries ids
// only.
type SelectionGap struct {
	// SelectionID is the choice.
	SelectionID uuid.UUID `json:"selection_id"`

	// CredentialClass is the class of the credential the choice belongs to.
	CredentialClass CredentialClass `json:"credential_class"`

	// CredentialID is the credential.
	CredentialID uuid.UUID `json:"credential_id"`

	// MCPServerID is the server the choice is for.
	MCPServerID uuid.UUID `json:"mcp_server_id"`

	// ProjectID is the server's project.
	ProjectID uuid.UUID `json:"project_id"`

	// Outcome is why the choice has no usable auth client.
	Outcome SelectionOutcome `json:"outcome"`
}

func (r *Report) selectionClass(class CredentialClass) *SelectionCounts {
	counts, ok := r.Selections[class]
	if !ok {
		counts = &SelectionCounts{Selections: 0, Outcomes: map[SelectionOutcome]int{}, Written: 0}
		r.Selections[class] = counts
	}
	return counts
}

// decideSelection decides one choice's mapping, returning the auth client to
// name when there is exactly one.
func decideSelection(row ListSelectionMappingsRow) (SelectionOutcome, uuid.UUID) {
	switch {
	case row.AuthClientID.Valid && row.MappedUsable:
		return SelectionMapped, uuid.Nil
	case row.AuthClientID.Valid:
		return SelectionUnusable, uuid.Nil
	case row.NoAuth && row.ServerHasOauthClients:
		return SelectionStaleNoAuth, uuid.Nil
	case row.NoAuth && len(row.CandidateIds) == 0:
		return SelectionNoAuth, uuid.Nil
	case len(row.CandidateIds) == 1:
		return SelectionMap, row.CandidateIds[0]
	case len(row.CandidateIds) > 1:
		return SelectionAmbiguous, uuid.Nil
	default:
		return SelectionNoClient, uuid.Nil
	}
}

// liveCredentialClasses reads the class of every live user session credential
// in the organization.
func liveCredentialClasses(ctx context.Context, q *Queries, organizationID string, size int32) (map[uuid.UUID]CredentialClass, error) {
	classes := map[uuid.UUID]CredentialClass{}
	cursor := uuid.Nil
	for {
		rows, err := q.ListLiveCredentialClasses(ctx, ListLiveCredentialClassesParams{Cursor: cursor, OrganizationID: organizationID, PageSize: size})
		if err != nil {
			return nil, fmt.Errorf("list live credentials: %w", err)
		}
		for _, row := range rows {
			classes[row.CredentialID] = CredentialClass(row.CredentialClass)
			cursor = row.CredentialID
		}
		if len(rows) < int(size) {
			return classes, nil
		}
	}
}

// runSelections names a typed auth client on every live choice that exactly
// one can serve, and reports every live choice's coverage by credential class.
// Choices of credentials that are no longer live are left alone: they are
// never served again.
func runSelections(ctx context.Context, q *Queries, opts Options, size int32, report *Report) error {
	classes, err := liveCredentialClasses(ctx, q, opts.OrganizationID, size)
	if err != nil {
		return err
	}
	cursor := uuid.Nil
	for {
		rows, err := q.ListSelectionMappings(ctx, ListSelectionMappingsParams{OrganizationID: opts.OrganizationID, Cursor: cursor, PageSize: size})
		if err != nil {
			return fmt.Errorf("list selections: %w", err)
		}
		for _, row := range rows {
			cursor = row.ID
			if err := decideSelectionRow(ctx, q, opts, classes, row, report); err != nil {
				return err
			}
		}
		if len(rows) < int(size) {
			return nil
		}
	}
}

func decideSelectionRow(ctx context.Context, q *Queries, opts Options, classes map[uuid.UUID]CredentialClass, row ListSelectionMappingsRow, report *Report) error {
	class, live := selectionCredentialClass(classes, row)
	if !live {
		return nil
	}
	outcome, clientID := decideSelection(row)
	counts := report.selectionClass(class)
	counts.Selections++
	counts.Outcomes[outcome]++
	if outcome == SelectionMap {
		report.PendingSelectionMappings++
		if opts.Apply {
			n, err := q.MapSelectionToAuthClient(ctx, MapSelectionToAuthClientParams{
				ID:                    row.ID,
				OrganizationID:        opts.OrganizationID,
				NoAuth:                row.NoAuth,
				RemoteSessionClientID: row.RemoteSessionClientID,
				AuthClientID:          clientID,
			})
			if err != nil {
				return fmt.Errorf("map selection %s: %w", row.ID, err)
			}
			if n == 0 {
				outcome = SelectionSkippedRace
			} else {
				counts.Written++
				report.SelectionsMapped++
			}
		}
	}
	if !outcome.Covered() {
		report.SelectionGaps = append(report.SelectionGaps, SelectionGap{
			SelectionID:     row.ID,
			CredentialClass: class,
			CredentialID:    row.CredentialID,
			MCPServerID:     row.McpServerID,
			ProjectID:       row.ProjectID,
			Outcome:         outcome,
		})
	}
	return nil
}

// selectionCredentialClass is the class of the credential a choice belongs to,
// and whether that credential is live. A user session credential is live while
// a row of it can still refresh; other credential kinds are their own class.
func selectionCredentialClass(classes map[uuid.UUID]CredentialClass, row ListSelectionMappingsRow) (CredentialClass, bool) {
	if row.CredentialKind != upstreamclients.CredentialKindUserSession {
		return CredentialClass(row.CredentialKind), true
	}
	class, live := classes[row.CredentialID]
	return class, live
}
