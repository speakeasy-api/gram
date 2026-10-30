// Package hmodel is Option D's schema: Option A's declaration language, with
// every grant mapped onto a scope that already exists in principal_grants.
// The semantics are A's; the rows are B's.
package hmodel

import (
	s "github.com/speakeasy-api/gram/spikes/clean-slate-authz/schema"

	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/model"
)

var types = []*s.Type{
	s.NewType("organization",
		s.Grant("agent_creator", s.StoredAs("agent:create"), s.AnchoredAt(s.Self), s.Describe("Create agents you own")),
		s.Permission("create_agent", s.Rel("agent_creator")),
	),

	s.NewType("project", s.Parent("organization"),
		s.Grant("viewer", s.StoredAs("project:read"), s.AnchoredAt("organization", s.Self)),
		s.Permission("read", s.Rel("viewer")),
	),

	s.NewType("mcp_server", s.Parent("project"),
		s.Grant("viewer", s.StoredAs("mcp:read"), s.AnchoredAt("organization", "project", s.Self),
			s.Describe("Inspect non-secret configuration")),
		s.Grant("editor", s.StoredAs("mcp:write"), s.AnchoredAt("organization", "project", s.Self),
			s.Describe("Edit configuration, connection audience and visibility")),
		s.Grant("connector", s.StoredAs("mcp:connect"), s.AnchoredAt("organization", "project", s.Self),
			s.Conditions(model.CondMCPTools), s.Delegable(),
			s.Describe("Connect to the server and call its tools")),
		s.Block("connect_blocked", s.StoredAs("mcp:blocked_connect"), s.AnchoredAt("organization", "project", s.Self),
			s.Conditions(model.CondMCPTools)),

		s.Permission("edit", s.Rel("editor")),
		s.Permission("read", s.Union(s.Rel("viewer"), s.Perm("edit"))),
		// No edge from edit or read: managing a server never grants use.
		s.Permission("connect", s.Rel("connector"), s.ExcludedBy("connect_blocked")),
	),

	s.NewType("user", s.Parent("organization"),
		// One row on the person, not a block on every reviewer grant.
		s.MarkerAs("protected", "Hide activity from ordinary reviewers", "user:protected"),
		s.Permission("is_protected", s.Marked("protected")),
	),

	s.NewType("agent", s.Parent("project"), s.Attr("owner", "user"),
		s.Grant("manager", s.StoredAs("agent:write"), s.AnchoredAt("organization", "project", s.Self)),
		s.Grant("viewer", s.StoredAs("agent:read"), s.AnchoredAt("organization", "project", s.Self)),
		s.Permission("manage", s.Union(s.Is("owner"), s.Rel("manager"))),
		s.Permission("read", s.Union(s.Perm("manage"), s.Rel("viewer"))),
	),

	s.NewType("chat", s.Parent("project"), s.Attr("author", "user"), s.Attr("agent", "agent"),
		// chat:read with a subject_principal selector, as Option B stores it.
		s.Grant("reader", s.StoredAs("chat:read"), s.AnchoredAt("organization", "project"),
			s.SubjectScoped("author", "agent"), s.Sensitive(),
			s.Describe("Read transcripts of the selected people's sessions")),
		s.Grant("protected_reader", s.StoredAs("chat:read_protected"), s.AnchoredAt("organization", "project"),
			s.SubjectScoped("author", "agent"), s.Sensitive(),
			s.Describe("Read protected people's sessions (temporary access)")),
		s.Block("read_blocked", s.StoredAs("chat:blocked_read"), s.AnchoredAt("organization", "project"),
			s.SubjectScoped("author", "agent")),

		s.Permission("read", s.Union(
			s.Is("author"),           // personal history
			s.Via("agent", "manage"), // the agent's current owner
			s.Minus(s.Rel("reader"), s.Via("author", "is_protected")),
			s.Rel("protected_reader"),
		), s.ExcludedBy("read_blocked")), // a matching block beats everything, self-access included
	),

	s.NewType("group", s.Parent("organization")),
	s.NewType("role", s.Parent("organization")),
}

var Schema = func() *s.Schema {
	sc, err := s.Compile(types, []*s.Condition{model.MCPTools})
	if err != nil {
		panic(err)
	}
	return sc
}()
