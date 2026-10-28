package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

const (
	// connectionCatalogVersion identifies the immutable record format.
	connectionCatalogVersion = 1
	// connectionCatalogMaxBytes bounds one immutable Redis value to 16 MiB.
	connectionCatalogMaxBytes = 16 << 20
)

// errConnectionCatalogExpired requires a new consent instead of live discovery.
var errConnectionCatalogExpired = errors.New("connection catalog expired; give consent again to approve tools")

var errConnectionCatalogTooLarge = errors.New("approved tool definitions exceed the supported size")

// catalogMemberNotApproved explains a member outside the approved catalog.
const catalogMemberNotApproved = "This server was not approved. Give consent again to add it."

// connectionCatalogMember identifies the route and client verified at consent.
type connectionCatalogMember struct {
	// ServerID binds the source slug to its approved MCP server.
	ServerID uuid.UUID `json:"server_id"`
	// ProjectID scopes route and credential resolution to the member's project.
	ProjectID uuid.UUID `json:"project_id"`
	// BackendHash detects changes to destinations, headers, and server settings.
	BackendHash string `json:"backend_hash"`
	// Auth binds a proxied member's shared-runtime choice; hosted members use
	// their toolset's authentication and leave this nil.
	Auth *connectionCatalogAuth `json:"auth,omitempty"`
}

type connectionCatalogAuth struct {
	ClientID       uuid.UUID     `json:"client_id"`
	RegistrationID uuid.NullUUID `json:"registration_id"`
}

// connectionCatalog is immutable data, bound to one approved OAuth connection.
// Session validation and live authorization remain separate from this record.
type connectionCatalog struct {
	// Version controls the stored data contract.
	Version int `json:"version"`
	// ID identifies this exact immutable catalog.
	ID uuid.UUID `json:"id"`
	// OrganizationID isolates the record's tenant.
	OrganizationID string `json:"organization_id"`
	// IssuerID binds the record to the inbound authorization server.
	IssuerID uuid.UUID `json:"issuer_id"`
	// ClientID binds approval to the requesting OAuth client.
	ClientID string `json:"client_id"`
	// Subject is the represented person or agent whose access was discovered.
	Subject urn.SessionSubject `json:"subject"`
	// Resource pins the gateway identity independently of its display slug.
	Resource string `json:"resource"`
	// Members contains only members with complete successful discovery.
	Members map[string]connectionCatalogMember `json:"members"`
	// MemberOutcomes records unavailable members without authorizing them.
	MemberOutcomes []consentMemberOutcome `json:"member_outcomes,omitempty"`
	// Tools retains complete qualified tool definitions, sorted by name.
	Tools []*toolListEntry `json:"tools,omitempty"`
	// ExpiresAt covers the authorization lifetime and code redemption window.
	ExpiresAt time.Time `json:"expires_at"`
}

func connectionCatalogKey(organizationID string, id uuid.UUID) string {
	return fmt.Sprintf("mcpConnectionCatalog:v%d:%s:%s", connectionCatalogVersion, organizationID, id)
}

func (c connectionCatalog) CacheKey() string { return connectionCatalogKey(c.OrganizationID, c.ID) }

// publishConnectionCatalog completes all fallible catalog work while the
// challenge remains retryable. A losing approval may leave an unreferenced
// record, which expires; an authorization code never names a partial record.
func (s *Service) publishConnectionCatalog(ctx context.Context, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState, inventory *consentToolInventory, selection *toolfilter.SessionSelection, mode toolfilter.CatalogMode, allTools bool, upstreamPicks map[uuid.UUID]uuid.UUID, durationHours int, now time.Time) (*toolfilter.CatalogReference, error) {
	if inventory == nil || !inventory.Complete || inventory.StateID != state.ID || inventory.Resource != endpointToolSelectionResource(endpoint) {
		return nil, fmt.Errorf("a completed gateway catalog is required; reload consent")
	}
	if state.Subject == nil || state.Subject.IsZero() || (mode != toolfilter.CatalogModeCached && mode != toolfilter.CatalogModeLive) {
		return nil, fmt.Errorf("invalid connection catalog approval")
	}
	issuer, err := usersessionsrepo.New(s.db).GetUserSessionIssuerByID(ctx, usersessionsrepo.GetUserSessionIssuerByIDParams{
		ID: endpoint.UserSessionIssuerID, ProjectID: endpoint.ProjectID, OrganizationID: endpoint.OrganizationID,
	})
	if err != nil {
		return nil, fmt.Errorf("load catalog authorization lifetime: %w", err)
	}
	lifetime, ok := conv.PGIntervalToDuration(issuer.SessionDuration)
	// The record's expiry adds the grant window, so leave room for it.
	if !ok || lifetime <= 0 || lifetime > math.MaxInt64-userSessionGrantTTL {
		return nil, fmt.Errorf("invalid catalog authorization lifetime")
	}
	if durationHours > 0 {
		lifetime = min(lifetime, time.Duration(durationHours)*time.Hour)
	}
	ids := make([]uuid.UUID, 0, len(inventory.MemberBindings))
	seen := make(map[uuid.UUID]bool, len(inventory.MemberBindings))
	for _, member := range inventory.MemberBindings {
		if seen[member.ServerID] {
			return nil, errConsentConfigurationChanged
		}
		seen[member.ServerID] = true
		ids = append(ids, member.ServerID)
	}
	hashes, err := s.catalogMemberConfigurations(ctx, endpoint.OrganizationID, ids)
	if err != nil {
		return nil, err
	}
	members := make(map[string]connectionCatalogMember, len(inventory.MemberBindings))
	for slug, member := range inventory.MemberBindings {
		if member.ServerID == uuid.Nil || hashes[member.ServerID] != member.BackendHash {
			return nil, errConsentConfigurationChanged
		}
		if member.Auth != nil {
			if member.Auth.RegistrationID.UUID != upstreamPicks[member.ServerID] {
				return nil, errConsentConfigurationChanged
			}
			resolved, err := s.resolveAuthClientForConsent(ctx, endpoint.OrganizationID, member.ServerID, member.ProjectID, member.Auth.RegistrationID, *state.Subject)
			if err != nil {
				return nil, err
			}
			if resolved.outcome == authClientGap || resolved.outcome == authClientNoAccess || resolved.clientID != member.Auth.ClientID {
				return nil, errConsentConfigurationChanged
			}
		}
		members[slug] = member
	}
	tools := make([]*toolListEntry, 0, len(inventory.Tools))
	var outcomes []consentMemberOutcome
	if mode == toolfilter.CatalogModeCached {
		outcomes = inventory.MemberOutcomes
		for _, tool := range inventory.Tools {
			if tool.Definition == nil || tool.Definition.Name != tool.Name || consentEvidenceHash(tool.Definition) != tool.DefinitionHash {
				return nil, fmt.Errorf("gateway catalog is incomplete; reload consent")
			}
			slug, _, err := metamcp.SplitQualifiedName(tool.Name)
			if err != nil || members[slug].ServerID == uuid.Nil {
				return nil, fmt.Errorf("gateway tool has no verified source")
			}
			allowed := selection == nil || selection.AllowsName(tool.Name)
			if allowed {
				tools = append(tools, tool.Definition)
			}
		}
	}
	slices.SortFunc(tools, func(a, b *toolListEntry) int { return strings.Compare(a.Name, b.Name) })
	catalog := connectionCatalog{
		Version: connectionCatalogVersion, ID: uuid.New(), OrganizationID: endpoint.OrganizationID,
		IssuerID: endpoint.UserSessionIssuerID, ClientID: state.ClientID, Subject: *state.Subject,
		Resource: inventory.Resource, Members: members, MemberOutcomes: outcomes, Tools: tools,
		ExpiresAt: now.Add(lifetime + userSessionGrantTTL),
	}
	raw, err := json.Marshal(catalog)
	if err != nil {
		return nil, fmt.Errorf("encode approved catalog: %w", err)
	}
	if len(raw) > connectionCatalogMaxBytes {
		return nil, fmt.Errorf("%w (%d bytes)", errConnectionCatalogTooLarge, connectionCatalogMaxBytes)
	}
	conditional, ok := s.connectionCatalogCache.(cache.ConditionalCache)
	if !ok {
		return nil, fmt.Errorf("connection catalog store does not support atomic publication")
	}
	stored, err := conditional.SetIfAbsent(ctx, catalog.CacheKey(), raw, catalog.ExpiresAt.Sub(now))
	if err != nil {
		return nil, fmt.Errorf("publish connection catalog: %w", err)
	}
	if !stored {
		return nil, fmt.Errorf("connection catalog publication conflict")
	}
	return &toolfilter.CatalogReference{ID: catalog.ID, Mode: mode, AllTools: allTools}, nil
}

// loadConnectionCatalog never substitutes a live upstream fetch for a missing
// snapshot. A cache outage is an operational error; eviction needs consent.
func (s *Service) loadConnectionCatalog(ctx context.Context, endpoint *ResolvedMcpEndpoint, subject urn.SessionSubject, reference *toolfilter.CatalogReference, now time.Time) (*connectionCatalog, error) {
	if reference == nil || reference.ID == uuid.Nil {
		return nil, errConnectionCatalogExpired
	}
	var raw []byte
	err := s.connectionCatalogCache.Get(ctx, connectionCatalogKey(endpoint.OrganizationID, reference.ID), &raw)
	if errors.Is(err, redisCache.ErrCacheMiss) {
		return nil, errConnectionCatalogExpired
	}
	if err != nil {
		return nil, fmt.Errorf("load connection catalog: %w", err)
	}
	var catalog connectionCatalog
	if len(raw) > connectionCatalogMaxBytes || json.Unmarshal(raw, &catalog) != nil {
		return nil, errConnectionCatalogExpired
	}
	if catalog.Version != connectionCatalogVersion || catalog.ID != reference.ID || catalog.OrganizationID != endpoint.OrganizationID || catalog.IssuerID != endpoint.UserSessionIssuerID || catalog.Resource != endpointToolSelectionResource(endpoint) || catalog.Subject != subject || !now.Before(catalog.ExpiresAt) {
		return nil, errConnectionCatalogExpired
	}
	clientID, _ := contextvalues.GetOAuthClientID(ctx)
	if catalog.ClientID != clientID {
		return nil, errConnectionCatalogExpired
	}
	return &catalog, nil
}

// describeCachedMember applies current tool permissions to stored definitions.
// It never resolves upstream tokens or starts an MCP session.
func (s *Service) describeCachedMember(ctx context.Context, gate *metaGateContext, member metaMember) (*memberCatalog, error) {
	catalog := &memberCatalog{entries: nil, byName: map[string]*toolListEntry{}, incomplete: false}
	checks := []authz.Check{}
	var dispositions map[string]string
	private := member.visibility != "public"
	if member.backend == metaMemberBackendHosted {
		_, hosting, _, err := s.loadMemberToolset(ctx, s.logger, gate, member)
		if err != nil {
			return nil, err
		}
		private = gate.authenticated && !hosting.Public
	}
	if private && member.backend == metaMemberBackendProxied {
		var err error
		dispositions, err = s.remoteProxyManager.Dispositions(ctx, member.serverID.String(), member.projectID.String())
		if err != nil {
			return nil, fmt.Errorf("read cached member tool permissions: %w", err)
		}
	}
	for _, stored := range gate.catalog.Tools {
		slug, name, err := metamcp.SplitQualifiedName(stored.Name)
		if err != nil || slug != member.slug {
			continue
		}
		entry := *stored
		entry.Name = name
		catalog.entries = append(catalog.entries, &entry)
		if private {
			resourceID := member.serverID
			disposition := dispositions[name]
			if member.backend == metaMemberBackendHosted {
				resourceID = member.toolsetID.UUID
				// Cached discovery keeps consented annotations; execution checks the live tool disposition.
				disposition = dispositionFromAnnotations(entry.Annotations)
			}
			checks = append(checks, authz.MCPToolCallCheck(resourceID.String(), authz.MCPToolCallDimensions{Tool: name, Disposition: disposition, ProjectID: member.projectID.String()}))
		}
	}
	if len(checks) > 0 {
		matched, err := s.authz.FindMatched(ctx, checks)
		if err != nil {
			return nil, fmt.Errorf("authorize cached member tools: %w", err)
		}
		kept := catalog.entries[:0]
		for i, entry := range catalog.entries {
			if matched[i] {
				kept = append(kept, entry)
			}
		}
		catalog.entries = kept
	}
	for _, entry := range catalog.entries {
		catalog.byName[entry.Name] = entry
	}
	return catalog, nil
}

// approvedMembers keeps the members this catalog approved, matched by their
// stable server and project identity rather than by display slug: a derived
// gateway qualifies a slug only while another member shares it, so the slug
// can change after consent. Each approved member keeps the slug it was
// approved under, so stored tool names, selections and outcomes still
// resolve. The remaining members serve only status reporting; discovery and
// execution never see them.
func (c *connectionCatalog) approvedMembers(members []metaMember) (approved, unapproved []metaMember) {
	slugs := make(map[uuid.UUID]string, len(c.Members))
	for slug, member := range c.Members {
		slugs[member.ServerID] = slug
	}
	for _, member := range members {
		slug, found := slugs[member.serverID]
		if !found || c.Members[slug].ProjectID != member.projectID {
			unapproved = append(unapproved, member)
			continue
		}
		member.slug = slug
		approved = append(approved, member)
	}
	return approved, unapproved
}

// validateCatalogMember prevents a changed destination from inheriting consent.
func (s *Service) validateCatalogMember(ctx context.Context, gate *metaGateContext, member metaMember) error {
	if gate.catalog == nil {
		return nil
	}
	approved, found := gate.catalog.Members[member.slug]
	if !found || approved.ServerID != member.serverID || approved.ProjectID != member.projectID {
		return s.rejectCatalogMember(ctx, gate, member, "member_not_approved", catalogMemberNotApproved,
			map[string]bool{"member_recorded": found, "server_id_matches": found && approved.ServerID == member.serverID, "project_id_matches": found && approved.ProjectID == member.projectID})
	}

	if member.backend == metaMemberBackendProxied && (gate.execute || gate.catalogRef.Mode == toolfilter.CatalogModeLive) {
		clientID := uuid.Nil
		if member.authClient != nil {
			clientID = member.authClient.clientID
		}
		if member.upstreamUnavailable != nil {
			return member.upstreamUnavailable
		}
		if approved.Auth == nil || approved.Auth.RegistrationID != member.chosenClientID || approved.Auth.ClientID != clientID {
			return s.rejectCatalogMember(ctx, gate, member, "authentication_changed", "This server's authentication changed. Give consent again to approve it.",
				map[string]bool{"auth_recorded": approved.Auth != nil, "registration_id_matches": approved.Auth != nil && approved.Auth.RegistrationID == member.chosenClientID, "auth_client_id_matches": approved.Auth != nil && approved.Auth.ClientID == clientID})
		}
	}
	if member.configurationHash == "" || member.configurationHash != approved.BackendHash {
		return s.rejectCatalogMember(ctx, gate, member, "configuration_changed", "This server's configuration changed. Give consent again to approve it.",
			map[string]bool{"configuration_hash_present": member.configurationHash != "", "approved_hash_recorded": approved.BackendHash != ""})
	}
	return nil
}

// Log before discovery collapses the refusal into consent_required. Keep the
// record to correlation IDs and comparison results: never log the catalog,
// credentials, configuration hashes, headers, or tool definitions.
func (s *Service) rejectCatalogMember(ctx context.Context, gate *metaGateContext, member metaMember, reason, message string, comparisons map[string]bool) error {
	err := catalogConsentError{&metaMemberError{message: message}}
	if gate.consentDiagnostics != nil {
		key := consentDiagnosticKey{catalogID: gate.catalog.ID, memberID: member.serverID, reason: reason}
		if _, seen := gate.consentDiagnostics.seen.LoadOrStore(key, struct{}{}); seen {
			return err
		}
	}
	catalogMode := ""
	if gate.catalogRef != nil {
		catalogMode = string(gate.catalogRef.Mode)
	}
	s.logger.LogAttrs(ctx, slog.LevelInfo, "gateway consent validation failed",
		attr.SlogMetaMcpServerID(gate.metaServerID.String()),
		attr.SlogMcpServerID(member.serverID.String()),
		attr.SlogProjectID(gate.projectID.String()),
		attr.SlogMcpConsentMemberProjectID(member.projectID.String()),
		attr.SlogReason(reason),
		attr.SlogMcpConsentCatalogID(gate.catalog.ID.String()),
		attr.SlogMcpConsentCatalogMode(catalogMode),
		attr.SlogMcpConsentDiscoveryMode(string(gate.discoveryMode)),
		attr.SlogMcpConsentExecute(gate.execute),
		attr.SlogMcpConsentComparisons(comparisons))
	return err
}

// A Code Mode execution can validate the same member concurrently through
// several helpers. Keep its diagnostic set shared across refreshed gate copies.
type consentDiagnostics struct{ seen sync.Map }

type consentDiagnosticKey struct {
	catalogID uuid.UUID
	memberID  uuid.UUID
	reason    string
}

// catalogConsentError is a member whose approval no longer matches it. Only a
// new consent fixes it; retrying or changing the call cannot.
type catalogConsentError struct{ *metaMemberError }

func (e catalogConsentError) Unwrap() error { return e.metaMemberError }

func (s *Service) catalogMemberConfigurations(ctx context.Context, organizationID string, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	hashes := make(map[uuid.UUID]string, len(ids))
	if len(ids) == 0 {
		return hashes, nil
	}
	rows, err := mcpserversrepo.New(s.db).ListMCPServerConsentConfigurations(ctx, mcpserversrepo.ListMCPServerConsentConfigurationsParams{OrganizationID: organizationID, McpServerIds: ids})
	if err != nil {
		return nil, fmt.Errorf("read approved server configurations: %w", err)
	}
	for _, row := range rows {
		hashes[row.ID] = row.ConfigurationHash
	}
	return hashes, nil
}
