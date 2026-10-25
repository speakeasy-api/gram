package adminmcp

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// toolDirectoryTimeout bounds local MCP discovery if a runtime stops responding.
const toolDirectoryTimeout = 5 * time.Second

type directoryAccess string

const (
	// directoryRead identifies tools that do not change state.
	directoryRead directoryAccess = "read"

	// directoryApproval identifies preparation and execution in the browser-approved flow.
	directoryApproval directoryAccess = "approval"

	// directoryWrite identifies direct writes outside the proposal flow.
	directoryWrite directoryAccess = "write"
)

// directoryTool exposes only server-authored metadata, never schemas or tool results.
type directoryTool struct {
	// Name is the identifier clients use to invoke the tool.
	Name string `json:"name"`

	// Title is the registered human-readable tool name.
	Title string `json:"title"`

	// Description explains the registered behavior and safeguards.
	Description string `json:"description"`

	// Access distinguishes reads, approval flows, and direct writes.
	Access directoryAccess `json:"access"`
}

type toolDirectory struct {
	// Tools contains the runtime's registered tools in name order.
	Tools []directoryTool `json:"tools"`
}

// ToolDirectoryHandler lets a verified staff browser inspect the same tools a
// connected MCP client discovers, before installing a client. It belongs only
// on the private admin mux and never accepts or executes tool calls.
func (r *Runtime) ToolDirectoryHandler(session *StaffOAuthAuthorization) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		if req.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r == nil || r.server == nil || session == nil || session.verifier == nil {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		if _, _, err := session.staffSession(req); err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx, cancel := context.WithTimeout(req.Context(), toolDirectoryTimeout)
		defer cancel()
		directory, err := r.toolDirectory(ctx)
		if err != nil {
			http.Error(w, "tool directory unavailable", http.StatusServiceUnavailable)
			return
		}
		staffJSON(w, http.StatusOK, directory)
	})
}

func (r *Runtime) toolDirectory(ctx context.Context) (toolDirectory, error) {
	// The SDK exposes discovery through a client session, not a registry getter.
	// This local connection only lists metadata; no caller controls MCP messages.
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := r.server.Connect(ctx, serverTransport, nil)
	if err != nil {
		return toolDirectory{}, fmt.Errorf("connect tool directory server: %w", err)
	}
	defer o11y.NoLogDefer(serverSession.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "admin-tool-directory", Version: "1", Title: "Admin Tool Directory", Description: "", WebsiteURL: "", Icons: nil}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		return toolDirectory{}, fmt.Errorf("connect tool directory client: %w", err)
	}
	defer o11y.NoLogDefer(clientSession.Close)

	result := toolDirectory{Tools: []directoryTool{}}
	for tool, err := range clientSession.Tools(ctx, nil) {
		if err != nil {
			return toolDirectory{}, fmt.Errorf("list registered admin tools: %w", err)
		}
		access := directoryWrite
		switch {
		case strings.HasPrefix(tool.Name, "prepare_") || tool.Name == "execute_admin_proposal":
			access = directoryApproval
		case tool.Annotations != nil && tool.Annotations.ReadOnlyHint:
			access = directoryRead
		}
		result.Tools = append(result.Tools, directoryTool{Name: tool.Name, Title: tool.Title, Description: tool.Description, Access: access})
	}
	slices.SortFunc(result.Tools, func(a, b directoryTool) int { return cmp.Compare(a.Name, b.Name) })
	return result, nil
}
