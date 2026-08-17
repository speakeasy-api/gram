package dialect

import otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"

type NilSpan struct{}

func (e NilSpan) AppliesTo(span *otelv1.InboundSpan) bool {
	return false
}

func (e NilSpan) Content(span *otelv1.InboundSpan) (key string, val []string, err error) {
	return
}

func (e NilSpan) SessionID(span *otelv1.InboundSpan) (key string, val string, err error) {
	return
}

func (e NilSpan) ExternalUserEmail(span *otelv1.InboundSpan) (key string, val string, err error) {
	return
}

func (e NilSpan) ExternalUserID(span *otelv1.InboundSpan) (key string, val string, err error) {
	return
}

func (e NilSpan) ResponseID(span *otelv1.InboundSpan) (key string, val string, err error) {
	return
}
