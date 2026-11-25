package authclientbackfill

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// linkKey is one server offering one auth client.
type linkKey struct {
	serverID uuid.UUID
	clientID uuid.UUID
}

// expected collects the derived clients and links the configuration yields
// this run, so reconcileDerived retires the rest. A raced client's links are
// left alone, since the race cut its decision short.
type expected struct {
	clients map[uuid.UUID]bool
	links   map[linkKey]bool
	raced   map[uuid.UUID]bool
}

func newExpected() *expected {
	return &expected{clients: map[uuid.UUID]bool{}, links: map[linkKey]bool{}, raced: map[uuid.UUID]bool{}}
}

// race keeps a client whose write raced, with its links.
func (e *expected) race(clientID uuid.UUID) {
	e.clients[clientID] = true
	e.raced[clientID] = true
}

// kindOfType is the kind whose phase records a derived client type.
func kindOfType(clientType string) (Kind, bool) {
	switch clientType {
	case TypeStaticCredential, TypePassThrough:
		return KindHeader, true
	case TypeEMA:
		return KindEMA, true
	default:
		return "", false
	}
}

// reconcileDerived counts, and with Apply retires, backfilled
// static_credential, pass_through and ema clients the configuration no longer
// yields, their header copies and EMA details with them, and backfilled links
// to such clients the configuration no longer yields. Types whose phase
// failed this run are left alone. It runs after every phase, so a dry run and
// an apply see the same set. A retired client the configuration still yields
// is revived by its phase, so its links count as kept.
func reconcileDerived(ctx context.Context, q *Queries, opts Options, report *Report, want *expected, failed map[Kind]bool, startedAt pgtype.Timestamptz) error {
	var types []string
	for _, clientType := range []string{TypeStaticCredential, TypePassThrough, TypeEMA} {
		if kind, _ := kindOfType(clientType); !failed[kind] {
			types = append(types, clientType)
		}
	}
	if len(types) == 0 {
		return nil
	}
	clients, err := q.ListBackfillClientsOfTypes(ctx, ListBackfillClientsOfTypesParams{OrganizationID: opts.OrganizationID, ClientTypes: types})
	if err != nil {
		return fmt.Errorf("list derived auth clients: %w", err)
	}
	retiring := map[uuid.UUID]bool{}
	for _, client := range clients {
		if want.clients[client.ID] {
			continue
		}
		kind, _ := kindOfType(client.ClientType)
		retiring[client.ID] = true
		report.PendingRetiredClients++
		report.kind(kind).PendingRetirements++
		if !opts.Apply {
			continue
		}
		n, err := q.RetireBackfillClient(ctx, RetireBackfillClientParams{ID: client.ID, OrganizationID: opts.OrganizationID, ClientTypes: types, EvidenceSince: startedAt})
		if err != nil {
			return fmt.Errorf("retire auth client %s: %w", client.ID, err)
		}
		if n == 0 {
			// Its configuration changed during the run: keep it and its links
			// for the next run to decide.
			delete(retiring, client.ID)
			want.race(client.ID)
		}
		report.ClientsRetired += int(n)
		report.kind(kind).Retired += int(n)
	}
	links, err := q.ListBackfillLinksOfTypes(ctx, ListBackfillLinksOfTypesParams{OrganizationID: opts.OrganizationID, ClientTypes: types})
	if err != nil {
		return fmt.Errorf("list derived server links: %w", err)
	}
	for _, link := range links {
		if want.raced[link.AuthClientID] {
			continue
		}
		clientKept := !link.ClientDeleted || want.clients[link.AuthClientID]
		if clientKept && !retiring[link.AuthClientID] && want.links[linkKey{serverID: link.McpServerID, clientID: link.AuthClientID}] {
			continue
		}
		kind, _ := kindOfType(link.ClientType)
		report.PendingRetiredLinks++
		report.kind(kind).PendingRetirements++
		if !opts.Apply {
			continue
		}
		n, err := q.RetireBackfillLink(ctx, RetireBackfillLinkParams{ID: link.ID, OrganizationID: opts.OrganizationID})
		if err != nil {
			return fmt.Errorf("retire server link %s: %w", link.ID, err)
		}
		report.LinksRetired += int(n)
		report.kind(kind).Retired += int(n)
	}
	return nil
}
