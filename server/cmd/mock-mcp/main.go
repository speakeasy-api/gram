// Command mock-mcp is a local-only MCP server over Streamable HTTP that lists
// the fictional task tracker in [mocktasks.Tools]. The local seed registers it
// as a remote MCP server, so the dashboard Inspect tab can connect, list and
// call tools on a stack with no internet access. Never deployed.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/demoseed/mocktasks"
)

// slowToolDelay is how long a slow tool takes to answer.
const slowToolDelay = 2500 * time.Millisecond

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	addr := os.Getenv("GRAM_MOCK_MCP_ADDRESS")
	if addr == "" {
		addr = "127.0.0.1:4100"
	}
	if err := requireLoopback(addr); err != nil {
		logger.Error("mock-mcp refused to start", "error", err)
		os.Exit(1)
	}
	if err := run(addr, logger); err != nil {
		logger.Error("mock-mcp failed", "error", err)
		os.Exit(1)
	}
}

// requireLoopback rejects listen addresses that are not on a loopback
// interface. The mock has no authentication, so it must never be reachable
// from another machine.
func requireLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("parse GRAM_MOCK_MCP_ADDRESS %q: %w", addr, err)
	}
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("GRAM_MOCK_MCP_ADDRESS %q must bind a loopback host such as 127.0.0.1", addr)
}

func run(addr string, logger *slog.Logger) error {
	server := mcp.NewServer(&mcp.Implementation{Name: "acme-tasks-mock", Title: "Acme Tasks (local mock)", Version: "1.0.0"}, nil)
	for _, tool := range mocktasks.Tools {
		server.AddTool(&mcp.Tool{
			Name:        tool.Name,
			Title:       tool.Title,
			Description: tool.Description,
			InputSchema: json.RawMessage(tool.InputSchema),
			Annotations: &mcp.ToolAnnotations{
				Title:           tool.Title,
				ReadOnlyHint:    tool.ReadOnly,
				DestructiveHint: &tool.Destructive,
				IdempotentHint:  tool.Idempotent,
				OpenWorldHint:   &tool.OpenWorld,
			},
		}, handler(tool))
	}

	mux := http.NewServeMux()
	mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	logger.Info("mock-mcp listening on " + addr + " (/mcp)")
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

// handler answers every call with canned text: an error result for a failing
// tool, after a delay for a slow one.
func handler(tool mocktasks.Tool) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if tool.Slow {
			select {
			case <-time.After(slowToolDelay):
			case <-ctx.Done():
				return nil, fmt.Errorf("call cancelled: %w", ctx.Err())
			}
		}
		if tool.Fails {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "calendar provider rejected the sync: token for calendar \"team-acme\" expired (example error from the local mock)"}},
			}, nil
		}
		args := "{}"
		if req.Params != nil && len(req.Params.Arguments) > 0 {
			args = string(req.Params.Arguments)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(`{"tool":%q,"ok":true,"arguments":%s,"note":"fixed sample data from the local mock"}`, tool.Name, args)}},
		}, nil
	}
}
