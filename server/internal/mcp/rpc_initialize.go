package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	metadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

type initializeResult struct {
	ProtocolVersion string                     `json:"protocolVersion"`
	Capabilities    map[string]json.RawMessage `json:"capabilities"`
	ServerInfo      serverInfo                 `json:"serverInfo"`
	Instructions    string                     `json:"instructions,omitempty"`
}
type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// initializeParams captures the subset of the MCP initialize request params we
// record: the requested protocol version, the client identity, and the
// capabilities the client advertised.
type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
	ClientInfo      struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"clientInfo"`
	Capabilities map[string]json.RawMessage `json:"capabilities"`
}

// parseInitializeParams decodes the initialize request params we record. It
// returns the parsed params, the sorted set of advertised capability keys, and
// whether the params were well-formed. Malformed params must not fail the RPC,
// so callers log and continue rather than surfacing the error.
func parseInitializeParams(raw json.RawMessage) (initializeParams, []string, error) {
	var params initializeParams
	if len(raw) == 0 {
		return params, nil, nil
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return initializeParams{}, nil, fmt.Errorf("unmarshal initialize params: %w", err)
	}
	return params, slices.Sorted(maps.Keys(params.Capabilities)), nil
}

func handleInitialize(ctx context.Context, logger *slog.Logger, telemetry *mcpmetrics.Metrics, req *rawRequest, payload *mcpInputs, productMetrics *posthog.Posthog, toolsetsRepoParam *toolsets_repo.Queries, metadataRepoParam *metadata_repo.Queries, clientInfoStore sessionClientInfoStore) (json.RawMessage, error) {
	params, capabilities, err := parseInitializeParams(req.Params)
	validParams := err == nil
	if err != nil {
		// Malformed params should not fail the RPC; we just lose the
		// recorded client info for this request.
		logger.WarnContext(ctx, "failed to parse mcp initialize params", attr.SlogError(err))
	}

	// Genuine version negotiation: echo the requested revision when this
	// surface supports it, otherwise answer the newest supported one. The
	// answer is written back into the request's resolution because initialize
	// is the one request whose in-effect revision is established mid-handling
	// — the entry-time value is provisional — and anything downstream of
	// dispatch must see the negotiated value. The body's requested version
	// wins over any MCP-Protocol-Version header a nonconforming client sent
	// on initialize: the body is the negotiation.
	negotiated := mcpversions.Negotiate(params.ProtocolVersion, mcpversions.SupportedHostedToolset())
	payload.protocolVersion.InEffect = negotiated

	// Recording requested and negotiated separately keeps a downgrade — a
	// client asking for a revision outside the supported set — visible rather
	// than collapsed.
	recordMCPProtocolVersionSpan(ctx, params.ProtocolVersion, negotiated)
	telemetry.RecordMCPInitialize(ctx, params.ProtocolVersion, negotiated)

	storeSessionClientInfo(ctx, logger, clientInfoStore, payload, params.ClientInfo.Name, params.ClientInfo.Version, params.ProtocolVersion)

	if requestContext, _ := contextvalues.GetRequestContext(ctx); requestContext != nil {
		if err := productMetrics.CaptureEvent(ctx, "mcp_initialized", payload.sessionID, map[string]any{
			"project_id":           payload.projectID.String(),
			"authenticated":        payload.authenticated,
			"mcp_domain":           requestContext.Host,
			"mcp_url":              requestContext.Host + requestContext.ReqURL,
			"disable_notification": true,
			"mcp_session_id":       payload.sessionID,
			"protocol_version":     params.ProtocolVersion,
			"client_name":          conv.PtrEmpty(conv.TruncateString(params.ClientInfo.Name, 100)),
			"client_version":       conv.PtrEmpty(conv.TruncateString(params.ClientInfo.Version, 100)),
			"capabilities":         conv.Ternary(validParams, capabilities, nil),
		}); err != nil {
			logger.ErrorContext(ctx, "failed to capture mcp_initialized event", attr.SlogError(err))
		}
	}

	instructions := fetchInstructions(ctx, logger, toolsetsRepoParam, metadataRepoParam, payload.toolset, payload.projectID)

	result := &result[initializeResult]{
		ID:             req.ID,
		serverIdentity: serverInfoHostedToolset,
		Result: initializeResult{
			ProtocolVersion: negotiated,
			Capabilities: map[string]json.RawMessage{
				"tools":     json.RawMessage("{}"),
				"prompts":   json.RawMessage("{}"),
				"resources": json.RawMessage("{}"),
			},
			ServerInfo:   serverInfoHostedToolset,
			Instructions: instructions,
		},
	}

	bs, err := json.Marshal(result)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize initialize response").LogError(ctx, logger)
	}

	return bs, nil
}

// fetchInstructions will attempt to find an MCP servers' instructions. If it can't it will just return an empty string.
func fetchInstructions(ctx context.Context, logger *slog.Logger, toolsetsRepo *toolsets_repo.Queries, metadataRepo *metadata_repo.Queries, toolsetSlug string, projectID uuid.UUID) string {
	toolset, err := toolsetsRepo.GetToolset(ctx, toolsets_repo.GetToolsetParams{
		Slug:      toolsetSlug,
		ProjectID: projectID,
	})
	if err != nil {
		// not finding a toolset is OK - any other errors are unexpected and should be logged
		if !errors.Is(err, pgx.ErrNoRows) {
			logger.WarnContext(ctx, "failed to fetch toolset for instructions", attr.SlogError(err))
		}
		return ""
	}

	metadata, err := metadataRepo.GetMetadataForToolset(ctx, uuid.NullUUID{UUID: toolset.ID, Valid: true})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			logger.WarnContext(ctx, "failed to fetch MCP metadata for instructions", attr.SlogError(err))
		}
		return ""
	}

	if !metadata.Instructions.Valid {
		return ""
	}

	return metadata.Instructions.String
}
