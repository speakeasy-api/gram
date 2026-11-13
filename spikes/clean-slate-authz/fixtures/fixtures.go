// Package fixtures is the example organization from the design doc
// (section 2), shared by both backends so they answer identical questions.
package fixtures

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/authz"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/objects"
)

const (
	Org     = "org_acme"
	Project = "proj_web"
	Server  = "srv_linear"
	AgentID = "agt_triage"
	KeyID   = "key_123"
)

// TemporaryAccessEnds is when Erin's protected-review access (row 15) expires:
// 30 minutes after the process starts, so it is live when the tests run.
var TemporaryAccessEnds = time.Now().UTC().Truncate(time.Second).Add(30 * time.Minute)

// RowID gives row n (1-based, as numbered in the doc) a stable id.
func RowID(n int) uuid.UUID {
	return uuid.MustParse(fmt.Sprintf("00000000-0000-7000-8000-%012d", n))
}

func obj(t, id string) authz.ObjectID { return authz.ObjectID{Type: t, ID: id} }
func set(t, id, rel string) authz.Subject {
	return authz.Subject{Type: t, ID: id, Relation: rel}
}
func tools(p map[string][]string) json.RawMessage { b, _ := json.Marshal(p); return b }

// Relationships are the rows of authz_relationships, numbered as in the doc.
// Rows 18–21 add Dave, Erin and Frank as members, and make Frank an
// organization-wide reviewer so every option is tested on protection.
func Relationships() []authz.Relationship {
	ends := TemporaryAccessEnds
	rows := []authz.Relationship{
		{Object: obj("organization", Org), Relation: "member", Subject: authz.User("alice"), Source: "directory_sync"},
		{Object: obj("organization", Org), Relation: "member", Subject: authz.User("bob"), Source: "directory_sync"},
		{Object: obj("organization", Org), Relation: "member", Subject: authz.User("carol"), Source: "directory_sync"},
		{Object: obj("role", "role_member"), Relation: "assignee", Subject: set("organization", Org, "member"), Source: "provisioned"},
		{Object: obj("group", "grp_eng"), Relation: "member", Subject: authz.User("alice"), Source: "directory_sync"},
		{Object: obj("group", "grp_eng"), Relation: "member", Subject: authz.User("bob"), Source: "directory_sync"},
		{Object: obj("role", "role_eng"), Relation: "assignee", Subject: set("group", "grp_eng", "member"), Source: "admin"},
		{Object: obj("organization", Org), Relation: "agent_creator", Subject: set("role", "role_member", "assignee"), Source: "provisioned"},
		{Object: obj("project", Project), Relation: "mcp_server.viewer", Subject: set("role", "role_member", "assignee"), Source: "provisioned"},
		{Object: obj("mcp_server", Server), Relation: "connector", Subject: set("role", "role_eng", "assignee"), Source: "admin"},
		{Object: obj("mcp_server", Server), Relation: "connect_blocked", Subject: set("role", "role_eng", "assignee"),
			ConditionName: "mcp_tools", ConditionParams: tools(map[string][]string{"dispositions": {"destructive"}}), Source: "admin"},
		{Object: obj("project", Project), Relation: "mcp_server.editor", Subject: authz.User("dave"), Source: "admin"},
		{Object: obj("group", "grp_eng"), Relation: "user.activity_reviewer", Subject: authz.User("alice"), Source: "admin"},
		{Object: obj("user", "carol"), Relation: "protected", Subject: authz.Subject{Type: "organization", ID: Org}, Source: "admin"},
		{Object: obj("organization", Org), Relation: "user.protected_reviewer", Subject: authz.User("erin"), ExpiresAt: &ends,
			Source: "temporary_access", SourceRef: "req_77"},
		{Object: obj("mcp_server", Server), Relation: "connector", Subject: authz.Agent(AgentID),
			ConditionName: "mcp_tools", ConditionParams: tools(map[string][]string{"tools": {"list_issues", "get_issue", "update_issue"}}), Source: "self_service"},
		{Object: obj("mcp_server", Server), Relation: "connector", Subject: authz.APIKey(KeyID), Source: "self_service"},
		{Object: obj("organization", Org), Relation: "member", Subject: authz.User("dave"), Source: "directory_sync"},
		{Object: obj("organization", Org), Relation: "member", Subject: authz.User("erin"), Source: "directory_sync"},
		{Object: obj("organization", Org), Relation: "member", Subject: authz.User("frank"), Source: "directory_sync"},
		{Object: obj("organization", Org), Relation: "user.activity_reviewer", Subject: authz.User("frank"), Source: "admin"},
	}
	for i := range rows {
		rows[i].ID = RowID(i + 1)
	}
	return rows
}

var Agents = []authz.AgentRow{{ID: AgentID, OwnerUserID: "alice", ProjectID: Project, Status: "active"}}

var APIKeys = []authz.APIKeyRow{{ID: KeyID, AgentID: AgentID}}

// Domain objects the handlers would load.
var (
	ServerObj = objects.MCPServer{ID: Server, ProjectID: Project, OrganizationID: Org}
	AgentObj  = objects.Agent{ID: AgentID, ProjectID: Project, OrganizationID: Org, OwnerUserID: "alice"}
	OrgObj    = objects.Organization{ID: Org}
)

type ChatRow struct {
	ID           string
	AuthorUserID string // "" for agent-only sessions
	AgentID      string
}

var Chats = []ChatRow{
	{ID: "chat_alice", AuthorUserID: "alice"},
	{ID: "chat_bob", AuthorUserID: "bob"},
	{ID: "chat_carol", AuthorUserID: "carol"},
	{ID: "chat_dave", AuthorUserID: "dave"},
	{ID: "chat_agent", AgentID: AgentID},
}

func ChatObj(c ChatRow) objects.Chat {
	o := objects.Chat{ID: c.ID, ProjectID: Project, OrganizationID: Org, AuthorUserID: c.AuthorUserID}
	if c.AgentID != "" {
		a := AgentObj
		o.Agent = &a
	}
	return o
}
