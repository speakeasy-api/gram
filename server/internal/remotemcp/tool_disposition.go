package remotemcp

import "context"

// ToolDispositionResolver is the read-only seam the per-tool authz interceptors
// depend on to fill the RBAC `disposition` dimension for a remote-MCP tool. The
// production implementation is mcpservers.ToolDispositionCache (which owns the
// backing table and the cache); tests inject a fake.
type ToolDispositionResolver interface {
	// Dispositions returns the tool-name -> disposition token map for a server.
	// An empty (non-nil) map means the server has no classifying metadata; a
	// missing tool key reads as the empty disposition.
	//
	// A non-nil error means resolution itself failed. Callers gating a tool
	// call MUST fail closed on it — disposition is a security dimension, and
	// silently substituting the empty disposition would relax an
	// annotation-scoped policy exactly when the store is unavailable.
	Dispositions(ctx context.Context, mcpServerID, projectID string) (map[string]string, error)
}
