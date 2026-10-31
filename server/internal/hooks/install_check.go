package hooks

import (
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

func isInstallCheck(payload *gen.IngestPayload) bool {
	return payload != nil && payload.Session != nil && strings.TrimSpace(conv.PtrValOr(payload.Session.ID, "")) == InstallCheckSessionID
}
