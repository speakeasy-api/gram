package metamcp

// Member connection states. Hosted (toolset-backed) members execute
// in-process, so they are always available; proxied members report
// Member connection states surfaced by list_servers. Remote members stay
// unknown until cached health exists; tunneled report live route state.
const (
	StatusUnknown     = "unknown"
	StatusAvailable   = "available"
	StatusUnavailable = "unavailable"
)

// ListedServer is one member entry in a list_servers result.
type ListedServer struct {
	Slug      string `json:"slug"`
	Name      string `json:"name,omitempty"`
	SortOrder int    `json:"sortOrder"`
	Status    string `json:"status"`
}

// ListServersResult is the structuredContent payload of a list_servers call.
type ListServersResult struct {
	Servers []ListedServer `json:"servers"`
}
