package litellm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protowire"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	maxOTLPAttributes       = 128
	maxOTLPAttributeBytes   = 64 * 1024
	maxOTLPResourceBytes    = 8 * 1024
	maxOTLPResourceGroups   = 256
	maxOTLPScopeGroups      = 256
	maxOTLPSpansPerExport   = 256
	maxOTLPNestedValueNodes = 4096
	maxOTLPAnyValueDepth    = 32
	litellmOTLPResourceURN  = "litellm:otel:traces"
)

type otlpAttributeValueKind uint8

const (
	otlpAttributeString otlpAttributeValueKind = iota
	otlpAttributeBool
	otlpAttributeInteger
	otlpAttributeNumber
)

type otlpAttributeSpec struct {
	target attribute.Key
	kind   otlpAttributeValueKind
}

var spanAttributeAllowlist = map[string]otlpAttributeSpec{
	"gen_ai.operation.name":                     {target: attr.GenAIOperationNameKey, kind: otlpAttributeString},
	"gen_ai.provider.name":                      {target: attr.GenAIProviderNameKey, kind: otlpAttributeString},
	"gen_ai.system":                             {target: attr.GenAISystemKey, kind: otlpAttributeString},
	"gen_ai.request.model":                      {target: attr.GenAIRequestModelKey, kind: otlpAttributeString},
	"gen_ai.response.model":                     {target: attr.GenAIResponseModelKey, kind: otlpAttributeString},
	"gen_ai.response.id":                        {target: attr.GenAIResponseIDKey, kind: otlpAttributeString},
	"gen_ai.conversation.id":                    {target: attr.GenAIConversationIDKey, kind: otlpAttributeString},
	"gen_ai.usage.input_tokens":                 {target: attr.GenAIUsageInputTokensKey, kind: otlpAttributeInteger},
	"gen_ai.usage.output_tokens":                {target: attr.GenAIUsageOutputTokensKey, kind: otlpAttributeInteger},
	"gen_ai.usage.total_tokens":                 {target: attr.GenAIUsageTotalTokensKey, kind: otlpAttributeInteger},
	"gen_ai.usage.prompt_tokens":                {target: attr.GenAIUsagePromptTokensKey, kind: otlpAttributeInteger},
	"gen_ai.usage.completion_tokens":            {target: attr.GenAIUsageCompletionTokensKey, kind: otlpAttributeInteger},
	"gen_ai.usage.cache_read.input_tokens":      {target: attr.GenAIUsageCacheReadInputTokensKey, kind: otlpAttributeInteger},
	"gen_ai.usage.cache_creation.input_tokens":  {target: attr.GenAIUsageCacheCreationInputTokensKey, kind: otlpAttributeInteger},
	"gen_ai.usage.reasoning_tokens":             {target: attr.GenAIUsageReasoningTokensKey, kind: otlpAttributeInteger},
	"gen_ai.usage.cost":                         {target: attr.GenAIUsageCostKey, kind: otlpAttributeNumber},
	"gen_ai.request.is_streaming":               {target: attr.GenAIRequestIsStreamingKey, kind: otlpAttributeBool},
	"gen_ai.request.streaming":                  {target: attr.GenAIRequestIsStreamingKey, kind: otlpAttributeBool},
	"litellm.is_streaming":                      {target: attr.GenAIRequestIsStreamingKey, kind: otlpAttributeBool},
	"litellm.request.streaming":                 {target: attr.GenAIRequestIsStreamingKey, kind: otlpAttributeBool},
	"llm.is_streaming":                          {target: attr.GenAIRequestIsStreamingKey, kind: otlpAttributeBool},
	"litellm.response.cost":                     {target: attr.GenAIUsageCostKey, kind: otlpAttributeNumber},
	"litellm.cost.total":                        {target: attr.GenAIUsageCostKey, kind: otlpAttributeNumber},
	"litellm.cost.input":                        {target: attr.LiteLLMInputCostKey, kind: otlpAttributeNumber},
	"litellm.cost.output":                       {target: attr.LiteLLMOutputCostKey, kind: otlpAttributeNumber},
	"litellm.cost.cache_read":                   {target: attr.LiteLLMCacheReadCostKey, kind: otlpAttributeNumber},
	"litellm.cost.cache_creation":               {target: attr.LiteLLMCacheWriteCostKey, kind: otlpAttributeNumber},
	"litellm.provider.model":                    {target: attr.GenAIResponseModelKey, kind: otlpAttributeString},
	"litellm.call_id":                           {target: attr.LiteLLMCallIDKey, kind: otlpAttributeString},
	"litellm_call_id":                           {target: attr.LiteLLMCallIDKey, kind: otlpAttributeString},
	"litellm.trace_id":                          {target: attr.LiteLLMTraceIDKey, kind: otlpAttributeString},
	"litellm_trace_id":                          {target: attr.LiteLLMTraceIDKey, kind: otlpAttributeString},
	"litellm.user_id":                           {target: attr.LiteLLMUserIDKey, kind: otlpAttributeString},
	"user_api_key_user_id":                      {target: attr.LiteLLMUserIDKey, kind: otlpAttributeString},
	"metadata.user_api_key_user_id":             {target: attr.LiteLLMUserIDKey, kind: otlpAttributeString},
	"litellm.user_email":                        {target: attr.LiteLLMUserEmailKey, kind: otlpAttributeString},
	"user_api_key_user_email":                   {target: attr.LiteLLMUserEmailKey, kind: otlpAttributeString},
	"metadata.user_api_key_user_email":          {target: attr.LiteLLMUserEmailKey, kind: otlpAttributeString},
	"litellm.team_id":                           {target: attr.LiteLLMTeamIDKey, kind: otlpAttributeString},
	"user_api_key_team_id":                      {target: attr.LiteLLMTeamIDKey, kind: otlpAttributeString},
	"metadata.user_api_key_team_id":             {target: attr.LiteLLMTeamIDKey, kind: otlpAttributeString},
	"litellm.team_alias":                        {target: attr.LiteLLMTeamAliasKey, kind: otlpAttributeString},
	"user_api_key_team_alias":                   {target: attr.LiteLLMTeamAliasKey, kind: otlpAttributeString},
	"metadata.user_api_key_team_alias":          {target: attr.LiteLLMTeamAliasKey, kind: otlpAttributeString},
	"litellm.org_id":                            {target: attr.LiteLLMOrganizationIDKey, kind: otlpAttributeString},
	"litellm.organization_id":                   {target: attr.LiteLLMOrganizationIDKey, kind: otlpAttributeString},
	"user_api_key_org_id":                       {target: attr.LiteLLMOrganizationIDKey, kind: otlpAttributeString},
	"metadata.user_api_key_org_id":              {target: attr.LiteLLMOrganizationIDKey, kind: otlpAttributeString},
	"user_api_key_end_user_id":                  {target: attr.LiteLLMEndUserIDKey, kind: otlpAttributeString},
	"metadata.user_api_key_end_user_id":         {target: attr.LiteLLMEndUserIDKey, kind: otlpAttributeString},
	"litellm.team.id":                           {target: attr.LiteLLMTeamIDKey, kind: otlpAttributeString},
	"litellm.team.alias":                        {target: attr.LiteLLMTeamAliasKey, kind: otlpAttributeString},
	"litellm.api_key.hash":                      {target: attr.LiteLLMAPIKeyHashKey, kind: otlpAttributeString},
	"litellm.end_user.id":                       {target: attr.LiteLLMEndUserIDKey, kind: otlpAttributeString},
	"litellm.metadata.user_api_key_user_id":     {target: attr.LiteLLMUserIDKey, kind: otlpAttributeString},
	"litellm.metadata.user_api_key_user_email":  {target: attr.LiteLLMUserEmailKey, kind: otlpAttributeString},
	"litellm.metadata.user_api_key_org_id":      {target: attr.LiteLLMOrganizationIDKey, kind: otlpAttributeString},
	"litellm.metadata.user_api_key_alias":       {target: attr.LiteLLMAPIKeyAliasKey, kind: otlpAttributeString},
	"litellm.metadata.user_api_key_end_user_id": {target: attr.LiteLLMEndUserIDKey, kind: otlpAttributeString},
}

var metricAttributeAllowlist = map[string]otlpAttributeSpec{
	"gen_ai.operation.name": {target: attr.GenAIOperationNameKey, kind: otlpAttributeString},
	"gen_ai.system":         {target: attr.GenAISystemKey, kind: otlpAttributeString},
	"gen_ai.request.model":  {target: attr.GenAIRequestModelKey, kind: otlpAttributeString},
	"gen_ai.framework":      {target: attribute.Key("gen_ai.framework"), kind: otlpAttributeString},
	"gen_ai.token.type":     {target: attribute.Key("gen_ai.token.type"), kind: otlpAttributeString},
}

var resourceAttributeAllowlist = map[string]otlpAttributeSpec{
	"service.name":                {target: attr.ServiceNameKey, kind: otlpAttributeString},
	"service.namespace":           {target: attr.ServiceNamespaceKey, kind: otlpAttributeString},
	"service.version":             {target: attr.ServiceVersionKey, kind: otlpAttributeString},
	"service.instance.id":         {target: attr.ServiceInstanceIDKey, kind: otlpAttributeString},
	"deployment.environment":      {target: attr.DeploymentEnvironmentKey, kind: otlpAttributeString},
	"deployment.environment.name": {target: attr.ServiceEnvKey, kind: otlpAttributeString},
	"telemetry.sdk.name":          {target: attr.TelemetrySDKNameKey, kind: otlpAttributeString},
	"telemetry.sdk.language":      {target: attr.TelemetrySDKLanguageKey, kind: otlpAttributeString},
	"telemetry.sdk.version":       {target: attr.TelemetrySDKVersionKey, kind: otlpAttributeString},
}

var metricResourceAttributeAllowlist = map[string]otlpAttributeSpec{
	"service.name":                {target: attr.ServiceNameKey, kind: otlpAttributeString},
	"service.namespace":           {target: attr.ServiceNamespaceKey, kind: otlpAttributeString},
	"service.version":             {target: attr.ServiceVersionKey, kind: otlpAttributeString},
	"deployment.environment":      {target: attr.DeploymentEnvironmentKey, kind: otlpAttributeString},
	"deployment.environment.name": {target: attr.ServiceEnvKey, kind: otlpAttributeString},
	"telemetry.sdk.name":          {target: attr.TelemetrySDKNameKey, kind: otlpAttributeString},
	"telemetry.sdk.language":      {target: attr.TelemetrySDKLanguageKey, kind: otlpAttributeString},
	"telemetry.sdk.version":       {target: attr.TelemetrySDKVersionKey, kind: otlpAttributeString},
}

type otlpExportRequest struct {
	ResourceSpans []otlpResourceSpans `json:"resourceSpans"`
}

type otlpResourceSpans struct {
	Resource   *otlpResource    `json:"resource,omitempty"`
	ScopeSpans []otlpScopeSpans `json:"scopeSpans,omitempty"`
}

type otlpResource struct {
	Attributes             []otlpKeyValue `json:"attributes,omitempty"`
	DroppedAttributesCount uint32         `json:"droppedAttributesCount,omitempty"`
}

type otlpScopeSpans struct {
	Scope *otlpScope `json:"scope,omitempty"`
	Spans []otlpSpan `json:"spans,omitempty"`
}

type otlpScope struct {
	Name                   string         `json:"name,omitempty"`
	Version                string         `json:"version,omitempty"`
	Attributes             []otlpKeyValue `json:"attributes,omitempty"`
	DroppedAttributesCount uint32         `json:"droppedAttributesCount,omitempty"`
}

type otlpSpan struct {
	TraceID                string         `json:"traceId,omitempty"`
	SpanID                 string         `json:"spanId,omitempty"`
	ParentSpanID           string         `json:"parentSpanId,omitempty"`
	Name                   string         `json:"name,omitempty"`
	Kind                   jsonInt32      `json:"kind,omitempty"`
	StartTimeUnixNano      jsonUint64     `json:"startTimeUnixNano,omitempty"`
	EndTimeUnixNano        jsonUint64     `json:"endTimeUnixNano,omitempty"`
	Attributes             []otlpKeyValue `json:"attributes,omitempty"`
	DroppedAttributesCount uint32         `json:"droppedAttributesCount,omitempty"`
	Status                 *otlpStatus    `json:"status,omitempty"`
}

type otlpStatus struct {
	Code jsonInt32 `json:"code,omitempty"`
}

type otlpKeyValue struct {
	Key   string       `json:"key"`
	Value otlpAnyValue `json:"value"`
}

type otlpAnyValue struct {
	StringValue *string           `json:"stringValue,omitempty"`
	BoolValue   *bool             `json:"boolValue,omitempty"`
	IntValue    *jsonInt64        `json:"intValue,omitempty"`
	DoubleValue *jsonFloat64      `json:"doubleValue,omitempty"`
	ArrayValue  *otlpArrayValue   `json:"arrayValue,omitempty"`
	KvlistValue *otlpKeyValueList `json:"kvlistValue,omitempty"`
	BytesValue  *string           `json:"bytesValue,omitempty"`
}

type otlpArrayValue struct {
	Values []otlpAnyValue `json:"values,omitempty"`
}

type otlpKeyValueList struct {
	Values []otlpKeyValue `json:"values,omitempty"`
}

type jsonInt64 int64

func (v *jsonInt64) UnmarshalJSON(data []byte) error {
	value, err := parseJSONInteger(data, 64)
	if err != nil {
		return err
	}
	*v = jsonInt64(value)
	return nil
}

type jsonUint64 uint64

func (v *jsonUint64) UnmarshalJSON(data []byte) error {
	value, err := decodeOTLPNumericJSON(data)
	if err != nil {
		return err
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return fmt.Errorf("parse OTLP unsigned integer: %w", err)
	}
	*v = jsonUint64(parsed)
	return nil
}

type jsonFloat64 float64

func (v *jsonFloat64) UnmarshalJSON(data []byte) error {
	value, err := decodeOTLPNumericJSON(data)
	if err != nil {
		return err
	}
	var parsed float64
	switch value {
	case "NaN":
		parsed = math.NaN()
	case "Infinity":
		parsed = math.Inf(1)
	case "-Infinity":
		parsed = math.Inf(-1)
	default:
		parsed, err = strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("parse OTLP double: %w", err)
		}
	}
	*v = jsonFloat64(parsed)
	return nil
}

type jsonInt32 int32

func (v *jsonInt32) UnmarshalJSON(data []byte) error {
	value, err := parseJSONInteger(data, 32)
	if err != nil {
		return err
	}
	// #nosec G115 -- parseJSONInteger enforces a signed 32-bit range above.
	*v = jsonInt32(value)
	return nil
}

func parseJSONInteger(data []byte, bitSize int) (int64, error) {
	value, err := decodeOTLPNumericJSON(data)
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseInt(value, 10, bitSize)
	if err != nil {
		return 0, fmt.Errorf("parse OTLP integer: %w", err)
	}
	return parsed, nil
}

func decodeOTLPNumericJSON(data []byte) (string, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '"' {
		return string(data), nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return "", fmt.Errorf("decode OTLP numeric string: %w", err)
	}
	return value, nil
}

func decodeOTLPJSON(body []byte) (*otlpExportRequest, error) {
	if err := preflightOTLPJSON(body); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	var request otlpExportRequest
	if err := decoder.Decode(&request); err != nil {
		return nil, fmt.Errorf("decode OTLP JSON: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
	}
	if err := request.validateStructure(); err != nil {
		return nil, err
	}
	if err := request.validateAnyValues(); err != nil {
		return nil, err
	}
	return &request, nil
}

type otlpCollectionCounts struct {
	resourceGroups int
	scopeGroups    int
	spans          int
	nestedNodes    int
}

func (c otlpCollectionCounts) validate() error {
	if err := validateOTLPCollectionCounts(c.resourceGroups, c.scopeGroups, c.spans); err != nil {
		return err
	}
	if c.nestedNodes > maxOTLPNestedValueNodes {
		return fmt.Errorf("OTLP trace export contains too many nested AnyValue nodes: %d", c.nestedNodes)
	}
	return nil
}

func (c *otlpCollectionCounts) addNestedNode(depth int) error {
	if depth > maxOTLPAnyValueDepth {
		return fmt.Errorf("OTLP trace export AnyValue nesting exceeds maximum depth: %d", depth)
	}
	c.nestedNodes++
	return c.validate()
}

type otlpJSONCollection uint8

const (
	otlpJSONResources otlpJSONCollection = iota
	otlpJSONScopes
	otlpJSONSpans
)

func preflightOTLPJSON(body []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("preflight OTLP JSON: %w", err)
	}
	if token != json.Delim('{') {
		return fmt.Errorf("preflight OTLP JSON: expected top-level object")
	}

	counts := otlpCollectionCounts{resourceGroups: 0, scopeGroups: 0, spans: 0, nestedNodes: 0}
	if err := preflightJSONObject(decoder, &counts, "resourceSpans", otlpJSONResources); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("preflight OTLP JSON: multiple values")
		}
		return fmt.Errorf("preflight trailing OTLP JSON: %w", err)
	}
	return nil
}

func preflightJSONObject(decoder *json.Decoder, counts *otlpCollectionCounts, collectionKey string, collection otlpJSONCollection) error {
	for decoder.More() {
		key, err := nextJSONKey(decoder)
		if err != nil {
			return err
		}
		value, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("preflight OTLP JSON field %q: %w", key, err)
		}
		switch {
		case strings.EqualFold(key, collectionKey) && value == json.Delim('['):
			if err := preflightJSONArray(decoder, counts, collection); err != nil {
				return err
			}
		case collection == otlpJSONScopes && strings.EqualFold(key, "resource") && value == json.Delim('{'):
			if err := preflightJSONAttributesObject(decoder, counts); err != nil {
				return err
			}
		case collection == otlpJSONSpans && strings.EqualFold(key, "scope") && value == json.Delim('{'):
			if err := preflightJSONAttributesObject(decoder, counts); err != nil {
				return err
			}
		default:
			if err := skipJSONValue(decoder, value); err != nil {
				return err
			}
		}
	}
	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("preflight OTLP JSON object: %w", err)
	}
	return nil
}

func preflightJSONArray(decoder *json.Decoder, counts *otlpCollectionCounts, collection otlpJSONCollection) error {
	for decoder.More() {
		switch collection {
		case otlpJSONResources:
			counts.resourceGroups++
		case otlpJSONScopes:
			counts.scopeGroups++
		case otlpJSONSpans:
			counts.spans++
		}
		if err := counts.validate(); err != nil {
			return err
		}
		value, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("preflight OTLP JSON collection: %w", err)
		}
		if value == json.Delim('{') {
			switch collection {
			case otlpJSONResources:
				if err := preflightJSONObject(decoder, counts, "scopeSpans", otlpJSONScopes); err != nil {
					return err
				}
			case otlpJSONScopes:
				if err := preflightJSONObject(decoder, counts, "spans", otlpJSONSpans); err != nil {
					return err
				}
			case otlpJSONSpans:
				if err := preflightJSONAttributesObject(decoder, counts); err != nil {
					return err
				}
			}
		} else if err := skipJSONValue(decoder, value); err != nil {
			return err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("preflight OTLP JSON collection: %w", err)
	}
	return nil
}

func preflightJSONAttributesObject(decoder *json.Decoder, counts *otlpCollectionCounts) error {
	for decoder.More() {
		key, err := nextJSONKey(decoder)
		if err != nil {
			return err
		}
		value, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("preflight OTLP attributes field %q: %w", key, err)
		}
		if strings.EqualFold(key, "attributes") && value == json.Delim('[') {
			if err := preflightJSONKeyValues(decoder, counts, 1); err != nil {
				return err
			}
		} else if err := skipJSONValue(decoder, value); err != nil {
			return err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("preflight OTLP attributes object: %w", err)
	}
	return nil
}

func preflightJSONKeyValues(decoder *json.Decoder, counts *otlpCollectionCounts, depth int) error {
	attributeCount := 0
	for decoder.More() {
		attributeCount++
		if attributeCount > maxOTLPAttributes {
			return fmt.Errorf("OTLP attribute collection contains too many KeyValues: %d", attributeCount)
		}
		value, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("preflight OTLP KeyValue: %w", err)
		}
		if value == json.Delim('{') {
			if err := preflightJSONKeyValue(decoder, counts, depth); err != nil {
				return err
			}
		} else if err := skipJSONValue(decoder, value); err != nil {
			return err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("preflight OTLP KeyValues: %w", err)
	}
	return nil
}

func preflightJSONKeyValue(decoder *json.Decoder, counts *otlpCollectionCounts, depth int) error {
	for decoder.More() {
		key, err := nextJSONKey(decoder)
		if err != nil {
			return err
		}
		value, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("preflight OTLP KeyValue field %q: %w", key, err)
		}
		if strings.EqualFold(key, "value") && value == json.Delim('{') {
			if err := preflightJSONAnyValue(decoder, counts, depth); err != nil {
				return err
			}
		} else if err := skipJSONValue(decoder, value); err != nil {
			return err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("preflight OTLP KeyValue: %w", err)
	}
	return nil
}

func preflightJSONAnyValue(decoder *json.Decoder, counts *otlpCollectionCounts, depth int) error {
	if depth > maxOTLPAnyValueDepth {
		return fmt.Errorf("OTLP trace export AnyValue nesting exceeds maximum depth: %d", depth)
	}
	for decoder.More() {
		key, err := nextJSONKey(decoder)
		if err != nil {
			return err
		}
		value, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("preflight OTLP AnyValue field %q: %w", key, err)
		}
		switch {
		case strings.EqualFold(key, "arrayValue") && value == json.Delim('{'):
			if err := preflightJSONAnyValueContainer(decoder, counts, depth+1, false); err != nil {
				return err
			}
		case strings.EqualFold(key, "kvlistValue") && value == json.Delim('{'):
			if err := preflightJSONAnyValueContainer(decoder, counts, depth+1, true); err != nil {
				return err
			}
		default:
			if err := skipJSONValue(decoder, value); err != nil {
				return err
			}
		}
	}
	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("preflight OTLP AnyValue: %w", err)
	}
	return nil
}

func preflightJSONAnyValueContainer(decoder *json.Decoder, counts *otlpCollectionCounts, childDepth int, keyValues bool) error {
	for decoder.More() {
		key, err := nextJSONKey(decoder)
		if err != nil {
			return err
		}
		value, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("preflight OTLP AnyValue container field %q: %w", key, err)
		}
		if !strings.EqualFold(key, "values") || value != json.Delim('[') {
			if err := skipJSONValue(decoder, value); err != nil {
				return err
			}
			continue
		}
		keyValueCount := 0
		for decoder.More() {
			keyValueCount++
			if keyValues && keyValueCount > maxOTLPAttributes {
				return fmt.Errorf("OTLP nested KeyValue collection contains too many entries: %d", keyValueCount)
			}
			if err := counts.addNestedNode(childDepth); err != nil {
				return err
			}
			child, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("preflight OTLP nested AnyValue: %w", err)
			}
			if child != json.Delim('{') {
				if err := skipJSONValue(decoder, child); err != nil {
					return err
				}
				continue
			}
			if keyValues {
				err = preflightJSONKeyValue(decoder, counts, childDepth)
			} else {
				err = preflightJSONAnyValue(decoder, counts, childDepth)
			}
			if err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return fmt.Errorf("preflight OTLP nested AnyValue values: %w", err)
		}
	}
	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("preflight OTLP AnyValue container: %w", err)
	}
	return nil
}

func nextJSONKey(decoder *json.Decoder) (string, error) {
	token, err := decoder.Token()
	if err != nil {
		return "", fmt.Errorf("preflight OTLP JSON key: %w", err)
	}
	key, ok := token.(string)
	if !ok {
		return "", fmt.Errorf("preflight OTLP JSON: expected object key")
	}
	return key, nil
}

func skipJSONValue(decoder *json.Decoder, token json.Token) error {
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if delimiter != '{' && delimiter != '[' {
		return fmt.Errorf("preflight OTLP JSON: unexpected delimiter %q", delimiter)
	}
	depth := 1
	for depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("preflight OTLP JSON value: %w", err)
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			continue
		}
		switch delimiter {
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		}
	}
	return nil
}

func preflightOTLPProtobuf(data []byte) error {
	counts := otlpCollectionCounts{resourceGroups: 0, scopeGroups: 0, spans: 0, nestedNodes: 0}
	for len(data) > 0 {
		number, wireType, value, rest, err := consumeProtobufField(data)
		if err != nil {
			return fmt.Errorf("preflight OTLP protobuf export: %w", err)
		}
		data = rest
		if number != 1 || wireType != protowire.BytesType {
			continue
		}
		counts.resourceGroups++
		if err := counts.validate(); err != nil {
			return err
		}
		if err := preflightProtobufResourceSpans(value, &counts); err != nil {
			return err
		}
	}
	return nil
}

func preflightProtobufResourceSpans(data []byte, counts *otlpCollectionCounts) error {
	resourceAttributeCount := 0
	for len(data) > 0 {
		number, wireType, value, rest, err := consumeProtobufField(data)
		if err != nil {
			return fmt.Errorf("preflight OTLP protobuf resource group: %w", err)
		}
		data = rest
		if wireType != protowire.BytesType {
			continue
		}
		if number == 1 {
			if err := preflightProtobufAttributes(value, 1, counts, &resourceAttributeCount); err != nil {
				return err
			}
			continue
		}
		if number != 2 {
			continue
		}
		counts.scopeGroups++
		if err := counts.validate(); err != nil {
			return err
		}
		if err := preflightProtobufScopeSpans(value, counts); err != nil {
			return err
		}
	}
	return nil
}

func preflightProtobufScopeSpans(data []byte, counts *otlpCollectionCounts) error {
	scopeAttributeCount := 0
	for len(data) > 0 {
		number, wireType, value, rest, err := consumeProtobufField(data)
		if err != nil {
			return fmt.Errorf("preflight OTLP protobuf scope group: %w", err)
		}
		data = rest
		if wireType != protowire.BytesType {
			continue
		}
		if number == 1 {
			if err := preflightProtobufAttributes(value, 3, counts, &scopeAttributeCount); err != nil {
				return err
			}
			continue
		}
		if number != 2 {
			continue
		}
		counts.spans++
		if err := counts.validate(); err != nil {
			return err
		}
		attributeCount := 0
		if err := preflightProtobufAttributes(value, 9, counts, &attributeCount); err != nil {
			return err
		}
	}
	return nil
}

func preflightProtobufAttributes(data []byte, attributesField protowire.Number, counts *otlpCollectionCounts, attributeCount *int) error {
	for len(data) > 0 {
		number, wireType, value, rest, err := consumeProtobufField(data)
		if err != nil {
			return fmt.Errorf("preflight OTLP protobuf attributes: %w", err)
		}
		data = rest
		if number == attributesField && wireType == protowire.BytesType {
			*attributeCount++
			if *attributeCount > maxOTLPAttributes {
				return fmt.Errorf("OTLP attribute collection contains too many KeyValues: %d", *attributeCount)
			}
			if err := preflightProtobufKeyValue(value, counts, 1); err != nil {
				return err
			}
		}
	}
	return nil
}

func preflightProtobufKeyValue(data []byte, counts *otlpCollectionCounts, depth int) error {
	for len(data) > 0 {
		number, wireType, value, rest, err := consumeProtobufField(data)
		if err != nil {
			return fmt.Errorf("preflight OTLP protobuf KeyValue: %w", err)
		}
		data = rest
		if number == 2 && wireType == protowire.BytesType {
			if err := preflightProtobufAnyValue(value, counts, depth); err != nil {
				return err
			}
		}
	}
	return nil
}

func preflightProtobufAnyValue(data []byte, counts *otlpCollectionCounts, depth int) error {
	if depth > maxOTLPAnyValueDepth {
		return fmt.Errorf("OTLP trace export AnyValue nesting exceeds maximum depth: %d", depth)
	}
	for len(data) > 0 {
		number, wireType, value, rest, err := consumeProtobufField(data)
		if err != nil {
			return fmt.Errorf("preflight OTLP protobuf AnyValue: %w", err)
		}
		data = rest
		if wireType != protowire.BytesType || (number != 5 && number != 6) {
			continue
		}
		if err := preflightProtobufAnyValueContainer(value, counts, depth+1, number == 6); err != nil {
			return err
		}
	}
	return nil
}

func preflightProtobufAnyValueContainer(data []byte, counts *otlpCollectionCounts, childDepth int, keyValues bool) error {
	keyValueCount := 0
	for len(data) > 0 {
		number, wireType, value, rest, err := consumeProtobufField(data)
		if err != nil {
			return fmt.Errorf("preflight OTLP protobuf AnyValue container: %w", err)
		}
		data = rest
		if number != 1 || wireType != protowire.BytesType {
			continue
		}
		keyValueCount++
		if keyValues && keyValueCount > maxOTLPAttributes {
			return fmt.Errorf("OTLP nested KeyValue collection contains too many entries: %d", keyValueCount)
		}
		if err := counts.addNestedNode(childDepth); err != nil {
			return err
		}
		if keyValues {
			err = preflightProtobufKeyValue(value, counts, childDepth)
		} else {
			err = preflightProtobufAnyValue(value, counts, childDepth)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func consumeProtobufField(data []byte) (protowire.Number, protowire.Type, []byte, []byte, error) {
	number, wireType, tagLength := protowire.ConsumeTag(data)
	if tagLength < 0 {
		return 0, 0, nil, nil, fmt.Errorf("consume protobuf tag: %w", protowire.ParseError(tagLength))
	}
	data = data[tagLength:]
	if wireType == protowire.BytesType {
		value, fieldLength := protowire.ConsumeBytes(data)
		if fieldLength < 0 {
			return 0, 0, nil, nil, fmt.Errorf("consume protobuf bytes: %w", protowire.ParseError(fieldLength))
		}
		return number, wireType, value, data[fieldLength:], nil
	}
	fieldLength := protowire.ConsumeFieldValue(number, wireType, data)
	if fieldLength < 0 {
		return 0, 0, nil, nil, fmt.Errorf("consume protobuf field value: %w", protowire.ParseError(fieldLength))
	}
	return number, wireType, nil, data[fieldLength:], nil
}

func (r *otlpExportRequest) validateStructure() error {
	scopeGroups := 0
	spans := 0
	for _, resourceSpans := range r.ResourceSpans {
		scopeGroups += len(resourceSpans.ScopeSpans)
		for _, scopeSpans := range resourceSpans.ScopeSpans {
			spans += len(scopeSpans.Spans)
		}
	}
	return validateOTLPCollectionCounts(len(r.ResourceSpans), scopeGroups, spans)
}

func validateProtoOTLPStructure(request *collectortracev1.ExportTraceServiceRequest) error {
	scopeGroups := 0
	spans := 0
	for _, resourceSpans := range request.ResourceSpans {
		if resourceSpans != nil {
			scopeGroups += len(resourceSpans.ScopeSpans)
			for _, scopeSpans := range resourceSpans.ScopeSpans {
				if scopeSpans != nil {
					spans += len(scopeSpans.Spans)
				}
			}
		}
	}
	return validateOTLPCollectionCounts(len(request.ResourceSpans), scopeGroups, spans)
}

func validateOTLPCollectionCounts(resourceGroups, scopeGroups, spans int) error {
	if resourceGroups > maxOTLPResourceGroups {
		return fmt.Errorf("OTLP trace export contains too many resource groups: %d", resourceGroups)
	}
	if scopeGroups > maxOTLPScopeGroups {
		return fmt.Errorf("OTLP trace export contains too many scope groups: %d", scopeGroups)
	}
	if spans > maxOTLPSpansPerExport {
		return fmt.Errorf("%w: %d", errTooManyOTLPSpans, spans)
	}
	return nil
}

func (r *otlpExportRequest) validateAnyValues() error {
	for _, resourceSpans := range r.ResourceSpans {
		if resourceSpans.Resource != nil {
			if err := validateOTLPKeyValues(resourceSpans.Resource.Attributes); err != nil {
				return err
			}
		}
		for _, scopeSpans := range resourceSpans.ScopeSpans {
			if scopeSpans.Scope != nil {
				if err := validateOTLPKeyValues(scopeSpans.Scope.Attributes); err != nil {
					return err
				}
			}
			for _, span := range scopeSpans.Spans {
				if err := validateOTLPKeyValues(span.Attributes); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func validateOTLPKeyValues(values []otlpKeyValue) error {
	for _, value := range values {
		if err := value.Value.validate(); err != nil {
			return fmt.Errorf("validate OTLP attribute %q: %w", value.Key, err)
		}
	}
	return nil
}

func (v otlpAnyValue) validate() error {
	count := 0
	for _, present := range []bool{
		v.StringValue != nil,
		v.BoolValue != nil,
		v.IntValue != nil,
		v.DoubleValue != nil,
		v.ArrayValue != nil,
		v.KvlistValue != nil,
		v.BytesValue != nil,
	} {
		if present {
			count++
		}
	}
	if count > 1 {
		return fmt.Errorf("AnyValue must contain at most one value field")
	}
	if v.ArrayValue != nil {
		for _, child := range v.ArrayValue.Values {
			if err := child.validate(); err != nil {
				return fmt.Errorf("validate array child: %w", err)
			}
		}
	}
	if v.KvlistValue != nil {
		if err := validateOTLPKeyValues(v.KvlistValue.Values); err != nil {
			return fmt.Errorf("validate kvlist child: %w", err)
		}
	}
	return nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != nil {
		if err == io.EOF {
			return nil
		}
		return fmt.Errorf("decode trailing OTLP JSON: %w", err)
	}
	return fmt.Errorf("decode OTLP JSON: multiple values")
}

func exportRequestFromProto(request *collectortracev1.ExportTraceServiceRequest) *otlpExportRequest {
	result := &otlpExportRequest{ResourceSpans: make([]otlpResourceSpans, 0, len(request.ResourceSpans))}
	for _, resourceSpans := range request.ResourceSpans {
		if resourceSpans == nil {
			continue
		}
		converted := otlpResourceSpans{
			Resource:   resourceFromProto(resourceSpans.Resource),
			ScopeSpans: make([]otlpScopeSpans, 0, len(resourceSpans.ScopeSpans)),
		}
		for _, scopeSpans := range resourceSpans.ScopeSpans {
			if scopeSpans == nil {
				continue
			}
			convertedScope := otlpScopeSpans{
				Scope: scopeFromProto(scopeSpans.Scope),
				Spans: make([]otlpSpan, 0, len(scopeSpans.Spans)),
			}
			for _, span := range scopeSpans.Spans {
				if span != nil {
					convertedScope.Spans = append(convertedScope.Spans, spanFromProto(span))
				}
			}
			converted.ScopeSpans = append(converted.ScopeSpans, convertedScope)
		}
		result.ResourceSpans = append(result.ResourceSpans, converted)
	}
	return result
}

func resourceFromProto(resource *resourcev1.Resource) *otlpResource {
	if resource == nil {
		return nil
	}
	return &otlpResource{
		Attributes:             keyValuesFromProto(resource.Attributes),
		DroppedAttributesCount: resource.DroppedAttributesCount,
	}
}

func scopeFromProto(scope *commonv1.InstrumentationScope) *otlpScope {
	if scope == nil {
		return nil
	}
	return &otlpScope{
		Name:                   scope.Name,
		Version:                scope.Version,
		Attributes:             keyValuesFromProto(scope.Attributes),
		DroppedAttributesCount: scope.DroppedAttributesCount,
	}
}

func spanFromProto(span *tracev1.Span) otlpSpan {
	status := (*otlpStatus)(nil)
	if span.Status != nil {
		status = &otlpStatus{Code: jsonInt32(span.Status.Code)}
	}
	return otlpSpan{
		TraceID:                hex.EncodeToString(span.TraceId),
		SpanID:                 hex.EncodeToString(span.SpanId),
		ParentSpanID:           hex.EncodeToString(span.ParentSpanId),
		Name:                   span.Name,
		Kind:                   jsonInt32(span.Kind),
		StartTimeUnixNano:      jsonUint64(span.StartTimeUnixNano),
		EndTimeUnixNano:        jsonUint64(span.EndTimeUnixNano),
		Attributes:             keyValuesFromProto(span.Attributes),
		DroppedAttributesCount: span.DroppedAttributesCount,
		Status:                 status,
	}
}

func keyValuesFromProto(values []*commonv1.KeyValue) []otlpKeyValue {
	result := make([]otlpKeyValue, 0, len(values))
	for _, value := range values {
		if value != nil {
			result = append(result, otlpKeyValue{Key: value.Key, Value: anyValueFromProto(value.Value)})
		}
	}
	return result
}

func anyValueFromProto(value *commonv1.AnyValue) otlpAnyValue {
	result := otlpAnyValue{
		StringValue: nil,
		BoolValue:   nil,
		IntValue:    nil,
		DoubleValue: nil,
		ArrayValue:  nil,
		KvlistValue: nil,
		BytesValue:  nil,
	}
	if value == nil {
		return result
	}
	switch typed := value.Value.(type) {
	case *commonv1.AnyValue_StringValue:
		result.StringValue = &typed.StringValue
	case *commonv1.AnyValue_BoolValue:
		result.BoolValue = &typed.BoolValue
	case *commonv1.AnyValue_IntValue:
		converted := jsonInt64(typed.IntValue)
		result.IntValue = &converted
	case *commonv1.AnyValue_DoubleValue:
		converted := jsonFloat64(typed.DoubleValue)
		result.DoubleValue = &converted
	case *commonv1.AnyValue_ArrayValue:
		result.ArrayValue = &otlpArrayValue{Values: make([]otlpAnyValue, 0, len(typed.ArrayValue.Values))}
		for _, item := range typed.ArrayValue.Values {
			result.ArrayValue.Values = append(result.ArrayValue.Values, anyValueFromProto(item))
		}
	case *commonv1.AnyValue_KvlistValue:
		result.KvlistValue = &otlpKeyValueList{Values: keyValuesFromProto(typed.KvlistValue.Values)}
	case *commonv1.AnyValue_BytesValue:
		encoded := base64.StdEncoding.EncodeToString(typed.BytesValue)
		result.BytesValue = &encoded
	}
	return result
}

func (v otlpAnyValue) value() (any, bool) {
	switch {
	case v.StringValue != nil:
		return *v.StringValue, true
	case v.BoolValue != nil:
		return *v.BoolValue, true
	case v.IntValue != nil:
		return int64(*v.IntValue), true
	case v.DoubleValue != nil:
		return float64(*v.DoubleValue), true
	case v.ArrayValue != nil:
		values := make([]any, 0, len(v.ArrayValue.Values))
		for _, item := range v.ArrayValue.Values {
			if converted, ok := item.value(); ok {
				values = append(values, converted)
			}
		}
		return values, true
	case v.KvlistValue != nil:
		values := make(map[string]any, len(v.KvlistValue.Values))
		for _, item := range v.KvlistValue.Values {
			if converted, ok := item.Value.value(); ok {
				values[item.Key] = converted
			}
		}
		return values, true
	case v.BytesValue != nil:
		decoded, err := base64.StdEncoding.DecodeString(*v.BytesValue)
		if err != nil {
			return nil, false
		}
		return decoded, true
	default:
		return nil, false
	}
}

func (s *Service) traceLogParams(ctx context.Context, request *otlpExportRequest, organizationID, projectID string) []telemetry.LogParams {
	observed := time.Now().UTC()
	params := make([]telemetry.LogParams, 0)
	for _, resourceSpans := range request.ResourceSpans {
		resourceAttributes := map[attr.Key]any{}
		if resourceSpans.Resource != nil {
			resourceAttributes = s.sanitizeOTLPResourceAttributes(ctx, resourceSpans.Resource.Attributes)
		}
		for _, scopeSpans := range resourceSpans.ScopeSpans {
			for _, span := range scopeSpans.Spans {
				spanAttributes := s.sanitizeOTLPAttributes(ctx, span.Attributes, spanAttributeAllowlist)
				invalidIDs := 0
				if traceID, ok := normalizeOTLPID(span.TraceID, 32); ok {
					spanAttributes[attr.TraceIDKey] = traceID
				} else if span.TraceID != "" {
					invalidIDs++
				}
				if spanID, ok := normalizeOTLPID(span.SpanID, 16); ok {
					spanAttributes[attr.SpanIDKey] = spanID
				} else if span.SpanID != "" {
					invalidIDs++
				}
				if parentID, ok := normalizeOTLPID(span.ParentSpanID, 16); ok {
					spanAttributes[attr.SpanParentIDKey] = parentID
				} else if span.ParentSpanID != "" {
					invalidIDs++
				}
				s.traces.recordInvalidIdentifiers(ctx, invalidIDs)

				spanName, spanNameChanged, keepSpanName := boundOTLPAttributeValue(span.Name)
				if spanNameChanged {
					s.traces.recordTruncatedAttributes(ctx, 1)
				}
				spanAttributes[attr.HookSourceKey] = "litellm"
				spanAttributes[attr.EventSourceKey] = string(telemetry.EventSourceHook)
				spanAttributes[attr.ResourceURNKey] = litellmOTLPResourceURN
				if keepSpanName {
					spanAttributes[attr.OTelSpanNameKey] = spanName
				}
				spanAttributes[attr.OTelSpanKindKey] = otlpSpanKind(int32(span.Kind))
				spanAttributes[attr.OTelSpanStatusCodeKey] = otlpStatusCode(span.Status)
				spanAttributes[attr.OTelSpanStartTimeUnixNanoKey] = uint64(span.StartTimeUnixNano)
				spanAttributes[attr.OTelSpanEndTimeUnixNanoKey] = uint64(span.EndTimeUnixNano)
				if end, start := uint64(span.EndTimeUnixNano), uint64(span.StartTimeUnixNano); start > 0 && start <= math.MaxInt64 && end <= math.MaxInt64 && end >= start {
					spanAttributes[attr.OTelSpanDurationMSKey] = float64(end-start) / float64(time.Millisecond)
				}
				if scopeSpans.Scope != nil {
					maps.Copy(spanAttributes, s.otlpScopeAttributes(ctx, s.traces, scopeSpans.Scope.Name, scopeSpans.Scope.Version))
				}

				deriveLiteLLMTotalTokens(spanAttributes)
				operation := liteLLMModelOperation(spanAttributes, int32(span.Kind))
				userInfo := telemetry.UserInfoByID("")
				if operation != "unknown" {
					callID, _ := spanAttributes[attr.LiteLLMCallIDKey].(string)
					traceID, _ := spanAttributes[attr.LiteLLMTraceIDKey].(string)
					if conversationID := conv.Default(traceID, callID); conversationID != "" {
						spanAttributes[attr.GenAIConversationIDKey] = conversationID
					}
					if email, _ := spanAttributes[attr.LiteLLMUserEmailKey].(string); email != "" {
						userInfo = telemetry.UserInfoByEmail(email)
					}
				} else {
					stripLiteLLMUsageAttributes(spanAttributes)
				}
				eventURN := liteLLMEventURN(operation)
				spanAttributes[attr.EventURNKey] = eventURN

				params = append(params, telemetry.WithOTELMetadata(telemetry.LogParams{
					Timestamp: timestampFromUnixNano(uint64(span.StartTimeUnixNano), observed),
					ToolInfo: telemetry.ToolInfo{
						ID:             "",
						URN:            litellmOTLPResourceURN,
						Name:           "litellm",
						ProjectID:      projectID,
						DeploymentID:   "",
						FunctionID:     nil,
						OrganizationID: organizationID,
					},
					UserInfo:   userInfo,
					Attributes: spanAttributes,
				}, observed, resourceAttributes))
			}
		}
	}
	return params
}

func (s *Service) otlpScopeAttributes(ctx context.Context, recorder *TraceProcessor, name, version string) map[attr.Key]any {
	result := make(map[attr.Key]any, 2)
	for key, value := range map[attr.Key]string{attr.OTelScopeNameKey: name, attr.OTelScopeVersionKey: version} {
		if value == "" {
			continue
		}
		bounded, changed, keep := boundOTLPAttributeValue(value)
		if keep {
			result[key] = bounded
		}
		if changed {
			recorder.recordTruncatedAttributes(ctx, 1)
		}
	}
	return result
}

func (s *Service) sanitizeOTLPAttributes(ctx context.Context, values []otlpKeyValue, allowlist map[string]otlpAttributeSpec) map[attr.Key]any {
	return s.sanitizeOTLPAttributesWithBudget(ctx, s.traces, values, allowlist, 0)
}

func (s *Service) sanitizeOTLPResourceAttributes(ctx context.Context, values []otlpKeyValue) map[attr.Key]any {
	return s.sanitizeOTLPAttributesWithBudget(ctx, s.traces, values, resourceAttributeAllowlist, maxOTLPResourceBytes)
}

func (s *Service) sanitizeOTLPMetricAttributes(ctx context.Context, values []otlpKeyValue) map[attr.Key]any {
	return s.sanitizeOTLPAttributesWithBudget(ctx, s.metrics.TraceProcessor, values, metricAttributeAllowlist, 0)
}

func (s *Service) sanitizeOTLPMetricResourceAttributes(ctx context.Context, values []otlpKeyValue) map[attr.Key]any {
	return s.sanitizeOTLPAttributesWithBudget(ctx, s.metrics.TraceProcessor, values, metricResourceAttributeAllowlist, maxOTLPResourceBytes)
}

func (s *Service) sanitizeOTLPAttributesWithBudget(ctx context.Context, recorder *TraceProcessor, values []otlpKeyValue, allowlist map[string]otlpAttributeSpec, byteBudget int) map[attr.Key]any {
	result := make(map[attr.Key]any)
	retainedSizes := make(map[attr.Key]int)
	retainedBytes := 2 // JSON object braces; entry accounting is conservatively one byte over for the first key.
	truncated := max(0, len(values)-maxOTLPAttributes)
	for _, value := range values[:min(len(values), maxOTLPAttributes)] {
		spec, allowed := allowlist[value.Key]
		if !allowed {
			continue
		}
		converted, ok := value.Value.value()
		if !ok {
			truncated++
			continue
		}
		if !validOTLPAttributeValue(converted, spec.kind) {
			truncated++
			continue
		}
		encoded, err := json.Marshal(converted)
		if err != nil || len(encoded) > maxOTLPAttributeBytes {
			truncated++
			continue
		}
		entrySize := len(spec.target) + len(encoded) + 4
		previousSize := retainedSizes[spec.target]
		if byteBudget > 0 && retainedBytes-previousSize+entrySize > byteBudget {
			truncated++
			continue
		}
		result[spec.target] = converted
		retainedSizes[spec.target] = entrySize
		retainedBytes += entrySize - previousSize
	}
	recorder.recordTruncatedAttributes(ctx, truncated)
	return result
}

func validOTLPAttributeValue(value any, kind otlpAttributeValueKind) bool {
	switch kind {
	case otlpAttributeString:
		_, ok := value.(string)
		return ok
	case otlpAttributeBool:
		_, ok := value.(bool)
		return ok
	case otlpAttributeInteger:
		number, ok := value.(int64)
		return ok && number >= 0
	case otlpAttributeNumber:
		switch number := value.(type) {
		case int64:
			return number >= 0
		case float64:
			return number >= 0 && !math.IsNaN(number) && !math.IsInf(number, 0)
		default:
			return false
		}
	default:
		return false
	}
}

func boundOTLPAttributeValue(value any) (bounded any, changed bool, ok bool) {
	encoded, err := json.Marshal(value)
	if err == nil && len(encoded) <= maxOTLPAttributeBytes {
		return value, false, true
	}
	return nil, true, false
}

func normalizeOTLPID(value string, length int) (string, bool) {
	if len(value) != length {
		return "", false
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", false
	}
	if strings.Trim(value, "0") == "" {
		return "", false
	}
	return strings.ToLower(value), true
}

func lowCardinalityGenAIOperation(operation string) string {
	switch operation {
	case "chat", "embeddings", "text_completion":
		return operation
	default:
		return "unknown"
	}
}

func liteLLMModelOperation(attributes map[attr.Key]any, kind int32) string {
	operation, _ := attributes[attr.GenAIOperationNameKey].(string)
	operation = lowCardinalityGenAIOperation(operation)
	if operation == "unknown" || tracev1.Span_SpanKind(kind) != tracev1.Span_SPAN_KIND_CLIENT {
		return "unknown"
	}
	for _, key := range []attr.Key{
		attr.GenAIRequestModelKey,
		attr.GenAIResponseModelKey,
		attr.LiteLLMCallIDKey,
		attr.GenAIUsageInputTokensKey,
		attr.GenAIUsageOutputTokensKey,
		attr.GenAIUsageTotalTokensKey,
		attr.GenAIUsageCostKey,
	} {
		if _, ok := attributes[key]; ok {
			return operation
		}
	}
	return "unknown"
}

func liteLLMEventURN(operation string) string {
	return urn.NewTelemetryEvent(urn.TelemetryEventOriginProviderOTEL, urn.TelemetryEventKindSpan, operation).String()
}

func deriveLiteLLMTotalTokens(attributes map[attr.Key]any) {
	if _, exists := attributes[attr.GenAIUsageTotalTokensKey]; exists {
		return
	}
	input, hasInput := attributes[attr.GenAIUsageInputTokensKey].(int64)
	output, hasOutput := attributes[attr.GenAIUsageOutputTokensKey].(int64)
	if (!hasInput && !hasOutput) || input > math.MaxInt64-output {
		return
	}
	attributes[attr.GenAIUsageTotalTokensKey] = input + output
}

func stripLiteLLMUsageAttributes(attributes map[attr.Key]any) {
	for _, key := range []attr.Key{
		attr.GenAIProviderNameKey,
		attr.GenAISystemKey,
		attr.GenAIRequestModelKey,
		attr.GenAIResponseModelKey,
		attr.GenAIResponseIDKey,
		attr.GenAIConversationIDKey,
		attr.GenAIUsageInputTokensKey,
		attr.GenAIUsageOutputTokensKey,
		attr.GenAIUsageTotalTokensKey,
		attr.GenAIUsagePromptTokensKey,
		attr.GenAIUsageCompletionTokensKey,
		attr.GenAIUsageCacheReadInputTokensKey,
		attr.GenAIUsageCacheCreationInputTokensKey,
		attr.GenAIUsageReasoningTokensKey,
		attr.GenAIUsageCostKey,
		attr.GenAIRequestIsStreamingKey,
		attr.LiteLLMInputCostKey,
		attr.LiteLLMOutputCostKey,
		attr.LiteLLMCacheReadCostKey,
		attr.LiteLLMCacheWriteCostKey,
	} {
		delete(attributes, key)
	}
}

func timestampFromUnixNano(value uint64, fallback time.Time) time.Time {
	if value == 0 || value > math.MaxInt64 {
		return fallback
	}
	return time.Unix(0, int64(value)).UTC()
}

func otlpSpanKind(kind int32) string {
	switch tracev1.Span_SpanKind(kind) {
	case tracev1.Span_SPAN_KIND_INTERNAL:
		return "internal"
	case tracev1.Span_SPAN_KIND_SERVER:
		return "server"
	case tracev1.Span_SPAN_KIND_CLIENT:
		return "client"
	case tracev1.Span_SPAN_KIND_PRODUCER:
		return "producer"
	case tracev1.Span_SPAN_KIND_CONSUMER:
		return "consumer"
	default:
		return "unspecified"
	}
}

func otlpStatusCode(status *otlpStatus) string {
	if status == nil {
		return "unset"
	}
	switch tracev1.Status_StatusCode(status.Code) {
	case tracev1.Status_STATUS_CODE_OK:
		return "ok"
	case tracev1.Status_STATUS_CODE_ERROR:
		return "error"
	default:
		return "unset"
	}
}
