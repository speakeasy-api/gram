package security

import (
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/constants"
	. "goa.design/goa/v3/dsl"
)

// DeviceAgentToken is the bearer token a headless device agent obtains by
// exchanging its workload identity at the device-agent token endpoint, and
// presents to the device-agent API in place of an agent API key.
var DeviceAgentToken = JWTSecurity(constants.DeviceAgentTokenSecurityScheme, func() {
	Description("Device-agent token from the workload assertion grant at /oauth/workload/device-agent/{organization_id}/token.")
})

var DeviceAgentTokenPayload = func() {
	Token("device_agent_token", String)
}

var DeviceAgentTokenHeader = func() {
	Header(fmt.Sprintf("device_agent_token:%s", constants.DeviceAgentTokenHeader), String, "Device-agent bearer token")
}
