package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deviceidentity"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
)

// CrashReportRate bounds crash report batches per device. The agent sends at
// start, when a crash is queued and every 15 minutes, folding repeats, so a
// device stays far below it; it stops a leaked org key from flooding logs.
var CrashReportRate = ratelimit.PerMinute(10)

// WithCrashReportLimiter sets the per-device limit on reportCrash. Nil leaves
// it unlimited.
func (s *Service) WithCrashReportLimiter(limiter *ratelimit.Limiter) *Service {
	s.crashReportLimiter = limiter
	return s
}

// WithMeterProvider records crash report counts. Without it reports are only
// logged.
func (s *Service) WithMeterProvider(provider metric.MeterProvider) *Service {
	counter, err := provider.Meter("github.com/speakeasy-api/gram/server/internal/agent").Int64Counter(
		"device_agent.crash_reports",
		metric.WithDescription("Device agent crash reports by kind, component, build and platform."),
		metric.WithUnit("{report}"),
	)
	if err != nil {
		s.logger.ErrorContext(context.Background(), "create crash report counter", attr.SlogError(err))
		return s
	}
	s.crashReports = counter
	return s
}

// maxCrashReportBytes bounds a reportCrash body before Goa decodes it. Ten
// reports at their declared caps fit in well under 1 MiB.
const maxCrashReportBytes = 1 << 20

// limitCrashReportBody caps the request body, so the decoder fails on an
// oversized batch instead of buffering it.
func limitCrashReportBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxCrashReportBytes)
		next.ServeHTTP(w, r)
	})
}

// Closed sets from device-agent ADR-0030. Anything else is recorded as
// crashUnknown: a newer agent may send a value this server does not know, and
// every recorded value becomes a metric tag, so none may be caller-chosen.
var (
	crashKinds      = []string{"fatal", "recovered", "rollback"}
	crashComponents = []string{"ipc", "policy_scheduler", "update_scheduler", "aiscan", "sessions", "ui", "runtime"}
	crashBinaries   = []string{"speakeasyd", "speakeasy-ui"}
	crashChannels   = []string{"stable", "internal"}
	crashOSes       = []string{"darwin", "linux", "windows"}
	crashArches     = []string{"amd64", "arm64"}
)

const crashUnknown = "unknown"

// crashVersionPattern admits release versions as metric tags; dev builds and
// anything else are tagged unknown, though the log keeps what was sent.
var crashVersionPattern = regexp.MustCompile(`^v?\d{1,4}\.\d{1,4}\.\d{1,4}(-[0-9A-Za-z.]{1,32})?$`)

// agentModulePrefix marks the agent's own frames, which are what a crash
// signature is made of.
const agentModulePrefix = "github.com/speakeasy-api/device-agent/"

// signatureFrames is how many of the innermost agent frames identify a crash.
const signatureFrames = 5

// Second-layer scrubbing. The agent already sends only runtime error text and
// scrubs it; these catch an older or misbehaving agent.
var (
	crashEmailPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-]+(?:@|%40)[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	crashTokenPattern = regexp.MustCompile(`gram_(?:live|test)_[A-Za-z0-9]{16,}`)
	crashHomePattern  = regexp.MustCompile(`(?i)(/Users/|/home/|[A-Z]:\\Users\\)[^/\\\s"']+`)
)

// ReportCrash forwards device agent crash reports to monitoring as one log
// line each plus a counter. Nothing is stored. The reporting identity mirrors
// ReportAIScan.
func (s *Service) ReportCrash(ctx context.Context, payload *gen.ReportCrashPayload) (*gen.ReportCrashResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	email, err := pollingEmail(authCtx, conv.PtrValOr(payload.Email, ""))
	if err != nil {
		return nil, err
	}

	serial := deviceidentity.NormalizeSerial(payload.SerialNumber)
	if err := s.allowCrashReport(ctx, authCtx.ActiveOrganizationID, serial, email); err != nil {
		return nil, err
	}
	environment := deviceidentity.NormalizeEnvironment(payload.Environment)

	for _, report := range payload.Reports {
		if report == nil || report.Build == nil {
			return nil, oops.E(oops.CodeBadRequest, nil, "reports must not contain null entries")
		}
	}
	for _, report := range payload.Reports {
		s.recordCrash(ctx, authCtx.ActiveOrganizationID, serial, environment, report)
	}
	return &gen.ReportCrashResult{Accepted: len(payload.Reports)}, nil
}

// allowCrashReport charges one batch to the device's budget, keyed like
// reportState's. A limiter store outage fails open: it isn't a throttle.
func (s *Service) allowCrashReport(ctx context.Context, organizationID, serial, email string) error {
	if s.crashReportLimiter == nil {
		return nil
	}
	key := organizationID + ":serial:" + serial
	if serial == "" {
		key = organizationID + ":email:" + email
	}
	res, err := s.crashReportLimiter.Allow(ctx, key)
	switch {
	case err != nil:
		s.logger.WarnContext(ctx, "reportCrash rate limit unavailable", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
		return nil
	case !res.Allowed:
		return oops.E(oops.CodeRateLimitExceeded, nil, "too many crash reports from this device; retry shortly")
	}
	return nil
}

// recordCrash logs one report and counts it. The email is deliberately left
// out of both: the organization and serial are enough to find the device.
func (s *Service) recordCrash(ctx context.Context, organizationID, serial, environment string, report *gen.AgentCrashReport) {
	kind := closedValue(report.Kind, crashKinds)
	component := closedValue(report.Component, crashComponents)
	build := report.Build
	version := strings.TrimSpace(build.Version)
	versionTag := crashUnknown
	if crashVersionPattern.MatchString(version) {
		versionTag = strings.TrimPrefix(version, "v")
	}
	binary := closedValue(build.Binary, crashBinaries)
	channel := closedValue(conv.PtrValOr(build.Channel, ""), crashChannels)
	goos := closedValue(conv.PtrValOr(build.Os, ""), crashOSes)
	arch := closedValue(conv.PtrValOr(build.Arch, ""), crashArches)

	message := scrubCrashText(conv.PtrValOr(report.Message, ""))
	panicType := scrubCrashText(conv.PtrValOr(report.PanicType, ""))
	errorMessage := message
	if errorMessage == "" {
		errorMessage = panicType
	}
	signature := crashSignature(report.Stack)

	attrs := []any{
		attr.SlogOrganizationID(organizationID),
		attr.SlogAgentDeviceEnvironment(environment),
		attr.SlogErrorKind("device_agent." + kind),
		attr.SlogErrorMessage(errorMessage),
		attr.SlogErrorStack(formatCrashStack(report.Stack)),
		attr.SlogAgentCrashID(report.ID),
		attr.SlogAgentCrashKind(kind),
		attr.SlogAgentCrashComponent(component),
		attr.SlogAgentCrashSignature(signature),
		attr.SlogAgentCrashOccurredAt(report.OccurredAt),
		attr.SlogAgentCrashCount(report.Count),
		attr.SlogAgentCrashSchemaVersion(report.SchemaVersion),
		attr.SlogAgentBuildBinary(binary),
		attr.SlogAgentBuildVersion(scrubCrashText(version)),
		attr.SlogAgentBuildChannel(channel),
		attr.SlogAgentBuildOS(goos),
		attr.SlogAgentBuildArch(arch),
	}
	if serial != "" {
		attrs = append(attrs, attr.SlogAgentDeviceSerial(serial))
	}
	if report.OtherGoroutines != nil {
		attrs = append(attrs, attr.SlogAgentCrashOtherGoroutines(*report.OtherGoroutines))
	}
	if rb := report.Rollback; rb != nil {
		attrs = append(attrs,
			attr.SlogAgentCrashRollbackFrom(scrubCrashText(conv.PtrValOr(rb.From, ""))),
			attr.SlogAgentCrashRollbackTo(scrubCrashText(conv.PtrValOr(rb.To, ""))),
		)
	}
	// Error level so Datadog Error Tracking groups these by stack.
	s.logger.ErrorContext(ctx, "device agent crash report", attrs...)

	if s.crashReports != nil {
		s.crashReports.Add(ctx, 1, metric.WithAttributes(
			attribute.String("kind", kind),
			attribute.String("component", component),
			attribute.String("binary", binary),
			attribute.String("version", versionTag),
			attribute.String("channel", channel),
			attribute.String("os", goos),
		))
	}
}

func closedValue(v string, allowed []string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if slices.Contains(allowed, v) {
		return v
	}
	return crashUnknown
}

func scrubCrashText(s string) string {
	s = crashEmailPattern.ReplaceAllString(s, "[redacted]")
	s = crashTokenPattern.ReplaceAllString(s, "[redacted]")
	return crashHomePattern.ReplaceAllString(s, "~")
}

// crashSignature identifies a crash across releases and devices: a hash of the
// innermost agent function names, without lines, which move between builds.
// A stack with no agent frames (a runtime-only fatal) hashes what it has.
func crashSignature(stack []*gen.AgentCrashFrame) string {
	var names []string
	for _, frame := range stack {
		if frame != nil && strings.HasPrefix(frame.Function, agentModulePrefix) {
			names = append(names, frame.Function)
		}
	}
	if len(names) == 0 {
		for _, frame := range stack {
			if frame != nil {
				names = append(names, frame.Function)
			}
		}
	}
	if len(names) == 0 {
		return crashUnknown
	}
	sum := sha256.Sum256([]byte(strings.Join(names[:min(len(names), signatureFrames)], "\n")))
	return hex.EncodeToString(sum[:8])
}

// formatCrashStack renders frames the way Go prints a traceback, which is the
// shape Datadog Error Tracking parses.
func formatCrashStack(stack []*gen.AgentCrashFrame) string {
	var b strings.Builder
	for _, frame := range stack {
		if frame == nil {
			continue
		}
		fmt.Fprintf(&b, "%s()\n\t%s:%d\n", scrubCrashText(frame.Function), scrubCrashText(conv.PtrValOr(frame.File, "")), conv.PtrValOr(frame.Line, 0))
	}
	return b.String()
}
