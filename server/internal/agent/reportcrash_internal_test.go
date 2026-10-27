package agent

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
)

func TestScrubCrashText(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"open /Users/jane/.claude/settings.json: denied":      "open ~/.claude/settings.json: denied",
		"stat /home/jane.doe/x":                               "stat ~/x",
		`read C:\Users\Jane\AppData\x`:                        `read ~\AppData\x`,
		"owner jane@example.com missing":                      "owner [redacted] missing",
		"url ?u=jane%40example.com":                           "url ?u=[redacted]",
		"key gram_live_abcdefghijklmnop0123":                  "key [redacted]",
		"runtime error: index out of range [5] with length 3": "runtime error: index out of range [5] with length 3",
	}
	for in, want := range cases {
		require.Equal(t, want, scrubCrashText(in), in)
	}
}

func TestCrashSignature(t *testing.T) {
	t.Parallel()

	frame := func(fn string, line int) *gen.AgentCrashFrame {
		return &gen.AgentCrashFrame{Function: fn, File: nil, Line: &line}
	}
	tick := agentModulePrefix + "core/policy.(*Scheduler).tick"
	run := agentModulePrefix + "core/policy.(*Scheduler).run"

	base := crashSignature([]*gen.AgentCrashFrame{frame("runtime.gopanic", 1), frame(tick, 204), frame(run, 90)})
	// Lines move between releases; the signature must not.
	require.Equal(t, base, crashSignature([]*gen.AgentCrashFrame{frame("runtime.gopanic", 7), frame(tick, 230), frame(run, 95)}))
	// Runtime frames above the agent's do not split one crash into two.
	require.Equal(t, base, crashSignature([]*gen.AgentCrashFrame{frame(tick, 204), frame(run, 90)}))
	require.NotEqual(t, base, crashSignature([]*gen.AgentCrashFrame{frame(run, 90), frame(tick, 204)}))

	runtimeOnly := crashSignature([]*gen.AgentCrashFrame{frame("runtime.throw", 1)})
	require.NotEqual(t, crashUnknown, runtimeOnly)
	require.Equal(t, crashUnknown, crashSignature(nil))
}

func TestRecordCrash_LogsScrubbedReportAndCountsClosedTags(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(t.Context())) })
	svc := new(Service)
	svc.logger = slog.New(slog.NewJSONHandler(&logs, nil))
	svc.WithMeterProvider(provider)

	message := "runtime error: hash of unhashable type []string"
	line := 204
	file := "github.com/speakeasy-api/device-agent/core/policy/scheduler.go"
	svc.recordCrash(t.Context(), "org-a", "c02abc", "endpoint", &gen.AgentCrashReport{
		SchemaVersion: 1,
		ID:            "r-1",
		Kind:          "Recovered",
		Component:     "a-component-from-the-future",
		OccurredAt:    "2026-10-27T12:00:00Z",
		Count:         3,
		Build: &gen.AgentCrashBuild{
			Binary:  "speakeasyd",
			Version: "0.42.0",
			Channel: new("stable"),
			Os:      new("darwin"),
			Arch:    new("arm64"),
		},
		Message:         &message,
		PanicType:       nil,
		Stack:           []*gen.AgentCrashFrame{{Function: agentModulePrefix + "core/policy.(*Scheduler).tick", File: &file, Line: &line}},
		OtherGoroutines: nil,
		Rollback:        nil,
	})

	var entry map[string]any
	require.NoError(t, json.Unmarshal(logs.Bytes(), &entry))
	require.Equal(t, "ERROR", entry["level"])
	require.Equal(t, "device_agent.recovered", entry["error.kind"])
	require.Equal(t, "runtime error: hash of unhashable type []string", entry["error.message"])
	require.Contains(t, entry["error.stack"], "core/policy.(*Scheduler).tick()\n\tgithub.com/speakeasy-api/device-agent/core/policy/scheduler.go:204")
	require.Equal(t, "unknown", entry["gram.agent.crash.component"])
	require.Equal(t, "c02abc", entry["gram.agent.device.serial"])
	require.NotContains(t, logs.String(), "jane")

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	require.Len(t, rm.ScopeMetrics, 1)
	sum, ok := rm.ScopeMetrics[0].Metrics[0].Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.Len(t, sum.DataPoints, 1)
	point := sum.DataPoints[0]
	require.Equal(t, int64(1), point.Value)
	want := attribute.NewSet(
		attribute.String("kind", "recovered"),
		attribute.String("component", "unknown"),
		attribute.String("binary", "speakeasyd"),
		attribute.String("channel", "stable"),
		attribute.String("os", "darwin"),
	)
	require.True(t, point.Attributes.Equals(&want), point.Attributes.Encoded(attribute.DefaultEncoder()))
}

func validReport() *gen.AgentCrashReport {
	file := "github.com/speakeasy-api/device-agent/core/policy/scheduler.go"
	line := 204
	return &gen.AgentCrashReport{
		SchemaVersion: 1,
		ID:            "0f3c9a2b4d5e6f70",
		Kind:          "recovered",
		Component:     "policy_scheduler",
		OccurredAt:    "2026-10-27T12:00:00Z",
		Count:         1,
		Build:         &gen.AgentCrashBuild{Binary: "speakeasyd", Version: "0.42.0", Channel: nil, Os: nil, Arch: nil},
		Message:       new("runtime error: index out of range [5] with length 3"),
		PanicType:     nil,
		Stack: []*gen.AgentCrashFrame{
			{Function: agentModulePrefix + "core/policy.(*Scheduler).tick.func1", File: &file, Line: &line},
			{Function: "github.com/example/dep.Map[...]", File: new("github.com/example/dep@v1.2.3/map.go"), Line: &line},
			{Function: "runtime.gopanic", File: new("runtime/panic.go"), Line: &line},
		},
		OtherGoroutines: nil,
		Rollback:        nil,
	}
}

func TestValidCrashReport(t *testing.T) {
	t.Parallel()

	require.Empty(t, validCrashReport(validReport()))

	rollback := validReport()
	rollback.Kind, rollback.Stack, rollback.Message = "rollback", nil, nil
	rollback.Rollback = &gen.AgentCrashRollback{From: new("0.42.0"), To: new("0.41.3")}
	require.Empty(t, validCrashReport(rollback))

	cases := map[string]func(r *gen.AgentCrashReport){
		"id":             func(r *gen.AgentCrashReport) { r.ID = "not an id" },
		"schema_version": func(r *gen.AgentCrashReport) { r.SchemaVersion = 2 },
		"build.version":  func(r *gen.AgentCrashReport) { r.Build.Version = "attacker-chosen-123" },
		"occurred_at":    func(r *gen.AgentCrashReport) { r.OccurredAt = "last tuesday" },
		"panic_type":     func(r *gen.AgentCrashReport) { r.PanicType = new("*fs.PathError\nhello") },
		"stack":          func(r *gen.AgentCrashReport) { r.Stack = nil },
		"stack.function": func(r *gen.AgentCrashReport) { r.Stack[0].Function = "my customer list" },
		"stack.file":     func(r *gen.AgentCrashReport) { r.Stack[0].File = new("/Users/jane/src/main.go") },
		"rollback":       func(r *gen.AgentCrashReport) { r.Rollback = &gen.AgentCrashRollback{From: new("x"), To: new("y")} },
	}
	for want, mutate := range cases {
		r := validReport()
		mutate(r)
		require.Equal(t, want, validCrashReport(r), want)
	}
	for _, file := range []string{"", "../etc/passwd.go", `C:\Users\jane\main.go`, "a//b.go", "notes.txt"} {
		r := validReport()
		r.Stack[0].File = new(file)
		require.Equal(t, "stack.file", validCrashReport(r), file)
	}
}

func TestCrashMessage_KeepsOnlyRuntimeMessages(t *testing.T) {
	t.Parallel()

	kept := []string{
		"runtime error: invalid memory address or nil pointer dereference",
		"runtime error: index out of range [5] with length 3",
		"runtime error: index out of range [-1]",
		"runtime error: slice bounds out of range [:5] with capacity 3",
		"runtime error: slice bounds out of range [5:3]",
		"runtime error: hash of unhashable type []int",
		"runtime error: makeslice: len out of range",
		"fatal error: concurrent map writes",
		"fatal error: all goroutines are asleep - deadlock!",
		"interface conversion: interface {} is string, not int",
		"interface conversion: interface {} is nil, not *fs.PathError",
		"interface conversion: *main.T is not io.Reader: missing method Read",
		"assignment to entry in nil map",
	}
	for _, m := range kept {
		require.Equal(t, m, crashMessage(m))
	}
	for _, m := range []string{
		"open /Users/jane/x: denied",
		"customer list attached",
		"runtime error: x\nsecret",
		"runtime error: index out of range [5] with length 3 and the customer is acme",
		"fatal error: Contact jane@example.com",
		"interface conversion: interface {} is string, not int; also my notes",
		"",
	} {
		require.Empty(t, crashMessage(m), m)
	}
}
