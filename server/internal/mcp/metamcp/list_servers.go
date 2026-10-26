package metamcp

// Member connection states surfaced by list_servers. Hosted
// (toolset-backed) members execute in-process, so they are always available;
// remote members stay unknown until cached health exists; tunneled members
// report live route state.
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

// HiddenServer is a member the caller hid. Its tools are not served until
// the caller shows it again.
type HiddenServer struct {
	Slug string `json:"slug"`
	Name string `json:"name,omitempty"`
}

// HiddenNote tells the model why hidden servers are missing and where the
// user can show them again.
const HiddenNote = "The user hid these servers, so their tools are unavailable. The user can show one again at connectUrl."

// ListServersResult is the structuredContent payload of a list_servers call.
type ListServersResult struct {
	Servers    []ListedServer `json:"servers"`
	Hidden     []HiddenServer `json:"hidden,omitempty"`
	ConnectURL string         `json:"connectUrl,omitempty"`
	Note       string         `json:"note,omitempty"`
}
