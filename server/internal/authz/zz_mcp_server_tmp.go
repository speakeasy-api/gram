package authz

// MCPServerRef names one MCP server in its project for typed operations.
type MCPServerRef struct {
	projectID string
	id        string
}

// MCPServer names an MCP server in its project.
func MCPServer(projectID, serverID string) MCPServerRef {
	return MCPServerRef{projectID: projectID, id: serverID}
}

// MCPServersInProject names every MCP server of a project.
func MCPServersInProject(projectID string) MCPServerRef {
	return MCPServerRef{projectID: projectID, id: projectID}
}

func (m MCPServerRef) dims() map[string]string {
	return map[string]string{SelectorKeyProjectID: m.projectID}
}

// Read asks to inspect the server.
func (m MCPServerRef) Read() Operation { return newOperation("mcp_server", "read", m.id, m.dims()) }

// Edit asks to change the server.
func (m MCPServerRef) Edit() Operation { return newOperation("mcp_server", "edit", m.id, m.dims()) }

// Connect asks to connect to the server.
func (m MCPServerRef) Connect() Operation {
	return newOperation("mcp_server", "connect", m.id, m.dims())
}
