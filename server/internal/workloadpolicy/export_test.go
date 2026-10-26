package workloadpolicy

import (
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"log/slog"
)

// SetRegisterLimitersForTest swaps the enrollment limiters so a test can
// observe which buckets a request consumes.
func SetRegisterLimitersForTest(s *Service, fleet, token *ratelimit.Limiter) {
	s.registerLimiter, s.registerTokenLimiter = fleet, token
}

// RenderDeviceInstallScriptForTest renders the install script for a record
// with these values, so a test can inspect the script without an install code.
func RenderDeviceInstallScriptForTest(controlPlaneURL, hooksProjectSlug, joinToken string) (string, error) {
	return renderDeviceInstallScript(deviceInstallRecord{
		Code:             "",
		JoinToken:        joinToken,
		ControlPlaneURL:  controlPlaneURL,
		HooksProjectSlug: hooksProjectSlug,
		ttl:              0,
	})
}

// SetDeviceInstallLoggerForTest captures service and typed-cache diagnostics at
// the same level so privacy tests include debug messages from cache cleanup.
func SetDeviceInstallLoggerForTest(s *Service, logger *slog.Logger, storage cache.Cache) {
	s.logger = logger
	s.deviceInstallCache = cache.NewTypedObjectCache[deviceInstallRecord](logger, storage, cache.SuffixNone)
}
