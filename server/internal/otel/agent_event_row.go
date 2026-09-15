package otel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/otel/chrepo"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"google.golang.org/protobuf/proto"
)

// The row builders below project a normalized OTLP record into one
// agent_events row. They are pure: the only input besides the record is the
// time the consumer observed it, so the same record always yields the same
// row and golden tests can pin the projection.
//
// The dialects that know each producer's vocabulary take the inbound record
// types, while a consumer on the normalized topics holds the outbound ones.
// The two are wire-compatible by construction (the inbound protos are kept as
// carbon copies), so the builders round-trip the record back to its inbound
// shape, exactly the way the transform handler goes the other direction, and
// hand that to the dialect. That keeps every dialect method on one type
// family and costs one marshal per record.

// agentEventRowFromLog maps a normalized log record to its agent_events row.
// A non-empty skipReason marks the record unprocessable; redelivery cannot
// fix such a record, so the caller drops it instead of failing the batch.
func agentEventRowFromLog(record *otelv1.LogRecord, observedAtUnixNano int64) (chrepo.AgentEventRow, string) {
	var zero chrepo.AgentEventRow

	if record == nil {
		return zero, "nil_record"
	}
	organizationID := record.GetProvenance().GetOrganizationId()
	if organizationID == "" {
		return zero, "missing_organization_id"
	}

	// Observation time is when Gram received the record. The ingest edge
	// stamps it on log records; the consumer's clock stands in only when it
	// did not. Event time falls back to observation time, and a record with
	// neither has no usable time at all.
	observedNano := eventUnixNano(record.GetObservedTimeUnixNano())
	if observedNano == 0 {
		observedNano = observedAtUnixNano
	}
	occurredNano := eventUnixNano(record.GetTimeUnixNano())
	if occurredNano == 0 {
		occurredNano = observedNano
	}
	if occurredNano == 0 {
		return zero, "missing_timestamp"
	}

	recordID := record.GetRecordId()
	if recordID == "" {
		minted, err := mintedRecordID(record)
		if err != nil {
			return zero, "missing_record_id"
		}
		recordID = minted
	}

	inbound, err := inboundLogFromRecord(record)
	if err != nil {
		return zero, "convert_inbound"
	}

	attributes, err := logEventAttributesJSON(record.GetAttributes())
	if err != nil {
		return zero, "encode_log_attributes"
	}
	resourceAttributes, err := logEventAttributesJSON(record.GetResource().GetAttributes())
	if err != nil {
		return zero, "encode_resource_attributes"
	}
	scopeAttributes, err := logEventAttributesJSON(record.GetScope().GetAttributes())
	if err != nil {
		return zero, "encode_scope_attributes"
	}

	var enrichment rowEnrichment
	for _, kv := range record.GetAttributes() {
		enrichment.absorb(kv.GetKey(), logEventAnyValue(kv.GetValue()))
	}

	d := dialect.ForLog(inbound)
	row := agentEventRow(logAnswers{d: d, record: inbound}, enrichment)
	row.OrganizationID = organizationID
	row.ProjectID = record.GetProvenance().GetProjectId()
	row.OccurredAtUnixNano = occurredNano
	row.ObservedAtUnixNano = observedNano
	row.RecordID = recordID
	row.EventID = subjectOrRecordID(row.EventID, recordID)
	row.Source = canonicalEventSource(logEventServiceName(record))
	row.InputContent = contentJSON(d.InputContent(inbound))
	row.OutputContent = contentJSON(d.OutputContent(inbound))
	row.Attributes = attributes
	row.ResourceAttributes = resourceAttributes
	row.ScopeAttributes = scopeAttributes

	// Text is the record in words. When the dialect found none and could not
	// classify the record, a string body is the closest thing the producer
	// offered, unless the body is just the event name again.
	if row.Text == "" && row.EventType == dialect.EventTypeUnclassified {
		if body := record.GetBody(); body.HasStringValue() && !dialect.BodyRepeatsEventName(body.GetStringValue(), row.RawEventName) {
			row.Text = body.GetStringValue()
		}
	}
	return row, ""
}

// agentEventRowFromSpan maps a normalized span to its agent_events row. A
// span's delivery identity is its own trace and span id, and its raw name is
// the span name.
func agentEventRowFromSpan(span *otelv1.Span, observedAtUnixNano int64) (chrepo.AgentEventRow, string) {
	var zero chrepo.AgentEventRow

	if span == nil {
		return zero, "nil_span"
	}
	organizationID := span.GetProvenance().GetOrganizationId()
	if organizationID == "" {
		return zero, "missing_organization_id"
	}
	traceID := hexEventID(span.GetTraceId())
	spanID := hexEventID(span.GetSpanId())
	if traceID == "" || spanID == "" {
		return zero, "missing_span_identity"
	}

	startNano := eventUnixNano(span.GetStartTimeUnixNano())
	endNano := eventUnixNano(span.GetEndTimeUnixNano())
	if startNano == 0 {
		startNano = endNano
	}
	if startNano == 0 {
		return zero, "missing_timestamp"
	}
	// Spans carry no observation time of their own; the consumer's clock is
	// the closest honest reading of when Gram received it.
	observedNano := observedAtUnixNano
	if observedNano == 0 {
		return zero, "missing_observed_time"
	}

	inbound, err := inboundSpanFromSpan(span)
	if err != nil {
		return zero, "convert_inbound"
	}

	attributes, err := spanEventAttributesJSON(span.GetAttributes())
	if err != nil {
		return zero, "encode_span_attributes"
	}
	resourceAttributes, err := spanEventAttributesJSON(span.GetResource().GetAttributes())
	if err != nil {
		return zero, "encode_resource_attributes"
	}
	scopeAttributes, err := spanEventAttributesJSON(span.GetScope().GetAttributes())
	if err != nil {
		return zero, "encode_scope_attributes"
	}

	var enrichment rowEnrichment
	for _, kv := range span.GetAttributes() {
		enrichment.absorb(kv.GetKey(), spanEventAnyValue(kv.GetValue()))
	}

	d := dialect.ForSpan(inbound)
	recordID := traceID + ":" + spanID
	row := agentEventRow(spanAnswers{d: d, span: inbound}, enrichment)
	row.OrganizationID = organizationID
	row.ProjectID = span.GetProvenance().GetProjectId()
	row.OccurredAtUnixNano = startNano
	row.ObservedAtUnixNano = observedNano
	row.RecordID = recordID
	row.EventID = subjectOrRecordID(row.EventID, recordID)
	row.Source = canonicalEventSource(spanEventServiceName(span))
	row.InputContent = contentJSON(d.InputContent(inbound))
	row.OutputContent = contentJSON(d.OutputContent(inbound))
	row.Attributes = attributes
	row.ResourceAttributes = resourceAttributes
	row.ScopeAttributes = scopeAttributes
	return row, ""
}

// answers is what a dialect says about one record, one question at a time.
// A dialect answers (key, value, err), where the key names the attribute the
// answer was read from. The row keeps only values the producer stated: an
// empty key or a read error is absent, never a guess, so the adapters below
// reduce every answer to its value or the zero value.
type answers interface {
	SessionID() string
	ExternalUserEmail() string
	ExternalUserID() string
	Provider() string
	Surface() string
	EventName() string
	EventType() string
	SubjectID() string
	TurnID() string
	Model() string
	ToolName() string
	Outcome() string
	OutcomeMessage() string
	Text() string
	QuerySource() string
	SkillName() string
	AgentName() string
	MCPServerName() string
	MCPToolName() string
	ExternalOrgID() string
	DurationNano() int64
	InputTokens() int64
	OutputTokens() int64
	CacheReadTokens() int64
	CacheWriteTokens() int64
	CostUSD() float64
}

type logAnswers struct {
	d      dialect.LogDialect
	record *otelv1.InboundLogRecord
}

func (a logAnswers) SessionID() string         { return stated(a.d.SessionID(a.record)) }
func (a logAnswers) ExternalUserEmail() string { return stated(a.d.ExternalUserEmail(a.record)) }
func (a logAnswers) ExternalUserID() string    { return stated(a.d.ExternalUserID(a.record)) }
func (a logAnswers) Provider() string          { return stated(a.d.Provider(a.record)) }
func (a logAnswers) Surface() string           { return stated(a.d.Surface(a.record)) }
func (a logAnswers) EventName() string         { return stated(a.d.EventName(a.record)) }
func (a logAnswers) EventType() string         { return stated(a.d.EventType(a.record)) }
func (a logAnswers) SubjectID() string         { return stated(a.d.SubjectID(a.record)) }
func (a logAnswers) TurnID() string            { return stated(a.d.TurnID(a.record)) }
func (a logAnswers) Model() string             { return stated(a.d.Model(a.record)) }
func (a logAnswers) ToolName() string          { return stated(a.d.ToolName(a.record)) }
func (a logAnswers) Outcome() string           { return stated(a.d.Outcome(a.record)) }
func (a logAnswers) OutcomeMessage() string    { return stated(a.d.OutcomeMessage(a.record)) }
func (a logAnswers) Text() string              { return stated(a.d.Text(a.record)) }
func (a logAnswers) QuerySource() string       { return stated(a.d.QuerySource(a.record)) }
func (a logAnswers) SkillName() string         { return stated(a.d.SkillName(a.record)) }
func (a logAnswers) AgentName() string         { return stated(a.d.AgentName(a.record)) }
func (a logAnswers) MCPServerName() string     { return stated(a.d.MCPServerName(a.record)) }
func (a logAnswers) MCPToolName() string       { return stated(a.d.MCPToolName(a.record)) }
func (a logAnswers) ExternalOrgID() string     { return stated(a.d.ExternalOrgID(a.record)) }
func (a logAnswers) DurationNano() int64       { return stated(a.d.DurationNano(a.record)) }
func (a logAnswers) InputTokens() int64        { return stated(a.d.InputTokens(a.record)) }
func (a logAnswers) OutputTokens() int64       { return stated(a.d.OutputTokens(a.record)) }
func (a logAnswers) CacheReadTokens() int64    { return stated(a.d.CacheReadTokens(a.record)) }
func (a logAnswers) CacheWriteTokens() int64   { return stated(a.d.CacheWriteTokens(a.record)) }
func (a logAnswers) CostUSD() float64          { return stated(a.d.CostUSD(a.record)) }

type spanAnswers struct {
	d    dialect.SpanDialect
	span *otelv1.InboundSpan
}

func (a spanAnswers) SessionID() string         { return stated(a.d.SessionID(a.span)) }
func (a spanAnswers) ExternalUserEmail() string { return stated(a.d.ExternalUserEmail(a.span)) }
func (a spanAnswers) ExternalUserID() string    { return stated(a.d.ExternalUserID(a.span)) }
func (a spanAnswers) Provider() string          { return stated(a.d.Provider(a.span)) }
func (a spanAnswers) Surface() string           { return stated(a.d.Surface(a.span)) }
func (a spanAnswers) EventName() string         { return stated(a.d.EventName(a.span)) }
func (a spanAnswers) EventType() string         { return stated(a.d.EventType(a.span)) }
func (a spanAnswers) SubjectID() string         { return stated(a.d.SubjectID(a.span)) }
func (a spanAnswers) TurnID() string            { return stated(a.d.TurnID(a.span)) }
func (a spanAnswers) Model() string             { return stated(a.d.Model(a.span)) }
func (a spanAnswers) ToolName() string          { return stated(a.d.ToolName(a.span)) }
func (a spanAnswers) Outcome() string           { return stated(a.d.Outcome(a.span)) }
func (a spanAnswers) OutcomeMessage() string    { return stated(a.d.OutcomeMessage(a.span)) }
func (a spanAnswers) Text() string              { return stated(a.d.Text(a.span)) }
func (a spanAnswers) QuerySource() string       { return stated(a.d.QuerySource(a.span)) }
func (a spanAnswers) SkillName() string         { return stated(a.d.SkillName(a.span)) }
func (a spanAnswers) AgentName() string         { return stated(a.d.AgentName(a.span)) }
func (a spanAnswers) MCPServerName() string     { return stated(a.d.MCPServerName(a.span)) }
func (a spanAnswers) MCPToolName() string       { return stated(a.d.MCPToolName(a.span)) }
func (a spanAnswers) ExternalOrgID() string     { return stated(a.d.ExternalOrgID(a.span)) }
func (a spanAnswers) DurationNano() int64       { return stated(a.d.DurationNano(a.span)) }
func (a spanAnswers) InputTokens() int64        { return stated(a.d.InputTokens(a.span)) }
func (a spanAnswers) OutputTokens() int64       { return stated(a.d.OutputTokens(a.span)) }
func (a spanAnswers) CacheReadTokens() int64    { return stated(a.d.CacheReadTokens(a.span)) }
func (a spanAnswers) CacheWriteTokens() int64   { return stated(a.d.CacheWriteTokens(a.span)) }
func (a spanAnswers) CostUSD() float64          { return stated(a.d.CostUSD(a.span)) }

// stated keeps a dialect's answer only when it stated one: an empty key or
// a read error means absent, never a guess.
func stated[T any](key string, value T, err error) T {
	var zero T
	if err != nil || key == "" {
		return zero
	}
	return value
}

// agentEventRow lays down the parts of a row that come from what the
// dialect said and what the pipeline stamped, leaving tenancy, timing and
// delivery identity to the caller. Pipeline-resolved attribution wins over
// what the dialect infers.
func agentEventRow(a answers, enrichment rowEnrichment) chrepo.AgentEventRow {
	provider := enrichment.provider
	if provider == "" {
		provider = a.Provider()
	}

	return chrepo.AgentEventRow{
		OrganizationID:     "",
		ProjectID:          "",
		OccurredAtUnixNano: 0,
		ObservedAtUnixNano: 0,
		RecordID:           "",
		SessionID:          a.SessionID(),
		TurnID:             a.TurnID(),
		EventID:            a.SubjectID(),
		EventType:          a.EventType(),
		RawEventName:       a.EventName(),
		Source:             "",
		Provider:           provider,
		Surface:            a.Surface(),
		UserID:             enrichment.userID,
		UserEmail:          a.ExternalUserEmail(),
		ExternalUserID:     a.ExternalUserID(),
		AccountType:        enrichment.accountType,
		BillingMode:        enrichment.billingMode,
		ExternalOrgID:      a.ExternalOrgID(),
		DeviceID:           enrichment.deviceID,
		DepartmentName:     enrichment.departmentName,
		DivisionName:       enrichment.divisionName,
		JobTitle:           enrichment.jobTitle,
		EmployeeType:       enrichment.employeeType,
		CostCenterName:     enrichment.costCenterName,
		Roles:              enrichment.roles,
		Groups:             enrichment.groups,
		Model:              a.Model(),
		QuerySource:        a.QuerySource(),
		SkillName:          a.SkillName(),
		AgentName:          a.AgentName(),
		MCPServerName:      a.MCPServerName(),
		MCPToolName:        a.MCPToolName(),
		ToolName:           a.ToolName(),
		Text:               a.Text(),
		Outcome:            a.Outcome(),
		OutcomeMessage:     a.OutcomeMessage(),
		DurationNano:       a.DurationNano(),
		InputContent:       "",
		OutputContent:      "",
		InputTokens:        a.InputTokens(),
		OutputTokens:       a.OutputTokens(),
		CacheReadTokens:    a.CacheReadTokens(),
		CacheWriteTokens:   a.CacheWriteTokens(),
		CostUSD:            a.CostUSD(),
		Attributes:         "",
		ResourceAttributes: "",
		ScopeAttributes:    "",
	}
}

// subjectOrRecordID is the row's event_id: the subject's natural identity
// when the producer stated one, else a minted one. Minting reuses the
// delivery identity, which is stable across redelivery, so a re-observation
// of the same record never gets a second subject id.
func subjectOrRecordID(subjectID, recordID string) string {
	if subjectID != "" {
		return subjectID
	}
	return recordID
}

// mintedRecordID derives a delivery identity from the record's own bytes for
// records that reached the topic without one. Content-derived, so a
// redelivered copy still collapses.
func mintedRecordID(record *otelv1.LogRecord) (string, error) {
	encoded, err := proto.Marshal(record)
	if err != nil {
		return "", fmt.Errorf("marshal log record for record id: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func inboundLogFromRecord(record *otelv1.LogRecord) (*otelv1.InboundLogRecord, error) {
	encoded, err := proto.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("marshal log record: %w", err)
	}
	inbound := &otelv1.InboundLogRecord{}
	if err := proto.Unmarshal(encoded, inbound); err != nil {
		return nil, fmt.Errorf("unmarshal log record as gram.otel.v1.InboundLogRecord: %w", err)
	}

	// The transform rewrote the instrumentation scope to Gram's own and kept
	// the producer's under an attribute. Dialects recognise a producer by its
	// scope name, so put the original back before asking them anything.
	if original := logOriginalScopeName(record); original != "" {
		if inbound.GetScope() == nil {
			inbound.SetScope((&otelv1.InboundLogRecord_InstrumentationScope_builder{Name: &original}).Build())
		} else {
			inbound.GetScope().SetName(original)
		}
	}
	return inbound, nil
}

func logOriginalScopeName(record *otelv1.LogRecord) string {
	for _, kv := range record.GetAttributes() {
		if kv.GetKey() == string(OriginalInstrumentationScopeNameKey) && kv.GetValue().HasStringValue() {
			return kv.GetValue().GetStringValue()
		}
	}
	return ""
}

func inboundSpanFromSpan(span *otelv1.Span) (*otelv1.InboundSpan, error) {
	encoded, err := proto.Marshal(span)
	if err != nil {
		return nil, fmt.Errorf("marshal span: %w", err)
	}
	inbound := &otelv1.InboundSpan{}
	if err := proto.Unmarshal(encoded, inbound); err != nil {
		return nil, fmt.Errorf("unmarshal span as gram.otel.v1.InboundSpan: %w", err)
	}

	if original := spanOriginalScopeName(span); original != "" {
		if inbound.GetScope() == nil {
			inbound.SetScope((&otelv1.InboundSpan_InstrumentationScope_builder{Name: &original}).Build())
		} else {
			inbound.GetScope().SetName(original)
		}
	}
	return inbound, nil
}

func spanOriginalScopeName(span *otelv1.Span) string {
	for _, kv := range span.GetAttributes() {
		if kv.GetKey() == string(OriginalInstrumentationScopeNameKey) && kv.GetValue().HasStringValue() {
			return kv.GetValue().GetStringValue()
		}
	}
	return ""
}

// contentJSON renders the dialect's normalized messages for a content column.
// Absent or unreadable content is an empty column, never an invented one.
func contentJSON[E any, M ~[]E](_ string, messages M, err error) string {
	if err != nil || len(messages) == 0 {
		return ""
	}
	encoded, err := json.Marshal(messages)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// rowEnrichment is what the transform pipeline stamped onto the record
// before it reached the topic: directory enrichment, resolved attribution,
// and the producer-stated user id. Read from the outbound attributes, where
// the enrichers put it.
type rowEnrichment struct {
	userID      string
	provider    string
	accountType string
	billingMode string
	deviceID    string

	departmentName string
	divisionName   string
	jobTitle       string
	employeeType   string
	costCenterName string
	roles          []string
	groups         []string
}

var (
	directoryDepartmentNameKey = DirectoryAttribute("department_name")
	directoryDivisionNameKey   = DirectoryAttribute("division_name")
	directoryJobTitleKey       = DirectoryAttribute("job_title")
	directoryEmployeeTypeKey   = DirectoryAttribute("employee_type")
	directoryCostCenterNameKey = DirectoryAttribute("cost_center_name")
)

func (e *rowEnrichment) absorb(key string, value any) {
	switch key {
	case string(attr.UserIDKey):
		e.userID = enrichmentString(value)
	case string(attr.ProviderKey):
		e.provider = enrichmentString(value)
	case string(attr.AccountTypeKey):
		e.accountType = enrichmentString(value)
	case string(attr.BillingModeKey):
		e.billingMode = enrichmentString(value)
	case string(attr.DeviceIDKey):
		e.deviceID = enrichmentString(value)
	case string(directoryDepartmentNameKey):
		e.departmentName = enrichmentString(value)
	case string(directoryDivisionNameKey):
		e.divisionName = enrichmentString(value)
	case string(directoryJobTitleKey):
		e.jobTitle = enrichmentString(value)
	case string(directoryEmployeeTypeKey):
		e.employeeType = enrichmentString(value)
	case string(directoryCostCenterNameKey):
		e.costCenterName = enrichmentString(value)
	case string(GramUserRolesKey):
		e.roles = enrichmentStrings(value)
	case string(DirectoryGroupNamesKey):
		e.groups = enrichmentStrings(value)
	}
}

func enrichmentString(value any) string {
	text, _ := value.(string)
	return text
}

func enrichmentStrings(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok && text != "" {
			out = append(out, text)
		}
	}
	return out
}
