package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

func TestRemoteMemberAuth(t *testing.T) {
	t.Parallel()
	callerKey := proxy.ConfiguredHeader{Name: "X-Key", StaticValue: "", ValueFromRequestHeader: "X-Key", IsRequired: true}
	staticKey := proxy.ConfiguredHeader{Name: "X-Key", StaticValue: "configured", ValueFromRequestHeader: "", IsRequired: true}
	callerAuthorization := proxy.ConfiguredHeader{Name: "authorization", StaticValue: "", ValueFromRequestHeader: "Authorization", IsRequired: true}
	staticAuthorization := proxy.ConfiguredHeader{Name: "Authorization", StaticValue: "Bearer configured", ValueFromRequestHeader: "", IsRequired: true}
	for _, tt := range []struct {
		name           string
		headers        []proxy.ConfiguredHeader
		oauth          bool
		wantSupported  bool
		wantNeedsOAuth bool
	}{
		{"no headers", nil, false, true, false},
		{"no headers with sign-in", nil, true, true, true},
		{"optional caller header", []proxy.ConfiguredHeader{{Name: "X-Key", StaticValue: "", ValueFromRequestHeader: "X-Key", IsRequired: false}}, false, true, false},
		{"required caller key", []proxy.ConfiguredHeader{callerKey}, false, false, false},
		{"missing key", []proxy.ConfiguredHeader{{Name: "X-Key", StaticValue: "", ValueFromRequestHeader: "", IsRequired: true}}, false, false, false},
		{"invalid static key", []proxy.ConfiguredHeader{{Name: "X-Key", StaticValue: "invalid\nvalue", ValueFromRequestHeader: "", IsRequired: true}}, false, false, false},
		{"passthrough takes precedence", []proxy.ConfiguredHeader{{Name: "X-Key", StaticValue: "configured", ValueFromRequestHeader: "X-Key", IsRequired: true}}, false, false, false},
		{"static key", []proxy.ConfiguredHeader{staticKey}, false, true, false},
		{"static key with sign-in", []proxy.ConfiguredHeader{staticKey}, true, true, false},
		{"static authorization with sign-in", []proxy.ConfiguredHeader{staticAuthorization}, true, true, false},
		{"sign-in replaces authorization", []proxy.ConfiguredHeader{callerAuthorization}, true, true, true},
		{"sign-in replaces authorization beside static key", []proxy.ConfiguredHeader{callerAuthorization, staticKey}, true, true, true},
		{"sign-in cannot replace api key", []proxy.ConfiguredHeader{callerKey}, true, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			supported, needsOAuth := remoteMemberAuth(tt.headers, tt.oauth)
			require.Equal(t, tt.wantSupported, supported)
			require.Equal(t, tt.wantNeedsOAuth, needsOAuth)
		})
	}
}
