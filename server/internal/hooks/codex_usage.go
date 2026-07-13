package hooks

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

const (
	// codexServiceName is the OTEL resource service.name the Codex CLI reports.
	codexServiceName = "codex_cli_rs"
	// codexUsageMetricsURN types a Codex usage row, mirroring the
	// "claude-code:usage:metrics" / "cursor:usage:metrics" convention.
	codexUsageMetricsURN = "codex:usage:metrics"
	// Codex emits token usage on the SSE event that closes a turn.
	codexSSEEventName          = "codex.sse_event"
	codexResponseCompletedKind = "response.completed"
)

// codexUsageDataPoint is one token-bearing Codex response.completed event.
//
// Each token-bearing event becomes its own ClickHouse row, keyed to a
// conversation via ConversationID. Downstream queries sum across rows, so we do
// not aggregate per conversation here.
type codexUsageDataPoint struct {
	ConversationID  string
	Model           string
	UserEmail       string
	InputTokens     int64
	OutputTokens    int64
	CachedTokens    int64
	ReasoningTokens int64
	// ToolTokens is Codex's tool_token_count, stored verbatim for fidelity.
	// It equals the RAW (cache-inclusive) input + output — NOT this struct's
	// InputTokens, which is normalized to the uncached remainder — so it must
	// never feed a total downstream.
	ToolTokens    int64
	TimestampNano int64
}

// isCodexLogsPayload reports whether an OTLP logs payload originated from the
// Codex CLI, identified by its resource service.name. Claude Code reports a
// different service name and is handled by the session-seeding path instead.
func isCodexLogsPayload(payload *gen.LogsPayload) bool {
	if payload == nil {
		return false
	}
	for _, rl := range payload.ResourceLogs {
		if rl == nil {
			continue
		}
		if extractResourceAttribute(rl.Resource, "service.name") == codexServiceName {
			return true
		}
	}
	return false
}

// extractCodexUsage pulls token usage out of every token-bearing
// codex.sse_event/response.completed log record in the payload.
func extractCodexUsage(payload *gen.LogsPayload) []codexUsageDataPoint {
	if payload == nil {
		return nil
	}

	var points []codexUsageDataPoint
	for _, rl := range payload.ResourceLogs {
		if rl == nil {
			continue
		}
		for _, sl := range rl.ScopeLogs {
			if sl == nil {
				continue
			}
			for _, rec := range sl.LogRecords {
				if rec == nil {
					continue
				}
				if dp, ok := codexUsageFromRecord(rec); ok {
					points = append(points, dp)
				}
			}
		}
	}

	return points
}

// codexUsageFromRecord returns a usage data point if the log record is a
// token-bearing response.completed event. Records that are not SSE completions,
// or that carry no token counts, are skipped (ok=false).
func codexUsageFromRecord(rec *gen.OTELLogRecord) (codexUsageDataPoint, bool) {
	var none codexUsageDataPoint

	attrs := indexLogAttributes(rec.Attributes)

	if logAttrString(attrs, "event.name") != codexSSEEventName {
		return none, false
	}
	if logAttrString(attrs, "event.kind") != codexResponseCompletedKind {
		return none, false
	}

	// Only some response.completed events carry usage. Treat the presence of an
	// input or output count as the signal that this event reports tokens.
	input, hasInput := logAttrInt64(attrs, "input_token_count")
	output, hasOutput := logAttrInt64(attrs, "output_token_count")
	if !hasInput && !hasOutput {
		return none, false
	}

	cached, _ := logAttrInt64(attrs, "cached_token_count")
	reasoning, _ := logAttrInt64(attrs, "reasoning_token_count")
	toolTokens, _ := logAttrInt64(attrs, "tool_token_count")

	// Codex reports input_token_count INCLUSIVE of cached_token_count (OpenAI
	// usage semantics: cached tokens are a subset of input), while the
	// canonical gen_ai.usage.* shape is disjoint (Anthropic-style: input
	// excludes cache reads, which land in cache_read.input_tokens). Normalize
	// here so a codex row's input means the same thing as a Claude or Cursor
	// row's everywhere downstream — tokens-under-management's cache exclusion
	// depends on it. Malformed counts are clamped into 0 <= cached <= input
	// so bad client data can never INCREASE usage.
	if input < 0 {
		input = 0
	}
	if cached < 0 {
		cached = 0
	}
	if cached > input {
		cached = input
	}
	input -= cached

	return codexUsageDataPoint{
		ConversationID:  logAttrString(attrs, "conversation.id"),
		Model:           logAttrString(attrs, "model"),
		UserEmail:       strings.TrimSpace(logAttrString(attrs, "user.email")),
		InputTokens:     input,
		OutputTokens:    output,
		CachedTokens:    cached,
		ReasoningTokens: reasoning,
		ToolTokens:      toolTokens,
		TimestampNano:   codexRecordTimestamp(rec, attrs),
	}, true
}

// writeCodexUsageToClickHouse normalizes Codex usage into the canonical
// gen_ai.usage.* shape and writes one telemetry row per token-bearing event.
// The rows are indistinguishable from Claude/Cursor usage rows except for the
// gram.hook.source ("codex") and gram.resource.urn ("codex:usage:metrics"),
// so existing per-user and time-series queries pick them up unchanged.
func (s *Service) writeCodexUsageToClickHouse(ctx context.Context, payload *gen.LogsPayload, orgID, projectID string) {
	points := extractCodexUsage(payload)
	if len(points) == 0 {
		return
	}

	parsedProjectID, err := uuid.Parse(projectID)
	if err != nil {
		s.logger.ErrorContext(ctx, "invalid project ID for Codex usage", attr.SlogError(err))
		return
	}

	// Resolve each unique email to a user ID once so multiple events sharing an
	// email don't each trigger a DB round-trip.
	emailToUserID := make(map[string]string)
	for _, p := range points {
		email := conv.NormalizeEmail(p.UserEmail)
		if email == "" {
			continue
		}
		if _, seen := emailToUserID[email]; seen {
			continue
		}
		emailToUserID[email] = s.resolveUserByEmail(ctx, email, orgID)
	}

	for _, p := range points {
		attrs := map[attr.Key]any{
			attr.EventSourceKey:    string(telemetry.EventSourceHook),
			attr.LogBodyKey:        "Codex usage metrics",
			attr.ProjectIDKey:      projectID,
			attr.OrganizationIDKey: orgID,
			attr.ResourceURNKey:    codexUsageMetricsURN,
			attr.HookSourceKey:     "codex",
			attr.ProviderKey:       providerOpenAI,
		}

		// Only include non-zero values, matching the Claude usage writer.
		if p.InputTokens > 0 {
			attrs[attr.GenAIUsageInputTokensKey] = p.InputTokens
		}
		if p.OutputTokens > 0 {
			attrs[attr.GenAIUsageOutputTokensKey] = p.OutputTokens
		}
		if p.CachedTokens > 0 {
			attrs[attr.GenAIUsageCacheReadInputTokensKey] = p.CachedTokens
		}
		// The disjoint sum, matching Claude semantics (input + output + cache
		// read; codex reports no cache writes). Without it codex rows count 0
		// toward every total_tokens measure.
		if total := p.InputTokens + p.OutputTokens + p.CachedTokens; total > 0 {
			attrs[attr.GenAIUsageTotalTokensKey] = total
		}
		if p.ReasoningTokens > 0 {
			// Saved for completeness; not surfaced on the dashboard yet.
			attrs[attr.GenAIUsageReasoningTokensKey] = p.ReasoningTokens
		}
		if p.ToolTokens > 0 {
			// Stored raw for fidelity; intentionally not used downstream.
			attrs[attr.CodexUsageToolTokensKey] = p.ToolTokens
		}
		if p.Model != "" {
			attrs[attr.GenAIResponseModelKey] = p.Model
		}
		if p.ConversationID != "" {
			attrs[attr.GenAIConversationIDKey] = p.ConversationID
		}

		toolInfo := telemetry.ToolInfo{
			Name:           "codex",
			OrganizationID: orgID,
			ProjectID:      parsedProjectID.String(),
			ID:             "",
			URN:            codexUsageMetricsURN,
			DeploymentID:   "",
			FunctionID:     nil,
		}

		ts := time.Now()
		if p.TimestampNano > 0 {
			ts = time.Unix(0, p.TimestampNano)
		}

		userInfo := telemetry.UserInfoByEmail(p.UserEmail)
		if userID := emailToUserID[conv.NormalizeEmail(p.UserEmail)]; userID != "" {
			userInfo = telemetry.UserInfoByID(userID)
		}

		s.telemetryLogger.Log(ctx, telemetry.LogParams{
			Timestamp:  ts,
			ToolInfo:   toolInfo,
			UserInfo:   userInfo,
			Attributes: attrs,
		})
	}
}

// indexLogAttributes builds a key->value lookup over a log record's attributes.
func indexLogAttributes(attrs []*gen.OTELAttribute) map[string]*gen.OTELAttributeValue {
	m := make(map[string]*gen.OTELAttributeValue, len(attrs))
	for _, a := range attrs {
		if a == nil || a.Value == nil {
			continue
		}
		m[a.Key] = a.Value
	}
	return m
}

// logAttrString returns the string value of an attribute, or "" if absent.
func logAttrString(attrs map[string]*gen.OTELAttributeValue, key string) string {
	v := attrs[key]
	if v == nil || v.StringValue == nil {
		return ""
	}
	return *v.StringValue
}

// logAttrInt64 reads an integer attribute. Codex encodes token counts as OTLP
// stringValues ("92728"), but we also accept int and double encodings
// defensively. Returns ok=false when the attribute is absent or unparseable.
func logAttrInt64(attrs map[string]*gen.OTELAttributeValue, key string) (int64, bool) {
	v := attrs[key]
	if v == nil {
		return 0, false
	}
	if v.StringValue != nil {
		n, err := strconv.ParseInt(strings.TrimSpace(*v.StringValue), 10, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	if v.IntValue != nil {
		return parseLooseInt64(v.IntValue)
	}
	if v.DoubleValue != nil {
		f := *v.DoubleValue
		if f != float64(int64(f)) {
			return 0, false
		}
		return int64(f), true
	}
	return 0, false
}

// codexRecordTimestamp resolves the event time, preferring the record's own
// nanosecond fields and falling back to the RFC3339 event.timestamp attribute.
func codexRecordTimestamp(rec *gen.OTELLogRecord, attrs map[string]*gen.OTELAttributeValue) int64 {
	if rec.TimeUnixNano != nil {
		if n, err := strconv.ParseInt(*rec.TimeUnixNano, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	if rec.ObservedTimeUnixNano != nil {
		if n, err := strconv.ParseInt(*rec.ObservedTimeUnixNano, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	if ts := logAttrString(attrs, "event.timestamp"); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			return t.UnixNano()
		}
	}
	return 0
}
