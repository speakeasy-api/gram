package contextvalues

import (
	"context"
	"strings"
	"sync"
)

// connectionReferenceLabel starts the line a person passes on to their admin.
const connectionReferenceLabel = "Reference: "

// ConnectionReferenceMarker is what splits a message from its reference line.
const ConnectionReferenceMarker = "\n" + connectionReferenceLabel

type connectionReferenceKey struct{}

type connectionReferenceSlot struct {
	mu        sync.Mutex
	reference string

	// refresh is a provider refresh row's id, shown only once claimed: a failed
	// refresh doesn't by itself mean the error shown is about it.
	refresh string

	// refreshAmbiguous says refreshes with different ids failed, so none can be claimed.
	refreshAmbiguous bool
}

// WithConnectionReferenceSlot gives ctx a place for the reference of a connection row
// recorded while serving it, for the error the person sees. A nested slot shadows ctx's.
func WithConnectionReferenceSlot(ctx context.Context) context.Context {
	return context.WithValue(ctx, connectionReferenceKey{}, &connectionReferenceSlot{mu: sync.Mutex{}, reference: "", refresh: "", refreshAmbiguous: false})
}

// ConnectionReferenceWanted reports that someone will read a reference left on ctx.
func ConnectionReferenceWanted(ctx context.Context) bool {
	_, ok := ctx.Value(connectionReferenceKey{}).(*connectionReferenceSlot)
	return ok
}

// SetConnectionReference leaves the reference on ctx's slot. The last one set is the one
// shown, and "" clears it.
func SetConnectionReference(ctx context.Context, reference string) {
	if slot, ok := ctx.Value(connectionReferenceKey{}).(*connectionReferenceSlot); ok {
		slot.mu.Lock()
		defer slot.mu.Unlock()
		slot.reference = reference
	}
}

// SetRefreshConnectionReference notes a failed refresh's id without showing it.
func SetRefreshConnectionReference(ctx context.Context, reference string) {
	if slot, ok := ctx.Value(connectionReferenceKey{}).(*connectionReferenceSlot); ok {
		slot.mu.Lock()
		defer slot.mu.Unlock()
		if slot.refresh != "" && slot.refresh != reference {
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
			slot.reference = slot.refresh
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
	if line == "" || strings.Contains(message, line) {
		return message
	}
	return message + "\n" + line
}
