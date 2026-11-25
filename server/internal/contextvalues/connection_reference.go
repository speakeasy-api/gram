package contextvalues

import (
	"context"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// connectionReferenceLabel starts the line a person passes on to their admin.
const connectionReferenceLabel = "Reference: "

// ConnectionReferenceMarker is what splits a message from its reference line.
const ConnectionReferenceMarker = "\n" + connectionReferenceLabel

// connectionReferenceIDLen is the length of a UUID in its usual text form.
const connectionReferenceIDLen = 36

type connectionReferenceKey struct{}

type connectionReferenceSlot struct {
	mu        sync.Mutex
	reference string

	// refresh is a provider refresh row's id, shown only once claimed: a failed
	// refresh doesn't by itself mean the error shown is about it.
	refresh string

	// refreshAmbiguous says refreshes with different ids failed, or one couldn't leave
	// an id, so none can be claimed.
	refreshAmbiguous bool

	// conflict says failures with different ids were recorded, so none can be shown.
	conflict bool
}

// WithConnectionReferenceSlot gives ctx a place for the reference of a connection row
// recorded while serving it, for the error the person sees. A nested slot shadows ctx's.
func WithConnectionReferenceSlot(ctx context.Context) context.Context {
	return context.WithValue(ctx, connectionReferenceKey{}, &connectionReferenceSlot{mu: sync.Mutex{}, reference: "", refresh: "", refreshAmbiguous: false, conflict: false})
}

// ConnectionReferenceWanted reports that someone will read a reference left on ctx.
func ConnectionReferenceWanted(ctx context.Context) bool {
	_, ok := ctx.Value(connectionReferenceKey{}).(*connectionReferenceSlot)
	return ok
}

// set records a failure's id. Two different ids, or a failure that left none, can't be told
// apart from the error shown, so nothing is shown.
func (s *connectionReferenceSlot) set(reference string) {
	switch {
	case s.conflict:
	case reference == "" || (s.reference != "" && s.reference != reference):
		s.reference, s.conflict = "", true
	default:
		s.reference = reference
	}
}

// SetConnectionReference leaves a failure's id on ctx's slot. "" is a failure that left no id.
func SetConnectionReference(ctx context.Context, reference string) {
	if slot, ok := ctx.Value(connectionReferenceKey{}).(*connectionReferenceSlot); ok {
		slot.mu.Lock()
		defer slot.mu.Unlock()
		slot.set(reference)
	}
}

// ReplaceConnectionReference makes reference the slot's whole answer, "" for none, for a
// nested slot handing its result back.
func ReplaceConnectionReference(ctx context.Context, reference string) {
	if slot, ok := ctx.Value(connectionReferenceKey{}).(*connectionReferenceSlot); ok {
		slot.mu.Lock()
		defer slot.mu.Unlock()
		slot.reference, slot.conflict = reference, false
	}
}

// SetRefreshConnectionReference notes a failed refresh's id without showing it. "" is a
// failed refresh that left no id, which rules out claiming any.
func SetRefreshConnectionReference(ctx context.Context, reference string) {
	if slot, ok := ctx.Value(connectionReferenceKey{}).(*connectionReferenceSlot); ok {
		slot.mu.Lock()
		defer slot.mu.Unlock()
		if reference == "" || (slot.refresh != "" && slot.refresh != reference) {
			slot.refreshAmbiguous = true
		}
		slot.refresh = reference
	}
}

// ClaimRefreshConnectionReference shows the failed refresh's id, for an error known to be
// that refresh. It does nothing when no refresh failed or several did.
func ClaimRefreshConnectionReference(ctx context.Context) {
	if slot, ok := ctx.Value(connectionReferenceKey{}).(*connectionReferenceSlot); ok {
		slot.mu.Lock()
		defer slot.mu.Unlock()
		if slot.refresh != "" && !slot.refreshAmbiguous {
			slot.set(slot.refresh)
		}
	}
}

// ConnectionReference returns the reference left on ctx, or "" when there is none.
func ConnectionReference(ctx context.Context) string {
	if slot, ok := ctx.Value(connectionReferenceKey{}).(*connectionReferenceSlot); ok {
		slot.mu.Lock()
		defer slot.mu.Unlock()
		return slot.reference
	}
	return ""
}

// ConnectionReferenceLine is the line a person passes on to their admin, or "" when there is no reference.
func ConnectionReferenceLine(ctx context.Context) string {
	reference := ConnectionReference(ctx)
	if reference == "" {
		return ""
	}
	return connectionReferenceLabel + reference
}

// AppendConnectionReference adds the reference line under a message, unchanged when there is none.
func AppendConnectionReference(ctx context.Context, message string) string {
	line := ConnectionReferenceLine(ctx)
	if line == "" || strings.HasSuffix(message, "\n"+line) {
		return message
	}
	return message + "\n" + line
}

// SplitConnectionReference separates a trailing reference line from message. Only the marker
// and a UUID at the very end count, so a marker elsewhere in upstream text is plain text.
func SplitConnectionReference(message string) (body, referenceLine string) {
	lineLen := len(ConnectionReferenceMarker) + connectionReferenceIDLen
	if len(message) < lineLen {
		return message, ""
	}
	at := len(message) - lineLen
	if !strings.HasPrefix(message[at:], ConnectionReferenceMarker) {
		return message, ""
	}
	if _, err := uuid.Parse(message[at+len(ConnectionReferenceMarker):]); err != nil {
		return message, ""
	}
	return message[:at], message[at:]
}
