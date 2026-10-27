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
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/deviceagenttoken"
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

// CrashReportCredentialRate bounds crash report batches per credential. The
// device serial is caller-supplied, so rotating it buys fresh device budgets;
// this caps what one key can send in total. An org install key is shared by a
// fleet, hence the headroom.
var CrashReportCredentialRate = ratelimit.PerMinute(600)

// WithCrashReportLimiter sets the per-device limit on reportCrash. Nil leaves
// it unlimited.
func (s *Service) WithCrashReportLimiter(limiter *ratelimit.Limiter) *Service {
	s.crashReportLimiter = limiter
	return s
}

// WithCrashReportCredentialLimiter sets the per-credential limit on
// reportCrash. Nil leaves it unlimited.
func (s *Service) WithCrashReportCredentialLimiter(limiter *ratelimit.Limiter) *Service {
	s.crashReportCredentialLimiter = limiter
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

// crashVersionPattern is a release or dev build version. A report with any
// other version is rejected. Versions are never metric tags: a key holder
// could mint unbounded series. Per-version views come from the log.
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
// line each plus a counter. Nothing is stored. It accepts getPlugins'
// credentials, so a server or CI box reports as its agent.
//
// A report is logged only if it has the shape of a crash the agent produces
// (see validCrashReport). Any key holder can still forge a well-formed report,
// but what reaches the logs is limited to closed sets, Go symbols, relative
// file paths and the runtime's own messages: no free text.
func (s *Service) ReportCrash(ctx context.Context, payload *gen.ReportCrashPayload) (*gen.ReportCrashResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	// An agent key, or a workload acting for its agent, has no person to vouch
	// for; the auth step already required the device-sync grant.
	reporter := ""
	if agent, ok := deviceagenttoken.EffectiveAgent(ctx); ok {
		reporter = "agent:" + agent.String()
	} else {
		email, err := pollingEmail(authCtx, conv.PtrValOr(payload.Email, ""))
		if err != nil {
			return nil, err
		}
		reporter = "email:" + email
	}

	// The key that authenticated, or the workload or agent behind a token.
	credential := "key:" + authCtx.APIKeyID
	if authCtx.APIKeyID == "" {
		credential = reporter
	}
	if err := s.allowCrashReport(ctx, s.crashReportCredentialLimiter, authCtx.ActiveOrganizationID+":"+credential, "too many crash reports for this credential; retry shortly"); err != nil {
		return nil, err
	}
	serial := deviceidentity.NormalizeSerial(payload.SerialNumber)
	deviceKey := authCtx.ActiveOrganizationID + ":serial:" + serial
	if serial == "" {
		deviceKey = authCtx.ActiveOrganizationID + ":" + reporter
	}
	if err := s.allowCrashReport(ctx, s.crashReportLimiter, deviceKey, "too many crash reports from this device; retry shortly"); err != nil {
		return nil, err
	}
	environment := deviceidentity.NormalizeEnvironment(payload.Environment)

	for _, report := range payload.Reports {
		if report == nil || report.Build == nil {
			return nil, oops.E(oops.CodeBadRequest, nil, "reports must not contain null entries")
		}
	}
	accepted := 0
	for _, report := range payload.Reports {
		if reason := validCrashReport(report); reason != "" {
			s.rejectCrash(ctx, authCtx.ActiveOrganizationID, serial, reason)
			continue
		}
		s.recordCrash(ctx, authCtx.ActiveOrganizationID, serial, environment, report)
		accepted++
	}
	return &gen.ReportCrashResult{Accepted: accepted}, nil
}

// allowCrashReport charges one batch to key's budget on limiter: a device's
// (keyed like reportState's, or by reporter when there is no serial) or a
// credential's. A limiter store outage fails open: it isn't a throttle.
func (s *Service) allowCrashReport(ctx context.Context, limiter *ratelimit.Limiter, key, refusal string) error {
	if limiter == nil {
		return nil
	}
	res, err := limiter.Allow(ctx, key)
	switch {
	case err != nil:
		s.logger.WarnContext(ctx, "reportCrash rate limit unavailable", attr.SlogError(err))
		return nil
	case !res.Allowed:
		return oops.E(oops.CodeRateLimitExceeded, nil, "%s", refusal)
	}
	return nil
}

// Shapes a device agent crash report's fields take (ADR-0030). They are
// checked after Goa's length caps.
var (
	// crashIDPattern is the agent's random hex id.
	crashIDPattern = regexp.MustCompile(`^[0-9a-f]{16,64}$`)
	// crashFunctionPattern is a Go symbol: package path, receiver, closures
	// and generic brackets, never whitespace.
	crashFunctionPattern = regexp.MustCompile(`^[A-Za-z0-9_.\-/()*\[\]{},~@]+$`)
	// crashFilePattern is a module-relative or stdlib-relative path, which is
	// what a -trimpath build records; the agent reduces anything absolute to
	// its base name. Frames in the runtime's assembly (runtime.goexit) are in
	// .s files.
	crashFilePattern = regexp.MustCompile(`^[A-Za-z0-9_.\-+@~]+(/[A-Za-z0-9_.\-+@~]+)*\.(go|s)$`)
	// crashTypePattern is a Go type as reflect prints it.
	crashTypePattern = regexp.MustCompile(`^[A-Za-z0-9_.*/\[\]{}(),\- ]+$`)
)

// crashGoType is a Go type as the runtime prints it in its messages.
const crashGoType = `[A-Za-z0-9_.*/\[\]{}(), ]{1,200}`

// crashMessagePatterns are the Go runtime's own panic and fatal messages,
// whole: each is built from fixed words, numbers and type names. The agent
// sends only those (ADR-0030); any other message is dropped here while the
// rest of the report is kept.
var crashMessagePatterns = []*regexp.Regexp{
	regexp.MustCompile(`^runtime error: index out of range \[-?\d{1,20}\]( with length \d{1,20})?$`),
	regexp.MustCompile(`^runtime error: slice bounds out of range \[[\d:-]{1,64}\]( with (length|capacity) \d{1,20})?$`),
	regexp.MustCompile(`^runtime error: cannot convert slice with length \d{1,20} to array or pointer to array with length \d{1,20}$`),
	regexp.MustCompile(`^runtime error: (invalid memory address or nil pointer dereference|integer divide by zero|integer overflow|negative shift amount|(makeslice|unsafe\.Slice|unsafe\.String): (len|cap|ptr) out of range|makechan: size out of range|growslice: len out of range)$`),
	regexp.MustCompile(`^runtime error: (hash of unhashable|comparing uncomparable) type ` + crashGoType + `$`),
	regexp.MustCompile(`^interface conversion: ` + crashGoType + ` is (nil|` + crashGoType + `), not ` + crashGoType + `$`),
	regexp.MustCompile(`^interface conversion: ` + crashGoType + ` is not ` + crashGoType + `: missing method [A-Za-z_][A-Za-z0-9_]{0,100}$`),
	// The runtime's fatal errors are constant lowercase strings.
	regexp.MustCompile(`^fatal error: [a-z][a-z0-9 :;,'()!.-]{0,120}$`),
	regexp.MustCompile(`^(assignment to entry in nil map|close of nil channel|close of closed channel|send on closed channel)$`),
}

// validCrashReport returns why a report is not shaped like one the device
// agent produces, or "" when it is. Closed-set fields are not checked here:
// unknown values are recorded as crashUnknown.
func validCrashReport(r *gen.AgentCrashReport) string {
	switch {
	case !crashIDPattern.MatchString(r.ID):
		return "id"
	case r.SchemaVersion != 1:
		return "schema_version"
	case !validCrashTime(r.OccurredAt):
		return "occurred_at"
	case !crashVersionPattern.MatchString(r.Build.Version):
		return "build.version"
	case r.PanicType != nil && *r.PanicType != "" && !crashTypePattern.MatchString(*r.PanicType):
		return "panic_type"
	}
	if r.Kind == "rollback" {
		rb := r.Rollback
		if rb == nil || !crashVersionPattern.MatchString(conv.PtrValOr(rb.From, "")) || !crashVersionPattern.MatchString(conv.PtrValOr(rb.To, "")) {
			return "rollback"
		}
		return ""
	}
	if r.Rollback != nil {
		return "rollback"
	}
	// Fatal crashes and recovered panics always have the crashing goroutine.
	if len(r.Stack) == 0 {
		return "stack"
	}
	for _, f := range r.Stack {
		if f == nil || !crashFunctionPattern.MatchString(f.Function) {
			return "stack.function"
		}
		// Every frame in a Go traceback names its file.
		file := conv.PtrValOr(f.File, "")
		if !crashFilePattern.MatchString(file) || slices.ContainsFunc(strings.Split(file, "/"), isDotSegment) {
			return "stack.file"
		}
	}
	return ""
}

// validCrashTime reports whether s is an RFC 3339 time. Goa's DateTime format
// already enforces this on decode; checking here keeps the log field safe if
// the design changes.
func validCrashTime(s string) bool {
	_, err := time.Parse(time.RFC3339, s)
	return err == nil
}

func isDotSegment(s string) bool { return s == "." || s == ".." }

// crashMessage returns msg if it is a Go runtime message, else "".
func crashMessage(msg string) string {
	if slices.ContainsFunc(crashMessagePatterns, func(p *regexp.Regexp) bool { return p.MatchString(msg) }) {
		return msg
	}
	return ""
}

// rejectCrash records that a report was dropped and why, without any of its
// content.
func (s *Service) rejectCrash(ctx context.Context, organizationID, serial, reason string) {
	attrs := []any{attr.SlogOrganizationID(organizationID), attr.SlogReason(reason)}
	if serial != "" {
		attrs = append(attrs, attr.SlogAgentDeviceSerial(serial))
	}
	s.logger.WarnContext(ctx, "device agent crash report rejected", attrs...)
	if s.crashReports != nil {
		s.crashReports.Add(ctx, 1, metric.WithAttributes(
			attribute.String("kind", "rejected"),
			attribute.String("reason", reason),
		))
	}
}

// recordCrash logs one report and counts it. The email is deliberately left
// out of both: the organization and serial are enough to find the device.
func (s *Service) recordCrash(ctx context.Context, organizationID, serial, environment string, report *gen.AgentCrashReport) {
	kind := closedValue(report.Kind, crashKinds)
	component := closedValue(report.Component, crashComponents)
	build := report.Build
	version := strings.TrimPrefix(build.Version, "v")
	binary := closedValue(build.Binary, crashBinaries)
	channel := closedValue(conv.PtrValOr(build.Channel, ""), crashChannels)
	goos := closedValue(conv.PtrValOr(build.Os, ""), crashOSes)
	arch := closedValue(conv.PtrValOr(build.Arch, ""), crashArches)

	message := scrubCrashText(crashMessage(conv.PtrValOr(report.Message, "")))
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
		attr.SlogAgentBuildVersion(version),
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
