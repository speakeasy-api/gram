package litellm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	maxOTLPAttributes      = 128
	maxOTLPAttributeBytes  = 64 * 1024
	maxOTLPSpansPerExport  = 1000
	litellmOTLPResourceURN = "litellm:otel:traces"
)

type otlpAttributeValueKind uint8

const (
	otlpAttributeString otlpAttributeValueKind = iota
	otlpAttributeBool
	otlpAttributeInteger
	otlpAttributeNumber
	otlpAttributeStringArray
)

type otlpAttributeSpec struct {
	target attribute.Key
	kind   otlpAttributeValueKind
}

var spanAttributeAllowlist = map[string]otlpAttributeSpec{
	"gen_ai.operation.name":                    {target: attr.GenAIOperationNameKey, kind: otlpAttributeString},
	"gen_ai.provider.name":                     {target: attr.GenAIProviderNameKey, kind: otlpAttributeString},
	"gen_ai.system":                            {target: attribute.Key("gen_ai.system"), kind: otlpAttributeString},
	"gen_ai.request.model":                     {target: attr.GenAIRequestModelKey, kind: otlpAttributeString},
	"gen_ai.response.model":                    {target: attr.GenAIResponseModelKey, kind: otlpAttributeString},
	"gen_ai.response.id":                       {target: attr.GenAIResponseIDKey, kind: otlpAttributeString},
	"gen_ai.response.finish_reasons":           {target: attr.GenAIResponseFinishReasonsKey, kind: otlpAttributeStringArray},
	"gen_ai.conversation.id":                   {target: attr.GenAIConversationIDKey, kind: otlpAttributeString},
	"gen_ai.usage.input_tokens":                {target: attr.GenAIUsageInputTokensKey, kind: otlpAttributeInteger},
	"gen_ai.usage.output_tokens":               {target: attr.GenAIUsageOutputTokensKey, kind: otlpAttributeInteger},
	"gen_ai.usage.total_tokens":                {target: attr.GenAIUsageTotalTokensKey, kind: otlpAttributeInteger},
	"gen_ai.usage.prompt_tokens":               {target: attribute.Key("gen_ai.usage.prompt_tokens"), kind: otlpAttributeInteger},
	"gen_ai.usage.completion_tokens":           {target: attribute.Key("gen_ai.usage.completion_tokens"), kind: otlpAttributeInteger},
	"gen_ai.usage.cache_read.input_tokens":     {target: attr.GenAIUsageCacheReadInputTokensKey, kind: otlpAttributeInteger},
	"gen_ai.usage.cache_creation.input_tokens": {target: attr.GenAIUsageCacheCreationInputTokensKey, kind: otlpAttributeInteger},
	"gen_ai.usage.reasoning_tokens":            {target: attr.GenAIUsageReasoningTokensKey, kind: otlpAttributeInteger},
	"gen_ai.usage.cost":                        {target: attr.GenAIUsageCostKey, kind: otlpAttributeNumber},
	"gen_ai.request.is_streaming":              {target: attribute.Key("gen_ai.request.is_streaming"), kind: otlpAttributeBool},
	"gen_ai.request.streaming":                 {target: attribute.Key("gen_ai.request.streaming"), kind: otlpAttributeBool},
	"litellm.is_streaming":                     {target: attribute.Key("litellm.is_streaming"), kind: otlpAttributeBool},
	"litellm.response.cost":                    {target: attribute.Key("litellm.response.cost"), kind: otlpAttributeNumber},
	"litellm.call_id":                          {target: attr.LiteLLMCallIDKey, kind: otlpAttributeString},
	"litellm_call_id":                          {target: attr.LiteLLMCallIDKey, kind: otlpAttributeString},
	"litellm.trace_id":                         {target: attr.LiteLLMTraceIDKey, kind: otlpAttributeString},
	"litellm_trace_id":                         {target: attr.LiteLLMTraceIDKey, kind: otlpAttributeString},
	"litellm.user_id":                          {target: attr.LiteLLMUserIDKey, kind: otlpAttributeString},
	"user_api_key_user_id":                     {target: attr.LiteLLMUserIDKey, kind: otlpAttributeString},
	"metadata.user_api_key_user_id":            {target: attr.LiteLLMUserIDKey, kind: otlpAttributeString},
	"litellm.user_email":                       {target: attr.LiteLLMUserEmailKey, kind: otlpAttributeString},
	"user_api_key_user_email":                  {target: attr.LiteLLMUserEmailKey, kind: otlpAttributeString},
	"metadata.user_api_key_user_email":         {target: attr.LiteLLMUserEmailKey, kind: otlpAttributeString},
	"litellm.team_id":                          {target: attr.LiteLLMTeamIDKey, kind: otlpAttributeString},
	"user_api_key_team_id":                     {target: attr.LiteLLMTeamIDKey, kind: otlpAttributeString},
	"metadata.user_api_key_team_id":            {target: attr.LiteLLMTeamIDKey, kind: otlpAttributeString},
	"litellm.team_alias":                       {target: attr.LiteLLMTeamAliasKey, kind: otlpAttributeString},
	"user_api_key_team_alias":                  {target: attr.LiteLLMTeamAliasKey, kind: otlpAttributeString},
	"metadata.user_api_key_team_alias":         {target: attr.LiteLLMTeamAliasKey, kind: otlpAttributeString},
	"litellm.org_id":                           {target: attr.LiteLLMOrganizationIDKey, kind: otlpAttributeString},
	"litellm.organization_id":                  {target: attr.LiteLLMOrganizationIDKey, kind: otlpAttributeString},
	"user_api_key_org_id":                      {target: attr.LiteLLMOrganizationIDKey, kind: otlpAttributeString},
	"metadata.user_api_key_org_id":             {target: attr.LiteLLMOrganizationIDKey, kind: otlpAttributeString},
	"user_api_key_end_user_id":                 {target: attr.LiteLLMEndUserIDKey, kind: otlpAttributeString},
	"metadata.user_api_key_end_user_id":        {target: attr.LiteLLMEndUserIDKey, kind: otlpAttributeString},
}

var resourceAttributeAllowlist = map[string]otlpAttributeSpec{
	"service.name":                {target: attr.ServiceNameKey, kind: otlpAttributeString},
	"service.namespace":           {target: attribute.Key("service.namespace"), kind: otlpAttributeString},
	"service.version":             {target: attr.ServiceVersionKey, kind: otlpAttributeString},
	"service.instance.id":         {target: attribute.Key("service.instance.id"), kind: otlpAttributeString},
	"deployment.environment":      {target: attribute.Key("deployment.environment"), kind: otlpAttributeString},
	"deployment.environment.name": {target: attribute.Key("deployment.environment.name"), kind: otlpAttributeString},
	"telemetry.sdk.name":          {target: attribute.Key("telemetry.sdk.name"), kind: otlpAttributeString},
	"telemetry.sdk.language":      {target: attribute.Key("telemetry.sdk.language"), kind: otlpAttributeString},
	"telemetry.sdk.version":       {target: attribute.Key("telemetry.sdk.version"), kind: otlpAttributeString},
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
	value := strings.Trim(string(data), `"`)
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return fmt.Errorf("parse OTLP unsigned integer: %w", err)
	}
	*v = jsonUint64(parsed)
	return nil
}

type jsonFloat64 float64

func (v *jsonFloat64) UnmarshalJSON(data []byte) error {
	value := strings.Trim(string(data), `"`)
	var parsed float64
	switch value {
	case "NaN":
		parsed = math.NaN()
	case "Infinity":
		parsed = math.Inf(1)
	case "-Infinity":
		parsed = math.Inf(-1)
	default:
		var err error
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
	value := strings.Trim(string(data), `"`)
	parsed, err := strconv.ParseInt(value, 10, bitSize)
	if err != nil {
		return 0, fmt.Errorf("parse OTLP integer: %w", err)
	}
	return parsed, nil
}

func decodeOTLPJSON(body []byte) (*otlpExportRequest, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	var request otlpExportRequest
	if err := decoder.Decode(&request); err != nil {
		return nil, fmt.Errorf("decode OTLP JSON: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
	}
	if err := request.validateAnyValues(); err != nil {
		return nil, err
	}
	return &request, nil
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
	if count != 1 {
		return fmt.Errorf("AnyValue must contain exactly one value field")
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
			resourceAttributes = s.sanitizeOTLPAttributes(ctx, resourceSpans.Resource.Attributes, resourceAttributeAllowlist)
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
					spanAttributes[attribute.Key("span.parent_id")] = parentID
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
					spanAttributes[attribute.Key("otel.span.name")] = spanName
				}
				spanAttributes[attribute.Key("otel.span.kind")] = otlpSpanKind(int32(span.Kind))
				spanAttributes[attribute.Key("otel.span.status_code")] = otlpStatusCode(span.Status)
				spanAttributes[attribute.Key("otel.span.start_time_unix_nano")] = uint64(span.StartTimeUnixNano)
				spanAttributes[attribute.Key("otel.span.end_time_unix_nano")] = uint64(span.EndTimeUnixNano)
				if end, start := uint64(span.EndTimeUnixNano), uint64(span.StartTimeUnixNano); end >= start {
					spanAttributes[attribute.Key("otel.span.duration_ms")] = float64(end-start) / float64(time.Millisecond)
				}
				if scopeSpans.Scope != nil {
					if scopeSpans.Scope.Name != "" {
						name, changed, keep := boundOTLPAttributeValue(scopeSpans.Scope.Name)
						if keep {
							spanAttributes[attribute.Key("otel.scope.name")] = name
						}
						if changed {
							s.traces.recordTruncatedAttributes(ctx, 1)
						}
					}
					if scopeSpans.Scope.Version != "" {
						version, changed, keep := boundOTLPAttributeValue(scopeSpans.Scope.Version)
						if keep {
							spanAttributes[attribute.Key("otel.scope.version")] = version
						}
						if changed {
							s.traces.recordTruncatedAttributes(ctx, 1)
						}
					}
				}

				operation, _ := spanAttributes[attr.GenAIOperationNameKey].(string)
				operation = lowCardinalityGenAIOperation(operation)
				eventURN := urn.NewTelemetryEvent(urn.TelemetryEventOriginProviderOTEL, urn.TelemetryEventKindSpan, operation).String()
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
					UserInfo:   telemetry.UserInfoByID(""),
					Attributes: spanAttributes,
				}, observed, resourceAttributes))
			}
		}
	}
	return params
}

func (s *Service) sanitizeOTLPAttributes(ctx context.Context, values []otlpKeyValue, allowlist map[string]otlpAttributeSpec) map[attr.Key]any {
	result := make(map[attr.Key]any)
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
		bounded, changed, ok := boundOTLPAttributeValue(converted)
		if changed {
			truncated++
		}
		if ok {
			result[spec.target] = bounded
		}
	}
	s.traces.recordTruncatedAttributes(ctx, truncated)
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
		_, ok := value.(int64)
		return ok
	case otlpAttributeNumber:
		switch number := value.(type) {
		case int64:
			return true
		case float64:
			return !math.IsNaN(number) && !math.IsInf(number, 0)
		default:
			return false
		}
	case otlpAttributeStringArray:
		values, ok := value.([]any)
		if !ok {
			return false
		}
		for _, item := range values {
			if _, ok := item.(string); !ok {
				return false
			}
		}
		return true
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

func countOTLPSpans(request *otlpExportRequest) int {
	count := 0
	for _, resourceSpans := range request.ResourceSpans {
		for _, scopeSpans := range resourceSpans.ScopeSpans {
			count += len(scopeSpans.Spans)
		}
	}
	return count
}

func lowCardinalityGenAIOperation(operation string) string {
	switch operation {
	case "chat", "embeddings", "text_completion":
		return operation
	default:
		return "unknown"
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
