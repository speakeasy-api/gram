package zedspike

// RelationKey names a relation on a type.
type RelationKey struct {
	Type     string
	Relation string
}

// EdgeSource says where an edge's value comes from at check time.
type EdgeSource int

const (
	// FromReference: the typed reference carries it, as
	// MCPServer(projectID, id) carries the project.
	FromReference EdgeSource = iota + 1
	// FromCaller: the caller's organization. Every organization edge is this,
	// so the table does not list them.
	FromCaller
)

// Edges lists every edge except organization edges, which always point at
// the caller's organization. The loader fails on an edge relation missing
// here and on an entry no relation uses.
var Edges = map[RelationKey]EdgeSource{
	{"mcp_server", "project"}:  FromReference,
	{"environment", "project"}: FromReference,
	{"skill", "project"}:       FromReference,
	{"assistant", "project"}:   FromReference,
	{"inference", "project"}:   FromReference,
	{"chat", "project"}:        FromReference,
}

// Facts lists relations filled from a column of the checked row rather than
// from grants. The handler loads the row and passes it through a typed
// method, such as Chat(projectID, id).WithOwner(owner).
var Facts = map[RelationKey]string{
	{"chat", "author"}: "chats.user_id",
	{"chat", "agent"}:  "chats.agent_id",
	{"agent", "owner"}: "agents.owner_user_id",
}

// landsOnProject reports whether rows of a scope whose resource type has a
// project edge land on the project when they name every resource of it.
// Types without a project edge, such as mcp_auth_client, keep project_id as
// a caveat parameter instead.
func landsOnProject(resourceType string) bool {
	_, ok := Edges[RelationKey{resourceType, "project"}]
	return ok || resourceType == "project"
}
