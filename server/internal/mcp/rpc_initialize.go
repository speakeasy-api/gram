package mcp

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
)

type initializeResult struct {
	ProtocolVersion string                     `json:"protocolVersion"`
	Capabilities    map[string]json.RawMessage `json:"capabilities"`
	ServerInfo      serverInfo                 `json:"serverInfo"`
}
type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func handleInitialize(ctx context.Context, logger *slog.Logger, req *rawRequest, payload *mcpInputs, productMetrics *posthog.Posthog) (json.RawMessage, error) {
	if requestContext, _ := contextvalues.GetRequestContext(ctx); requestContext != nil {
		if err := productMetrics.CaptureEvent(ctx, "mcp_initialized", payload.sessionID, map[string]interface{}{
			"project_id":           payload.projectID.String(),
			"authenticated":        payload.authenticated,
			"mcp_domain":           requestContext.Host,
			"mcp_url":              requestContext.Host + requestContext.ReqURL,
			"disable_notification": true,
			"mcp_session_id":       payload.sessionID,
		}); err != nil {
			logger.ErrorContext(ctx, "failed to capture mcp_initialized event", attr.SlogError(err))
		}
	}

	result := &result[initializeResult]{
		ID: req.ID,
		Result: initializeResult{
			ProtocolVersion: "2024-11-05",
			Capabilities: map[string]json.RawMessage{
				"tools":     json.RawMessage("{}"),
				"prompts":   json.RawMessage("{}"),
				"resources": json.RawMessage("{}"),
			},
			ServerInfo: serverInfo{
				Name:    "Gram",
				Version: "0.0.0",
			},
		},
	}

	bs, err := json.Marshal(result)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize initialize response").Log(ctx, logger)
	}

	return bs, nil
}
