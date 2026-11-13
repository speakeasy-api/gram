// Package model is the Speakeasy authorization schema for the spike: the one
// place permissions are declared. In production this would live at
// server/internal/authz/schema and feed code generation.
package model

import (
	"slices"

	s "github.com/speakeasy-api/gram/spikes/clean-slate-authz/schema"
)

const (
	Organization = "organization"
	Project      = "project"
	MCPServer    = "mcp_server"
	Agent        = "agent"
	Chat         = "chat"
	User         = "user"
	Group        = "group"
	Role         = "role"
)

var types = []*s.Type{
	s.NewType(Organization,
		s.Grant("agent_creator", s.AnchoredAt(s.Self), s.Describe("Create agents you own")),
		s.Grant("access_admin", s.AnchoredAt(s.Self), s.Describe("Manage roles, grants and protection")),
		s.Permission("create_agent", s.Rel("agent_creator")),
		s.Permission("manage_access", s.Rel("access_admin")),
	),

	s.NewType(Project, s.Parent(Organization),
		s.Grant("viewer", s.AnchoredAt(Organization, s.Self), s.Describe("View project metadata")),
		s.Grant("writer", s.AnchoredAt(Organization, s.Self), s.Describe("Change project settings")),
		s.Permission("write", s.Rel("writer")),
		s.Permission("read", s.Union(s.Rel("viewer"), s.Perm("write"))),
	),

	s.NewType(MCPServer, s.Parent(Project),
		s.Grant("editor", s.AnchoredAt(Organization, Project, s.Self),
			s.Describe("Edit configuration, connection audience and visibility")),
		s.Grant("viewer", s.AnchoredAt(Organization, Project, s.Self),
			s.Describe("Inspect non-secret configuration")),
		s.Grant("connector", s.AnchoredAt(Organization, Project, s.Self),
			s.Conditions(CondMCPTools), s.Delegable(),
			s.Describe("Connect to the server and call its tools")),
		s.Block("connect_blocked", s.AnchoredAt(Organization, Project, s.Self),
			s.Conditions(CondMCPTools)),

		s.Permission("edit", s.Rel("editor")),
		s.Permission("read", s.Union(s.Rel("viewer"), s.Perm("edit"))),
		// No edge from edit or read: managing a server never grants use.
		s.Permission("connect", s.Rel("connector"), s.ExcludedBy("connect_blocked")),
	),

	s.NewType(User, s.Parent(Organization), s.MemberOf(Group),
		s.Grant("activity_reviewer", s.AnchoredAt(Organization, Group), s.Sensitive(),
			s.Describe("Read transcripts of this person's sessions")),
		s.Grant("protected_reviewer", s.AnchoredAt(Organization), s.Sensitive()),
		s.Marker("protected", "Hide activity from ordinary reviewers"),

		s.Permission("review_activity", s.Union(
			s.Minus(s.Rel("activity_reviewer"), s.Marked("protected")),
			s.Rel("protected_reviewer"),
		)),
	),

	s.NewType(Agent, s.Parent(Project), s.Attr("owner", User),
		s.Grant("manager", s.AnchoredAt(Organization, Project, s.Self)),
		s.Grant("viewer", s.AnchoredAt(Organization, Project, s.Self)),
		s.Permission("manage", s.Union(s.Is("owner"), s.Rel("manager"))),
		s.Permission("read", s.Union(s.Perm("manage"), s.Rel("viewer"))),
		s.Permission("review_activity", s.Via("owner", "review_activity")),
	),

	s.NewType(Chat, s.Parent(Project), s.Attr("author", User), s.Attr("agent", Agent),
		s.Permission("read", s.Union(
			s.Is("author"),                     // personal history stays with the user
			s.Via("agent", "manage"),           // the agent's current owner
			s.Via("author", "review_activity"), // team or org review, aware of protection
			s.Via("agent", "review_activity"),  // review of the owner's team
		)),
	),

	// Subject-set types. They carry membership relations only.
	s.NewType(Group, s.Parent(Organization)),
	s.NewType(Role, s.Parent(Organization)),
}

// MembershipRelations are followed when computing a caller's subject closure.
var MembershipRelations = []string{"member", "assignee"}

// Schema is compiled once at startup; a bad declaration fails the process.
var Schema = func() *s.Schema {
	sc, err := s.Compile(types, []*s.Condition{MCPTools})
	if err != nil {
		panic(err)
	}
	return sc
}()

// IsMembership reports whether a relation extends the subject closure.
func IsMembership(relation string) bool { return slices.Contains(MembershipRelations, relation) }
