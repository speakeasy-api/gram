package assistants

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

// assistantLogContext carries the identifiers every assistant-pipeline log
// event needs to join a group in the logs UI. It lives on the request
// context so a telemetry-emitting decorator around RuntimeBackend can
// label runtime-layer events without the interface having to grow.
type assistantLogContext struct {
	OrganizationID string
	ProjectID      string
	AssistantID    string
	AssistantName  string
	ThreadID       string
	CorrelationID  string
	RuntimeID      string
	RuntimeBackend string
	// Event-scoped fields below are only set once a specific event is
	// being processed.
	EventID           string
	TriggerEventID    string
	TriggerInstanceID string
	Attempt           int
}

type assistantLogContextKey struct{}

func withAssistantLogContext(ctx context.Context, c assistantLogContext) context.Context {
	return context.WithValue(ctx, assistantLogContextKey{}, c)
}

func withAssistantLogEvent(ctx context.Context, event assistantThreadEventRecord) context.Context {
	c, ok := ctx.Value(assistantLogContextKey{}).(assistantLogContext)
	if !ok {
		c = assistantLogContext{
			OrganizationID:    "",
			ProjectID:         "",
			AssistantID:       "",
			AssistantName:     "",
			ThreadID:          "",
			CorrelationID:     "",
			RuntimeID:         "",
			RuntimeBackend:    "",
			EventID:           "",
			TriggerEventID:    "",
			TriggerInstanceID: "",
			Attempt:           0,
		}
	}
	c.EventID = event.ID.String()
	c.TriggerEventID = event.EventID
	c.Attempt = event.Attempts
	if event.CorrelationID != "" {
		c.CorrelationID = event.CorrelationID
	}
	if event.TriggerInstanceID.Valid {
		c.TriggerInstanceID = event.TriggerInstanceID.UUID.String()
	}
	return context.WithValue(ctx, assistantLogContextKey{}, c)
}

func assistantLogContextFrom(ctx context.Context) (assistantLogContext, bool) {
	c, ok := ctx.Value(assistantLogContextKey{}).(assistantLogContext)
	return c, ok
}

// telemetryRuntimeBackend wraps a concrete RuntimeBackend to emit one
// telemetry log per operation. Correlation ids, assistant ids and event ids
// are pulled from context so the interface stays unchanged and any backend
// (local firecracker, fly, future remotes) inherits the same instrumentation.
type telemetryRuntimeBackend struct {
	inner  RuntimeBackend
	logger *telemetry.Logger
}

func newTelemetryRuntimeBackend(inner RuntimeBackend, logger *telemetry.Logger) RuntimeBackend {
	return &telemetryRuntimeBackend{inner: inner, logger: logger}
}

func (t *telemetryRuntimeBackend) Backend() string {
	return t.inner.Backend()
}

func (t *telemetryRuntimeBackend) SupportsBackend(backend string) bool {
	return t.inner.SupportsBackend(backend)
}

func (t *telemetryRuntimeBackend) Ensure(ctx context.Context, runtime assistantRuntimeRecord) (RuntimeBackendEnsureResult, error) {
	result, err := t.inner.Ensure(ctx, runtime)
	if err != nil {
		t.emit(ctx, runtime, "runtime_ensure", "runtime ensure failed", "ERROR", err)
		return result, fmt.Errorf("runtime ensure: %w", err)
	}
	if result.ColdStart {
		t.emit(ctx, runtime, "runtime_ensure", "runtime cold-started", "INFO", nil)
	} else {
		t.emit(ctx, runtime, "runtime_ensure", "runtime reused (warm)", "INFO", nil)
	}
	return result, nil
}

func (t *telemetryRuntimeBackend) Configure(ctx context.Context, runtime assistantRuntimeRecord, config runtimeStartupConfig) error {
	if err := t.inner.Configure(ctx, runtime, config); err != nil {
		t.emit(ctx, runtime, "runtime_configure", "runtime configure failed", "ERROR", err)
		return fmt.Errorf("runtime configure: %w", err)
	}
	t.emit(ctx, runtime, "runtime_configure", "runtime configured", "INFO", nil)
	return nil
}

func (t *telemetryRuntimeBackend) RunTurn(
	ctx context.Context,
	runtime assistantRuntimeRecord,
	idempotencyKey string,
	authToken string,
	prompt string,
) error {
	t.emit(ctx, runtime, "runtime_turn", "runtime turn dispatched", "INFO", nil)
	if err := t.inner.RunTurn(ctx, runtime, idempotencyKey, authToken, prompt); err != nil {
		t.emit(ctx, runtime, "runtime_turn", "runtime turn errored", "ERROR", err)
		return fmt.Errorf("runtime run turn: %w", err)
	}
	t.emit(ctx, runtime, "runtime_turn", "runtime turn ok", "INFO", nil)
	return nil
}

func (t *telemetryRuntimeBackend) ServerURL(ctx context.Context, runtime assistantRuntimeRecord, raw *url.URL) (*url.URL, error) {
	u, err := t.inner.ServerURL(ctx, runtime, raw)
	if err != nil {
		return nil, fmt.Errorf("runtime server url: %w", err)
	}
	return u, nil
}

func (t *telemetryRuntimeBackend) Status(ctx context.Context, runtime assistantRuntimeRecord) (RuntimeBackendStatus, error) {
	status, err := t.inner.Status(ctx, runtime)
	if err != nil {
		// Status is a probe; failures during expire are an expected race
		// (runner already gone), not a fatal — keep the signal but at WARN.
		t.emit(ctx, runtime, "runtime_status", "runtime status failed", "WARN", err)
		return status, fmt.Errorf("runtime status: %w", err)
	}
	return status, nil
}

func (t *telemetryRuntimeBackend) Stop(ctx context.Context, runtime assistantRuntimeRecord) error {
	err := t.inner.Stop(ctx, runtime)
	if err != nil {
		t.emit(ctx, runtime, "runtime_stop", "runtime stop failed", "ERROR", err)
		return fmt.Errorf("runtime stop: %w", err)
	}
	t.emit(ctx, runtime, "runtime_stop", "runtime stopped", "INFO", nil)
	return nil
}

func (t *telemetryRuntimeBackend) Reap(ctx context.Context, runtime assistantRuntimeRecord) error {
	err := t.inner.Reap(ctx, runtime)
	if err != nil {
		t.emit(ctx, runtime, "runtime_reap", "runtime reap failed", "ERROR", err)
		return fmt.Errorf("runtime reap: %w", err)
	}
	t.emit(ctx, runtime, "runtime_reap", "runtime reaped", "INFO", nil)
	return nil
}

func (t *telemetryRuntimeBackend) emit(
	ctx context.Context,
	runtime assistantRuntimeRecord,
	phase string,
	body string,
	severity string,
	err error,
) {
	lc, _ := assistantLogContextFrom(ctx)

	attrs := map[attr.Key]any{
		attr.EventSourceKey:             string(telemetry.EventSourceAssistant),
		attr.LogBodyKey:                 body,
		attr.LogSeverityKey:             severity,
		attr.AssistantPhaseKey:          phase,
		attr.AssistantThreadIDKey:       runtime.AssistantThreadID.String(),
		attr.AssistantIDKey:             runtime.AssistantID.String(),
		attr.AssistantRuntimeIDKey:      runtime.ID.String(),
		attr.AssistantRuntimeBackendKey: runtime.Backend,
	}
	if err != nil {
		attrs[attr.ErrorMessageKey] = err.Error()
	}
	if lc.CorrelationID != "" {
		attrs[attr.TriggerCorrelationIDKey] = lc.CorrelationID
	}
	if lc.EventID != "" {
		attrs[attr.AssistantEventIDKey] = lc.EventID
	}
	if lc.TriggerEventID != "" {
		attrs[attr.TriggerEventIDKey] = lc.TriggerEventID
	}
	if lc.TriggerInstanceID != "" {
		attrs[attr.TriggerInstanceIDKey] = lc.TriggerInstanceID
	}
	if lc.Attempt > 0 {
		attrs[attr.AssistantAttemptKey] = int64(lc.Attempt)
	}

	name := "assistant:" + lc.AssistantName
	if lc.AssistantName == "" {
		name = "assistant"
	}

	t.logger.Log(ctx, telemetry.LogParams{
		Timestamp: time.Now().UTC(),
		ToolInfo: telemetry.ToolInfo{
			ID:             runtime.AssistantID.String(),
			URN:            "urn:uuid:" + runtime.AssistantID.String(),
			Name:           name,
			ProjectID:      runtime.ProjectID.String(),
			DeploymentID:   "",
			FunctionID:     nil,
			OrganizationID: lc.OrganizationID,
		},
		Attributes: attrs,
	})
}
