package metamcp

// StatusUnknown is the fixed member connection state until the meta-server
// runtime (AGE-3291) holds live member sessions to report on.
const StatusUnknown = "unknown"

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
