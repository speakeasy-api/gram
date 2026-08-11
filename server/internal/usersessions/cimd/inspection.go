package cimd

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
)

// Inspection is the management surface's rich view of one resolution attempt.
//
// Detail is composed from the outcome, never from the underlying transport
// error, so it is safe to show an authenticated operator without leaking
// guardian denials, DNS failures, or internal hostnames.
type Inspection struct {
	// Outcome is what happened. Always set.
	Outcome Outcome

	// Document is the validated document, non-nil only when Outcome is
	// OutcomeValid.
	Document *Document

	// HTTPStatus is the status the document endpoint returned, or 0 when no
	// response was received (or no fetch was attempted).
	HTTPStatus int

	// Reason is the stable machine label for the rule that rejected the
	// document, e.g. "client_id_mismatch" or "missing_client_name". Set only
	// for OutcomeInvalidURL and OutcomeInvalidDocument; empty otherwise.
	Reason string

	// Detail is a human-readable, operator-facing explanation. Always set.
	Detail string
}

// Inspect resolves the document like Resolve, but reports the full outcome
// taxonomy instead of an opaque error. It is for AUTHENTICATED management
// surfaces only — never the OAuth endpoints, whose callers must not be able
// to use Gram as a probe oracle for external hosts.
func (r *Resolver) Inspect(ctx context.Context, clientID string) Inspection {
	result := r.inspect(ctx, clientID)
	return Inspection{
		Outcome:    result.outcome,
		Document:   result.Document,
		HTTPStatus: result.status,
		Reason:     string(result.reason),
		Detail:     result.detail(),
	}
}

// inspection is the shared internal result of one resolution attempt,
// produced by Resolver.inspect in cimd.go alongside the fetch it wraps.
// Resolve reads only Document and err; Inspect reads the rest.
type inspection struct {
	Document *Document
	outcome  Outcome
	status   int
	reason   validationReason

	// err is exactly the error Resolve returns, preserving that path's
	// established wrapping. nil when outcome is OutcomeValid.
	err error

	// safeDescription is the client-safe text from an *oauthwire.Error, set
	// only for the two validation outcomes. Never holds transport error text.
	safeDescription string

	// tooLarge distinguishes the one unreachable case that arrives with a
	// 200: the body ran past the size cap. Without it, every 200 that failed
	// to read would be blamed on size.
	tooLarge bool
}

// detail renders the operator-facing explanation. Validation rejections reuse
// the OAuth description, which is client-safe by construction; the transport
// outcomes get fixed text plus the status, so no internal condition is ever
// echoed.
func (i inspection) detail() string {
	switch i.outcome {
	case OutcomeValid:
		return "The client ID metadata document is reachable and valid."
	case OutcomeInvalidURL, OutcomeInvalidDocument:
		if i.safeDescription != "" {
			return i.safeDescription
		}
		return "The client ID metadata document is not valid."
	case OutcomeUnreachable:
		// A 200 that still lands here means the response arrived but the body
		// could not be read to completion. Saying it "returned HTTP 200, it
		// must return 200" would be a contradiction, so name the real
		// problem, and only blame the size cap when that is what happened.
		switch {
		case i.tooLarge:
			return fmt.Sprintf("The document endpoint responded, but the document is larger than the %d byte limit.", maxDocumentBytes)
		case i.status == 0:
			return "Gram could not reach the document endpoint."
		case i.status == http.StatusOK:
			return "The document endpoint responded, but Gram could not read the document to completion."
		default:
			return fmt.Sprintf("The document endpoint returned HTTP %d. It must return 200 without redirecting.", i.status)
		}
	case OutcomeUnparseable:
		return "The document endpoint responded, but the body is not valid JSON."
	default:
		return "The client ID metadata document could not be verified."
	}
}

// safeDescriptionOf extracts the client-safe OAuth description from a
// validation rejection. Returns "" for any error that is not one, which keeps
// transport error text out of operator-facing detail by construction.
func safeDescriptionOf(err error) string {
	if oauthErr, ok := errors.AsType[*oauthwire.Error](err); ok {
		return oauthErr.Description
	}
	return ""
}
