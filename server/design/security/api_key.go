package security

import (
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/constants"
	. "goa.design/goa/v3/dsl"
)

var ByKey = APIKeySecurity(constants.KeySecurityScheme, func() {
	Description("key based auth.")
	Scope("consumer", "read and operate project resources (toolsets, MCP servers, install metadata, roles); excludes end-user conversation content")
	Scope("producer", "manage project resources (uploads, deployments, toolsets) and read sensitive data including chat transcripts, telemetry, and risk findings (superset of consumer and chat)")
	Scope("chat", "chat based model usage access")
	Scope("hooks", "hooks based access for Claude Code integrations")
	Scope("agent", "device-agent org install credential: exchanges for per-user device-agent keys, and (as a superset of agent_user) reads the data endpoints")
	Scope("agent_user", "per-user device-agent data credential minted via token-exchange; reads the data endpoints but cannot mint")
})

var ByKeyPayload = func() {
	APIKey(constants.KeySecurityScheme, "apikey_token", String)
}

var ByKeyHeader = func() {
	Header(fmt.Sprintf("apikey_token:%s", constants.APIKeyHeader), String, "API Key header")
}

var ByKeyNamedHeader = func(name string) {
	Header(fmt.Sprintf("apikey_token:%s", name), String, "API Key header")
}
