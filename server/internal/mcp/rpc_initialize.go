package mcp

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/speakeasy-api/gram/internal/oops"
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

func handleInitialize(ctx context.Context, logger *slog.Logger, req *rawRequest) (json.RawMessage, error) {
	result := &result[initializeResult]{
		ID: req.ID,
		Result: initializeResult{
			ProtocolVersion: "2024-11-05",
			Capabilities: map[string]json.RawMessage{
				"tools":   json.RawMessage("{}"),
				"prompts": json.RawMessage("{}"),
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
