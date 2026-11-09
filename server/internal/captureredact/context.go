package captureredact

import "context"

type passiveCaptureKey struct{}

// WithPassiveCapture marks a trusted in-process request whose observability
// copies must be redacted independently of its original execution payload.
func WithPassiveCapture(ctx context.Context) context.Context {
	return context.WithValue(ctx, passiveCaptureKey{}, true)
}

// PassiveCapture reports whether the owning request boundary selected passive
// capture redaction. External callers cannot set this context value.
func PassiveCapture(ctx context.Context) bool {
	enabled, _ := ctx.Value(passiveCaptureKey{}).(bool)
	return enabled
}
