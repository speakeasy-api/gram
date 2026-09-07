package dialect

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
	"github.com/stretchr/testify/require"
)

func TestLogFallbackUsesSemconvOutput(t *testing.T) {
	t.Parallel()

	record := (&otelv1.InboundLogRecord_builder{
		Scope: (&otelv1.InboundLogRecord_InstrumentationScope_builder{
			Name: new("com.anthropic.claude_code.tracing"),
		}).Build(),
		Attributes: []*otelv1.InboundLogRecord_KeyValue{
			logDialectStringAttribute("gen_ai.output.messages", `[{"role":"assistant","parts":[{"type":"text","content":"done"}],"finish_reason":"stop"}]`),
		},
	}).Build()

	key, output, err := ForLog(record).OutputContent(record)

	require.NoError(t, err)
	require.Equal(t, "gen_ai.output.messages", key)
	require.Equal(t, genaiconv.RoleAssistant, output[0].Role)
}
