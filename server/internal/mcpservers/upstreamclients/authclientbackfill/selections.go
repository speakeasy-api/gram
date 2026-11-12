package authclientbackfill

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
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

	// SelectionStaleNoAuth is a no-auth choice, or one naming the server's
	// headers, on a server that now has an upstream OAuth client association
	// or offers an enabled OAuth-backed client. It is already unusable, or is
	// once the typed resolver is enabled; the credential reconnects. A later
	// issuance replaces only an untyped no-auth choice, never one naming an
	// auth client.
	SelectionStaleNoAuth SelectionOutcome = "stale_no_auth"

	// SelectionUnusable is an OAuth choice the upstream client choice already
	// rejects, because its registration is no longer associated with the
	// server or bound to the credential's user session issuer, or a choice
	// naming an auth client that is no longer usable for its server. A named
	// client is never replaced by another, so the credential reconnects.
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

	// Outcomes counts choices per outcome, as finally decided: a choice
	// decided again under lock before its write counts as that decision.
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
		counts = &SelectionCounts{Selections: 0, Outcomes: make(map[SelectionOutcome]int), Written: 0}
		r.Selections[class] = counts
	}
	return counts
}

// liveCredential is a live user session credential as coverage reads it.
type liveCredential struct {
	class  CredentialClass
	issuer uuid.UUID
}

// decideSelection decides one choice's mapping, returning the auth client to
// name when there is exactly one. issuer is the user session issuer of the
// choice's credential. A choice the upstream client choice reads as no-auth,
// explicit or naming the server's headers, holds while the server has no
// upstream OAuth client association, as GetUpstreamClientSelection reads it,
// and the server offers no enabled OAuth-backed client, counting an EMA
// client reached only through the gateways only for a choice whose credential
// the gateways' issuer issued. A choice already
// naming an auth client is usable while that client is, as the typed
// resolver reads it, whatever the legacy issuer binding. An unmapped OAuth
// choice holds while its registration is associated with the server and
// bound to issuer, which is what the upstream client choice serves it on.
// For an organization enforcing upstream auth (typedOnly), a server's upstream
// OAuth client associations make no choice stale or rejected, as issuance reads
// them there, but an unmapped OAuth choice still needs its registration and
// that registration's issuer to be live.
func decideSelection(row ListSelectionMappingsRow, issuer uuid.UUID, typedOnly bool) (SelectionOutcome, uuid.UUID) {
	readsNoAuth := row.NoAuth || row.NamesHeaders
	staleNoAuth := readsNoAuth && ((row.ServerHasAssociations && !typedOnly) || row.ServerHasOauthClients || slices.Contains(row.EmaGatewayIssuerIds, issuer))
	registrationServes := row.RegistrationLive
	if !typedOnly {
		registrationServes = row.RegistrationAssociated
	}
	oauthRejected := !readsNoAuth && (!registrationServes || !slices.Contains(row.RegistrationIssuerIds, issuer))
	switch {
	case staleNoAuth:
		return SelectionStaleNoAuth, uuid.Nil
	case row.AuthClientID.Valid && row.MappedUsable:
		return SelectionMapped, uuid.Nil
	case row.AuthClientID.Valid:
		return SelectionUnusable, uuid.Nil
	case oauthRejected:
		return SelectionUnusable, uuid.Nil
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

// liveCredentials reads every live user session credential in the
// organization.
func liveCredentials(ctx context.Context, q *Queries, organizationID string, size int32) (map[uuid.UUID]liveCredential, error) {
	credentials := map[uuid.UUID]liveCredential{}
	cursor := uuid.Nil
	for {
		rows, err := q.ListLiveCredentialClasses(ctx, ListLiveCredentialClassesParams{Cursor: cursor, OrganizationID: organizationID, PageSize: size})
		if err != nil {
			return nil, fmt.Errorf("list live credentials: %w", err)
		}
		for _, row := range rows {
			credentials[row.CredentialID] = liveCredential{class: CredentialClass(row.CredentialClass), issuer: row.UserSessionIssuerID}
			cursor = row.CredentialID
		}
		if len(rows) < int(size) {
			return credentials, nil
		}
	}
}

// runSelections reports every live choice's coverage by credential class and,
// when write is set, names a typed auth client on every live choice that
// exactly one can serve. Choices of credentials that are no longer live are
// left alone: they are never served again.
func runSelections(ctx context.Context, q *Queries, opts Options, size int32, write bool, report *Report) error {
	credentials, err := liveCredentials(ctx, q, opts.OrganizationID, size)
	if err != nil {
		return err
	}
	return visitSelectionMappings(ctx, q, ListSelectionMappingsParams{
		OrganizationID: opts.OrganizationID, PageSize: size, OnlyUnmapped: false, ScopeMcpServerIds: nil,
		Cursor: uuid.Nil, OnlyID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, ScopeUserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	}, func(rows []ListSelectionMappingsRow) error {
		for _, row := range rows {
			credential, live := credentials[row.CredentialID]
			if !live {
				continue
			}
			if err := decideSelectionRow(ctx, q, opts, write, credential, row, report); err != nil {
				return err
			}
		}
		return nil
	})
}

// visitSelectionMappings advances past every row, even if its callback skips or
// maps it out of the result set. Scoped lifecycle writes and backfill reporting
// therefore use the same cursor and page termination rules.
func visitSelectionMappings(ctx context.Context, q *Queries, params ListSelectionMappingsParams, visit func([]ListSelectionMappingsRow) error) error {
	for {
		rows, err := q.ListSelectionMappings(ctx, params)
		if err != nil {
			return fmt.Errorf("list selection mappings: %w", err)
		}
		if len(rows) > 0 {
			params.Cursor = rows[len(rows)-1].ID
			if err := visit(rows); err != nil {
				return err
			}
		}
		if len(rows) < int(params.PageSize) {
			return nil
		}
	}
}

func decideSelectionRow(ctx context.Context, q *Queries, opts Options, write bool, credential liveCredential, row ListSelectionMappingsRow, report *Report) error {
	outcome, clientID := decideSelection(row, credential.issuer, opts.TypedOnly)
	counts := report.selectionClass(credential.class)
	counts.Selections++
	if outcome == SelectionMap {
		report.PendingSelectionMappings++
		if write {
			written, final, err := mapSelection(ctx, q, opts.OrganizationID, credential.issuer, opts.TypedOnly, row, clientID)
			if err != nil {
				return err
			}
			outcome = final
			if written {
				counts.Written++
				report.SelectionsMapped++
			}
		}
	}
	counts.Outcomes[outcome]++
	if !outcome.Covered() {
		report.SelectionGaps = append(report.SelectionGaps, SelectionGap{
			SelectionID:     row.ID,
			CredentialClass: credential.class,
			CredentialID:    row.CredentialID,
			MCPServerID:     row.McpServerID,
			ProjectID:       row.ProjectID,
			Outcome:         outcome,
		})
	}
	return nil
}

// mapSelection names clientID on a choice decided as SelectionMap, after
// locking what the decision reads against the writers that change it and
// deciding again from a read taken after the locks: an auth client enabled,
// disabled or detached, an upstream OAuth client association or a credential
// header changed concurrently either commits before the read or waits for
// this phase to commit. It returns whether it wrote and the choice's outcome.
// The locks are held until the phase commits.
func mapSelection(ctx context.Context, q *Queries, organizationID string, issuerID uuid.UUID, typedOnly bool, row ListSelectionMappingsRow, clientID uuid.UUID) (bool, SelectionOutcome, error) {
	if err := q.LockSelectionRemoteSource(ctx, row.McpServerID); err != nil {
		return false, "", fmt.Errorf("lock source of selection %s: %w", row.ID, err)
	}
	if err := q.LockSelectionTunneledSource(ctx, row.McpServerID); err != nil {
		return false, "", fmt.Errorf("lock source of selection %s: %w", row.ID, err)
	}
	if err := q.LockSelectionServer(ctx, row.McpServerID); err != nil {
		return false, "", fmt.Errorf("lock server of selection %s: %w", row.ID, err)
	}
	if err := q.LockSelectionServerAuthClients(ctx, LockSelectionServerAuthClientsParams{McpServerID: row.McpServerID, OrganizationID: organizationID}); err != nil {
		return false, "", fmt.Errorf("lock auth clients of selection %s: %w", row.ID, err)
	}
	if row.RemoteSessionClientID.Valid {
		if err := q.LockSelectionIssuerBinding(ctx, LockSelectionIssuerBindingParams{RemoteSessionClientID: row.RemoteSessionClientID.UUID, UserSessionIssuerID: issuerID}); err != nil {
			return false, "", fmt.Errorf("lock issuer binding of selection %s: %w", row.ID, err)
		}
	}
	fresh, err := q.ListSelectionMappings(ctx, ListSelectionMappingsParams{OrganizationID: organizationID, Cursor: uuid.Nil, OnlyID: uuid.NullUUID{UUID: row.ID, Valid: true}, PageSize: 1, OnlyUnmapped: false, ScopeMcpServerIds: nil, ScopeUserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}})
	if err != nil {
		return false, "", fmt.Errorf("reread selection %s: %w", row.ID, err)
	}
	if len(fresh) == 0 {
		return false, SelectionSkippedRace, nil
	}
	outcome, decided := decideSelection(fresh[0], issuerID, typedOnly)
	switch {
	case outcome != SelectionMap:
		return false, outcome, nil
	case decided != clientID:
		return false, SelectionSkippedRace, nil
	}
	n, err := q.MapSelectionToAuthClient(ctx, MapSelectionToAuthClientParams{
		ID:                    row.ID,
		OrganizationID:        organizationID,
		NoAuth:                fresh[0].NoAuth,
		RemoteSessionClientID: fresh[0].RemoteSessionClientID,
		AuthClientID:          clientID,
	})
	if err != nil {
		return false, "", fmt.Errorf("map selection %s: %w", row.ID, err)
	}
	if n == 0 {
		return false, SelectionSkippedRace, nil
	}
	return true, SelectionMap, nil
}

// ReconcileUserSessionSelectionsInLockedTransaction names uniquely decided typed
// clients on existing unmapped choices of one issuer's live credentials. It uses
// the backfill's decision and guarded mapping writer, never replaces a named
// client, and grants nothing. The caller must hold the EMA writer scope for
// every reachable or recorded server and preflight its OAuth registration gates
// before its first mutation. Newly missing choices are recorded separately by
// the shared issuance writer.
func ReconcileUserSessionSelectionsInLockedTransaction(ctx context.Context, tx pgx.Tx, organizationID string, issuerID uuid.UUID) error {
	_, err := ReconcileUserSessionSelectionsForServersInLockedTransaction(ctx, tx, organizationID, issuerID, nil)
	return err
}

// ReconcileUserSessionSelectionsForServersInLockedTransaction independently
// scopes mapping to servers: nil keeps full reach, while an empty array maps
// nothing. It reports guarded bulk rows skipped because their evidence changed.
func ReconcileUserSessionSelectionsForServersInLockedTransaction(ctx context.Context, tx pgx.Tx, organizationID string, issuerID uuid.UUID, serverIDs []uuid.UUID) (int64, error) {
	if tx == nil || organizationID == "" || issuerID == uuid.Nil {
		return 0, errors.New("reconcile user session selections: no transaction or tenant scope")
	}
	// Read the feature repository directly to avoid a dependency cycle through
	// the product feature service, which invokes this backfill.
	typedOnly, err := productfeaturesrepo.New(tx).IsFeatureEnabled(ctx, productfeaturesrepo.IsFeatureEnabledParams{
		OrganizationID: organizationID, FeatureName: "upstream_auth_enforced",
	})
	if err != nil {
		return 0, err
	}
	q := New(tx)
	confirmed := make(map[selectionMappingGroup]bool)
	var skipped int64
	err = visitSelectionMappings(ctx, q, ListSelectionMappingsParams{
		OrganizationID: organizationID, PageSize: defaultPageSize, OnlyUnmapped: true, ScopeMcpServerIds: serverIDs,
		Cursor: uuid.Nil, OnlyID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, ScopeUserSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true},
	}, func(rows []ListSelectionMappingsRow) error {
		groups := make(map[selectionMappingGroup][]ListSelectionMappingsRow)
		var ordered []selectionMappingGroup
		for _, row := range rows {
			outcome, clientID := decideSelection(row, issuerID, typedOnly)
			if outcome != SelectionMap {
				continue
			}
			key := selectionMappingGroup{serverID: row.McpServerID, registration: row.RemoteSessionClientID, noAuth: row.NoAuth, clientID: clientID}
			if _, exists := groups[key]; !exists {
				ordered = append(ordered, key)
			}
			groups[key] = append(groups[key], row)
		}
		for _, key := range ordered {
			group := groups[key]
			if !confirmed[key] {
				// A row-specific race must not suppress the rest of the group.
				// Try another representative until the unchanged policy is locked
				// and a guarded write succeeds; only then cache across pages.
				for len(group) > 0 {
					representative := group[0]
					group = group[1:]
					written, outcome, err := mapSelection(ctx, q, organizationID, issuerID, typedOnly, representative, key.clientID)
					if err != nil {
						return err
					}
					if written && outcome == SelectionMap {
						confirmed[key] = true
						break
					}
				}
			}
			if !confirmed[key] || len(group) == 0 {
				continue
			}
			ids := make([]uuid.UUID, 0, len(group))
			for _, row := range group {
				ids = append(ids, row.ID)
			}
			n, err := q.MapSelectionGroupToAuthClient(ctx, MapSelectionGroupToAuthClientParams{
				OrganizationID: organizationID, Ids: ids, McpServerID: key.serverID,
				NoAuth: key.noAuth, RemoteSessionClientID: key.registration, AuthClientID: key.clientID,
			})
			if err != nil {
				return fmt.Errorf("map selection group for server %s: %w", key.serverID, err)
			}
			skipped += int64(len(ids)) - n
		}
		return nil
	})
	return skipped, err
}

// For one issuer, the mapping policy for unmapped choices depends only on the
// server, legacy registration and no-auth state. Source headers, offered clients,
// gateway ownership and issuer bindings are configuration of that server and
// registration, not of an individual credential. Include the decided client in
// the key too: a changed candidate must establish its own locked decision.
// Successful mapSelection retains source/server/client/binding locks until the
// transaction commits, so later pages can reuse that policy. Bulk writes still
// guard every row against concurrent explicit choices or changed legacy state.
type selectionMappingGroup struct {
	serverID     uuid.UUID
	registration uuid.NullUUID
	noAuth       bool
	clientID     uuid.UUID
}
