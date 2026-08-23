package wire_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/netgateway/wire"
)

func TestStripRemovesEveryForwardHeader(t *testing.T) {
	t.Parallel()

	h := http.Header{}
	h.Set(wire.HeaderForwardToken, "forged")
	h.Set(wire.HeaderIngressID, "x")
	h.Set(wire.HeaderUserLogin, "attacker@example.com")
	// Non-canonical spelling must be stripped too.
	h["x-gram-netingress-user-caps"] = []string{"cap"}
	h.Set("X-Unrelated", "stays")

	wire.Strip(h)

	require.Len(t, h, 1)
	require.Equal(t, "stays", h.Get("X-Unrelated"))
}
