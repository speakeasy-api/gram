package hooks

import (
	"context"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
)

// blockEffectContractVersion is the version stamped into the "block" effect.
// Consumers (the speakeasy-hooks binary, and through it the device agent)
// ignore effects whose version they don't understand, so bump it only on a
// breaking reshape of the payload.
const blockEffectContractVersion = 1

// blockEffect is the structured mirror of the "Request access" prose appended
// to a requestable deny. It rides in IngestHookResult.Effects under the
// "block" key so the hooks binary can hand the device agent something it can
// act on without parsing prose. Absence of the effect means the deny is not
// requestable (PII, secrets, spend, prompt policies carry nothing).
type blockEffect struct {
	Category         string
	RequestToken     string
	RequestURL       string
	RequestExpiresAt time.Time
	ServerName       string
	ServerURL        string
	PolicyName       string
	ToolName         string
	BlockURL         string
}

// blockEffectCollector carries an optional blockEffect from the deny branch
// that minted a request link up to the transport response. A context collector
// (same pattern as withRiskScanTracker) avoids threading a third return value
// through every branch of evaluateCanonicalHook.
type blockEffectCollector struct {
	effect *blockEffect
}

type blockEffectCollectorKey struct{}

func withBlockEffectCollector(ctx context.Context) (context.Context, *blockEffectCollector) {
	collector := &blockEffectCollector{effect: nil}
	return context.WithValue(ctx, blockEffectCollectorKey{}, collector), collector
}

// setBlockEffect records the requestable-deny metadata for the current event.
// No-op when no collector is installed (legacy per-provider endpoints).
func setBlockEffect(ctx context.Context, effect blockEffect) {
	if collector, ok := ctx.Value(blockEffectCollectorKey{}).(*blockEffectCollector); ok {
		collector.effect = &effect
	}
}

// setBlockEffectBlockURL attaches the durable block-row URL to an already
// recorded effect. Duplicate deliveries re-mint the request link but never a
// second block row, so the effect can exist without a block URL.
func setBlockEffectBlockURL(ctx context.Context, blockURL string) {
	if collector, ok := ctx.Value(blockEffectCollectorKey{}).(*blockEffectCollector); ok && collector.effect != nil {
		collector.effect.BlockURL = blockURL
	}
}

// withBlockEffect attaches the collected effect, if any, to a deny result.
func withBlockEffect(collector *blockEffectCollector, res *gen.IngestHookResult) *gen.IngestHookResult {
	if collector == nil || collector.effect == nil {
		return res
	}
	e := collector.effect
	payload := map[string]any{
		"v":             blockEffectContractVersion,
		"category":      e.Category,
		"requestable":   true,
		"request_token": e.RequestToken,
		"request_url":   e.RequestURL,
	}
	if !e.RequestExpiresAt.IsZero() {
		payload["request_expires_at"] = e.RequestExpiresAt.UTC().Format(time.RFC3339)
	}
	for key, value := range map[string]string{
		"server_name": e.ServerName,
		"server_url":  e.ServerURL,
		"policy_name": e.PolicyName,
		"tool_name":   e.ToolName,
		"block_url":   e.BlockURL,
	} {
		if value != "" {
			payload[key] = value
		}
	}
	if res.Effects == nil {
		res.Effects = map[string]any{}
	}
	res.Effects["block"] = payload
	return res
}
