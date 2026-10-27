package dialect

import (
	"testing"

	"github.com/stretchr/testify/require"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/aivendors"
)

// A dialect that names its producer by a constant must name a product
// aivendors knows; hooks and the gateway can only return what Identify does.
func TestEveryDialectIdentityIsAnAIVendorsProduct(t *testing.T) {
	t.Parallel()

	products := map[string]bool{}
	for _, product := range aivendors.Products() {
		products[product.ID] = true
	}

	logRecord := func(scope string, attributes ...*otelv1.InboundLogRecord_KeyValue) *otelv1.InboundLogRecord {
		return (&otelv1.InboundLogRecord_builder{
			Scope:      (&otelv1.InboundLogRecord_InstrumentationScope_builder{Name: new(scope)}).Build(),
			Attributes: attributes,
		}).Build()
	}
	span := (&otelv1.InboundSpan_builder{
		Scope: (&otelv1.InboundSpan_InstrumentationScope_builder{Name: new("com.anthropic.claude_code")}).Build(),
	}).Build()

	tests := []struct {
		name     string
		identity func() (string, aivendors.Identity, error)
	}{
		{name: "it names claude code on a log", identity: func() (string, aivendors.Identity, error) {
			return ClaudeCodeLog{}.Identity(logRecord("com.anthropic.claude_code.events"))
		}},
		{name: "it names codex on a log", identity: func() (string, aivendors.Identity, error) {
			return CodexLog{}.Identity(logRecord("codex_otel"))
		}},
		{name: "it names claude code on a span", identity: func() (string, aivendors.Identity, error) {
			return ClaudeCodeSpan{}.Identity(span)
		}},
		{name: "it names the hook source's product", identity: func() (string, aivendors.Identity, error) {
			return HookLog{SemconvLog: SemconvLog{}}.Identity(logRecord(HooksLogScope, logDialectStringAttribute(hookSourceKey, "cowork")))
		}},
		{name: "it names the MCP client's product", identity: func() (string, aivendors.Identity, error) {
			return GatewayLog{SemconvLog: SemconvLog{}}.Identity(logRecord(GatewayLogScope, logDialectStringAttribute(gatewayMCPClientNameKey, "cursor-vscode")))
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			key, identity, err := tt.identity()
			require.NoError(t, err)
			require.NotEmpty(t, key)
			require.True(t, products[identity.ProductID], "%q is not an aivendors product", identity.ProductID)
		})
	}
}

func TestHookAndGatewayIdentityIsEmptyForAnUnknownName(t *testing.T) {
	t.Parallel()

	record := (&otelv1.InboundLogRecord_builder{
		Scope: (&otelv1.InboundLogRecord_InstrumentationScope_builder{Name: new(HooksLogScope)}).Build(),
		Attributes: []*otelv1.InboundLogRecord_KeyValue{
			logDialectStringAttribute(hookSourceKey, "my-custom-agent-7"),
			logDialectStringAttribute(gatewayMCPClientNameKey, "my-custom-agent-7"),
		},
	}).Build()

	key, identity, err := HookLog{SemconvLog: SemconvLog{}}.Identity(record)
	require.NoError(t, err)
	require.Empty(t, key, "an unknown hook source states no identity")
	require.Empty(t, identity.ProductID)

	key, identity, err = GatewayLog{SemconvLog: SemconvLog{}}.Identity(record)
	require.NoError(t, err)
	require.Empty(t, key, "an unknown MCP client states no identity")
	require.Empty(t, identity.ProductID)
}
