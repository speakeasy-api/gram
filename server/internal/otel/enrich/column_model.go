package enrich

import "github.com/speakeasy-api/gram/server/internal/otel/dialect"

// columnModel fills model: the model the record names. The four API event
// types carry it, since a request, its response, its error and its refusal
// are all about one model call. The two payload captures carry it when the
// producer states it, so a captured request or response body can be
// filtered by model; counting requests still excludes them because they are
// their own types. A prompt, a tool event and a compaction are about the
// session or a tool, not a model, so they are absent.
func columnModel() columnDefinition {
	model := question[string]{log: dialect.LogDialect.Model, span: dialect.SpanDialect.Model}
	return column[string]{
		key: ModelColumnKey,
		byType: columnTable[string]{
			dialect.EventTypeAPIRequest:      model,
			dialect.EventTypeAPIResponse:     model,
			dialect.EventTypeAPIError:        model,
			dialect.EventTypeAPIRefusal:      model,
			dialect.EventTypeAPIRequestBody:  optional(model),
			dialect.EventTypeAPIResponseBody: optional(model),
		},
	}
}
