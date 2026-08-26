package dialect

import (
	"errors"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
)

type MetricFallback struct {
	Candidates []MetricDialect
}

func (f MetricFallback) AppliesTo(metric *otelv1.InboundMetric) bool {
	for _, candidate := range f.Candidates {
		if candidate.AppliesTo(metric) {
			return true
		}
	}
	return false
}

func firstMetricFallback(f MetricFallback, point MetricDataPoint, callback func(MetricDialect, MetricDataPoint) (string, string, error)) (string, string, error) {
	var errs []error
	for _, candidate := range f.Candidates {
		key, value, err := callback(candidate, point)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if key != "" {
			return key, value, nil
		}
	}
	return "", "", errors.Join(errs...)
}

func (f MetricFallback) SessionID(point MetricDataPoint) (string, string, error) {
	return firstMetricFallback(f, point, func(candidate MetricDialect, point MetricDataPoint) (string, string, error) {
		return candidate.SessionID(point)
	})
}

func (f MetricFallback) ExternalUserID(point MetricDataPoint) (string, string, error) {
	return firstMetricFallback(f, point, func(candidate MetricDialect, point MetricDataPoint) (string, string, error) {
		return candidate.ExternalUserID(point)
	})
}

func (f MetricFallback) ExternalUserEmail(point MetricDataPoint) (string, string, error) {
	return firstMetricFallback(f, point, func(candidate MetricDialect, point MetricDataPoint) (string, string, error) {
		return candidate.ExternalUserEmail(point)
	})
}

func (f MetricFallback) ResponseID(point MetricDataPoint) (string, string, error) {
	return firstMetricFallback(f, point, func(candidate MetricDialect, point MetricDataPoint) (string, string, error) {
		return candidate.ResponseID(point)
	})
}
