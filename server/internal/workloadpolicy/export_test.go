package workloadpolicy

import "github.com/speakeasy-api/gram/server/internal/ratelimit"

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
