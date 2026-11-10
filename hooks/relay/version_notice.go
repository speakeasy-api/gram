package relay

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/speakeasy-api/agenthooks"
)

const versionStatusUnsupported = "unsupported"

// versionNotice returns a once-per-session note telling the user this hooks
// binary is no longer supported, or "" when there is nothing to say, the
// provider cannot show one on this event, or the session already saw it. It
// only ever adds a message: the decision it rides on is unchanged.
func (r *Relay) versionNotice(e *agenthooks.Event, status string) string {
	if status != versionStatusUnsupported {
		return ""
	}
	if !agenthooks.Capabilities(e.Provider, e.Variant, e.Kind).Has(agenthooks.CapSystemMessage) {
		return ""
	}
	if !claimVersionNotice(e.Session.ID) {
		return ""
	}
	return fmt.Sprintf("Speakeasy: the Speakeasy hooks plugin on this machine (version %s) is no longer supported. Ask your Speakeasy admin to update it.", BinaryVersion)
}

func claimVersionNotice(sessionID string) bool {
	key := sessionID
	if key == "" {
		key = "session"
	}
	marker := filepath.Join(os.TempDir(), "speakeasy-hooks-version-notice-"+sanitizeMarker(key))
	f, err := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}
