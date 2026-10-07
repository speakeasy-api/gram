package enrich

import "github.com/speakeasy-api/gram/server/internal/otel/dialect"

// columnModel fills model: the model the record names. The four API event
// types carry it and are Required, since a request, its response, its error
// and its refusal are all about one model call. The two payload captures
// are Recommended: a capture states the model only sometimes, so a captured
// request or response body can be filtered by model when it does and its
// absence is not counted; counting requests still excludes captures because
// they are their own types. A prompt, a tool event and a compaction are
// about the session or a tool, not a model, so they are absent.
func columnModel() columnDefinition {
	model := getter[string]{log: dialect.LogDialect.Model, span: dialect.SpanDialect.Model}
	return column[string]{
		key: ModelColumnKey,
		byType: perEventType[string]{
			dialect.EventTypeAPIRequest:      model,
			dialect.EventTypeAPIResponse:     model,
			dialect.EventTypeAPIError:        model,
			dialect.EventTypeAPIRefusal:      model,
			dialect.EventTypeAPIRequestBody:  recommended(model),
			dialect.EventTypeAPIResponseBody: recommended(model),
		},
	}
}
