package agent

import (
	"context"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/deviceagentversions"
	"github.com/speakeasy-api/gram/server/internal/deviceidentity"
)

// SetAgentVersions enables recording each polling agent's speakeasyd version.
func (s *Service) SetAgentVersions(store *deviceagentversions.Store) { s.agentVersions = store }

// agentMachine is the identity one installation is counted under: its hardware
// serial when the endpoint reports one, otherwise the environment and the
// enrolled email, matching how the sync tables tell machines apart.
func agentMachine(payload *gen.GetPluginsPayload, email, environment string) string {
	if serial := deviceidentity.NormalizeSerial(payload.SerialNumber); serial != "" && environment == deviceidentity.EnvironmentEndpoint {
		return "serial:" + serial
	}
	return environment + ":" + email
}

// recordAgentVersion notes the version in the request's User-Agent. It never
// fails the poll: a missing or unparseable version is skipped, and a Redis
// error is only logged.
func (s *Service) recordAgentVersion(ctx context.Context, organizationID, machine string) {
	if s.agentVersions == nil {
		return
	}
	requestContext, ok := contextvalues.GetRequestContext(ctx)
	if !ok || requestContext == nil {
		return
	}
	version, ok := deviceagentversions.ParseUserAgent(requestContext.UserAgent)
	if !ok {
		return
	}
	if err := s.agentVersions.Record(ctx, organizationID, machine, version, time.Now()); err != nil {
		s.logger.WarnContext(ctx, "failed to record device agent version",
			attr.SlogError(err),
			attr.SlogOrganizationID(organizationID),
		)
	}
}
