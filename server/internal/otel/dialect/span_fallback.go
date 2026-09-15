package dialect

import (
	"errors"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
)

type Fallback struct {
	Candidates []SpanDialect
}

func (f Fallback) AppliesTo(span *otelv1.InboundSpan) bool {
	for _, d := range f.Candidates {
		if d.AppliesTo(span) {
			return true
		}
	}

	return false
}

func firstFallback[V any](f Fallback, span *otelv1.InboundSpan, cb func(d SpanDialect, span *otelv1.InboundSpan) (string, V, error)) (key string, val V, err error) {
	var zero V
	var errs error

	for _, d := range f.Candidates {
		key, val, err = cb(d, span)

		errs = errors.Join(errs, err)

		if err == nil && key != "" {
			return
		}
	}

	if errs != nil {
		return "", zero, errs
	}

	return
}

func (f Fallback) SessionID(span *otelv1.InboundSpan) (string, string, error) {
	return firstFallback(f, span, func(d SpanDialect, span *otelv1.InboundSpan) (string, string, error) {
		return d.SessionID(span)
	})
}

func (f Fallback) InputContent(span *otelv1.InboundSpan) (key string, val genaiconv.InputMessages, err error) {
	return firstFallback(f, span, func(d SpanDialect, span *otelv1.InboundSpan) (string, genaiconv.InputMessages, error) {
		return d.InputContent(span)
	})
}

func (f Fallback) OutputContent(span *otelv1.InboundSpan) (key string, val genaiconv.OutputMessages, err error) {
	return firstFallback(f, span, func(d SpanDialect, span *otelv1.InboundSpan) (string, genaiconv.OutputMessages, error) {
		return d.OutputContent(span)
	})
}

func (f Fallback) ExternalUserEmail(span *otelv1.InboundSpan) (string, string, error) {
	return firstFallback(f, span, func(d SpanDialect, span *otelv1.InboundSpan) (string, string, error) {
		return d.ExternalUserEmail(span)
	})
}

func (f Fallback) ExternalUserID(span *otelv1.InboundSpan) (string, string, error) {
	return firstFallback(f, span, func(d SpanDialect, span *otelv1.InboundSpan) (string, string, error) {
		return d.ExternalUserID(span)
	})
}

func (f Fallback) ResponseID(span *otelv1.InboundSpan) (string, string, error) {
	return firstFallback(f, span, func(d SpanDialect, span *otelv1.InboundSpan) (string, string, error) {
		return d.ResponseID(span)
	})
}
