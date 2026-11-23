package hooks

import (
	"context"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
)

// stampEndpointGeneration records which hook endpoint generation delivered
// the current request. In-process callers (AHP, LiteLLM) carry none.
func stampEndpointGeneration(ctx context.Context, attrs map[attr.Key]any) {
	if generation := middleware.HookEndpointGeneration(ctx); generation != "" {
		attrs[attr.HookEndpointGenerationKey] = generation
	}
}

// stampIngestClientVersion fills in the binary version and endpoint
// generation on a hooks.ingest row. The binary puts its own version in every
// envelope's source.adapter_version, and a spooled envelope keeps the version
// that captured it, so this covers replays and binaries that predate the
// X-Gram-Device-* headers. Bash/curl senders leave the field unset.
func stampIngestClientVersion(ctx context.Context, payload *gen.IngestPayload, attrs map[attr.Key]any) {
	generation := middleware.HookEndpointGeneration(ctx)
	if generation == "" {
		return
	}
	adapterVersion := ""
	if payload != nil && payload.Source != nil {
		adapterVersion = middleware.SanitizeDeviceValue(conv.PtrValOr(payload.Source.AdapterVersion, ""))
	}
	if adapterVersion != "" {
		generation = middleware.HookEndpointGenerationIngestBinary
		if _, ok := attrs[attr.HookDeviceBinaryVersionKey]; !ok {
			attrs[attr.HookDeviceBinaryVersionKey] = adapterVersion
		}
	}
	attrs[attr.HookEndpointGenerationKey] = generation
}
