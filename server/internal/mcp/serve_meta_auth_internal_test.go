package mcp

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/stretchr/testify/require"
)

func TestDerivedMemberHeaderSupported(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		header proxy.ConfiguredHeader
		oauth  bool
		want   bool
	}{
		{"optional caller header", proxy.ConfiguredHeader{Name: "X-Key", ValueFromRequestHeader: "X-Key"}, false, true},
		{"required caller key", proxy.ConfiguredHeader{Name: "X-Key", ValueFromRequestHeader: "X-Key", IsRequired: true}, false, false},
		{"configured key", proxy.ConfiguredHeader{Name: "X-Key", StaticValue: "configured", IsRequired: true}, false, true},
		{"missing key", proxy.ConfiguredHeader{Name: "X-Key", IsRequired: true}, false, false},
		{"oauth replaces authorization", proxy.ConfiguredHeader{Name: "authorization", ValueFromRequestHeader: "Authorization", IsRequired: true}, true, true},
		{"oauth cannot replace api key", proxy.ConfiguredHeader{Name: "X-Key", ValueFromRequestHeader: "X-Key", IsRequired: true}, true, false},
		{"passthrough takes precedence", proxy.ConfiguredHeader{Name: "X-Key", StaticValue: "configured", ValueFromRequestHeader: "X-Key", IsRequired: true}, false, false},
		{"invalid static credential", proxy.ConfiguredHeader{Name: "X-Key", StaticValue: "invalid\nvalue", IsRequired: true}, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, derivedMemberHeaderSupported(tt.header, tt.oauth))
		})
	}
}
