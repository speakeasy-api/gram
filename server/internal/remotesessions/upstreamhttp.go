// upstreamhttp.go selects the HTTP transport for calls to an upstream
// authorization server. Issuers without a tunnel binding dial directly;
// issuers with one route every request through that tunnel.

package remotesessions

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
)

// httpDoer is the request/response surface shared by guardian HTTP clients
// and the tunnel-bound doer below.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// tunnelDoer pins the tunnel HTTP client to one tunnel so call sites can hold
// a single Do surface regardless of which transport was selected.
type tunnelDoer struct {
	client   *tunnelrouting.HTTPClient
	tunnelID string
}

func (d tunnelDoer) Do(req *http.Request) (*http.Response, error) {
	resp, err := d.client.Do(req, d.tunnelID)
	if err != nil {
		return nil, fmt.Errorf("forward via MCP tunnel: %w", err)
	}
	return resp, nil
}

// upstreamHTTPDoer picks the transport from the issuer's persisted binding.
func upstreamHTTPDoer(direct httpDoer, tunnels *tunnelrouting.HTTPClient, tunneledMcpServerID uuid.NullUUID) (httpDoer, error) {
	if !tunneledMcpServerID.Valid {
		return direct, nil
	}
	if tunnels == nil {
		return nil, fmt.Errorf("tunnel transport is not configured")
	}
	return tunnelDoer{client: tunnels, tunnelID: tunneledMcpServerID.UUID.String()}, nil
}
