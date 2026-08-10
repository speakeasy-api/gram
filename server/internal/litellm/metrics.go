package litellm

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/codes"
	collectorv1 "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	metricsv1 "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	litellmOTLPMetricsURN        = "litellm:otel:metrics"
	maxOTLPMetricsPerExport      = 64
	maxOTLPMetricPointsPerExport = 512
	maxOTLPHistogramBuckets      = 256
	maxOTLPMetricFutureSkew      = 5 * time.Minute
)

var (
	errTooManyOTLPMetrics = errors.New("OTLP metric export exceeds collection limits")
	litellmMetricEventURN = urn.NewTelemetryEvent(urn.TelemetryEventOriginProviderOTEL, urn.TelemetryEventKindMetric, "litellm").String()
	litellmMetricNames    = map[string]struct{}{
		"gen_ai.client.operation.duration":             {},
		"gen_ai.client.token.usage":                    {},
		"gen_ai.client.token.cost":                     {},
		"gen_ai.client.response.time_to_first_token":   {},
		"gen_ai.client.response.time_per_output_token": {},
		"gen_ai.client.response.duration":              {},
	}
)

func (s *Service) metricHTTPHandler() http.Handler {
	return oops.ErrHandle(s.logger, s.serveMetricsHTTP)
}

func (s *Service) serveMetricsHTTP(w http.ResponseWriter, r *http.Request) (retErr error) {
	ctx, span := s.tracer.Start(r.Context(), "litellm.metrics")
	defer func() {
		if retErr != nil {
			span.SetStatus(codes.Error, retErr.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}()

	ctx, err := s.authenticateOTLPRequest(ctx, r.Header)
	if err != nil {
		return err
	}
	version := ""
	defer func() {
		s.health.Record(ctx, healthSignalOTEL, version, retErr)
	}()
	mediaType, body, err := readOTLPRequest(r)
	if err != nil {
		return err
	}
	request, err := decodeMetricExport(body, mediaType)
	if err != nil {
		return metricExportError(err)
	}
	version = metricReportedVersion(request)
	if err := s.ingestMetricExport(ctx, request); err != nil {
		return err
	}
	w.WriteHeader(http.StatusAccepted)
	return nil
}

func metricReportedVersion(request *collectorv1.ExportMetricsServiceRequest) string {
	if request == nil {
		return ""
	}
	for _, resourceMetrics := range request.GetResourceMetrics() {
		name := protoStringAttribute(resourceMetrics.GetResource().GetAttributes(), "service.name")
		version := protoStringAttribute(resourceMetrics.GetResource().GetAttributes(), "service.version")
		if version != "" && strings.EqualFold(name, "litellm") {
			return version
		}
		for _, scopeMetrics := range resourceMetrics.GetScopeMetrics() {
			scope := scopeMetrics.GetScope()
			if scope != nil && strings.Contains(strings.ToLower(scope.GetName()), "litellm") && strings.TrimSpace(scope.GetVersion()) != "" {
				return scope.GetVersion()
			}
		}
	}
	return ""
}

func protoStringAttribute(values []*commonv1.KeyValue, key string) string {
	for _, value := range values {
		if value.GetKey() == key {
			return strings.TrimSpace(value.GetValue().GetStringValue())
		}
	}
	return ""
}

func decodeMetricExport(body []byte, mediaType string) (*collectorv1.ExportMetricsServiceRequest, error) {
	request := &collectorv1.ExportMetricsServiceRequest{ResourceMetrics: nil}
	var err error
	if mediaType == "application/json" {
		var options protojson.UnmarshalOptions
		options.DiscardUnknown = true
		options.RecursionLimit = maxOTLPAnyValueDepth
		err = options.Unmarshal(body, request)
	} else {
		err = proto.Unmarshal(body, request)
	}
	if err != nil {
		return nil, fmt.Errorf("decode OTLP metric export: %w", err)
	}
	if err := validateMetricExport(request); err != nil {
		return nil, err
	}
	return request, nil
}

func metricExportError(err error) error {
	if errors.Is(err, errTooManyOTLPMetrics) {
		return oops.E(oops.CodeRequestTooLarge, err, "OTLP metric export exceeds collection limits")
	}
	return oops.E(oops.CodeBadRequest, err, "invalid OTLP metric export")
}

func (s *Service) ingestMetricExport(ctx context.Context, request *collectorv1.ExportMetricsServiceRequest) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.E(oops.CodeUnauthorized, nil, "unauthorized")
	}
	params := s.metricLogParams(ctx, request, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())
	if len(params) == 0 {
		return nil
	}
	enrichAcceptedTelemetryAttribution(ctx, s.instances, authCtx, params)
	s.metrics.Enqueue(ctx, params)
	return nil
}

func validateMetricExport(request *collectorv1.ExportMetricsServiceRequest) error {
	resources, scopes, instruments, points := len(request.GetResourceMetrics()), 0, 0, 0
	for _, resourceMetrics := range request.GetResourceMetrics() {
		scopes += len(resourceMetrics.GetScopeMetrics())
		for _, scopeMetrics := range resourceMetrics.GetScopeMetrics() {
			instruments += len(scopeMetrics.GetMetrics())
			for _, metric := range scopeMetrics.GetMetrics() {
				points += metricDataPointCount(metric)
				histogram := metric.GetHistogram()
				if histogram == nil {
					continue
				}
				for _, point := range histogram.GetDataPoints() {
					if point == nil {
						continue
					}
					if len(point.GetBucketCounts()) > maxOTLPHistogramBuckets || len(point.GetExplicitBounds()) > maxOTLPHistogramBuckets {
						return fmt.Errorf("%w: histogram has too many buckets", errTooManyOTLPMetrics)
					}
					if err := validateHistogramPoint(point); err != nil {
						return err
					}
				}
			}
		}
	}
	if resources > maxOTLPResourceGroups || scopes > maxOTLPScopeGroups || instruments > maxOTLPMetricsPerExport || points > maxOTLPMetricPointsPerExport {
		return fmt.Errorf("%w: resources=%d scopes=%d instruments=%d points=%d", errTooManyOTLPMetrics, resources, scopes, instruments, points)
	}
	return nil
}

func validateHistogramPoint(point *metricsv1.HistogramDataPoint) error {
	counts, bounds := point.GetBucketCounts(), point.GetExplicitBounds()
	if len(counts) > 0 && len(counts) != len(bounds)+1 {
		return fmt.Errorf("histogram bucket count does not match explicit bounds")
	}
	var total uint64
	for _, count := range counts {
		if count > math.MaxUint64-total {
			return fmt.Errorf("histogram bucket count overflows")
		}
		total += count
	}
	if len(counts) > 0 && total != point.GetCount() {
		return fmt.Errorf("histogram buckets do not match point count")
	}
	for index, bound := range bounds {
		if !finite(bound) || index > 0 && bound <= bounds[index-1] {
			return fmt.Errorf("histogram explicit bounds are invalid")
		}
	}
	if (point.Min != nil && !finite(*point.Min)) || (point.Max != nil && !finite(*point.Max)) {
		return fmt.Errorf("histogram min and max are invalid")
	}
	if point.Min != nil && point.Max != nil && *point.Min > *point.Max {
		return fmt.Errorf("histogram min and max are invalid")
	}
	return nil
}

func metricDataPointCount(metric *metricsv1.Metric) int {
	switch {
	case metric.GetGauge() != nil:
		return len(metric.GetGauge().GetDataPoints())
	case metric.GetSum() != nil:
		return len(metric.GetSum().GetDataPoints())
	case metric.GetHistogram() != nil:
		return len(metric.GetHistogram().GetDataPoints())
	case metric.GetExponentialHistogram() != nil:
		return len(metric.GetExponentialHistogram().GetDataPoints())
	case metric.GetSummary() != nil:
		return len(metric.GetSummary().GetDataPoints())
	default:
		return 0
	}
}

func (s *Service) metricLogParams(ctx context.Context, request *collectorv1.ExportMetricsServiceRequest, organizationID, projectID string) []telemetry.LogParams {
	observed := time.Now().UTC()
	params := make([]telemetry.LogParams, 0)
	toolInfo := telemetry.ToolInfo{
		ID:             "",
		URN:            litellmOTLPMetricsURN,
		Name:           "litellm",
		ProjectID:      projectID,
		DeploymentID:   "",
		FunctionID:     nil,
		OrganizationID: organizationID,
	}
	for _, resourceMetrics := range request.GetResourceMetrics() {
		resourceAttrs := map[attr.Key]any{}
		if resource := resourceFromProto(resourceMetrics.GetResource()); resource != nil {
			resourceAttrs = s.sanitizeOTLPMetricResourceAttributes(ctx, resource.Attributes)
		}
		for _, scopeMetrics := range resourceMetrics.GetScopeMetrics() {
			scopeAttrs := map[attr.Key]any{}
			if scope := scopeMetrics.GetScope(); scope != nil {
				scopeAttrs = s.otlpScopeAttributes(ctx, s.metrics.TraceProcessor, scope.GetName(), scope.GetVersion())
			}
			for _, metric := range scopeMetrics.GetMetrics() {
				if _, supported := litellmMetricNames[metric.GetName()]; !supported {
					continue
				}
				histogram := metric.GetHistogram()
				if histogram == nil {
					continue
				}
				for _, point := range histogram.GetDataPoints() {
					if point == nil {
						continue
					}
					attrs := s.sanitizeOTLPMetricAttributes(ctx, keyValuesFromProto(point.GetAttributes()))
					attrs[attr.HookSourceKey] = "litellm"
					attrs[attr.EventSourceKey] = string(telemetry.EventSourceHook)
					attrs[attr.ResourceURNKey] = litellmOTLPMetricsURN
					attrs[attr.EventURNKey] = litellmMetricEventURN
					attrs[attr.MetricNameKey] = metric.GetName()
					unit, changed, keep := boundOTLPAttributeValue(metric.GetUnit())
					if keep {
						attrs[attr.Key("metric.unit")] = unit
					}
					if changed {
						s.metrics.recordTruncatedAttributes(ctx, 1)
					}
					attrs[attr.Key("metric.aggregation_temporality")] = histogram.GetAggregationTemporality().String()
					attrs[attr.Key("metric.start_time_unix_nano")] = point.GetStartTimeUnixNano()
					attrs[attr.Key("metric.time_unix_nano")] = point.GetTimeUnixNano()
					attrs[attr.Key("metric.count")] = point.GetCount()
					if sum := point.Sum; sum != nil && finite(*sum) {
						attrs[attr.Key("metric.sum")] = *sum
						attrs[attr.Key("metric.value")] = *sum
					}
					if minValue := point.Min; minValue != nil && finite(*minValue) {
						attrs[attr.Key("metric.min")] = *minValue
					}
					if maxValue := point.Max; maxValue != nil && finite(*maxValue) {
						attrs[attr.Key("metric.max")] = *maxValue
					}
					if len(point.GetBucketCounts()) > 0 {
						attrs[attr.Key("metric.bucket_counts")] = point.GetBucketCounts()
					}
					if len(point.GetExplicitBounds()) > 0 && finiteSlice(point.GetExplicitBounds()) {
						attrs[attr.Key("metric.explicit_bounds")] = point.GetExplicitBounds()
					}
					maps.Copy(attrs, scopeAttrs)
					params = append(params, telemetry.WithOTELMetadata(telemetry.LogParams{
						Timestamp:  metricTimestamp(point.GetTimeUnixNano(), observed),
						ToolInfo:   toolInfo,
						UserInfo:   telemetry.UserInfoByID(""),
						Attributes: attrs,
					}, observed, resourceAttrs))
				}
			}
		}
	}
	return params
}

func metricTimestamp(unixNano uint64, observed time.Time) time.Time {
	timestamp := timestampFromUnixNano(unixNano, observed)
	if timestamp.After(observed.Add(maxOTLPMetricFutureSkew)) {
		return observed
	}
	return timestamp
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func finiteSlice(values []float64) bool {
	for _, value := range values {
		if !finite(value) {
			return false
		}
	}
	return true
}
