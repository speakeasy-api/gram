package remotemcp

import (
	"strings"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// headerOptsIntoAttachedEnv reports whether a remote MCP header should take
// its upstream value from the fronting server's attached environment.
func headerOptsIntoAttachedEnv(h remotemcprepo.RemoteMcpServerHeader) bool {
	return h.Value.Valid && h.Value.String == "" && !h.ValueFromRequestHeader.Valid
}

// valueFromAttachedEnv returns the attached-environment entry whose derived
// HTTP header name matches headerName (case-insensitive). Mirrors external
// MCP's ToHTTPHeader derivation in BuildHeaders.
func valueFromAttachedEnv(headerName string, env *toolconfig.CaseInsensitiveEnv) string {
	if env == nil {
		return ""
	}
	for envKey, envValue := range env.All() {
		if strings.EqualFold(toolconfig.ToHTTPHeader(envKey), headerName) {
			return envValue
		}
	}
	return ""
}

func configuredHeadersFromRepo(
	headers []remotemcprepo.RemoteMcpServerHeader,
	attachedEnv *toolconfig.CaseInsensitiveEnv,
) []proxy.ConfiguredHeader {
	configured := make([]proxy.ConfiguredHeader, 0, len(headers))
	for _, h := range headers {
		staticValue := h.Value.String
		if headerOptsIntoAttachedEnv(h) {
			staticValue = valueFromAttachedEnv(h.Name, attachedEnv)
		}
		configured = append(configured, proxy.ConfiguredHeader{
			Name:                   h.Name,
			StaticValue:            staticValue,
			ValueFromRequestHeader: h.ValueFromRequestHeader.String,
			IsRequired:             h.IsRequired,
		})
	}
	return configured
}
