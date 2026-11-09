package contextvalues

import (
	"context"
	"sync/atomic"
)

type connectionReferenceKey struct{}

type connectionReferenceSlot struct {
	reference atomic.Pointer[string]
}

// WithConnectionReferenceSlot gives ctx a place for the reference of a connection row
// recorded while serving it, for the error the person sees. A nested slot shadows ctx's.
func WithConnectionReferenceSlot(ctx context.Context) context.Context {
	return context.WithValue(ctx, connectionReferenceKey{}, &connectionReferenceSlot{reference: atomic.Pointer[string]{}})
}

// ConnectionReferenceWanted reports that someone will read a reference left on ctx.
func ConnectionReferenceWanted(ctx context.Context) bool {
	_, ok := ctx.Value(connectionReferenceKey{}).(*connectionReferenceSlot)
	return ok
}

// SetConnectionReference leaves the reference on ctx's slot. The last one set is the one shown.
func SetConnectionReference(ctx context.Context, reference string) {
	if slot, ok := ctx.Value(connectionReferenceKey{}).(*connectionReferenceSlot); ok {
		slot.reference.Store(&reference)
	}
}

// ConnectionReference returns the reference left on ctx, or "" when there is none.
func ConnectionReference(ctx context.Context) string {
	if slot, ok := ctx.Value(connectionReferenceKey{}).(*connectionReferenceSlot); ok {
		if reference := slot.reference.Load(); reference != nil {
			return *reference
		}
	}
	return ""
}

// ConnectionReferenceLine is the line a person passes on to their admin, or "" when there is no reference.
func ConnectionReferenceLine(ctx context.Context) string {
	reference := ConnectionReference(ctx)
	if reference == "" {
		return ""
	}
	return "Reference: " + reference
}

// AppendConnectionReference adds the reference line under a message, unchanged when there is none.
func AppendConnectionReference(ctx context.Context, message string) string {
	line := ConnectionReferenceLine(ctx)
	if line == "" {
		return message
	}
	return message + "\n" + line
}
