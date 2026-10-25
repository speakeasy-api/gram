package platformmcp

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	usersessionsgen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
)

// The session list's own limits, which its HTTP layer enforces and a direct
// call must repeat: search length in characters, and the largest page.
const (
	maxGatewayConnectionsSearchLength = 200
	maxGatewayConnectionsLimit        = 100
)

var errGatewayConnectionsInvalid = errors.New("invalid gateway connections request")

// ListGatewayConnectionsInput names one gateway and the filters the session
// list takes. Every filter is the session list's own; omitted ones match all.
type ListGatewayConnectionsInput struct {
	// ProjectID is the exact project the gateway belongs to.
	ProjectID string `json:"project_id" jsonschema:"ID of the project the gateway belongs to"`

	// GatewayID is the exact gateway whose sessions are listed.
	GatewayID string `json:"gateway_id" jsonschema:"exact gateway ID, as create_gateway or get_plugin (a member whose backend is gateway) returns it"`

	// Search matches the session list's search, at most 200 characters.
	Search string `json:"search,omitempty" jsonschema:"case-insensitive text matched against the person, readable agent, API key, workload, client, sign-in server or upstream provider; at most 200 characters"`

	// Status is active, expired, revoked or all; empty leaves out revoked.
	Status string `json:"status,omitempty" jsonschema:"active, expired, revoked or all; omitted lists every session that is not revoked"`

	// Identities keeps only these subjects.
	Identities []string `json:"identities,omitempty" jsonschema:"only these identities, as returned in identity; at most 50"`

	// ExcludeIdentities leaves out these subjects.
	ExcludeIdentities []string `json:"exclude_identities,omitempty" jsonschema:"leave out these identities; at most 50"`

	// Servers keeps only sessions from these sign-in servers.
	Servers []string `json:"servers,omitempty" jsonschema:"only sessions from these sign-in servers, as returned in sign_in_server_id; at most 50"`

	// ExcludeServers leaves out sessions from these sign-in servers.
	ExcludeServers []string `json:"exclude_servers,omitempty" jsonschema:"leave out sessions from these sign-in servers; at most 50"`

	// IdentityTypes keeps only these subject kinds.
	IdentityTypes []string `json:"identity_types,omitempty" jsonschema:"only these identity types: user, agent, apikey, workload or anonymous; at most 50"`

	// ExcludeIdentityTypes leaves out these subject kinds.
	ExcludeIdentityTypes []string `json:"exclude_identity_types,omitempty" jsonschema:"leave out these identity types; at most 50"`

	// Clients keeps only these clients.
	Clients []string `json:"clients,omitempty" jsonschema:"only these clients: a client_id, dashboard for dashboard sessions, or none for sessions with no client; at most 50"`

	// ExcludeClients leaves out these clients.
	ExcludeClients []string `json:"exclude_clients,omitempty" jsonschema:"leave out these clients, using the same values; at most 50"`

	// ClientRegistrations keeps only clients registered these ways.
	ClientRegistrations []string `json:"client_registrations,omitempty" jsonschema:"only clients registered this way: cimd, dcr, assistant or none; at most 50"`

	// ExcludeRegistrations leaves out clients registered these ways.
	ExcludeRegistrations []string `json:"exclude_client_registrations,omitempty" jsonschema:"leave out clients registered this way; at most 50"`

	// LastUsedWithin keeps sessions used within this many seconds.
	LastUsedWithin *int `json:"last_used_within_seconds,omitempty" jsonschema:"only sessions used within this many seconds; sessions with no recorded use do not match"`

	// LastUsedOlderThan keeps sessions last used longer ago than this.
	LastUsedOlderThan *int `json:"last_used_older_than_seconds,omitempty" jsonschema:"only sessions last used more than this many seconds ago; sessions with no recorded use do not match"`

	// LastUsedNever keeps only sessions with no recorded use.
	LastUsedNever bool `json:"last_used_never,omitempty" jsonschema:"only sessions with no recorded use"`

	// Cursor continues from a previous page.
	Cursor string `json:"cursor,omitempty" jsonschema:"next_cursor from the previous page"`

	// Limit is the page size, 1 to 100; zero means 50.
	Limit int `json:"limit,omitempty" jsonschema:"page size, 1 to 100; default 50"`
}

// ListGatewayConnectionsOutput is one page of a gateway's sessions.
type ListGatewayConnectionsOutput struct {
	// ProjectID is the project the page was read in.
	ProjectID string `json:"project_id"`

	// GatewayID is the gateway the page was read for.
	GatewayID string `json:"gateway_id"`

	// Connections are the sessions on this page, newest first.
	Connections []GatewayConnection `json:"connections"`

	// NextCursor continues the list; empty on the last page.
	NextCursor string `json:"next_cursor,omitempty"`

	// UnattributedCount counts sessions on the gateway's current sign-in
	// server that match the same filters but recorded no gateway, so they are
	// not listed.
	UnattributedCount int64 `json:"unattributed_count"`
}

// GatewayConnection is one session issued for the gateway. Credentials and
// token material are never included.
type GatewayConnection struct {
	// ID is the session ID.
	ID string `json:"id"`

	// Identity is the value identities and exclude_identities take.
	Identity string `json:"identity"`

	// IdentityType is user, agent, apikey, workload or anonymous.
	IdentityType string `json:"identity_type"`

	// DisplayName names a person, API key or workload. Agents are not named:
	// their names are readable per agent, which this list does not check.
	DisplayName string `json:"display_name,omitempty"`

	// ClientID is the value clients takes; empty for a session with no client.
	ClientID string `json:"client_id,omitempty"`

	// ClientName names the client; empty when it has none.
	ClientName string `json:"client_name,omitempty"`

	// SignInServerID is the value servers takes.
	SignInServerID string `json:"sign_in_server_id"`

	// SignInServer names the sign-in server that issued the session.
	SignInServer string `json:"sign_in_server"`

	// UpstreamProviders are the providers the session holds connections to.
	UpstreamProviders []string `json:"upstream_providers"`

	// CreatedAt is when the session was issued.
	CreatedAt string `json:"created_at"`

	// ExpiresAt is when the session stops being renewable.
	ExpiresAt string `json:"expires_at"`

	// LastUsedAt is the latest recorded use; empty when none is recorded.
	LastUsedAt string `json:"last_used_at,omitempty"`

	// RevokedAt is set when the session was revoked.
	RevokedAt string `json:"revoked_at,omitempty"`
}

// gatewaySessions is the session list and its facets, each authorizing,
// filtering and scoping to the gateway against the scoped auth context.
type gatewaySessions interface {
	ListUserSessions(ctx context.Context, payload *usersessionsgen.ListUserSessionsPayload) (*usersessionsgen.ListUserSessionsResult, error)
	QueryFacets(ctx context.Context, payload *usersessionsgen.QueryFacetsPayload) (*usersessionsgen.UserSessionFilterFacets, error)
}

// GatewayConnectionsService lists a gateway's sessions through the session
// service the dashboard uses, which stays the source of truth for
// authorization, filters and tenancy.
type GatewayConnectionsService struct {
	sessions       gatewaySessions
	resolveProject func(context.Context, Principal, FindMCPInput) (ResolvedProject, error)
}

// NewGatewayConnectionsService returns nil when a dependency is missing, so
// the tool is served as a stub.
func NewGatewayConnectionsService(sessions *usersessions.Service, projects *PostgresReader) *GatewayConnectionsService {
	if sessions == nil || projects == nil {
		return nil
	}
	return &GatewayConnectionsService{sessions: sessions, resolveProject: projects.ResolveProjectRead}
}

func (s *GatewayConnectionsService) valid() bool {
	return s != nil && s.sessions != nil && s.resolveProject != nil
}

func (s *GatewayConnectionsService) List(ctx context.Context, principal Principal, input ListGatewayConnectionsInput) (ListGatewayConnectionsOutput, error) {
	var zero ListGatewayConnectionsOutput
	if !s.valid() {
		return zero, ErrUnavailable
	}
	projectID, err := uuid.Parse(strings.TrimSpace(input.ProjectID))
	if err != nil || projectID == uuid.Nil {
		return zero, errGatewayConnectionsInvalid
	}
	gatewayID, err := uuid.Parse(strings.TrimSpace(input.GatewayID))
	if err != nil || gatewayID == uuid.Nil {
		return zero, errGatewayConnectionsInvalid
	}
	if utf8.RuneCountInString(input.Search) > maxGatewayConnectionsSearchLength || input.Limit < 0 || input.Limit > maxGatewayConnectionsLimit {
		return zero, errGatewayConnectionsInvalid
	}

	auth, ok := contextvalues.GetAuthContext(ctx)
	if !ok || auth == nil || principal.UserID == "" || auth.UserID != principal.UserID || auth.ActiveOrganizationID != principal.OrganizationID {
		return zero, ErrForbidden
	}
	project, err := s.resolveProject(ctx, principal, FindMCPInput{ProjectID: projectID.String(), ProjectSlug: "", Query: "", Cursor: "", Limit: 0, Readiness: ""})
	if err != nil {
		return zero, err
	}
	if project.ID != projectID {
		return zero, ErrForbidden
	}
	scoped := *auth
	scoped.ProjectID, scoped.ProjectSlug = &project.ID, &project.Slug
	ctx = contextvalues.SetAuthContext(ctx, &scoped)

	gateway := gatewayID.String()
	list, err := s.sessions.ListUserSessions(ctx, &usersessionsgen.ListUserSessionsPayload{
		SubjectUrn: nil, UserSessionIssuerID: nil, ClientID: nil, Servers: input.Servers, ExcludeServers: input.ExcludeServers,
		Status: conv.PtrEmpty(input.Status), Search: conv.PtrEmpty(input.Search), MetaMcpServerID: &gateway,
		Identities: input.Identities, ExcludeIdentities: input.ExcludeIdentities,
		IdentityTypes: input.IdentityTypes, ExcludeIdentityTypes: input.ExcludeIdentityTypes,
		Clients: input.Clients, ExcludeClients: input.ExcludeClients,
		ClientRegistrations: input.ClientRegistrations, ExcludeClientRegistrations: input.ExcludeRegistrations,
		LastUsedWithin: input.LastUsedWithin, LastUsedOlderThan: input.LastUsedOlderThan, LastUsedNever: &input.LastUsedNever,
		Cursor: conv.PtrEmpty(input.Cursor), Limit: conv.PtrEmpty(input.Limit),
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	if err != nil {
		return zero, gatewayConnectionsError(err)
	}
	facets, err := s.sessions.QueryFacets(ctx, &usersessionsgen.QueryFacetsPayload{
		SubjectUrn: nil, UserSessionIssuerID: nil, ClientID: nil, Servers: input.Servers, ExcludeServers: input.ExcludeServers,
		Status: conv.PtrEmpty(input.Status), Search: conv.PtrEmpty(input.Search), MetaMcpServerID: &gateway,
		Identities: input.Identities, ExcludeIdentities: input.ExcludeIdentities,
		IdentityTypes: input.IdentityTypes, ExcludeIdentityTypes: input.ExcludeIdentityTypes,
		Clients: input.Clients, ExcludeClients: input.ExcludeClients,
		ClientRegistrations: input.ClientRegistrations, ExcludeClientRegistrations: input.ExcludeRegistrations,
		LastUsedWithin: input.LastUsedWithin, LastUsedOlderThan: input.LastUsedOlderThan, LastUsedNever: &input.LastUsedNever,
		Field: nil, FieldSearch: nil,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	if err != nil {
		return zero, gatewayConnectionsError(err)
	}

	out := ListGatewayConnectionsOutput{
		ProjectID: project.ID.String(), GatewayID: gateway,
		Connections: make([]GatewayConnection, 0, len(list.Items)),
		NextCursor:  conv.PtrValOr(list.NextCursor, ""), UnattributedCount: 0,
	}
	if facets.UnattributedCount != nil {
		out.UnattributedCount = *facets.UnattributedCount
	}
	for _, session := range list.Items {
		out.Connections = append(out.Connections, gatewayConnection(session))
	}
	return out, nil
}

func gatewayConnection(session *types.UserSession) GatewayConnection {
	displayName := conv.PtrValOr(session.SubjectDisplayName, "")
	if session.SubjectType == string(urn.SessionSubjectKindAgent) {
		displayName = ""
	}
	providers := make([]string, 0, len(session.Upstreams))
	for _, upstream := range session.Upstreams {
		providers = append(providers, upstream.IssuerSlug)
	}
	return GatewayConnection{
		ID: session.ID, Identity: session.SubjectUrn, IdentityType: session.SubjectType, DisplayName: displayName,
		ClientID: conv.PtrValOr(session.UserSessionClientID, ""), ClientName: conv.PtrValOr(session.ClientName, ""),
		SignInServerID: session.UserSessionIssuerID, SignInServer: session.IssuerSlug, UpstreamProviders: providers,
		CreatedAt: session.CreatedAt, ExpiresAt: session.RefreshExpiresAt,
		LastUsedAt: conv.PtrValOr(session.LastUsedAt, ""), RevokedAt: conv.PtrValOr(session.RevokedAt, ""),
	}
}

// gatewayConnectionsError keeps the session service's client-fault messages,
// which name the offending filter, and reduces a denial to the bare sentinel.
func gatewayConnectionsError(err error) error {
	shareable, ok := errors.AsType[*oops.ShareableError](err)
	switch {
	case ok && (shareable.Code == oops.CodeForbidden || shareable.Code == oops.CodeUnauthorized):
		return ErrForbidden
	case ok && (shareable.Code == oops.CodeBadRequest || shareable.Code == oops.CodeNotFound):
		return shareable
	default:
		return err
	}
}
