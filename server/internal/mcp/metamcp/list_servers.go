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

// UnapprovedServer is a current member this connection's approval does not
// cover: the user skipped it at consent, or it joined the gateway after.
// Its tools are not served until the user reconnects the gateway.
type UnapprovedServer struct {
	Slug string `json:"slug"`
	Name string `json:"name,omitempty"`
}

// UnapprovedNote tells the model why unapproved servers are missing and how
// the user adds them.
const UnapprovedNote = "The servers under unapproved are on this gateway but outside what the user approved for this connection, so their tools are unavailable. The user adds them by reconnecting this gateway in their AI app. Call connection_status for each one's reason."

// HiddenNote tells the model why hidden servers are missing and where the
// user can show them again.
const HiddenNote = "The user hid these servers, so their tools are unavailable. The user can show one again at connectUrl."

// UnknownNote tells the model that "unknown" is an unchecked status, not a
// failure, and which tool does check it.
const UnknownNote = `Status "unknown" means not checked: this list does not contact remote servers. Call connection_status to check sign-in.`

// ListServersResult is the structuredContent payload of a list_servers call.
type ListServersResult struct {
	Servers    []ListedServer     `json:"servers"`
	Hidden     []HiddenServer     `json:"hidden,omitempty"`
	Unapproved []UnapprovedServer `json:"unapproved,omitempty"`
	ConnectURL string             `json:"connectUrl,omitempty"`
	Note       string             `json:"note,omitempty"`
}
