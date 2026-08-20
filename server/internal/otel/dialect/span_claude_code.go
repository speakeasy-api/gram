package dialect

import otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"

type ClaudeCodeSpan struct{}

func (e ClaudeCodeSpan) AppliesTo(span *otelv1.InboundSpan) bool {
	return span.GetScope().GetName() == "com.anthropic.claude_code.tracing"
}

func (e ClaudeCodeSpan) Content(span *otelv1.InboundSpan) (key string, val []string, err error) {
	k, v := getOneAttr(span, "user_prompt")
	if k == "" || v == "" {
		return "", nil, nil
	}

	return k, []string{v}, nil
}

func (e ClaudeCodeSpan) SessionID(span *otelv1.InboundSpan) (key string, val string, err error) {
	key, val = getOneAttr(span, "session.id")
	return key, val, nil
}

func (e ClaudeCodeSpan) ExternalUserEmail(span *otelv1.InboundSpan) (key string, val string, err error) {
	key, val = getOneAttr(span, "user.email")
	return key, val, nil
}

func (e ClaudeCodeSpan) ExternalUserID(span *otelv1.InboundSpan) (key string, val string, err error) {
	key, val = getOneAttr(span, "user.account_id")
	return key, val, nil
}

func (e ClaudeCodeSpan) ResponseID(span *otelv1.InboundSpan) (key string, val string, err error) {
	key, val = getOneAttr(span, "gen_ai.response.id")
	return key, val, nil
}
