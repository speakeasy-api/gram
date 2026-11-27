package orggateway

import "github.com/google/uuid"

// ProjectGatewayMember is one member server that the Project Gateway admits
// for a turn credential, together with the environment slug the gateway will
// use when serving it.
type ProjectGatewayMember struct {
	ServerID            uuid.UUID
	PlatformToolsetSlug string
	EnvironmentSlug     string
}

// ProjectGatewayEvaluation is the result of running Project Gateway
// availability, machine-access, credential admission, and member resolution
// for a single assistant turn credential.
type ProjectGatewayEvaluation struct {
	Admitted   bool
	GatewayURL string
	Reason     string
	Members    []ProjectGatewayMember
}
