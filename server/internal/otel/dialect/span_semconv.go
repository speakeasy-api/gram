package dialect

import otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"

type SemconvSpan struct{}

func (e SemconvSpan) AppliesTo(span *otelv1.InboundSpan) bool {
	return true
}

func (e SemconvSpan) Content(span *otelv1.InboundSpan) (string, []string, error) {
	const desired = "gen_ai.input.messages"

	for _, kv := range span.GetAttributes() {
		if kv.GetKey() != desired || !kv.GetValue().HasStringValue() {
			continue
		}

		return desired, []string{kv.GetValue().GetStringValue()}, nil
	}

	return "", nil, nil
}

func (e SemconvSpan) SessionID(span *otelv1.InboundSpan) (key string, val string, err error) {
	const desired = "gen_ai.conversation.id"

	for _, kv := range span.GetAttributes() {
		if kv.GetKey() != desired || !kv.GetValue().HasStringValue() {
			continue
		}

		return desired, kv.GetValue().GetStringValue(), nil
	}

	return "", "", nil
}

func (e SemconvSpan) ExternalUserEmail(span *otelv1.InboundSpan) (key string, val string, err error) {
	const desired = "user.email"

	for _, kv := range span.GetAttributes() {
		if kv.GetKey() != desired || !kv.GetValue().HasStringValue() {
			continue
		}

		return desired, kv.GetValue().GetStringValue(), nil
	}

	return "", "", nil
}

func (e SemconvSpan) ExternalUserID(span *otelv1.InboundSpan) (key string, val string, err error) {
	const desired = "user.id"

	for _, kv := range span.GetAttributes() {
		if kv.GetKey() != desired || !kv.GetValue().HasStringValue() {
			continue
		}

		return desired, kv.GetValue().GetStringValue(), nil
	}

	return "", "", nil
}

func (e SemconvSpan) ResponseID(span *otelv1.InboundSpan) (key string, val string, err error) {
	const desired = "gen_ai.response.id"

	for _, kv := range span.GetAttributes() {
		if kv.GetKey() != desired || !kv.GetValue().HasStringValue() {
			continue
		}

		return desired, kv.GetValue().GetStringValue(), nil
	}

	return "", "", nil
}
