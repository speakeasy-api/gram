package mcpconnectionhealth

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_connection_health"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/attemptlog"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionhealth/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// serverKinds maps stored server kinds to the API's.
var serverKinds = map[string]string{
	"mcp":                  "mcp",
	"meta":                 "gateway",
	"toolset":              "toolset",
	"authorization_server": "authorization_server",
}

// person is a known user's name and email.
type person struct {
	name  string
	email string
}

// names resolves ids on attempt rows to what the pages show. Ids not found keep no name.
type names struct {
	servers map[string]repo.ListServerNamesRow
	people  map[string]person
}

// collector gathers the ids a response needs names for.
type collector struct {
	servers map[uuid.UUID]struct{}
	users   map[string]struct{}
}

func newCollector() *collector {
	return &collector{servers: map[uuid.UUID]struct{}{}, users: map[string]struct{}{}}
}

func (c *collector) server(id string) {
	if parsed, err := uuid.Parse(id); err == nil {
		c.servers[parsed] = struct{}{}
	}
}

func (c *collector) subject(subjectURN string) {
	if kind, id, ok := strings.Cut(subjectURN, ":"); ok && kind == string(urn.SessionSubjectKindUser) && id != "" {
		c.users[id] = struct{}{}
	}
}

func (c *collector) row(r attemptlog.Row) {
	c.server(r.ServerID)
	c.server(r.MemberID)
	c.subject(r.Subject)
}

// resolve loads names from Postgres, scoped to the caller's organization and project.
func (s *Service) resolve(ctx context.Context, c caller, ids *collector) (names, error) {
	out := names{servers: map[string]repo.ListServerNamesRow{}, people: map[string]person{}}
	queries := repo.New(s.db)
	if len(ids.servers) > 0 {
		serverIDs := make([]uuid.UUID, 0, len(ids.servers))
		for id := range ids.servers {
			serverIDs = append(serverIDs, id)
		}
		rows, err := queries.ListServerNames(ctx, repo.ListServerNamesParams{ProjectID: c.projectID, Ids: serverIDs, OrganizationID: c.organizationID})
		if err != nil {
			return out, oops.E(oops.CodeUnexpected, err, "load server names").LogError(ctx, s.logger)
		}
		for _, row := range rows {
			out.servers[row.ID.String()] = row
		}
	}
	if len(ids.users) > 0 {
		userIDs := make([]string, 0, len(ids.users))
		for id := range ids.users {
			userIDs = append(userIDs, id)
		}
		rows, err := queries.ListMemberNames(ctx, repo.ListMemberNamesParams{OrganizationID: c.organizationID, Ids: userIDs})
		if err != nil {
			return out, oops.E(oops.CodeUnexpected, err, "load people").LogError(ctx, s.logger)
		}
		for _, row := range rows {
			out.people[row.ID] = person{name: row.DisplayName, email: row.Email}
		}
	}
	return out, nil
}

func (n names) server(id, storedKind string) *gen.ConnectionServer {
	kind, ok := serverKinds[storedKind]
	if id == "" || !ok {
		return nil
	}
	view := &gen.ConnectionServer{ID: id, Kind: kind, Name: nil}
	if row, found := n.servers[id]; found && row.Name != "" {
		view.Name = &row.Name
		view.Kind = row.Kind
	}
	return view
}

func (n names) member(id string) *gen.ConnectionServer {
	if id == "" {
		return nil
	}
	view := &gen.ConnectionServer{ID: id, Kind: "mcp", Name: nil}
	if row, found := n.servers[id]; found && row.Name != "" {
		view.Name = &row.Name
		view.Kind = row.Kind
	}
	return view
}

func (n names) serverName(id string) *string {
	if row, found := n.servers[id]; found && row.Name != "" {
		return &row.Name
	}
	return nil
}

// personKinds are the subject kinds the API names.
var personKinds = map[string]struct{}{
	string(urn.SessionSubjectKindUser):      {},
	string(urn.SessionSubjectKindAgent):     {},
	string(urn.SessionSubjectKindAPIKey):    {},
	string(urn.SessionSubjectKindWorkload):  {},
	string(urn.SessionSubjectKindAnonymous): {},
}

func (n names) person(subjectURN string, withEmail bool) *gen.ConnectionPerson {
	if subjectURN == "" {
		return nil
	}
	kind, id, _ := strings.Cut(subjectURN, ":")
	if _, ok := personKinds[kind]; !ok {
		kind = "other"
	}
	view := &gen.ConnectionPerson{SubjectUrn: subjectURN, Kind: kind, UserID: nil, Name: nil, Email: nil}
	if kind == string(urn.SessionSubjectKindUser) {
		view.UserID = &id
		if p, found := n.people[id]; found {
			view.Name = conv.PtrEmpty(p.name)
			if withEmail {
				view.Email = conv.PtrEmpty(p.email)
			}
		}
	}
	return view
}

func (n names) personName(subjectURN string) *string {
	kind, id, _ := strings.Cut(subjectURN, ":")
	if kind != string(urn.SessionSubjectKindUser) {
		return nil
	}
	if p, found := n.people[id]; found {
		return conv.PtrEmpty(p.name)
	}
	return nil
}

func appView(family, version, appKey string) *gen.ConnectionApp {
	family = attemptlog.AppFamily(family)
	return &gen.ConnectionApp{
		Family:  family,
		Name:    attemptlog.AppName(family),
		Version: conv.PtrEmpty(version),
		AppKey:  conv.PtrEmpty(appKey),
	}
}

func networkView(key string) *gen.ConnectionNetwork {
	if key == "" {
		return nil
	}
	return &gen.ConnectionNetwork{Key: key, Label: attemptlog.NetworkLabel(key)}
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func (n names) row(r attemptlog.Row) *gen.ConnectionAttemptRow {
	return &gen.ConnectionAttemptRow{
		ID:               r.ID.String(),
		Time:             formatTime(r.Time),
		FirstStartedAt:   formatTime(r.FirstStartedAt),
		Count:            count(r.Count),
		Outcome:          gen.ConnectionOutcome(r.Outcome),
		Step:             gen.ConnectionStepGroup(r.Step),
		Reason:           conv.PtrEmpty(r.Reason),
		Message:          conv.PtrEmpty(attemptlog.MessageOf(r.Reason)),
		TechnicalMessage: nil,
		Source:           conv.PtrEmpty(gen.ConnectionFailureSource(attemptlog.SourceOf(r.Reason, attemptlog.Outcome(r.Outcome)))),
		Person:           n.person(r.Subject, false),
		Network:          networkView(r.Network),
		App:              appView(r.App, r.AppVersion, r.AppKey),
		Server:           n.server(r.ServerID, r.ServerKind),
		Member:           n.member(r.MemberID),
		Service:          conv.PtrEmpty(r.Service),
	}
}

func count(v uint64) int64 {
	out, _ := conv.ClampedUint64ToInt64(v)
	return out
}
