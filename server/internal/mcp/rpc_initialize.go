package mcp

import (
	"context"
	"encoding/json"

	gen "github.com/speakeasy-api/gram/gen/mcp"
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

func handleInitialize(_ context.Context, _ *gen.ServePayload, req *rawRequest) (json.RawMessage, error) {
	result := &result[initializeResult]{
		ID: req.ID,
		Result: initializeResult{
			ProtocolVersion: "2024-11-05",
			Capabilities: map[string]json.RawMessage{
				"tools": json.RawMessage("{}"),
			},
			ServerInfo: serverInfo{
				Name:    "Gram",
				Version: "0.0.0",
			},
		},
	}

	return json.Marshal(result)
}
