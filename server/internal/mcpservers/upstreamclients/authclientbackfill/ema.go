package authclientbackfill

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// emaUpstream is one identity chaining upstream: the ready bindings that name
// one upstream issuer, registration and resource in the organization.
type emaUpstream struct {
	issuerID uuid.UUID
	clientID uuid.UUID
	resource string
}

// emaTarget is one decided ema client and the servers it is offered by. Its
// id is nil when a dry run would record it.
type emaTarget struct {
	clientID uuid.UUID
	recorded bool
	servers  []uuid.UUID
}

// emaPlan is what the EMA phase decided, for the phases after it: which
// servers an ema client serves, so their credential headers go onto that
// client, and the groups whose unmatched servers are warned about last.
type emaPlan struct {
	targets []emaTarget
	served  map[uuid.UUID]bool
	// direct holds the served servers an ema client serves on their direct
	// route too, not only through the gateways.
	direct  map[uuid.UUID]bool
	order   []emaUpstream
	groups  map[emaUpstream][]ListEMABackfillBindingsRow
	reached map[emaUpstream][]reachedServer
}

func newEMAPlan() *emaPlan {
	return &emaPlan{targets: nil, served: map[uuid.UUID]bool{}, direct: map[uuid.UUID]bool{}, order: nil, groups: map[emaUpstream][]ListEMABackfillBindingsRow{}, reached: map[emaUpstream][]reachedServer{}}
}

// reaches reports whether an ema client serves the server. A nil plan, as
// when the EMA phase failed, serves none.
func (p *emaPlan) reaches(serverID uuid.UUID) bool {
	return p != nil && p.served[serverID]
}

// reachesDirectly reports whether an ema client serves the server on its
// direct route. A server reached only through the gateways keeps its direct
// route's own clients, which the ema client cannot replace there.
func (p *emaPlan) reachesDirectly(serverID uuid.UUID) bool {
	return p.reaches(serverID) && p.direct[serverID]
}

// runEMA groups the organization's ready, client-bound identity chaining
// bindings into one ema client per upstream, offered by every server the
// bindings reach as the runtime matches them. The bindings' user session
// issuer is not carried over: it is inbound only. It records its decisions in
// plan for the header phase and the warnings after it.
func runEMA(ctx context.Context, tx pgx.Tx, opts Options, report *Report, want *expected, plan *emaPlan) error {
	q := New(tx)
	bindings, err := q.ListEMABackfillBindings(ctx, opts.OrganizationID)
	if err != nil {
		return fmt.Errorf("list identity chaining bindings: %w", err)
	}
	groups := map[emaUpstream][]ListEMABackfillBindingsRow{}
	var order []emaUpstream
	for _, binding := range bindings {
		if !binding.InReach {
			report.add(Result{
				Kind:                  KindEMA,
				SubjectID:             binding.BindingID,
				AssociationID:         uuid.Nil,
				MCPServerID:           uuid.Nil,
				ProjectID:             binding.ProjectID,
				RemoteSessionClientID: binding.RemoteSessionClientID,
				BindingIDs:            []uuid.UUID{binding.BindingID},
				SourceIDs:             nil,
				ServerIDs:             nil,
				AuthClientID:          uuid.NullUUID{UUID: uuid.Nil, Valid: false},
				Outcome:               OutcomeAmbiguous,
				Reason:                ReasonRegistrationOutOfReach,
			})
			continue
		}
		key := emaUpstream{issuerID: binding.RemoteSessionIssuerID, clientID: binding.RemoteSessionClientID, resource: binding.Resource}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], binding)
	}
	plan.order, plan.groups = order, groups
	for _, key := range order {
		servers, err := reachedServers(ctx, q, opts.OrganizationID, groups[key])
		if err != nil {
			return err
		}
		plan.reached[key] = servers
	}
	for _, key := range order {
		if err := decideEMA(ctx, tx, q, opts, key, groups[key], plan.reached[key], report, want, plan); err != nil {
			return err
		}
	}
	return nil
}

// reportEMAWarnings warns of same-origin servers the EMA phase's bindings
// could cover that no backfilled client covers. It runs after every phase
// that records clients, so committed holds all their decisions.
func reportEMAWarnings(ctx context.Context, q *Queries, opts Options, plan *emaPlan, committed *Report, report *Report) error {
	covered := map[uuid.UUID]bool{}
	for _, result := range committed.Results {
		if result.MCPServerID != uuid.Nil && result.AuthClientID.Valid && result.Outcome.Covered() && result.Outcome != OutcomeWarning {
			covered[result.MCPServerID] = true
		}
	}
	for _, key := range plan.order {
		for _, server := range plan.reached[key] {
			covered[server.id] = true
		}
	}
	for _, key := range plan.order {
		if err := reportUnmatchedServers(ctx, q, opts, key, plan.groups[key], covered, report); err != nil {
			return err
		}
	}
	return nil
}

// reachedServer is one live server a group's bindings reach; allowed reports
// that its protected resource metadata, if recorded, lists the issuer.
type reachedServer struct {
	id, projectID uuid.UUID
	allowed       bool
	direct        bool
}

func reachedServers(ctx context.Context, q *Queries, organizationID string, bindings []ListEMABackfillBindingsRow) ([]reachedServer, error) {
	seen := map[uuid.UUID]int{}
	var servers []reachedServer
	for _, binding := range bindings {
		rows, err := q.ListEMABindingServers(ctx, ListEMABindingServersParams{BindingID: binding.BindingID, OrganizationID: organizationID})
		if err != nil {
			return nil, fmt.Errorf("list servers of binding %s: %w", binding.BindingID, err)
		}
		for _, row := range rows {
			if i, ok := seen[row.McpServerID]; ok {
				// Reached by several bindings: directly if any reaches it so.
				servers[i].direct = servers[i].direct || row.Direct
				continue
			}
			seen[row.McpServerID] = len(servers)
			servers = append(servers, reachedServer{id: row.McpServerID, projectID: row.ProjectID, allowed: row.Match == "match", direct: row.Direct})
		}
	}
	return servers, nil
}

// reportUnmatchedServers warns, once per server, of each same-origin server a
// group's bindings could cover that has no other upstream identity: its
// metadata may name the group's resource, as the runtime's origin fallback
// allows, but only live discovery can confirm that. It never holds the run
// back.
func reportUnmatchedServers(ctx context.Context, q *Queries, opts Options, key emaUpstream, bindings []ListEMABackfillBindingsRow, covered map[uuid.UUID]bool, report *Report) error {
	ids := make([]uuid.UUID, 0, len(bindings))
	for _, binding := range bindings {
		ids = append(ids, binding.BindingID)
	}
	for _, binding := range bindings {
		rows, err := q.ListEMASameOriginServers(ctx, ListEMASameOriginServersParams{BindingID: binding.BindingID, OrganizationID: opts.OrganizationID})
		if err != nil {
			return fmt.Errorf("list same-origin servers of binding %s: %w", binding.BindingID, err)
		}
		for _, row := range rows {
			if covered[row.McpServerID] {
				continue
			}
			covered[row.McpServerID] = true
			report.add(Result{
				Kind:                  KindEMA,
				SubjectID:             bindings[0].BindingID,
				AssociationID:         uuid.Nil,
				MCPServerID:           row.McpServerID,
				ProjectID:             row.ProjectID,
				RemoteSessionClientID: key.clientID,
				BindingIDs:            ids,
				SourceIDs:             nil,
				ServerIDs:             nil,
				AuthClientID:          uuid.NullUUID{UUID: uuid.Nil, Valid: false},
				Outcome:               OutcomeWarning,
				Reason:                ReasonServerNotMatched,
			})
		}
	}
	return nil
}

func decideEMA(ctx context.Context, tx pgx.Tx, q *Queries, opts Options, key emaUpstream, bindings []ListEMABackfillBindingsRow, servers []reachedServer, report *Report, want *expected, plan *emaPlan) error {
	first := bindings[0]
	ids := make([]uuid.UUID, 0, len(bindings))
	for _, binding := range bindings {
		ids = append(ids, binding.BindingID)
	}
	result := Result{
		Kind:                  KindEMA,
		SubjectID:             first.BindingID,
		AssociationID:         uuid.Nil,
		MCPServerID:           uuid.Nil,
		ProjectID:             first.ProjectID,
		RemoteSessionClientID: key.clientID,
		BindingIDs:            ids,
		SourceIDs:             nil,
		ServerIDs:             nil,
		AuthClientID:          uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Outcome:               OutcomeExisting,
		Reason:                "",
	}
	// One client carries one resource and one scope set; bindings that
	// disagree are not merged.
	scopes := normalizedScopes(first.RequestedScopes)
	for _, binding := range bindings[1:] {
		switch {
		case binding.ExactResource != first.ExactResource:
			result.Outcome, result.Reason = OutcomeAmbiguous, ReasonResourceVariants
		case !slices.Equal(normalizedScopes(binding.RequestedScopes), scopes):
			result.Outcome, result.Reason = OutcomeAmbiguous, ReasonConflictingScopes
		default:
			continue
		}
		reachedDirectly := map[uuid.UUID]bool{}
		for _, server := range servers {
			reachedDirectly[server.id] = server.direct
		}
		// Disagreeing bindings decide nothing new, but a client already
		// recorded for one of them keeps serving with its links until they
		// agree again.
		kept := map[uuid.UUID]bool{}
		for _, binding := range bindings {
			existing, found, err := findEMA(ctx, q, opts.OrganizationID, key.issuerID, key.clientID, binding.ExactResource)
			if err != nil {
				return err
			}
			if !found || kept[existing.ID] {
				continue
			}
			kept[existing.ID] = true
			want.race(existing.ID)
			// The kept client keeps exactly the servers it is linked to, so it
			// keeps carrying their credential headers and no other server's.
			linked, err := q.ListEMAClientLiveServers(ctx, ListEMAClientLiveServersParams{OrganizationID: opts.OrganizationID, AuthClientID: existing.ID})
			if err != nil {
				return fmt.Errorf("list servers of kept ema auth client %s: %w", existing.ID, err)
			}
			target := emaTarget{clientID: existing.ID, recorded: true, servers: linked}
			for _, serverID := range linked {
				plan.served[serverID] = true
				// A linked server the bindings reach only through the gateways
				// stays gateway-only; one they no longer reach keeps being
				// treated as served directly until its link retires.
				direct, reached := reachedDirectly[serverID]
				plan.direct[serverID] = plan.direct[serverID] || !reached || direct
			}
			plan.targets = append(plan.targets, target)
		}
		report.add(result)
		return nil
	}

	// The runtime refuses an issuer the server's metadata does not list.
	var allowed []reachedServer
	for _, server := range servers {
		if server.allowed {
			allowed = append(allowed, server)
			continue
		}
		refused := result
		refused.MCPServerID, refused.ProjectID = server.id, server.projectID
		refused.Outcome, refused.Reason = OutcomeAmbiguous, ReasonIssuerNotAllowed
		report.add(refused)
	}
	switch {
	case len(servers) == 0:
		result.Outcome, result.Reason = OutcomeAmbiguous, ReasonResourceNotMatched
		report.add(result)
		return nil
	case len(allowed) == 0:
		return nil
	}
	servers = allowed

	existing, found, err := findEMA(ctx, q, opts.OrganizationID, key.issuerID, key.clientID, first.ExactResource)
	if err != nil {
		return err
	}
	clientID := existing.ID
	live := found
	switch {
	case found && !slices.Equal(normalizedScopes(existing.RequestedScopes), scopes):
		result.Outcome = OutcomeUpdated
		report.pendingHeader(KindEMA)
		if opts.Apply {
			n, err := q.UpdateEMAScopes(ctx, UpdateEMAScopesParams{RequestedScopes: scopes, AuthClientID: clientID, OrganizationID: opts.OrganizationID})
			if err != nil {
				return fmt.Errorf("update scopes of ema auth client %s: %w", clientID, err)
			}
			if n == 0 {
				result.Outcome = OutcomeSkippedRace
				result.AuthClientID = uuid.NullUUID{UUID: clientID, Valid: true}
				want.race(clientID)
				report.add(result)
				return nil
			}
			report.wroteHeader(KindEMA)
		}
	case !found:
		result.Outcome = OutcomeCreated
		report.pendingClient(KindEMA)
		if opts.Apply {
			name := emaName
			if first.IssuerName.Valid && first.IssuerName.String != "" {
				name = first.IssuerName.String
			}
			var id uuid.UUID
			err := inSavepoint(ctx, tx, func(sp pgx.Tx) error {
				var err error
				id, err = New(sp).CreateEMAAuthClient(ctx, CreateEMAAuthClientParams{
					ID:                    EMAClientID(opts.OrganizationID, key.issuerID, key.clientID, first.ExactResource),
					RemoteSessionIssuerID: key.issuerID,
					Resource:              first.ExactResource,
					RequestedScopes:       scopes,
					OrganizationID:        opts.OrganizationID,
					Name:                  name,
					RemoteSessionClientID: key.clientID,
				})
				return err
			})
			switch {
			case isUniqueViolation(err):
				// Recorded concurrently: use that client.
				existing, found, err = findEMA(ctx, q, opts.OrganizationID, key.issuerID, key.clientID, first.ExactResource)
				if err != nil {
					return err
				}
				if !found {
					result.Outcome = OutcomeSkippedRace
					report.add(result)
					return nil
				}
				result.Outcome = OutcomeExisting
				id = existing.ID
			case errors.Is(err, pgx.ErrNoRows):
				result.Outcome = OutcomeSkippedRace
				report.add(result)
				return nil
			case err != nil:
				return fmt.Errorf("record ema auth client for binding %s: %w", first.BindingID, err)
			default:
				report.wroteClient(KindEMA)
			}
			clientID = id
			live = true
		}
	}
	if clientID != uuid.Nil {
		result.AuthClientID = uuid.NullUUID{UUID: clientID, Valid: true}
		want.clients[clientID] = true
	}
	report.add(result)
	target := emaTarget{clientID: clientID, recorded: live, servers: nil}
	for _, server := range servers {
		target.servers = append(target.servers, server.id)
		plan.served[server.id] = true
		plan.direct[server.id] = plan.direct[server.id] || server.direct
	}
	plan.targets = append(plan.targets, target)

	for _, server := range servers {
		link := Result{
			Kind:                  KindEMA,
			SubjectID:             first.BindingID,
			AssociationID:         uuid.Nil,
			MCPServerID:           server.id,
			ProjectID:             server.projectID,
			RemoteSessionClientID: key.clientID,
			BindingIDs:            ids,
			SourceIDs:             nil,
			ServerIDs:             nil,
			AuthClientID:          result.AuthClientID,
			Outcome:               OutcomeExisting,
			Reason:                "",
		}
		serverID := server.id
		err := linkServer(ctx, q, opts, &link, live, report, want, func() (int64, error) {
			return q.LinkEMAServer(ctx, LinkEMAServerParams{OrganizationID: opts.OrganizationID, AuthClientID: clientID, McpServerID: serverID})
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func findEMA(ctx context.Context, q *Queries, organizationID string, issuerID, clientID uuid.UUID, resource string) (FindEMAAuthClientRow, bool, error) {
	row, err := q.FindEMAAuthClient(ctx, FindEMAAuthClientParams{
		OrganizationID:        organizationID,
		RemoteSessionIssuerID: issuerID,
		RemoteSessionClientID: clientID,
		Resource:              resource,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return row, false, nil
	case err != nil:
		return row, false, fmt.Errorf("find ema auth client: %w", err)
	}
	return row, true, nil
}

// normalizedScopes sorts and deduplicates scopes so equal sets compare equal,
// the same way the binding queries normalize them. Both sort bytewise: the
// queries sort under COLLATE "C", since the database's default collation
// orders mixed-case scopes differently.
func normalizedScopes(scopes []string) []string {
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		if scope = strings.Trim(scope, " "); scope != "" {
			out = append(out, scope)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}
