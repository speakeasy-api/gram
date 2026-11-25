package assistants

import "fmt"

var ErrKitMCPDesiredSetUnsupported = fmt.Errorf("%w: kit cannot reconcile an explicit MCP desired set on resume; the runtime is replaced with the new set", errKitRuntimeUnrecoverable)

// validateKitMCPStartup distinguishes initial configuration from reconciliation.
// The turn API's non-nil list is a desired set, not an additive update command.
// Kit's approved update_mcp contract upserts client-owned names, retains omitted
// names, and makes [] a no-op. It cannot implement desired-set removal. Host and
// plugin collisions remain Kit errors; the adapter must not rename around them.
// Resume accepts only omission as retain; even an explicit empty list is rejected.
// We cannot distinguish resident resume from cold restore here, so explicit
// desired sets fail closed for either. New sessions can take initial servers.
func validateKitMCPStartup(resuming, desiredSetProvided bool) error {
	if resuming && desiredSetProvided {
		return ErrKitMCPDesiredSetUnsupported
	}
	return nil
}
