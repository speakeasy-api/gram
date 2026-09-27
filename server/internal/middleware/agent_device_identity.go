package middleware

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deviceidentity"
)

// agentRoutePrefix bounds device-identity logging to the endpoints the
// Speakeasy device agent calls. The headers mean nothing anywhere else, and a
// route-scoped field keeps a caller that invents them on an unrelated route
// out of the device counts taken from these logs.
const agentRoutePrefix = "/rpc/agent."

// agentDeviceIdentityAttrs reads the Gram-Device-* headers the Speakeasy
// device agent sends and returns them as wide-event attributes, so the
// request log identifies the MACHINE and not just the credential. A fleet
// enrolled with one organization install key otherwise attributes every
// request to that key's owner, leaving no way to count devices or tell them
// apart short of joining ingress logs by request id for client IPs — which
// reports two machines behind one NAT as one.
//
// The serial is recorded as-is rather than hashed. It is not a credential:
// unlike the API key it accompanies, it authorizes nothing and is not
// replayable, and the same wide event already carries the authenticated
// organization, user, and email. Hashing would only cost the operator the one
// thing a device count is usually followed by — naming the machine in MDM
// inventory, which keys on the same serial.
//
// An unreported value yields no attribute at all. Agents predating these
// headers send none of them, and hardware with no readable serial sends no
// serial; recording those as empty strings would grow a bucket of
// unidentified devices that COUNT(DISTINCT serial) would count as one.
func agentDeviceIdentityAttrs(r *http.Request) []slog.Attr {
	if !strings.HasPrefix(r.URL.Path, agentRoutePrefix) {
		return nil
	}

	attrs := make([]slog.Attr, 0, 3)

	// Normalized the way the agent service normalizes it before storing a
	// per-device heartbeat, so a device count taken over these logs matches
	// one taken over the stored heartbeats: casing is collapsed, and an
	// SMBIOS placeholder serial — which many distinct machines report
	// identically — is dropped rather than counted as a device.
	reportedSerial := sanitizeDeviceHeader(r.Header.Get(deviceidentity.HeaderSerial))
	if serial := deviceidentity.NormalizeSerial(conv.PtrEmpty(reportedSerial)); serial != "" {
		attrs = append(attrs, attr.SlogAgentDeviceSerial(serial))
	}

	if hostname := sanitizeDeviceHeader(r.Header.Get(deviceidentity.HeaderHostname)); hostname != "" {
		attrs = append(attrs, attr.SlogAgentDeviceHostname(hostname))
	}

	// The normalized kind, which is the one the request was served under: a
	// value this server does not recognize degrades to endpoint rather than
	// failing the request, and the log says where it landed. Recorded only
	// when the agent declared a kind, because an absent header is the
	// deployed majority and logging endpoint for it would drown the
	// distinction the field exists to draw.
	if environment := sanitizeDeviceHeader(r.Header.Get(deviceidentity.HeaderEnvironment)); environment != "" {
		attrs = append(attrs, attr.SlogAgentDeviceEnvironment(deviceidentity.NormalizeEnvironment(&environment)))
	}

	return attrs
}
