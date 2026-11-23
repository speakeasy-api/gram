package associationbackfill

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/authenforcement"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// PinOutcome classifies one server for pinning: recording, before an issuer
// change, the associations its current configuration proves.
type PinOutcome string

const (
	// PinOutcomeNoAuth is a server recorded as needing no upstream
	// authentication. It is never pinned.
	PinOutcomeNoAuth PinOutcome = "no_auth"

	// PinOutcomeUnbound is a server whose user session issuer binds no client.
	// It uses no client today.
	PinOutcomeUnbound PinOutcome = "unbound"

	// PinOutcomeConfigured is a server that already has a live association,
	// or offers an OAuth client an administrator configured for it. Its
	// configuration already says which clients it uses.
	PinOutcomeConfigured PinOutcome = "configured"

	// PinOutcomeSoleConsumer is a server that is its issuer's only consumer,
	// so every client bound to the issuer is meant for it.
	PinOutcomeSoleConsumer PinOutcome = "sole_consumer"

	// PinOutcomeMetadataMatch is a server whose upstream advertises exactly
	// one of the bound clients' authorization servers.
	PinOutcomeMetadataMatch PinOutcome = "metadata_match"

	// PinOutcomeGrant is a proxied server for whose upstream exactly one of
	// the bound clients holds grants.
	PinOutcomeGrant PinOutcome = "grant"

	// PinOutcomeAmbiguous is a server whose evidence names several clients,
	// or a proxied sole consumer whose bound clients span several providers.
	PinOutcomeAmbiguous PinOutcome = "ambiguous"

	// PinOutcomeNoEvidence is a server no evidence decides: no metadata or
	// grant names one client, the decided client was explicitly detached
	// from it, or it is a hosted server on a shared issuer.
	PinOutcomeNoEvidence PinOutcome = "no_evidence"
)

// PinDecision is one server's pinning decision.
type PinDecision struct {
	// MCPServerID is the server decided.
	MCPServerID uuid.UUID

	// Outcome classifies the server.
	Outcome PinOutcome

	// Associate lists the decided clients to associate with the server.
	Associate []uuid.UUID
}

// Pin is one association written by PinServers.
type Pin struct {
	// MCPServerID is the server associated.
	MCPServerID uuid.UUID

	// RemoteSessionClientID is the client associated with it.
	RemoteSessionClientID uuid.UUID

	// Outcome is the evidence the association was decided from.
	Outcome PinOutcome
}

// DecidePin classifies one server from its candidate rows, which all belong
// to that server, and lists the clients to associate. It is stricter than
// Decide because it runs where the server's issuer is about to change, so it
// records only clients the server uses today and that nothing contradicts. A
// metadata or grant pin on a shared issuer can narrow a server that required
// every unclaimed client down to the one its evidence names; the caller
// reports that as a change rather than a preserved client.
//
//   - it never touches a server that already has an association or an
//     administrator-configured OAuth client;
//   - it never re-associates a client explicitly detached from the server;
//   - it never decides from the derived authorization server, which is
//     itself inferred from the issuer's bindings;
//   - a proxied sole consumer whose bound clients span several providers is
//     decided by metadata or grants instead, since one upstream has one
//     authorization server.
func DecidePin(rows []ListUpstreamClientBackfillCandidatesRow) PinDecision {
	first := rows[0]
	decision := PinDecision{MCPServerID: first.McpServerID, Outcome: PinOutcomeUnbound, Associate: nil}

	var bound, matched, granted []ListUpstreamClientBackfillCandidatesRow
	providers := make(map[uuid.UUID]struct{})
	for _, row := range rows {
		if !row.RemoteSessionClientID.Valid {
			continue
		}
		bound = append(bound, row)
		if row.ClientProviderID.Valid {
			providers[row.ClientProviderID.UUID] = struct{}{}
		}
		if row.MetadataMatch {
			matched = append(matched, row)
		}
		if row.GrantMatch {
			granted = append(granted, row)
		}
	}

	switch {
	case first.UpstreamAuthNotRequired:
		decision.Outcome = PinOutcomeNoAuth
		return decision
	case len(bound) == 0:
		return decision
	case first.LiveAssociations > 0 || first.OffersOwnOauthClient:
		decision.Outcome = PinOutcomeConfigured
		return decision
	}

	if first.IssuerServerCount <= 1 && !first.IssuerHasGateway && (!first.Proxied || len(providers) <= 1) {
		decision.Outcome = PinOutcomeSoleConsumer
		for _, row := range bound {
			if !row.PairRecorded {
				decision.Associate = append(decision.Associate, row.RemoteSessionClientID.UUID)
			}
		}
		if len(decision.Associate) == 0 {
			decision.Outcome = PinOutcomeNoEvidence
		}
		return decision
	}

	switch {
	case len(matched) > 1:
		decision.Outcome = PinOutcomeAmbiguous
	case len(matched) == 1:
		decision.Outcome = PinOutcomeMetadataMatch
		decision.Associate = unrecorded(matched)
	case !first.Proxied:
		decision.Outcome = PinOutcomeNoEvidence
	case len(granted) > 1:
		decision.Outcome = PinOutcomeAmbiguous
	case len(granted) == 1:
		decision.Outcome = PinOutcomeGrant
		decision.Associate = unrecorded(granted)
	default:
		decision.Outcome = PinOutcomeNoEvidence
	}
	if len(decision.Associate) == 0 && (decision.Outcome == PinOutcomeMetadataMatch || decision.Outcome == PinOutcomeGrant) {
		decision.Outcome = PinOutcomeNoEvidence
	}
	return decision
}

// unrecorded returns the clients of rows with no association row for the
// pair, live or removed.
func unrecorded(rows []ListUpstreamClientBackfillCandidatesRow) []uuid.UUID {
	var ids []uuid.UUID
	for _, row := range rows {
		if !row.PairRecorded {
			ids = append(ids, row.RemoteSessionClientID.UUID)
		}
	}
	return ids
}

// PinServers decides the named servers of organizationID with DecidePin and
// writes each decided association, conditional on the evidence it was decided
// from still holding. It returns the associations written. db is expected to
// be the caller's transaction, which must hold the writer scope of the
// servers' projects; the caller records the auth clients of the pinned
// servers.
//
// scopeIssuerIDs are the user session issuers whose servers the caller
// accounts for. A client also bound to another issuer where serving would
// count the new association as a claim, the organization's only
// organization-level issuer once retiringIssuerIDs are gone or the issuer of
// a gateway fronting one of the servers, is not pinned: the association would
// change what unassociated servers on that issuer require, out of the
// caller's sight.
//
// Under upstream auth enforcement it writes nothing: an enforced server
// offers only the auth clients configured for it, never an association
// inferred from its issuer.
//
// The write rechecks the evidence the way the backfill does. For a grant
// that includes the backfill's agreement with the derived authorization
// server, so a grant pin is skipped, never inverted, when the derived value
// names another client; the server then reports whatever the move does to it
// instead of being pinned.
func PinServers(ctx context.Context, db DBTX, organizationID string, serverIDs, scopeIssuerIDs, retiringIssuerIDs []uuid.UUID) ([]Pin, error) {
	if len(serverIDs) == 0 {
		return nil, nil
	}
	enforced, err := authenforcement.ReadEnforced(ctx, db, organizationID)
	if err != nil {
		return nil, fmt.Errorf("read upstream auth enforcement: %w", err)
	}
	if enforced {
		return nil, nil
	}
	q := New(db)
	rows, err := q.ListUpstreamClientBackfillCandidates(ctx, ListUpstreamClientBackfillCandidatesParams{
		Cursor:         uuid.Nil,
		OrganizationID: organizationID,
		McpServerIds:   serverIDs,
		PageSize:       conv.SafeInt32(len(serverIDs)),
	})
	if err != nil {
		return nil, fmt.Errorf("list pin candidates: %w", err)
	}
	servers := groupByServer(rows)
	decisions := make([]PinDecision, 0, len(servers))
	var decided []uuid.UUID
	for _, server := range servers {
		decision := DecidePin(server)
		decisions = append(decisions, decision)
		decided = append(decided, decision.Associate...)
	}
	// A client claimed for every server, through the organization issuer,
	// maps to uuid.Nil; one claimed through a gateway maps to its member.
	claimed := make(map[uuid.UUID]map[uuid.UUID]bool)
	if len(decided) > 0 {
		ids, err := q.ListClientsClaimedThroughOtherIssuers(ctx, ListClientsClaimedThroughOtherIssuersParams{
			RemoteSessionClientIds: decided,
			ScopeIssuerIds:         append([]uuid.UUID{}, scopeIssuerIDs...),
			OrganizationID:         organizationID,
			RetiringIssuerIds:      append([]uuid.UUID{}, retiringIssuerIDs...),
			McpServerIds:           serverIDs,
		})
		if err != nil {
			return nil, fmt.Errorf("list clients claimed through other issuers: %w", err)
		}
		for _, row := range ids {
			if claimed[row.RemoteSessionClientID] == nil {
				claimed[row.RemoteSessionClientID] = make(map[uuid.UUID]bool)
			}
			claimed[row.RemoteSessionClientID][row.McpServerID.UUID] = true
		}
	}

	var pins []Pin
	for i, server := range servers {
		decision := decisions[i]
		for _, clientID := range decision.Associate {
			if claimed[clientID][uuid.Nil] || claimed[clientID][decision.MCPServerID] {
				continue
			}
			n, err := q.BackfillUpstreamClientAssociation(ctx, BackfillUpstreamClientAssociationParams{
				RemoteSessionClientID: clientID,
				McpServerID:           decision.MCPServerID,
				UserSessionIssuerID:   server[0].UserSessionIssuerID,
				RequireSoleConsumer:   decision.Outcome == PinOutcomeSoleConsumer,
				RespectRemoved:        true,
				RequireMetadataMatch:  decision.Outcome == PinOutcomeMetadataMatch,
				RequireEvidenceMatch:  decision.Outcome == PinOutcomeGrant,
			})
			if err != nil {
				return nil, fmt.Errorf("pin association for server %s: %w", decision.MCPServerID, err)
			}
			if n > 0 {
				pins = append(pins, Pin{MCPServerID: decision.MCPServerID, RemoteSessionClientID: clientID, Outcome: decision.Outcome})
			}
		}
	}
	return pins, nil
}
