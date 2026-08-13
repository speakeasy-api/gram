package hooks

import (
	"context"

	"github.com/speakeasy-api/gram/hooks/wire"
	gen "github.com/speakeasy-api/gram/server/gen/hooks"
)

// The "block" effect payload is wire.BlockEffect — shared with the relay
// decoder in hooks/relay so the vocabulary cannot drift within the repo.

// blockEffectCollector carries an optional effect from the deny branch that
// minted a request link up to the transport response. A context collector
// (same pattern as withRiskScanTracker) avoids threading a third return value
// through every branch of evaluateCanonicalHook.
type blockEffectCollector struct {
	effect *wire.BlockEffect
}

type blockEffectCollectorKey struct{}

func withBlockEffectCollector(ctx context.Context) (context.Context, *blockEffectCollector) {
	collector := &blockEffectCollector{effect: nil}
	return context.WithValue(ctx, blockEffectCollectorKey{}, collector), collector
}

// setBlockEffect records the requestable-deny metadata for the current event.
// No-op when no collector is installed (legacy per-provider endpoints).
func setBlockEffect(ctx context.Context, effect wire.BlockEffect) {
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
	if res.Effects == nil {
		res.Effects = map[string]any{}
	}
	res.Effects[wire.BlockEffectKey] = *collector.effect
	return res
}
