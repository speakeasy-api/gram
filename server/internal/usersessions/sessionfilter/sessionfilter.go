// Package sessionfilter validates the filters shared by the session list, its
// facet counts and the Platform session tool, and turns one validated filter
// into the parameters of each query.
package sessionfilter

import (
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/usersessions/gatewayscope"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

const (
	// MaxValues bounds each multi-value filter, matching the API's limit, so a
	// link cannot make one request match against an unbounded set of values.
	MaxValues = 50

	// MaxLastUsedSeconds bounds the last-used comparisons at ten years.
	MaxLastUsedSeconds = 10 * 365 * 24 * 60 * 60

	// FacetLimit is how many values each facet field returns before any
	// selected value outside them.
	FacetLimit = 25

	// ClientDashboard selects sessions minted from the dashboard.
	ClientDashboard = "dashboard"

	// ClientNone selects sessions with no client that are not dashboard mints.
	ClientNone = "none"

	// FieldIdentity is the facet field whose search also matches readable
	// agent names.
	FieldIdentity = "identity"

	// FieldUnattributed is the facet row counting the gateway's sessions with
	// no recorded origin.
	FieldUnattributed = "unattributed"
)

var (
	statuses            = []string{"active", "expired", "revoked", "all"}
	identityTypes       = []string{"user", "agent", "workload", "apikey", "anonymous"}
	clientRegistrations = []string{"cimd", "dcr", "assistant", "none"}
	facetFields         = []string{FieldIdentity, "identity_type", "mcp_server", "client", "client_registration"}
)

// Input is a session filter as a caller received it.
type Input struct {
	Status                     *string
	SubjectURN                 *string
	IssuerID                   *string
	ClientID                   *string
	Search                     *string
	Identities                 []string
	ExcludeIdentities          []string
	IdentityTypes              []string
	ExcludeIdentityTypes       []string
	Servers                    []string
	ExcludeServers             []string
	Clients                    []string
	ExcludeClients             []string
	ClientRegistrations        []string
	ExcludeClientRegistrations []string
	LastUsedWithin             *int
	LastUsedOlderThan          *int
	LastUsedNever              bool
}

// Filter is a validated Input.
type Filter struct {
	// Term is the normalized search text, empty when there is none.
	Term string

	// Gateway scopes the sessions to one gateway; the zero value does not.
	Gateway gatewayscope.Gateway

	status                     pgtype.Text
	subjectURN                 pgtype.Text
	issuerID                   uuid.NullUUID
	clientID                   uuid.NullUUID
	search                     pgtype.Text
	identities                 []string
	excludeIdentities          []string
	identityTypes              []string
	excludeIdentityTypes       []string
	servers                    []uuid.UUID
	excludeServers             []uuid.UUID
	clients                    []string
	excludeClients             []string
	clientRegistrations        []string
	excludeClientRegistrations []string
	lastUsedWithin             pgtype.Interval
	lastUsedOlderThan          pgtype.Interval
	lastUsedNever              bool
}

// Parse validates and normalizes in.
func Parse(in Input) (Filter, error) {
	var f Filter
	var err error

	if in.Status != nil && *in.Status != "" && !slices.Contains(statuses, *in.Status) {
		return Filter{}, oops.E(oops.CodeBadRequest, nil, "invalid status %q", *in.Status)
	}
	f.status = conv.PtrToPGTextEmpty(in.Status)
	f.subjectURN = conv.PtrToPGTextEmpty(in.SubjectURN)
	if f.issuerID, err = conv.PtrToNullUUID(in.IssuerID); err != nil {
		return Filter{}, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer_id")
	}
	if f.clientID, err = conv.PtrToNullUUID(in.ClientID); err != nil {
		return Filter{}, oops.E(oops.CodeBadRequest, err, "invalid client_id")
	}
	f.Term = conv.SearchTerm(conv.PtrValOr(in.Search, ""))
	f.search = conv.ContainsPattern(f.Term)

	if f.identities, err = identities("identities", in.Identities); err != nil {
		return Filter{}, err
	}
	if f.excludeIdentities, err = identities("exclude_identities", in.ExcludeIdentities); err != nil {
		return Filter{}, err
	}
	if f.identityTypes, err = values("identity_types", in.IdentityTypes, identityTypes); err != nil {
		return Filter{}, err
	}
	if f.excludeIdentityTypes, err = values("exclude_identity_types", in.ExcludeIdentityTypes, identityTypes); err != nil {
		return Filter{}, err
	}
	if f.servers, err = uuids("servers", in.Servers); err != nil {
		return Filter{}, err
	}
	if f.excludeServers, err = uuids("exclude_servers", in.ExcludeServers); err != nil {
		return Filter{}, err
	}
	if f.clients, err = clients("clients", in.Clients); err != nil {
		return Filter{}, err
	}
	if f.excludeClients, err = clients("exclude_clients", in.ExcludeClients); err != nil {
		return Filter{}, err
	}
	if f.clientRegistrations, err = values("client_registrations", in.ClientRegistrations, clientRegistrations); err != nil {
		return Filter{}, err
	}
	if f.excludeClientRegistrations, err = values("exclude_client_registrations", in.ExcludeClientRegistrations, clientRegistrations); err != nil {
		return Filter{}, err
	}
	if f.lastUsedWithin, err = seconds("last_used_within", in.LastUsedWithin); err != nil {
		return Filter{}, err
	}
	if f.lastUsedOlderThan, err = seconds("last_used_older_than", in.LastUsedOlderThan); err != nil {
		return Filter{}, err
	}
	f.lastUsedNever = in.LastUsedNever

	return f, nil
}

// ListParams is the list query for f in the caller's tenancy. agentSubjectURNs
// names the agents whose names the caller may read and that match f.Term.
func (f Filter) ListParams(projectID uuid.UUID, organizationID string, agentSubjectURNs []string, cursor uuid.NullUUID, limit int32) repo.ListUserSessionsByProjectIDParams {
	return repo.ListUserSessionsByProjectIDParams{
		ProjectID:                  projectID,
		OrganizationID:             organizationID,
		Status:                     f.status,
		SubjectUrn:                 f.subjectURN,
		UserSessionIssuerID:        f.issuerID,
		ClientID:                   f.clientID,
		ID:                         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Search:                     f.search,
		AgentSubjectUrns:           conv.DefaultSlice(agentSubjectURNs, []string{}),
		FirstPartyClientName:       sessiontokens.FirstPartyClientName,
		Identities:                 f.identities,
		ExcludeIdentities:          f.excludeIdentities,
		IdentityTypes:              f.identityTypes,
		ExcludeIdentityTypes:       f.excludeIdentityTypes,
		Servers:                    f.servers,
		ExcludeServers:             f.excludeServers,
		Clients:                    f.clients,
		ExcludeClients:             f.excludeClients,
		ClientRegistrations:        f.clientRegistrations,
		ExcludeClientRegistrations: f.excludeClientRegistrations,
		LastUsedWithin:             f.lastUsedWithin,
		LastUsedOlderThan:          f.lastUsedOlderThan,
		LastUsedNever:              f.lastUsedNever,
		ResourceIdentity:           f.Gateway.ResourceIdentity,
		Cursor:                     cursor,
		LimitValue:                 limit,
	}
}

// FacetSearch narrows one facet field's values.
type FacetSearch struct {
	// Field is the facet field to narrow; empty when nothing is narrowed.
	Field string

	// Term is the normalized search text.
	Term string

	// AgentSubjectURNs names the readable agents whose names match Term.
	AgentSubjectURNs []string
}

// ParseFacetSearch validates a facet field search.
func ParseFacetSearch(field, search *string) (FacetSearch, error) {
	term := conv.SearchTerm(conv.PtrValOr(search, ""))
	name := conv.PtrValOr(field, "")
	if name != "" && !slices.Contains(facetFields, name) {
		return FacetSearch{}, oops.E(oops.CodeBadRequest, nil, "invalid field %q", name)
	}
	if term != "" && name == "" {
		return FacetSearch{}, oops.E(oops.CodeBadRequest, nil, "field_search requires field")
	}
	return FacetSearch{Field: name, Term: term, AgentSubjectURNs: []string{}}, nil
}

// FacetParams is the facet query for f in the caller's tenancy.
func (f Filter) FacetParams(projectID uuid.UUID, organizationID string, agentSubjectURNs []string, search FacetSearch) repo.ListUserSessionFilterFacetsParams {
	return repo.ListUserSessionFilterFacetsParams{
		FacetLimit:                  FacetLimit,
		Identities:                  f.identities,
		ExcludeIdentities:           f.excludeIdentities,
		IdentityTypes:               f.identityTypes,
		ExcludeIdentityTypes:        f.excludeIdentityTypes,
		Servers:                     f.servers,
		ExcludeServers:              f.excludeServers,
		Clients:                     f.clients,
		ExcludeClients:              f.excludeClients,
		ClientRegistrations:         f.clientRegistrations,
		ExcludeClientRegistrations:  f.excludeClientRegistrations,
		FirstPartyClientName:        sessiontokens.FirstPartyClientName,
		LastUsedWithin:              f.lastUsedWithin,
		LastUsedOlderThan:           f.lastUsedOlderThan,
		LastUsedNever:               f.lastUsedNever,
		ProjectID:                   projectID,
		OrganizationID:              organizationID,
		Status:                      f.status,
		SubjectUrn:                  f.subjectURN,
		UserSessionIssuerID:         f.issuerID,
		ClientID:                    f.clientID,
		Search:                      f.search,
		AgentSubjectUrns:            conv.DefaultSlice(agentSubjectURNs, []string{}),
		FieldSearch:                 conv.ContainsPattern(search.Term),
		SearchField:                 search.Field,
		FieldSearchAgentSubjectUrns: conv.DefaultSlice(search.AgentSubjectURNs, []string{}),
		ResourceIdentity:            f.Gateway.ResourceIdentity,
		UnattributedIssuerID:        f.Gateway.IssuerID,
	}
}

// Selected lists the values chosen for field, included or excluded, in the
// form the facet query reports them.
func (f Filter) Selected(field string) []string {
	switch field {
	case FieldIdentity:
		return slices.Concat(f.identities, f.excludeIdentities)
	case "identity_type":
		return slices.Concat(f.identityTypes, f.excludeIdentityTypes)
	case "mcp_server":
		out := make([]string, 0, len(f.servers)+len(f.excludeServers))
		for _, id := range slices.Concat(f.servers, f.excludeServers) {
			out = append(out, id.String())
		}
		return out
	case "client":
		return slices.Concat(f.clients, f.excludeClients)
	case "client_registration":
		return slices.Concat(f.clientRegistrations, f.excludeClientRegistrations)
	default:
		return nil
	}
}

func values(name string, in []string, allowed []string) ([]string, error) {
	if len(in) > MaxValues {
		return nil, oops.E(oops.CodeBadRequest, nil, "%s accepts at most %d values", name, MaxValues)
	}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		value := conv.SearchTerm(raw)
		if value == "" {
			continue
		}
		if allowed != nil && !slices.Contains(allowed, value) {
			return nil, oops.E(oops.CodeBadRequest, nil, "invalid %s value %q", name, value)
		}
		if !slices.Contains(out, value) {
			out = append(out, value)
		}
	}
	return out, nil
}

// identities keeps subject URNs exactly as given: a workload's external
// subject is opaque and may begin or end with whitespace.
func identities(name string, in []string) ([]string, error) {
	if len(in) > MaxValues {
		return nil, oops.E(oops.CodeBadRequest, nil, "%s accepts at most %d values", name, MaxValues)
	}
	out := make([]string, 0, len(in))
	for _, value := range in {
		if strings.ContainsRune(value, 0) {
			return nil, oops.E(oops.CodeBadRequest, nil, "invalid %s value", name)
		}
		if value != "" && !slices.Contains(out, value) {
			out = append(out, value)
		}
	}
	return out, nil
}

func uuids(name string, in []string) ([]uuid.UUID, error) {
	raw, err := values(name, in, nil)
	if err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, 0, len(raw))
	for _, value := range raw {
		id, err := uuid.Parse(value)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid %s value %q", name, value)
		}
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out, nil
}

func clients(name string, in []string) ([]string, error) {
	raw, err := values(name, in, nil)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(raw))
	for _, value := range raw {
		if value != ClientDashboard && value != ClientNone {
			id, err := uuid.Parse(value)
			if err != nil {
				return nil, oops.E(oops.CodeBadRequest, err, "invalid %s value %q", name, value)
			}
			value = id.String()
		}
		if !slices.Contains(out, value) {
			out = append(out, value)
		}
	}
	return out, nil
}

func seconds(name string, in *int) (pgtype.Interval, error) {
	if in == nil {
		return pgtype.Interval{Microseconds: 0, Days: 0, Months: 0, Valid: false}, nil
	}
	if *in < 1 || *in > MaxLastUsedSeconds {
		return pgtype.Interval{}, oops.E(oops.CodeBadRequest, nil, "%s must be between 1 and %d seconds", name, MaxLastUsedSeconds)
	}
	return pgtype.Interval{Microseconds: (time.Duration(*in) * time.Second).Microseconds(), Days: 0, Months: 0, Valid: true}, nil
}
