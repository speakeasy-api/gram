package assistants

import "errors"

var ErrKitMCPDesiredSetUnsupported = errors.New("kit cannot reconcile an explicit MCP desired set on resume: additive updates cannot remove omitted servers; revoke authority and retire the runtime before changing its configured set")

// validateKitMCPStartup distinguishes initial configuration from reconciliation.
// The turn API's non-nil list is a desired set, not an additive update command.
// Kit's approved update_mcp contract upserts client-owned names, retains omitted
// names, and makes [] a no-op. It cannot implement desired-set removal. Host and
// plugin collisions remain Kit errors; the adapter must not rename around them.
// Resident resume accepts omission/[] as retain and rejects nonempty lists.
// We cannot distinguish resident resume from cold restore here, so explicit
// desired sets fail closed for either. New sessions can take initial servers.
func validateKitMCPStartup(resuming, desiredSetProvided bool) error {
	if resuming && desiredSetProvided {
		return ErrKitMCPDesiredSetUnsupported
	}
	return nil
}
