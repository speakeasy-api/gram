package usersessions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authenforcement"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/associationbackfill"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// serverImpactSeverity ranks a server impact. A blocker stops the
// consolidation, a warning needs confirming through the fingerprint, and info
// only reports a pinned association.
type serverImpactSeverity string

const (
	serverImpactBlocker serverImpactSeverity = "blocker"
	serverImpactWarning serverImpactSeverity = "warning"
	serverImpactInfo    serverImpactSeverity = "info"
)

// serverImpactReason says why a server is listed in a consolidation preview.
type serverImpactReason string

const (
	// impactMultipleClientsForProvider: the server would use two or more
	// clients of one provider, a set it did not use before. Blocker.
	impactMultipleClientsForProvider serverImpactReason = "multiple_clients_for_provider"

	// impactProviderNotInMetadata: the server would gain a client whose
	// provider its upstream's protected resource metadata does not list.
	// Blocker.
	impactProviderNotInMetadata serverImpactReason = "provider_not_in_metadata"

	// impactGainsClient: the server uses no client today and would use some.
	impactGainsClient serverImpactReason = "gains_client"

	// impactLosesClient: the server uses clients today and would use none.
	impactLosesClient serverImpactReason = "loses_client"

	// impactChangesProvider: the providers the server uses would change.
	impactChangesProvider serverImpactReason = "changes_provider"

	// impactChangesClients: the same providers, different clients.
	impactChangesClients serverImpactReason = "changes_clients"

	// impactPinned: consolidation associates the server with the clients its
	// current configuration proves it uses.
	impactPinned serverImpactReason = "pinned"
)

// serverClientSnapshot is what a set of MCP servers looks like at one point
// of a consolidation: each server's details and the upstream OAuth clients it
// uses.
type serverClientSnapshot struct {
	// servers holds each live server's details, keyed by id.
	servers map[uuid.UUID]repo.ListUserSessionIssuerMigrationImpactServersRow

	// clients holds the clients each server uses, sorted, keyed by server id.
	clients map[uuid.UUID][]uuid.UUID
}

// snapshotServerClients reads, through tx, the upstream OAuth clients each of
// serverIDs uses, so it sees the consolidation's own uncommitted writes.
//
// The clients a server uses are the ones its callers must have connected: on
// an organization enforcing upstream auth, the clients its auth clients offer;
// otherwise, for a hosted server on a user session issuer, every client the
// issuer binds; for a proxied one, the clients serving requires of it
// (remotesessions.RequiredServerClients); and for any other server, such as
// one reached only as a gateway member, its associated clients.
func snapshotServerClients(ctx context.Context, tx pgx.Tx, organizationID string, serverIDs []uuid.UUID) (serverClientSnapshot, error) {
	snapshot := serverClientSnapshot{
		servers: make(map[uuid.UUID]repo.ListUserSessionIssuerMigrationImpactServersRow, len(serverIDs)),
		clients: make(map[uuid.UUID][]uuid.UUID, len(serverIDs)),
	}
	if len(serverIDs) == 0 {
		return snapshot, nil
	}

	servers, err := repo.New(tx).ListUserSessionIssuerMigrationImpactServers(ctx, repo.ListUserSessionIssuerMigrationImpactServersParams{
		McpServerIds:   serverIDs,
		OrganizationID: organizationID,
	})
	if err != nil {
		return snapshot, fmt.Errorf("list impact servers: %w", err)
	}
	for _, server := range servers {
		snapshot.servers[server.ID] = server
		snapshot.clients[server.ID] = nil
	}

	enforced, err := authenforcement.ReadEnforced(ctx, tx, organizationID)
	if err != nil {
		return snapshot, fmt.Errorf("read upstream auth enforcement: %w", err)
	}
	if enforced {
		offers, err := upstreamclientsrepo.New(tx).ListServerConsentAuthClients(ctx, upstreamclientsrepo.ListServerConsentAuthClientsParams{
			OrganizationID: organizationID,
			McpServerIds:   serverIDs,
		})
		if err != nil {
			return snapshot, fmt.Errorf("list offered auth clients: %w", err)
		}
		for _, offer := range offers {
			if _, live := snapshot.servers[offer.McpServerID]; !live || !offer.RemoteSessionClientID.Valid {
				continue
			}
			snapshot.clients[offer.McpServerID] = append(snapshot.clients[offer.McpServerID], offer.RemoteSessionClientID.UUID)
		}
		return snapshot.sorted(), nil
	}

	associations, err := repo.New(tx).ListUserSessionIssuerMigrationImpactAssociations(ctx, repo.ListUserSessionIssuerMigrationImpactAssociationsParams{
		McpServerIds:   serverIDs,
		OrganizationID: organizationID,
	})
	if err != nil {
		return snapshot, fmt.Errorf("list impact associations: %w", err)
	}
	associated := make(map[uuid.UUID][]uuid.UUID)
	for _, association := range associations {
		associated[association.McpServerID] = append(associated[association.McpServerID], association.RemoteSessionClientID)
	}
	// Servers of one project on one issuer see the same bound clients, so
	// each pair is read once per snapshot.
	type issuerProject struct{ issuerID, projectID uuid.UUID }
	bound := make(map[issuerProject][]uuid.UUID)
	for _, server := range servers {
		if !server.UserSessionIssuerID.Valid {
			snapshot.clients[server.ID] = associated[server.ID]
			continue
		}
		key := issuerProject{issuerID: server.UserSessionIssuerID.UUID, projectID: server.ProjectID}
		clientIDs, ok := bound[key]
		if !ok {
			clientIDs, err = remotesessions.BoundUserSessionIssuerClients(ctx, tx, organizationID, server.ProjectID, key.issuerID)
			if err != nil {
				return snapshot, fmt.Errorf("bound clients of server %s: %w", server.ID, err)
			}
			bound[key] = clientIDs
		}
		// A hosted server resolves every client its issuer binds; only a
		// proxied one narrows them to the clients it requires.
		if server.Hosted {
			snapshot.clients[server.ID] = slices.Clone(clientIDs)
			continue
		}
		required, err := remotesessions.RequiredServerClients(ctx, tx, organizationID, key.issuerID, server.ID, clientIDs)
		if err != nil {
			return snapshot, fmt.Errorf("required clients of server %s: %w", server.ID, err)
		}
		snapshot.clients[server.ID] = required
	}
	return snapshot.sorted(), nil
}

// sorted sorts and deduplicates every server's clients, so snapshots compare
// and fingerprint deterministically.
func (s serverClientSnapshot) sorted() serverClientSnapshot {
	for id, clients := range s.clients {
		slices.SortFunc(clients, compareUUIDs)
		s.clients[id] = slices.Compact(clients)
	}
	return s
}

// clientsOfServersOn returns the clients the snapshot's servers on issuerID
// use, sorted and deduplicated.
func (s serverClientSnapshot) clientsOfServersOn(issuerID uuid.UUID) []uuid.UUID {
	var ids []uuid.UUID
	for id, server := range s.servers {
		if server.UserSessionIssuerID.Valid && server.UserSessionIssuerID.UUID == issuerID {
			ids = append(ids, s.clients[id]...)
		}
	}
	slices.SortFunc(ids, compareUUIDs)
	return slices.Compact(ids)
}

// serversOn returns the ids of the snapshot's servers whose own user session
// issuer is one of issuerIDs, sorted.
func (s serverClientSnapshot) serversOn(issuerIDs ...uuid.UUID) []uuid.UUID {
	var ids []uuid.UUID
	for id, server := range s.servers {
		if server.UserSessionIssuerID.Valid && slices.Contains(issuerIDs, server.UserSessionIssuerID.UUID) {
			ids = append(ids, id)
		}
	}
	slices.SortFunc(ids, compareUUIDs)
	return ids
}

// migrationClient is an upstream OAuth client as a consolidation preview
// labels it.
type migrationClient struct {
	// id is the remote session client.
	id uuid.UUID

	// clientID is its OAuth client_id at its provider.
	clientID string

	// providerID is its provider, or uuid.Nil when unknown.
	providerID uuid.UUID

	// providerIssuer is its provider's issuer URL, or empty when unknown.
	providerIssuer string
}

// serverImpact is how a consolidation changes one MCP server's clients.
type serverImpact struct {
	// server holds the server's details as they are after the consolidation.
	server repo.ListUserSessionIssuerMigrationImpactServersRow

	// severity ranks the impact.
	severity serverImpactSeverity

	// reasons say why the server is listed, in a fixed order.
	reasons []serverImpactReason

	// before are the clients the server used, sorted.
	before []migrationClient

	// after are the clients the server uses, sorted.
	after []migrationClient

	// pinned are the clients consolidation associated with the server, sorted.
	pinned []uuid.UUID
}

// classifyServerImpacts compares each server's clients before and after the
// consolidation and lists the servers whose clients change or that were
// pinned, ordered by server id.
func classifyServerImpacts(ctx context.Context, tx pgx.Tx, organizationID string, before, after serverClientSnapshot, pins []associationbackfill.Pin) ([]serverImpact, error) {
	pinned := make(map[uuid.UUID][]uuid.UUID)
	for _, pin := range pins {
		pinned[pin.MCPServerID] = append(pinned[pin.MCPServerID], pin.RemoteSessionClientID)
	}

	var changedIDs, clientIDs []uuid.UUID
	for id := range after.servers {
		if slices.Equal(before.clients[id], after.clients[id]) && len(pinned[id]) == 0 {
			continue
		}
		changedIDs = append(changedIDs, id)
		clientIDs = append(clientIDs, before.clients[id]...)
		clientIDs = append(clientIDs, after.clients[id]...)
	}
	if len(changedIDs) == 0 {
		return nil, nil
	}
	slices.SortFunc(changedIDs, compareUUIDs)

	labels, err := labelMigrationClients(ctx, tx, organizationID, clientIDs)
	if err != nil {
		return nil, err
	}

	impacts := make([]serverImpact, 0, len(changedIDs))
	for _, id := range changedIDs {
		serverPins := pinned[id]
		slices.SortFunc(serverPins, compareUUIDs)
		impact := serverImpact{
			server:   after.servers[id],
			severity: serverImpactInfo,
			reasons:  nil,
			before:   labelled(labels, before.clients[id]),
			after:    labelled(labels, after.clients[id]),
			pinned:   serverPins,
		}
		impact.reasons = impactReasons(impact)
		impact.severity = impactSeverity(impact.reasons)
		impacts = append(impacts, impact)
	}
	return impacts, nil
}

// impactReasons decides why a server is listed, in a fixed order.
func impactReasons(impact serverImpact) []serverImpactReason {
	var reasons []serverImpactReason

	// Clients whose provider is unknown are not known to share one.
	beforeByProvider := clientsByProvider(impact.before)
	for provider, clients := range clientsByProvider(impact.after) {
		if provider != uuid.Nil && len(clients) > 1 && !slices.Equal(clients, beforeByProvider[provider]) {
			reasons = append(reasons, impactMultipleClientsForProvider)
			break
		}
	}

	// An upstream that advertises no authorization server says nothing about
	// which providers it accepts, the same as one with no metadata.
	if len(impact.server.AuthorizationServers) > 0 {
		for _, client := range impact.after {
			if containsClient(impact.before, client.id) {
				continue
			}
			if !advertisesIssuer(impact.server.AuthorizationServers, client.providerIssuer) {
				reasons = append(reasons, impactProviderNotInMetadata)
				break
			}
		}
	}

	beforeIDs, afterIDs := clientIDsOf(impact.before), clientIDsOf(impact.after)
	switch {
	case slices.Equal(beforeIDs, afterIDs):
	case len(beforeIDs) == 0:
		reasons = append(reasons, impactGainsClient)
	case len(afterIDs) == 0:
		reasons = append(reasons, impactLosesClient)
	case !slices.Equal(providersOf(impact.before), providersOf(impact.after)):
		reasons = append(reasons, impactChangesProvider)
	default:
		reasons = append(reasons, impactChangesClients)
	}

	if len(impact.pinned) > 0 {
		reasons = append(reasons, impactPinned)
	}
	return reasons
}

// impactSeverity ranks a server's reasons.
func impactSeverity(reasons []serverImpactReason) serverImpactSeverity {
	severity := serverImpactInfo
	for _, reason := range reasons {
		switch reason {
		case impactMultipleClientsForProvider, impactProviderNotInMetadata:
			return serverImpactBlocker
		case impactGainsClient, impactLosesClient, impactChangesProvider, impactChangesClients:
			severity = serverImpactWarning
		case impactPinned:
		}
	}
	return severity
}

// labelMigrationClients reads the provider of each named client.
func labelMigrationClients(ctx context.Context, tx pgx.Tx, organizationID string, ids []uuid.UUID) (map[uuid.UUID]migrationClient, error) {
	labels := make(map[uuid.UUID]migrationClient, len(ids))
	if len(ids) == 0 {
		return labels, nil
	}
	slices.SortFunc(ids, compareUUIDs)
	rows, err := repo.New(tx).ListUserSessionIssuerMigrationImpactClients(ctx, repo.ListUserSessionIssuerMigrationImpactClientsParams{
		RemoteSessionClientIds: slices.Compact(ids),
		OrganizationID:         organizationID,
	})
	if err != nil {
		return nil, fmt.Errorf("label impact clients: %w", err)
	}
	for _, row := range rows {
		labels[row.ID] = migrationClient{id: row.ID, clientID: row.ClientID, providerID: row.ProviderID, providerIssuer: row.ProviderIssuer}
	}
	return labels, nil
}

// labelled returns the labels of ids, keeping their order. A client with no
// label keeps its id and an unknown provider.
func labelled(labels map[uuid.UUID]migrationClient, ids []uuid.UUID) []migrationClient {
	clients := make([]migrationClient, 0, len(ids))
	for _, id := range ids {
		label, ok := labels[id]
		if !ok {
			label = migrationClient{id: id, clientID: "", providerID: uuid.Nil, providerIssuer: ""}
		}
		clients = append(clients, label)
	}
	return clients
}

// clientsByProvider groups client ids by provider, each group sorted.
func clientsByProvider(clients []migrationClient) map[uuid.UUID][]uuid.UUID {
	grouped := make(map[uuid.UUID][]uuid.UUID)
	for _, client := range clients {
		grouped[client.providerID] = append(grouped[client.providerID], client.id)
	}
	for provider := range grouped {
		slices.SortFunc(grouped[provider], compareUUIDs)
	}
	return grouped
}

// providersOf returns the distinct providers of clients, sorted.
func providersOf(clients []migrationClient) []uuid.UUID {
	providers := make([]uuid.UUID, 0, len(clients))
	for _, client := range clients {
		providers = append(providers, client.providerID)
	}
	slices.SortFunc(providers, compareUUIDs)
	return slices.Compact(providers)
}

func clientIDsOf(clients []migrationClient) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(clients))
	for _, client := range clients {
		ids = append(ids, client.id)
	}
	return ids
}

func containsClient(clients []migrationClient, id uuid.UUID) bool {
	return slices.ContainsFunc(clients, func(client migrationClient) bool { return client.id == id })
}

// advertisesIssuer reports whether an upstream's advertised authorization
// servers list issuer, ignoring a trailing slash, as serving compares them.
// An unknown provider issuer is never advertised.
func advertisesIssuer(authorizationServers []string, issuer string) bool {
	if issuer == "" {
		return false
	}
	want := strings.TrimRight(issuer, "/")
	return slices.ContainsFunc(authorizationServers, func(server string) bool {
		return strings.TrimRight(server, "/") == want
	})
}

func compareUUIDs(a, b uuid.UUID) int {
	return strings.Compare(a.String(), b.String())
}

// hasBlocker reports whether any impact blocks the consolidation.
func hasBlocker(impacts []serverImpact) bool {
	return slices.ContainsFunc(impacts, func(impact serverImpact) bool { return impact.severity == serverImpactBlocker })
}

// userSessionMigrationFingerprint fingerprints what an administrator confirms
// before a consolidation: the setting differences, the server impacts, and
// the dropped client links. It covers only ids, severities and reasons, never
// counts or timestamps, so it changes only when the consolidation would do
// something different. It is empty when there is nothing to confirm.
func userSessionMigrationFingerprint(sourceID, targetID uuid.UUID, warnings []*orggen.UserSessionIssuerFieldMismatch, impacts []serverImpact, dropped []migrationClient) string {
	// An info impact only reports a pinned association that leaves the
	// server's clients as they are, so there is nothing to decide about it.
	impacts = slices.DeleteFunc(slices.Clone(impacts), func(impact serverImpact) bool { return impact.severity == serverImpactInfo })
	if len(warnings) == 0 && len(impacts) == 0 && len(dropped) == 0 {
		return ""
	}

	hash := sha256.New()
	write := func(values ...string) {
		for _, value := range values {
			_, _ = hash.Write([]byte(value))
			_, _ = hash.Write([]byte{0})
		}
	}
	write(sourceID.String(), targetID.String())
	for _, warning := range warnings {
		write("warning", warning.Field, warning.SourceValue, warning.TargetValue)
	}
	for _, impact := range impacts {
		write("server", impact.server.ID.String(), string(impact.severity))
		for _, reason := range impact.reasons {
			write("reason", string(reason))
		}
		for _, client := range impact.before {
			write("before", client.id.String())
		}
		for _, client := range impact.after {
			write("after", client.id.String())
		}
		for _, id := range impact.pinned {
			write("pinned", id.String())
		}
	}
	for _, client := range dropped {
		write("dropped", client.id.String())
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func migrationClientViews(clients []migrationClient) []*orggen.UserSessionIssuerMigrationClient {
	views := make([]*orggen.UserSessionIssuerMigrationClient, 0, len(clients))
	for _, client := range clients {
		views = append(views, &orggen.UserSessionIssuerMigrationClient{
			RemoteSessionClientID: client.id.String(),
			ClientID:              client.clientID,
			ProviderIssuer:        client.providerIssuer,
		})
	}
	return views
}

func serverImpactViews(impacts []serverImpact) []*orggen.UserSessionIssuerMigrationServerImpact {
	views := make([]*orggen.UserSessionIssuerMigrationServerImpact, 0, len(impacts))
	for _, impact := range impacts {
		reasons := make([]orggen.UserSessionIssuerMigrationImpactReason, 0, len(impact.reasons))
		for _, reason := range impact.reasons {
			reasons = append(reasons, orggen.UserSessionIssuerMigrationImpactReason(reason))
		}
		pinned := make([]string, 0, len(impact.pinned))
		for _, id := range impact.pinned {
			pinned = append(pinned, id.String())
		}
		views = append(views, &orggen.UserSessionIssuerMigrationServerImpact{
			McpServerID:     impact.server.ID.String(),
			McpServerName:   conv.FromPGTextOrEmpty[string](impact.server.Name),
			McpServerSlug:   conv.FromPGTextOrEmpty[string](impact.server.Slug),
			ProjectID:       impact.server.ProjectID.String(),
			ProjectName:     impact.server.ProjectName,
			ImpactSeverity:  orggen.UserSessionIssuerMigrationImpactSeverity(impact.severity),
			Reasons:         reasons,
			ClientsBefore:   migrationClientViews(impact.before),
			ClientsAfter:    migrationClientViews(impact.after),
			PinnedClientIds: pinned,
		})
	}
	return views
}

func auditServerImpacts(impacts []serverImpact) []audit.UserSessionIssuerMigrationServerImpact {
	summaries := make([]audit.UserSessionIssuerMigrationServerImpact, 0, len(impacts))
	for _, impact := range impacts {
		reasons := make([]string, 0, len(impact.reasons))
		for _, reason := range impact.reasons {
			reasons = append(reasons, string(reason))
		}
		summaries = append(summaries, audit.UserSessionIssuerMigrationServerImpact{
			MCPServerID:   impact.server.ID,
			Severity:      string(impact.severity),
			Reasons:       reasons,
			ClientsBefore: clientIDsOf(impact.before),
			ClientsAfter:  clientIDsOf(impact.after),
		})
	}
	return summaries
}

func auditPins(pins []associationbackfill.Pin) []audit.UserSessionIssuerMigrationPin {
	recorded := make([]audit.UserSessionIssuerMigrationPin, 0, len(pins))
	for _, pin := range pins {
		recorded = append(recorded, audit.UserSessionIssuerMigrationPin{MCPServerID: pin.MCPServerID, RemoteSessionClientID: pin.RemoteSessionClientID})
	}
	return recorded
}
