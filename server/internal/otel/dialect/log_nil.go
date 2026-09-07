package dialect

import (
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
)

type NilLog struct{}

func (NilLog) AppliesTo(*otelv1.InboundLogRecord) bool { return false }
func (NilLog) InputContent(*otelv1.InboundLogRecord) (string, genaiconv.InputMessages, error) {
	return "", nil, nil
}
func (NilLog) OutputContent(*otelv1.InboundLogRecord) (string, genaiconv.OutputMessages, error) {
	return "", nil, nil
}
func (NilLog) SessionID(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}
func (NilLog) ExternalUserID(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}
func (NilLog) ExternalUserEmail(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}
func (NilLog) ResponseID(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}
