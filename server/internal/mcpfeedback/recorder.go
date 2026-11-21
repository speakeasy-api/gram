package mcpfeedback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpfeedback/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
)

const (
	// CallerLimitName names the bucket that bounds one caller on one server.
	CallerLimitName = "mcp-feedback-caller"

	// ServerLimitName names the bucket that bounds everything one server accepts.
	ServerLimitName = "mcp-feedback-server"

	// CallerReportsPerMinute lets an agent that hits several problems in one
	// task report each of them, while a looping agent is stopped quickly.
	CallerReportsPerMinute = 5

	// ServerReportsPerMinute bounds what one server stores. A public server's
	// callers are anonymous and can change session at will, so this is the
	// limit that holds against them.
	ServerReportsPerMinute = 60

	meterReports = "mcp.feedback.reports"

	// defaultSettingsFreshFor is how long a server's feedback settings are
	// served from memory before they are read again. Every tools/list and tools/call
	// on a server reads them, and a change an admin saves reaches agents
	// within this long.
	defaultSettingsFreshFor = 15 * time.Second

	// settingsUsableFor is how long settings read earlier keep being served
	// while the database cannot be reached, so a short outage does not take
	// the feedback tool away from agents already using it.
	settingsUsableFor = 5 * time.Minute

	// maxCachedSettings bounds how many servers' settings one replica holds.
	// An entry is a few hundred bytes, so this is about a megabyte.
	maxCachedSettings = 5_000
)

// reportOutcome is how one feedback tool call ended, as a metric dimension.
type reportOutcome string

const (
	outcomeAccepted    reportOutcome = "accepted"
	outcomeInvalid     reportOutcome = "invalid"
	outcomeRateLimited reportOutcome = "rate_limited"
	outcomeFailed      reportOutcome = "failed"
)

// ErrRateLimited reports that feedback was sent too often to be accepted now.
var ErrRateLimited = errors.New("mcp feedback rate limited")

// Redactor masks secrets and personal data in free text.
type Redactor interface {
	Text(ctx context.Context, text string) (string, bool, error)
}

// Limits bound how often feedback is accepted. A nil limiter is not enforced.
type Limits struct {
	// Caller bounds one caller on one server.
	Caller *ratelimit.Limiter

	// Server bounds everything one server accepts.
	Server *ratelimit.Limiter
}

// NewLimits returns the limits the gateway enforces, counted in store so they
// hold across replicas.
func NewLimits(store ratelimit.Store, meterProvider metric.MeterProvider) Limits {
	return Limits{
		Caller: ratelimit.New(store, CallerLimitName, ratelimit.PerMinute(CallerReportsPerMinute), ratelimit.WithMetrics(meterProvider)),
		Server: ratelimit.New(store, ServerLimitName, ratelimit.PerMinute(ServerReportsPerMinute), ratelimit.WithMetrics(meterProvider)),
	}
}

// Target is the MCP server a report was sent through, and who owns it.
type Target struct {
	// OrganizationID is the organization that owns the server.
	OrganizationID string

	// ProjectID is the server's project.
	ProjectID uuid.UUID

	// McpServerID is the server.
	McpServerID uuid.UUID
}

// Caller is who sent a report and from where, as far as the gateway knows.
// Every field is optional: a public server's callers are anonymous.
type Caller struct {
	// UserID is the caller's Speakeasy user id. Set it only for a member of
	// the server's organization.
	UserID string

	// UserEmail is that member's email. It is not stored: readers resolve it
	// from UserID.
	UserEmail string

	// ExternalUserID is the caller's subject in the identity provider the
	// server signs people in with.
	ExternalUserID string

	// ClientName is the MCP client's name, from its handshake.
	ClientName string

	// ClientVersion is the MCP client's version, from its handshake.
	ClientVersion string

	// LimitSubject is the most durable identity the gateway has for the
	// caller, whatever its organization: an authenticated principal or API
	// key, else the MCP session the call arrived on. It only picks the
	// caller's rate-limit bucket when no identity above names the caller, and
	// is never stored or sent on.
	LimitSubject string
}

// Receipt identifies a stored report.
type Receipt struct {
	// ID is the report's id.
	ID uuid.UUID `json:"id"`

	// Status is always "received". It is what the agent reads to know the
	// report was stored.
	Status string `json:"status"`
}

// CallResult is what an agent reads back from a feedback tool call.
type CallResult struct {
	// Text is the result's text content.
	Text string

	// Structured is the receipt as structured content. It is nil when the
	// report was not stored.
	Structured json.RawMessage

	// IsError reports that the report was not stored.
	IsError bool
}

// Recorder serves the feedback tool for the gateway: it knows which servers
// offer the tool and stores what agents send through it.
type Recorder struct {
	logger   *slog.Logger
	db       *pgxpool.Pool
	redactor Redactor
	limits   Limits
	reports  metric.Int64Counter
	now      func() time.Time

	// settingsFreshFor is how long settings are served from memory before
	// they are read again.
	settingsFreshFor time.Duration

	// settingsMu guards settings.
	settingsMu sync.Mutex

	// settings holds the feedback settings last read for each server.
	settings map[cachedSettingsKey]cachedSettings
}

// cachedSettingsKey names one server's cached settings.
type cachedSettingsKey struct {
	projectID   uuid.UUID
	mcpServerID uuid.UUID
}

// cachedSettings is one server's settings as last read.
type cachedSettings struct {
	// settings are the stored settings; the zero value when there are none.
	settings Settings

	// readAt is when they were read.
	readAt time.Time
}

// RecorderOption changes how a [Recorder] works.
type RecorderOption func(*Recorder)

// WithSettingsFreshFor sets how long a server's settings are served from
// memory before they are read again. Zero reads them on every request, which
// suits tests that change settings and read them straight back; settings
// read earlier are still served while the database cannot be reached.
func WithSettingsFreshFor(d time.Duration) RecorderOption {
	return func(r *Recorder) {
		r.settingsFreshFor = d
	}
}

// NewRecorder returns the recorder a gateway serves the feedback tool with.
func NewRecorder(logger *slog.Logger, meterProvider metric.MeterProvider, db *pgxpool.Pool, redactor Redactor, limits Limits, opts ...RecorderOption) *Recorder {
	logger = logger.With(attr.SlogComponent("mcp-feedback"))

	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/mcpfeedback")
	reports, err := meter.Int64Counter(
		meterReports,
		metric.WithDescription("Feedback tool calls, by how each one ended"),
		metric.WithUnit("{report}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric error", attr.SlogMetricName(meterReports), attr.SlogError(err))
	}

	recorder := &Recorder{
		logger:   logger,
		db:       db,
		redactor: redactor,
		limits:   limits,
		reports:  reports,
		now:      time.Now,

		settingsFreshFor: defaultSettingsFreshFor,
		settingsMu:       sync.Mutex{},
		settings:         map[cachedSettingsKey]cachedSettings{},
	}
	for _, opt := range opts {
		opt(recorder)
	}

	return recorder
}

// Tool returns the feedback tool a server lists. ok is false when the server
// does not offer one.
func (r *Recorder) Tool(ctx context.Context, projectID, mcpServerID uuid.UUID) (tool Tool, ok bool, err error) {
	settings, err := r.serverSettings(ctx, cachedSettingsKey{projectID: projectID, mcpServerID: mcpServerID})
	if err != nil {
		return Tool{Name: "", Title: "", Description: "", InputSchema: nil}, false, err
	}
	if !settings.Enabled {
		return Tool{Name: "", Title: "", Description: "", InputSchema: nil}, false, nil
	}

	return settings.Tool(), true, nil
}

// serverSettings returns a server's feedback settings, from memory while they
// are fresh. When they cannot be read, settings read within settingsUsableFor
// are served instead.
func (r *Recorder) serverSettings(ctx context.Context, key cachedSettingsKey) (Settings, error) {
	now := r.now()

	r.settingsMu.Lock()
	cached, found := r.settings[key]
	r.settingsMu.Unlock()
	if found && now.Sub(cached.readAt) < r.settingsFreshFor {
		return cached.settings, nil
	}

	settings := Settings{Enabled: false, ToolName: "", ToolDescription: ""}
	config, err := repo.New(r.db).GetFeedbackConfig(ctx, repo.GetFeedbackConfigParams{
		McpServerID: key.mcpServerID,
		ProjectID:   key.projectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		if found && now.Sub(cached.readAt) < settingsUsableFor {
			r.logger.WarnContext(ctx, "load feedback config; serving the settings read earlier", attr.SlogError(err), attr.SlogMcpServerID(key.mcpServerID.String()))
			return cached.settings, nil
		}
		return settings, fmt.Errorf("load feedback config: %w", err)
	default:
		settings = SettingsFromConfig(config)
	}

	r.settingsMu.Lock()
	if len(r.settings) >= maxCachedSettings {
		clear(r.settings)
	}
	r.settings[key] = cachedSettings{settings: settings, readAt: now}
	r.settingsMu.Unlock()

	return settings, nil
}

// Call answers one feedback tool call. It never fails the call at the
// protocol level: whatever went wrong, the agent gets a tool result it can
// read and act on.
func (r *Recorder) Call(ctx context.Context, target Target, caller Caller, arguments json.RawMessage) CallResult {
	report, err := ParseReport(arguments)
	if err != nil {
		r.count(ctx, outcomeInvalid)
		message := "it does not match the tool's input schema"
		if invalid, ok := errors.AsType[*InvalidReportError](err); ok {
			message = invalid.Message
		}
		return CallResult{Text: "Feedback was not recorded: " + message + ".", Structured: nil, IsError: true}
	}

	receipt, err := r.Submit(ctx, target, caller, report)
	switch {
	case errors.Is(err, ErrRateLimited):
		r.count(ctx, outcomeRateLimited)
		return CallResult{Text: "Feedback was sent too often just now. Wait a minute before sending more.", Structured: nil, IsError: true}
	case err != nil:
		r.count(ctx, outcomeFailed)
		r.logger.ErrorContext(ctx, "record mcp feedback",
			attr.SlogError(err),
			attr.SlogOrganizationID(target.OrganizationID),
			attr.SlogProjectID(target.ProjectID.String()),
			attr.SlogMcpServerID(target.McpServerID.String()),
		)
		return CallResult{Text: "Feedback could not be recorded right now. Try again later.", Structured: nil, IsError: true}
	}

	structured, err := json.Marshal(receipt)
	if err != nil {
		// The report is stored; only its receipt could not be encoded.
		structured = nil
	}

	r.count(ctx, outcomeAccepted)
	return CallResult{
		Text:       "Feedback received (reference " + receipt.ID.String() + "). No follow-up will reach this session.",
		Structured: structured,
		IsError:    false,
	}
}

// Submit stores one report.
// It returns ErrRateLimited when the caller or the server has sent too many.
func (r *Recorder) Submit(ctx context.Context, target Target, caller Caller, report Report) (Receipt, error) {
	var none Receipt

	if err := r.allow(ctx, target, caller); err != nil {
		return none, err
	}

	report, err := r.redact(ctx, report)
	if err != nil {
		return none, err
	}

	dbtx, err := r.db.Begin(ctx)
	if err != nil {
		return none, fmt.Errorf("begin feedback transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, err := repo.New(dbtx).InsertFeedback(ctx, repo.InsertFeedbackParams{
		OrganizationID: target.OrganizationID,
		ProjectID:      target.ProjectID,
		McpServerID:    target.McpServerID,
		Content:        report.Content,
		Severity:       conv.ToPGTextEmpty(string(report.Severity)),
		Workaround:     conv.ToPGTextEmpty(report.Workaround),
		Intention:      conv.ToPGTextEmpty(report.Intention),
		ToolName:       conv.ToPGTextEmpty(report.ToolName),
		Model:          conv.ToPGTextEmpty(report.Model),
		UserID:         conv.ToPGTextEmpty(caller.UserID),
		ExternalUserID: conv.ToPGTextEmpty(caller.ExternalUserID),
		ClientName:     conv.ToPGTextEmpty(caller.ClientName),
		ClientVersion:  conv.ToPGTextEmpty(caller.ClientVersion),
	})
	if err != nil {
		return none, fmt.Errorf("insert feedback: %w", err)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return none, fmt.Errorf("commit feedback: %w", err)
	}

	return Receipt{ID: row.ID, Status: "received"}, nil
}

// allow charges the caller's bucket and then the server's, so a throttled
// caller does not use up what the server's other callers may send. A limiter
// that cannot be reached lets the report through: losing feedback to a cache
// outage is worse than briefly accepting more of it.
func (r *Recorder) allow(ctx context.Context, target Target, caller Caller) error {
	server := target.McpServerID.String()

	checks := []struct {
		limiter *ratelimit.Limiter
		key     string
	}{
		{limiter: r.limits.Caller, key: callerLimitKey(server, caller)},
		{limiter: r.limits.Server, key: server},
	}
	for _, check := range checks {
		if check.limiter == nil || check.key == "" {
			continue
		}

		result, err := check.limiter.Allow(ctx, check.key)
		switch {
		case err != nil:
			r.logger.WarnContext(ctx, "mcp feedback rate limiter unavailable; allowing", attr.SlogError(err))
		case !result.Allowed:
			return ErrRateLimited
		}
	}

	return nil
}

// callerLimitKey names the caller's bucket on one server by the most durable
// identity the gateway has for it. A caller with none is bounded by the
// server's bucket alone.
func callerLimitKey(server string, caller Caller) string {
	switch {
	case caller.UserID != "":
		return server + ":user:" + caller.UserID
	case caller.ExternalUserID != "":
		return server + ":external:" + caller.ExternalUserID
	case caller.LimitSubject != "":
		return server + ":subject:" + caller.LimitSubject
	default:
		return ""
	}
}

// redact masks secrets and personal data in everything the agent wrote. A
// scan that cannot complete fails the report rather than storing and sending
// on text nobody checked. Masking can lengthen a field, so each one is cut
// back to its bound.
func (r *Recorder) redact(ctx context.Context, report Report) (Report, error) {
	fields := []struct {
		value     *string
		maxLength int
	}{
		{value: &report.Content, maxLength: MaxContentLength},
		{value: &report.Workaround, maxLength: MaxWorkaroundLength},
		{value: &report.Intention, maxLength: MaxIntentionLength},
		{value: &report.ToolName, maxLength: MaxReportedToolNameLength},
		{value: &report.Model, maxLength: MaxModelLength},
	}
	for _, field := range fields {
		if *field.value == "" {
			continue
		}

		safe, _, err := r.redactor.Text(ctx, *field.value)
		if err != nil {
			return report, fmt.Errorf("redact feedback: %w", err)
		}
		*field.value = conv.TruncateString(safe, field.maxLength)
	}

	return report, nil
}

func (r *Recorder) count(ctx context.Context, outcome reportOutcome) {
	if r.reports == nil {
		return
	}
	r.reports.Add(ctx, 1, metric.WithAttributes(attr.Outcome(outcome)))
}
