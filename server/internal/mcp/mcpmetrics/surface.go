package mcpmetrics

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

// Surface identifies which inbound MCP serving surface observed a request.
// The values match the policy boundaries mcpversions draws: arbitrary
// third-party clients versus the assistant-token-only platform surface. The
// /x/mcp backend fan-out (toolset-, remote-, and tunnel-backed) is
// deliberately not distinguished — all of it faces third-party clients.
//
// The go-sdk-served /platform-mcp surface (server/internal/platformmcp) is
// deliberately outside this instrument: it is served by neither Gram's
// JSON-RPC dispatch nor the remote proxy, so no emit site sees it, and its
// protocol version is the go-sdk dependency's rather than Gram's. AIS-558
// records the conditions under which it would come into scope.
type Surface string

const (
	// SurfaceHosting covers /mcp/{slug} and every /x/mcp/{slug} backend.
	SurfaceHosting Surface = "hosting"

	// SurfacePlatform covers /platform/mcp/{toolsetSlug}, which accepts only
	// the assistant token.
	SurfacePlatform Surface = "platform"

	// SurfaceMeta covers meta-MCP-backed /mcp/{slug} endpoints, which face
	// arbitrary third-party clients but answer a newer protocol revision
	// than the hosting surface.
	SurfaceMeta Surface = "meta"
)

type NetworkSurface string

const (
	NetworkSurfacePublic  NetworkSurface = "public"
	NetworkSurfacePrivate NetworkSurface = "private"
)

func NetworkSurfaceFromContext(ctx context.Context) NetworkSurface {
	origin, ok := requestorigin.FromContext(ctx)
	if ok && origin.Surface == requestorigin.SurfacePrivateNetwork {
		return NetworkSurfacePrivate
	}
	return NetworkSurfacePublic
}
