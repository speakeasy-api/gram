package dialect

import otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"

type ClaudeCodeSpan struct{}

func (e ClaudeCodeSpan) AppliesTo(span *otelv1.InboundSpan) bool {
	return span.GetScope().GetName() == "com.anthropic.claude_code.tracing"
}

func (e ClaudeCodeSpan) Content(span *otelv1.InboundSpan) (key string, val []string, err error) {
	k, v, err := getOneAttr(span, "user_prompt")
	if err != nil {
		return "", nil, err
	}

	if k == "" || v == "" {
		return "", nil, nil
	}

	return k, []string{v}, err
}

func (e ClaudeCodeSpan) SessionID(span *otelv1.InboundSpan) (key string, val string, err error) {
	return getOneAttr(span, "session.id")
}

func (e ClaudeCodeSpan) ExternalUserEmail(span *otelv1.InboundSpan) (key string, val string, err error) {
	return getOneAttr(span, "user.email")
}

func (e ClaudeCodeSpan) ExternalUserID(span *otelv1.InboundSpan) (key string, val string, err error) {
	return getOneAttr(span, "user.account_id")
}

func (e ClaudeCodeSpan) ResponseID(span *otelv1.InboundSpan) (key string, val string, err error) {
	return getOneAttr(span, "gen_ai.response.id")
}
