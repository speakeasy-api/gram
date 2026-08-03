package workos

import "fmt"

// Backend selects where the dev-idp's WorkOS surface gets its data.
type Backend int

const (
	// BackendLocal emulates the WorkOS REST API against the dev-idp's own
	// SQLite store. Fully offline, zero configuration.
	BackendLocal Backend = iota

	// BackendWorkOS passes REST calls through to a real WorkOS environment.
	// Login stays non-interactive -- see Handler.Handler for the endpoints
	// that are always served locally regardless of backend.
	BackendWorkOS
)

const (
	backendLocalName  = "local"
	backendWorkOSName = "workos"
)

func (b Backend) String() string {
	switch b {
	case BackendLocal:
		return backendLocalName
	case BackendWorkOS:
		return backendWorkOSName
	default:
		return fmt.Sprintf("Backend(%d)", int(b))
	}
}

// ParseBackend maps the GRAM_DEVIDP_BACKEND value onto a Backend. Empty
// defaults to local so a fresh checkout needs no configuration.
func ParseBackend(s string) (Backend, error) {
	switch s {
	case "", backendLocalName:
		return BackendLocal, nil
	case backendWorkOSName:
		return BackendWorkOS, nil
	default:
		return BackendLocal, fmt.Errorf("unrecognized GRAM_DEVIDP_BACKEND value %q (expected %q or %q)", s, backendLocalName, backendWorkOSName)
	}
}
