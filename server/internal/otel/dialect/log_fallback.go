package dialect

import (
	"errors"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
)

type LogFallback struct {
	Candidates []LogDialect
}

func (f LogFallback) AppliesTo(record *otelv1.InboundLogRecord) bool {
	for _, candidate := range f.Candidates {
		if candidate.AppliesTo(record) {
			return true
		}
	}
	return false
}

func firstLogFallback[V any](f LogFallback, record *otelv1.InboundLogRecord, callback func(LogDialect, *otelv1.InboundLogRecord) (string, V, error)) (string, V, error) {
	var zero V
	var errs []error
	for _, candidate := range f.Candidates {
		key, value, err := callback(candidate, record)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if key != "" {
			return key, value, nil
		}
	}
	return "", zero, errors.Join(errs...)
}

func (f LogFallback) InputContent(record *otelv1.InboundLogRecord) (string, genaiconv.InputMessages, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, genaiconv.InputMessages, error) {
		return d.InputContent(r)
	})
}

func (f LogFallback) OutputContent(record *otelv1.InboundLogRecord) (string, genaiconv.OutputMessages, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, genaiconv.OutputMessages, error) {
		return d.OutputContent(r)
	})
}

func (f LogFallback) SessionID(record *otelv1.InboundLogRecord) (string, string, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, string, error) {
		return d.SessionID(r)
	})
}

func (f LogFallback) ExternalUserID(record *otelv1.InboundLogRecord) (string, string, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, string, error) {
		return d.ExternalUserID(r)
	})
}

func (f LogFallback) ExternalUserEmail(record *otelv1.InboundLogRecord) (string, string, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, string, error) {
		return d.ExternalUserEmail(r)
	})
}

func (f LogFallback) ResponseID(record *otelv1.InboundLogRecord) (string, string, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, string, error) {
		return d.ResponseID(r)
	})
}
