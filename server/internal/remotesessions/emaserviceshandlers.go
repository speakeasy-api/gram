package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/issuerurl"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// emaService accumulates one vendor's bindings before the view is built.
type emaService struct {
	name     string
	issuerID uuid.UUID
	servers  map[uuid.UUID]*orgissuersgen.EMAServiceServer
	ready    map[uuid.UUID]struct{}
}

func newEMAService() *emaService {
	return &emaService{name: "", issuerID: uuid.Nil, servers: map[uuid.UUID]*orgissuersgen.EMAServiceServer{}, ready: map[uuid.UUID]struct{}{}}
}

// EMA server statuses, as the services view reports them.
const (
	emaServerWorking        = "working"
	emaServerNotProven      = "not_proven"
	emaServerNeedsAttention = "needs_attention"
)

// emaServerStatusRank orders statuses so a server with several bindings for
// one vendor shows its best one.
var emaServerStatusRank = map[string]int{emaServerNeedsAttention: 0, emaServerNotProven: 1, emaServerWorking: 2}

// emaBindingTarget keys a binding's credentials.
type emaBindingTarget struct {
	projectID           uuid.UUID
	userSessionIssuerID uuid.UUID
	remoteIssuerID      uuid.UUID
	resource            string
}

// emaEndpointOrderKey keys an endpoint's order: the sign-in decides its
// external provider, the vendor its saved order.
type emaEndpointOrderKey struct {
	userSessionIssuerID uuid.UUID
	vendor              string
}

// emaBindingSetUp reports whether a binding state means identity chaining was
// turned on for the server, whatever happened after.
func emaBindingSetUp(state string) bool {
	return state != "" && state != "unlinked" && state != "configuration_required"
}

func (s *Service) ListEMAServices(ctx context.Context, _ *orgissuersgen.ListEMAServicesPayload) (*orgissuersgen.ListEMAServicesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	org := authCtx.ActiveOrganizationID
	logger := s.logger.With(attr.SlogOrganizationID(org))
	if err := s.authz.Require(ctx, authz.Org(authz.OrgIDFrom(org)).Read()); err != nil {
		return nil, err
	}
	q := repo.New(s.db)

	rows, err := q.ListEMAServiceServers(ctx, org)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list identity chaining bindings").LogError(ctx, logger)
	}
	bindingAcceptance, err := s.listEMABindingAcceptance(ctx, q, org)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list ema binding acceptance").LogError(ctx, logger)
	}
	// Speakeasy readiness depends only on the organization's issuer, so one
	// check serves every vendor and endpoint in the request.
	readiness := memoizedReadiness(s.speakeasyReadiness)
	endpointOrders := map[emaEndpointOrderKey]EMAProviderOrder{}
	endpointOrder := func(userSessionIssuerID uuid.UUID, vendor string) (EMAProviderOrder, error) {
		key := emaEndpointOrderKey{userSessionIssuerID: userSessionIssuerID, vendor: vendor}
		if order, ok := endpointOrders[key]; ok {
			return order, nil
		}
		order, err := EndpointEMAProviders(ctx, q, org, userSessionIssuerID, vendor, s.speakeasySigningAvailable)
		if err != nil {
			return order, err
		}
		order, err = ApplyEMAProviderReadiness(ctx, org, order, readiness)
		if err != nil {
			logger.WarnContext(ctx, "check Speakeasy issuer readiness", attr.SlogError(err), attr.SlogOAuthIssuer(vendor))
		}
		endpointOrders[key] = order
		return order, nil
	}

	services := map[string]*emaService{}
	serverChecks := map[string]authz.Check{}
	for _, row := range rows {
		vendor, err := NormalizeVendorIssuer(row.Issuer)
		if err != nil {
			continue
		}
		svc := services[vendor]
		if svc == nil {
			svc = newEMAService()
			services[vendor] = svc
		}
		// Rows arrive records setEMAProviders accepts first, so the first
		// such record is the one to name; the name is the first one any
		// record has.
		if svc.issuerID == uuid.Nil && row.IssuerSettable {
			svc.issuerID = row.RemoteSessionIssuerID
		}
		if svc.name == "" {
			svc.name = row.Name
		}
		if !row.McpServerID.Valid || !emaBindingSetUp(row.State) {
			continue
		}
		serverChecks[row.McpServerID.UUID.String()] = authz.MCPServer(authz.MCPServerIDOf(mcpGrantResourceID(row.McpServerID.UUID, row.McpServerToolsetID)), authz.ProjectIDOf(row.ProjectID)).Read()
		server := &orgissuersgen.EMAServiceServer{
			ID: row.McpServerID.UUID.String(), Name: row.McpServerName, Slug: row.McpServerSlug,
			ProjectID: row.ProjectID.String(), ProjectSlug: row.ProjectSlug, ProjectName: row.ProjectName,
			Status: emaServerNeedsAttention, LastAcceptedAt: nil,
			SignInIssuerID: nil, SignInIssuerName: nil,
		}
		if row.SignInIssuerID.Valid {
			server.SignInIssuerID = conv.PtrEmpty(row.SignInIssuerID.UUID.String())
			server.SignInIssuerName = conv.PtrEmpty(row.SignInIssuerName)
		}
		if row.State == PreparationStateReady {
			svc.ready[row.McpServerID.UUID] = struct{}{}
			order, err := endpointOrder(row.UserSessionIssuerID, vendor)
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "read ema provider order").LogError(ctx, logger)
			}
			target := emaBindingTarget{projectID: row.ProjectID, userSessionIssuerID: row.UserSessionIssuerID, remoteIssuerID: row.RemoteSessionIssuerID, resource: row.Resource}
			switch at := ServingLastAccepted(order, bindingAcceptance[target]); {
			case len(order.Available()) == 0:
			case at != nil:
				server.Status = emaServerWorking
				server.LastAcceptedAt = conv.PtrEmpty(at.UTC().Format(time.RFC3339))
			default:
				server.Status = emaServerNotProven
			}
		}
		if prev, ok := svc.servers[row.McpServerID.UUID]; !ok || emaServerPreferred(server, prev) {
			svc.servers[row.McpServerID.UUID] = server
		}
	}
	saved, err := q.ListEMAProviderVendors(ctx, org)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list ema provider orders").LogError(ctx, logger)
	}
	for _, vendor := range saved {
		if services[vendor] == nil {
			services[vendor] = newEMAService()
		}
	}
	for vendor, svc := range services {
		if svc.issuerID != uuid.Nil {
			continue
		}
		// A saved order outlives its last binding, and a binding may name only
		// a record setEMAProviders rejects; find one it accepts so an
		// administrator can still change or clear the order.
		spellings := []string{vendor}
		if canonical, err := issuerurl.Parse(vendor); err == nil {
			spellings = canonical.MatchCandidates()
		}
		record, err := q.GetEMAVendorIssuerRecord(ctx, repo.GetEMAVendorIssuerRecordParams{Issuers: spellings, OrganizationID: org})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "read ema vendor issuer").LogError(ctx, logger)
		default:
			if record.Settable {
				svc.issuerID = record.ID
			}
			if svc.name == "" {
				svc.name = record.Name
			}
		}
	}

	acceptance, err := ListEMAVendorAcceptance(ctx, q, org)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list ema vendor acceptance").LogError(ctx, logger)
	}

	vendors := make([]string, 0, len(services))
	for vendor := range services {
		vendors = append(vendors, vendor)
	}
	sort.Strings(vendors)

	// One batched read check for every counted server, so a service never
	// names a server the caller cannot read.
	type candidate struct {
		vendor string
		server *orgissuersgen.EMAServiceServer
	}
	var candidates []candidate
	var checks []authz.Check
	for _, vendor := range vendors {
		for _, server := range services[vendor].servers {
			candidates = append(candidates, candidate{vendor: vendor, server: server})
			checks = append(checks, serverChecks[server.ID])
		}
	}
	readable, err := s.authz.FindMatched(ctx, checks)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "check MCP server read access").LogError(ctx, logger)
	}
	listed := map[string][]*orgissuersgen.EMAServiceServer{}
	hidden := map[string]int{}
	for i, c := range candidates {
		if !readable[i] {
			hidden[c.vendor]++
			continue
		}
		listed[c.vendor] = append(listed[c.vendor], c.server)
	}

	scope := EMAProviderScope{Endpoint: false, TrustedIssuer: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, SpeakeasySigns: s.speakeasySigningAvailable}
	items := make([]*orgissuersgen.EMAService, 0, len(vendors))
	for _, vendor := range vendors {
		svc := services[vendor]
		order, err := ResolveEMAProviders(ctx, q, org, vendor, scope)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "read ema provider order").LogError(ctx, logger)
		}
		order = StampEMAProviderLastAccepted(order, acceptance[vendor])
		order, err = ApplyEMAProviderReadiness(ctx, org, order, readiness)
		if err != nil {
			logger.WarnContext(ctx, "check Speakeasy issuer readiness", attr.SlogError(err), attr.SlogOAuthIssuer(vendor))
		}
		servers := listed[vendor]
		if servers == nil {
			servers = []*orgissuersgen.EMAServiceServer{}
		}
		sort.SliceStable(servers, func(a, b int) bool {
			if servers[a].ProjectName != servers[b].ProjectName {
				return servers[a].ProjectName < servers[b].ProjectName
			}
			return servers[a].Name < servers[b].Name
		})
		item := &orgissuersgen.EMAService{
			VendorIssuer:          vendor,
			Name:                  svc.name,
			RemoteSessionIssuerID: nil,
			EmaProviders:          EMAProviderOrderView(order),
			ServerCount:           len(svc.servers),
			ReadyServerCount:      len(svc.ready),
			Servers:               servers,
			HiddenServerCount:     hidden[vendor],
		}
		if svc.issuerID != uuid.Nil {
			item.RemoteSessionIssuerID = conv.PtrEmpty(svc.issuerID.String())
		}
		items = append(items, item)
	}
	return &orgissuersgen.ListEMAServicesResult{Items: items}, nil
}

// listEMABindingAcceptance reads every binding target's acceptance in the
// organization, per provider.
func (s *Service) listEMABindingAcceptance(ctx context.Context, q *repo.Queries, organizationID string) (map[emaBindingTarget]EMAVendorAcceptance, error) {
	rows, err := q.ListEMABindingAcceptanceForOrganization(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list ema binding acceptance: %w", err)
	}
	out := map[emaBindingTarget]EMAVendorAcceptance{}
	for _, row := range rows {
		target := emaBindingTarget{projectID: row.ProjectID.UUID, userSessionIssuerID: row.UserSessionIssuerID.UUID, remoteIssuerID: row.RemoteSessionIssuerID.UUID, resource: row.Resource}
		accepted := out[target]
		if accepted == nil {
			accepted = EMAVendorAcceptance{}
			out[target] = accepted
		}
		accepted.record(row.EmaProvider, row.ExternalIssuerID, row.InternalSessionIssuerID, row.LastAcceptedAt)
	}
	return out, nil
}

// memoizedReadiness answers each issuer's readiness once per request.
func memoizedReadiness(check EMAProviderReadiness) EMAProviderReadiness {
	if check == nil {
		return nil
	}
	type answer struct {
		reason EMAProviderUnavailableReason
		err    error
	}
	seen := map[uuid.UUID]answer{}
	return func(ctx context.Context, organizationID string, issuerID uuid.UUID) (EMAProviderUnavailableReason, error) {
		if a, ok := seen[issuerID]; ok {
			return a.reason, a.err
		}
		reason, err := check(ctx, organizationID, issuerID)
		seen[issuerID] = answer{reason: reason, err: err}
		return reason, err
	}
}

// emaServerPreferred reports whether next should replace prev as the binding a
// server shows: the better status wins, then the newest acceptance. Both
// timestamps are UTC RFC 3339, so they compare as strings.
func emaServerPreferred(next, prev *orgissuersgen.EMAServiceServer) bool {
	if nextRank, prevRank := emaServerStatusRank[next.Status], emaServerStatusRank[prev.Status]; nextRank != prevRank {
		return nextRank > prevRank
	}
	return next.LastAcceptedAt != nil && (prev.LastAcceptedAt == nil || *next.LastAcceptedAt > *prev.LastAcceptedAt)
}

// emaBindingServers names the project's servers one binding target applies to that the caller can read, counts the rest, and says whether that is all of them.
func (s *Service) emaBindingServers(ctx context.Context, in PreparationInput) ([]*clientsgen.IdentityChainingServer, int, bool, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, 0, false, errors.New("no project auth context")
	}
	reached, complete, ready, err := s.emaBindingReach(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, in)
	if err != nil {
		return nil, 0, false, err
	}
	queries := repo.New(s.db)
	rows, err := queries.ListEMABindingTargetServers(ctx, repo.ListEMABindingTargetServersParams{
		OrganizationID:      authCtx.ActiveOrganizationID,
		ProjectID:           *authCtx.ProjectID,
		ReachedServerIds:    reached,
		Ready:               ready,
		UserSessionIssuerID: uuid.NullUUID{UUID: in.UserSessionIssuerID, Valid: true},
		Resource:            in.Resource,
	})
	if err != nil {
		return nil, 0, false, fmt.Errorf("list identity chaining binding servers: %w", err)
	}
	if !ready {
		// Before the binding is on, the list is whole when every server it could reach is already in it.
		candidates, err := queries.ListEMABindingCandidateServers(ctx, repo.ListEMABindingCandidateServersParams{
			OrganizationID:      authCtx.ActiveOrganizationID,
			ProjectID:           *authCtx.ProjectID,
			UserSessionIssuerID: in.UserSessionIssuerID,
			Resource:            in.Resource,
		})
		if err != nil {
			return nil, 0, false, fmt.Errorf("list servers the binding could reach: %w", err)
		}
		complete = !slices.ContainsFunc(candidates, func(id uuid.UUID) bool {
			return !slices.ContainsFunc(rows, func(row repo.ListEMABindingTargetServersRow) bool { return row.ID == id })
		})
	}
	checks := make([]authz.Check, 0, len(rows))
	for _, row := range rows {
		checks = append(checks, authz.MCPServer(authz.MCPServerIDOf(mcpGrantResourceID(row.ID, row.ToolsetID)), authz.ProjectIDOf(*authCtx.ProjectID)).Read())
	}
	readable, err := s.authz.FindMatched(ctx, checks)
	if err != nil {
		return nil, 0, false, fmt.Errorf("check MCP server read access: %w", err)
	}
	servers := []*clientsgen.IdentityChainingServer{}
	hidden := 0
	for i, row := range rows {
		if !readable[i] {
			hidden++
			continue
		}
		servers = append(servers, &clientsgen.IdentityChainingServer{ID: row.ID.String(), Name: row.Name, Slug: row.Slug})
	}
	return servers, hidden, complete, nil
}

// emaBindingReach is the servers a ready binding's writer links, including gateway and tunneled members; complete is false
// when a same-origin server could also be covered, and ready is false before the binding is ready.
func (s *Service) emaBindingReach(ctx context.Context, organizationID string, projectID uuid.UUID, in PreparationInput) (reached []uuid.UUID, complete bool, ready bool, err error) {
	binding, err := repo.New(s.db).GetEMABinding(ctx, repo.GetEMABindingParams{ProjectID: projectID, OrganizationID: organizationID, UserSessionIssuerID: in.UserSessionIssuerID, RemoteSessionIssuerID: in.RemoteSessionIssuerID, Resource: in.Resource})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return []uuid.UUID{}, false, false, nil
	case err != nil:
		return nil, false, false, fmt.Errorf("read identity chaining binding: %w", err)
	case binding.State.String != "ready" || !binding.RemoteSessionClientID.Valid:
		return []uuid.UUID{}, false, false, nil
	}
	writer := authclientbackfill.New(s.db)
	rows, err := writer.ListEMABindingServers(ctx, authclientbackfill.ListEMABindingServersParams{BindingID: binding.ID, OrganizationID: organizationID})
	if err != nil {
		return nil, false, false, fmt.Errorf("list servers the binding reaches: %w", err)
	}
	reached = make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		// The runtime refuses a server whose metadata leaves the issuer out.
		if row.Match == "match" {
			reached = append(reached, row.McpServerID)
		}
	}
	plausible, err := writer.ListEMASameOriginServers(ctx, authclientbackfill.ListEMASameOriginServersParams{BindingID: binding.ID, OrganizationID: organizationID})
	if err != nil {
		return nil, false, false, fmt.Errorf("list same-origin servers the binding may reach: %w", err)
	}
	complete = true
	for _, row := range plausible {
		if !slices.Contains(reached, row.McpServerID) {
			complete = false
		}
	}
	return reached, complete, true, nil
}
