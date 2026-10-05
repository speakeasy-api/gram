package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	metadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
)

// serverDescription is shared by both protocol eras without any handshake
// recording or session establishment. Cache hints apply only to discovery.
type serverDescription struct {
	// capabilities lists the operations this surface offers.
	capabilities map[string]json.RawMessage

	// identity labels this server in both protocol eras.
	identity serverInfo

	// instructions carries the surface-specific operator guidance.
	instructions string

	// cacheHints protects the complete discovery payload.
	cacheHints *cacheHints
}

func (d serverDescription) initializeResult(version string) initializeResult {
	return initializeResult{
		ProtocolVersion: version,
		Capabilities:    d.capabilities,
		ServerInfo:      d.identity,
		Instructions:    d.instructions,
	}
}

// describePlatformServer keeps discovery caller-varying even though the payload
// is static today: the surface answers only assistant-token callers, so a
// shared cache must not serve its description to anyone else.
func describePlatformServer() serverDescription {
	return serverDescription{
		capabilities: map[string]json.RawMessage{"tools": json.RawMessage("{}")},
		identity:     serverInfoPlatformToolset,
		instructions: "",
		cacheHints:   cacheHintsCallerVarying,
	}
}

func describeMetaServer(meta *metamcprepo.MetaMcpServer) serverDescription {
	hints := cacheHintsCallerUniform
	if meta.UserSessionIssuerID.Valid {
		hints = cacheHintsCallerVarying
	}
	return serverDescription{
		capabilities: map[string]json.RawMessage{"tools": json.RawMessage("{}")},
		identity:     serverInfoMetaServer,
		instructions: metamcp.ResolveInstructions(conv.FromPGText[string](meta.Instructions)),
		cacheHints:   hints,
	}
}

// describeHostedServer tolerates missing metadata exactly as the
// initialize handshake does. It reads the toolset the request builder already loaded; a
// payload without one describes no instructions and keeps hints private.
func describeHostedServer(ctx context.Context, logger *slog.Logger, metadataRepo *metadata_repo.Queries, payload *mcpInputs) serverDescription {
	description := serverDescription{
		capabilities: map[string]json.RawMessage{
			"tools":     json.RawMessage("{}"),
			"prompts":   json.RawMessage("{}"),
			"resources": json.RawMessage("{}"),
		},
		identity:     serverInfoHostedToolset,
		instructions: "",
		cacheHints:   cacheHintsCallerVarying,
	}
	if !payload.toolsetID.Valid {
		return description
	}

	description.cacheHints = hostedListCacheHints(!payload.effectiveMCPPrivate(payload.toolsetIsPublic), payload.authenticated)

	metadata, err := metadataRepo.GetMetadataForToolset(ctx, payload.toolsetID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			logger.WarnContext(ctx, "failed to fetch MCP metadata for instructions", attr.SlogError(err))
		}
		return description
	}

	if !metadata.Instructions.Valid {
		return description
	}

	description.instructions = metadata.Instructions.String
	return description
}
