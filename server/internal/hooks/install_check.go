package hooks

import (
	"encoding/json"
	"strings"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// InstallCheckSessionID is the session id of the test tool call the agent
// install script sends through a freshly installed hooks plugin. The script
// only needs to know that the plugin runs and that the agent's key may send
// hooks, so ingest answers this call after authentication without recording
// it: otherwise every install would leave an empty session and a tool call
// the agent never made.
const InstallCheckSessionID = "speakeasy-smoke-test"

// The rest of the test call. The sender chooses every field, so only this
// exact harmless call skips policy and recording: any other event, including
// a real one that reuses the session id, takes the normal path.
const (
	installCheckToolCallID = "speakeasy-smoke-1"
	installCheckToolName   = "exec"
	installCheckInput      = `{"command":"true"}`
)

func isInstallCheck(payload *gen.IngestPayload) bool {
	if payload == nil || payload.Session == nil || payload.Event == nil {
		return false
	}
	if strings.TrimSpace(conv.PtrValOr(payload.Session.ID, "")) != InstallCheckSessionID ||
		strings.TrimSpace(payload.Event.Type) != "tool.requested" {
		return false
	}
	tool := canonicalToolCallData(payload)
	if tool == nil || conv.PtrValOr(tool.ID, "") != installCheckToolCallID || conv.PtrValOr(tool.Name, "") != installCheckToolName {
		return false
	}
	input, err := json.Marshal(tool.Input)
	return err == nil && string(input) == installCheckInput
}
