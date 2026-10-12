package enrich

// Instrumentation scope names of the Speakeasy producers that write into the
// pipeline. A record's channel is derived from the scope it arrived under.
const (
	GatewayScopeName = "github.com/speakeasy-api/gram/server/internal/mcp"
	HooksScopeName   = "github.com/speakeasy-api/gram/server/internal/hooks"
	ChatScopeName    = "github.com/speakeasy-api/gram/server/internal/chatevents"
)

// The lanes a record can enter through, as stored in agent_events.channel.
const (
	ChannelOTel    = "otel"
	ChannelHooks   = "hooks"
	ChannelGateway = "gateway"
	ChannelChat    = "chat"
)

// Channel is the lane a record entered through, derived from the producer's
// original instrumentation scope name. Anything that is not one of the
// Speakeasy producers, including an empty scope, is a producer's own OTel
// export.
func Channel(scopeName string) string {
	switch scopeName {
	case GatewayScopeName:
		return ChannelGateway
	case HooksScopeName:
		return ChannelHooks
	case ChatScopeName:
		return ChannelChat
	}
	return ChannelOTel
}
