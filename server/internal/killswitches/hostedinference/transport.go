package hostedinference

import (
	"context"
	"errors"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

// IsBoundaryError reports whether err is a hosted-inference outcome that an
// HTTP/Goa boundary must map to a safe public error.
func IsBoundaryError(err error) bool {
	if _, ok := errors.AsType[*MatchedDenialError](err); ok {
		return true
	}
	_, ok := errors.AsType[*InfrastructureUnavailableError](err)
	return ok
}

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

// MapBoundaryError converts hosted-inference errors and applies the one safe
// reporting policy: matched notes bypass internal reporting, while generic
// infrastructure failures retain and log their diagnostic cause.
func MapBoundaryError(ctx context.Context, logger *slog.Logger, err error) (error, bool) {
	shareable, ok := AsShareableError(err)
	if !ok {
		return nil, false
	}
	if shareable.SpanHandled() {
		return shareable, true
	}
	return shareable.LogError(ctx, logger), true
}
