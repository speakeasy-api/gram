package hooks

import (
	"context"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/hookversions"
	"github.com/speakeasy-api/gram/server/internal/middleware"
)

// effectHooksVersion is the ingest effect that tells the speakeasy-hooks
// binary how its version compares to the supported range. It is advisory and
// never changes the decision.
const effectHooksVersion = "hooks_version"

// legacyHooksNotice is shown once per session by the retired per-tool Claude
// endpoint, which is always unsupported.
const legacyHooksNotice = "Speakeasy: the hooks in this workspace use a retired endpoint and are no longer supported. Ask your Speakeasy admin to update the Speakeasy hooks plugin."

func (s *Service) withVersionStatus(ctx context.Context, res *gen.IngestHookResult) *gen.IngestHookResult {
	if res == nil {
		return res
	}
	version := middleware.HookDeviceAttributes(ctx)[attr.HookDeviceBinaryVersionKey]
	status := s.versionPolicy.Classify(version, middleware.HookEndpointGeneration(ctx))
	if status == hookversions.StatusUnknown {
		return res
	}
	effect := map[string]any{
		"status":  string(status),
		"version": version,
		"latest":  s.versionPolicy.Latest.String(),
	}
	if s.versionPolicy.Minimum != nil {
		effect["minimum"] = s.versionPolicy.Minimum.String()
	}
	if res.Effects == nil {
		res.Effects = map[string]any{}
	}
	res.Effects[effectHooksVersion] = effect
	return res
}
