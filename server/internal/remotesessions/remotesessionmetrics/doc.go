// Package remotesessionmetrics holds the metric instruments for the remote
// sessions subsystem — the outbound OAuth legs where Gram acts as an OAuth
// client against a customer's upstream identity provider. The inbound leg,
// where MCP clients authorize against Gram itself, is instrumented separately
// in internal/mcp/mcpmetrics.
//
// Every instrument here is an unsampled counter rather than a span attribute:
// traces are sampled at a low fixed rate service-wide, so span attributes can
// answer "what happened on this failing request" but not census questions
// like "what share of upstream revocations land". Instruments are named
// gram.remote_session.* and carry the issuer URL (attr.OAuthIssuer) as their
// upstream dimension — recognizable to a platform administrator, unlike a
// tenant-chosen slug — which stays bounded because issuers are
// operator-configured rows, not traffic.
//
// Constructors never fail: an instrument that cannot be created is logged and
// left nil, and every Record method is nil-receiver- and nil-instrument-safe,
// so a partially constructed value degrades to no metrics rather than a
// panic.
package remotesessionmetrics

// meterScope names the instrumentation scope for every instrument in this
// package. Deliberately the remotesessions package path rather than this
// package's own: the instruments predate the split into a metrics subpackage,
// and renaming the scope would change emitted telemetry metadata for metrics
// already shipped.
const meterScope = "github.com/speakeasy-api/gram/server/internal/remotesessions"
