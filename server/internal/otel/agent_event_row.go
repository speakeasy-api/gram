package otel

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/speakeasy-api/gram/server/internal/otel/enrich"

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
// Every column the transform's column enrichers fill arrives on the record
// as a canonical speakeasy.agent.<column> attribute, for logs and spans
// alike, and the builders copy it from there; neither asks a dialect. There
// is no dialect fallback: the transform and this writer deploy together, and
// a record that reached the normalized topic before the enrichers ran lands
// with those columns empty, which is accepted.

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
	var columns canonicalColumns
	for _, kv := range record.GetAttributes() {
		value := logEventAnyValue(kv.GetValue())
		enrichment.read(kv.GetKey(), value)
		columns.read(kv.GetKey(), value)
	}

	row := agentEventRow(columns, enrichment)
	row.OrganizationID = organizationID
	row.ProjectID = record.GetProvenance().GetProjectId()
	row.OccurredAtUnixNano = occurredNano
	row.ObservedAtUnixNano = observedNano
	row.RecordID = recordID
	row.EventID = subjectOrRecordID(row.EventID, recordID)
	row.Attributes = attributes
	row.ResourceAttributes = resourceAttributes
	row.ScopeAttributes = scopeAttributes

	// Text is the record in words. When the transform found none and could
	// not classify the record, a string body is the closest thing the producer
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
	var columns canonicalColumns
	for _, kv := range span.GetAttributes() {
		value := spanEventAnyValue(kv.GetValue())
		enrichment.read(kv.GetKey(), value)
		columns.read(kv.GetKey(), value)
	}

	recordID := traceID + ":" + spanID
	row := agentEventRow(columns, enrichment)
	row.OrganizationID = organizationID
	row.ProjectID = span.GetProvenance().GetProjectId()
	row.OccurredAtUnixNano = startNano
	row.ObservedAtUnixNano = observedNano
	row.RecordID = recordID
	row.EventID = subjectOrRecordID(row.EventID, recordID)
	row.Attributes = attributes
	row.ResourceAttributes = resourceAttributes
	row.ScopeAttributes = scopeAttributes
	return row, ""
}

// agentEventRow lays down the parts of a row that come from the canonical
// columns the transform wrote and what the pipeline stamped, leaving
// tenancy, timing and delivery identity to the caller. Each column has
// exactly one source. The deprecated content columns are written empty on
// purpose: text carries the words and the attributes carry the structure.
func agentEventRow(columns canonicalColumns, enrichment rowEnrichment) chrepo.AgentEventRow {
	return chrepo.AgentEventRow{
		OrganizationID:     "",
		ProjectID:          "",
		OccurredAtUnixNano: 0,
		ObservedAtUnixNano: 0,
		RecordID:           "",
		SessionID:          columns.sessionID,
		TurnID:             columns.turnID,
		EventID:            columns.eventID,
		EventType:          columns.eventType,
		RawEventName:       columns.rawEventName,
		Name:               columns.name,
		Source:             columns.source,
		Provider:           columns.provider,
		Surface:            columns.surface,
		UserID:             enrichment.userID,
		UserEmail:          columns.userEmail,
		ExternalUserID:     columns.externalUserID,
		AccountType:        enrichment.accountType,
		BillingMode:        enrichment.billingMode,
		ExternalOrgID:      columns.externalOrgID,
		DeviceID:           enrichment.deviceID,
		DepartmentName:     enrichment.departmentName,
		DivisionName:       enrichment.divisionName,
		JobTitle:           enrichment.jobTitle,
		EmployeeType:       enrichment.employeeType,
		CostCenterName:     enrichment.costCenterName,
		Roles:              enrichment.roles,
		Groups:             enrichment.groups,
		Model:              columns.model,
		QuerySource:        columns.querySource,
		SkillName:          columns.skillName,
		AgentName:          columns.agentName,
		MCPServerName:      columns.mcpServerName,
		MCPToolName:        columns.mcpToolName,
		ToolName:           columns.toolName,
		Text:               columns.text,
		Outcome:            columns.outcome,
		OutcomeMessage:     columns.outcomeMessage,
		DurationNano:       columns.durationNano,
		InputContent:       "",
		OutputContent:      "",
		InputTokens:        columns.inputTokens,
		OutputTokens:       columns.outputTokens,
		CacheReadTokens:    columns.cacheReadTokens,
		CacheWriteTokens:   columns.cacheWriteTokens,
		CostUSD:            columns.costUSD,
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

// canonicalColumns is what the transform's column enrichers wrote onto the
// record: one value per agent_events column they fill, read back from the
// speakeasy.agent.* keys. A key the transform did not write reads as the
// zero value, which the row stores as "not stated"; there is no dialect
// fallback.
type canonicalColumns struct {
	eventType    string
	rawEventName string
	source       string
	provider     string
	surface      string

	sessionID      string
	turnID         string
	eventID        string
	userEmail      string
	externalUserID string
	externalOrgID  string

	model          string
	querySource    string
	skillName      string
	agentName      string
	mcpServerName  string
	mcpToolName    string
	name           string
	toolName       string
	text           string
	outcome        string
	outcomeMessage string
	durationNano   int64

	inputTokens      int64
	outputTokens     int64
	cacheReadTokens  int64
	cacheWriteTokens int64
	costUSD          float64
}

func (c *canonicalColumns) read(key string, value any) {
	switch key {
	case string(enrich.EventTypeColumnKey):
		c.eventType = enrichmentString(value)
	case string(enrich.RawEventNameColumnKey):
		c.rawEventName = enrichmentString(value)
	case string(enrich.SourceColumnKey):
		c.source = enrichmentString(value)
	case string(enrich.ProviderColumnKey):
		c.provider = enrichmentString(value)
	case string(enrich.SurfaceColumnKey):
		c.surface = enrichmentString(value)
	case string(enrich.SessionIDColumnKey):
		c.sessionID = enrichmentString(value)
	case string(enrich.TurnIDColumnKey):
		c.turnID = enrichmentString(value)
	case string(enrich.EventIDColumnKey):
		c.eventID = enrichmentString(value)
	case string(enrich.UserEmailColumnKey):
		c.userEmail = enrichmentString(value)
	case string(enrich.ExternalUserIDColumnKey):
		c.externalUserID = enrichmentString(value)
	case string(enrich.ExternalOrgIDColumnKey):
		c.externalOrgID = enrichmentString(value)
	case string(enrich.ModelColumnKey):
		c.model = enrichmentString(value)
	case string(enrich.QuerySourceColumnKey):
		c.querySource = enrichmentString(value)
	case string(enrich.SkillNameColumnKey):
		c.skillName = enrichmentString(value)
	case string(enrich.AgentNameColumnKey):
		c.agentName = enrichmentString(value)
	case string(enrich.MCPServerNameColumnKey):
		c.mcpServerName = enrichmentString(value)
	case string(enrich.MCPToolNameColumnKey):
		c.mcpToolName = enrichmentString(value)
	case string(enrich.NameColumnKey):
		c.name = enrichmentString(value)
	case string(enrich.ToolNameColumnKey):
		c.toolName = enrichmentString(value)
	case string(enrich.TextColumnKey):
		c.text = enrichmentString(value)
	case string(enrich.OutcomeColumnKey):
		c.outcome = enrichmentString(value)
	case string(enrich.OutcomeMessageColumnKey):
		c.outcomeMessage = enrichmentString(value)
	case string(enrich.DurationNanoColumnKey):
		c.durationNano = enrichmentInt64(value)
	case string(enrich.InputTokensColumnKey):
		c.inputTokens = enrichmentInt64(value)
	case string(enrich.OutputTokensColumnKey):
		c.outputTokens = enrichmentInt64(value)
	case string(enrich.CacheReadTokensColumnKey):
		c.cacheReadTokens = enrichmentInt64(value)
	case string(enrich.CacheWriteTokensColumnKey):
		c.cacheWriteTokens = enrichmentInt64(value)
	case string(enrich.CostUSDColumnKey):
		c.costUSD = enrichmentFloat64(value)
	}
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
	directoryDepartmentNameKey = enrich.DirectoryAttribute("department_name")
	directoryDivisionNameKey   = enrich.DirectoryAttribute("division_name")
	directoryJobTitleKey       = enrich.DirectoryAttribute("job_title")
	directoryEmployeeTypeKey   = enrich.DirectoryAttribute("employee_type")
	directoryCostCenterNameKey = enrich.DirectoryAttribute("cost_center_name")
)

func (e *rowEnrichment) read(key string, value any) {
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
	case string(enrich.GramUserRolesKey):
		e.roles = enrichmentStrings(value)
	case string(enrich.DirectoryGroupNamesKey):
		e.groups = enrichmentStrings(value)
	}
}

func enrichmentString(value any) string {
	text, _ := value.(string)
	return text
}

// enrichmentInt64 reads an integer column the transform wrote. The OTLP
// value may arrive as an integer or, through some producers, as a double.
func enrichmentInt64(value any) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case float64:
		return int64(v)
	}
	return 0
}

// enrichmentFloat64 reads a floating-point column the transform wrote.
func enrichmentFloat64(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int64:
		return float64(v)
	}
	return 0
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
