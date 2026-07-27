package deviceintegrations

// Vendor providers register themselves from their package init. Importing
// them here — the package every consumer of the framework already imports
// (management API in the server, sync runner in the worker) — guarantees the
// registry is populated in every process without per-binary wiring.
import (
	_ "github.com/speakeasy-api/gram/server/internal/deviceintegrations/providers/jamf"
)
