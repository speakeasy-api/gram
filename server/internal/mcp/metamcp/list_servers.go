package metamcp

// Member connection states. Hosted (toolset-backed) members execute
// in-process, so they are always available; proxied members report
// StatusUnknown until the runtime holds live member sessions to report on
// (AGE-3291 PR 2).
const (
	StatusUnknown   = "unknown"
	StatusAvailable = "available"
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
