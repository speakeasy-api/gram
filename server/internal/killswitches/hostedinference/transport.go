package hostedinference

import (
	"errors"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

// AsShareableError maps checkpoint outcomes at HTTP/Goa boundaries. Matched
// denials intentionally have no wrapped cause so tenant-authored notes cannot
// leak through internal cause logging. Infrastructure rejection has only the
// generic unavailable public message and retains its cause for diagnostics.
func AsShareableError(err error) (*oops.ShareableError, bool) {
	if matched, ok := errors.AsType[*MatchedDenialError](err); ok {
		return oops.E(oops.CodeAIAccessDenied, nil, "%s", matched.ExternalNote()).SuppressInternalReporting(), true
	}

	if _, ok := errors.AsType[*InfrastructureUnavailableError](err); ok {
		return oops.E(oops.CodeUnavailable, err, "%s", oops.CodeUnavailable.UserMessage()), true
	}

	return nil, false
}
