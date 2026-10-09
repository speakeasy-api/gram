package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/environments"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// environmentHeaderSource reads an MCP server's backend, remote URL,
// environment link and MCP_HEADER_ entries in one snapshot.
type environmentHeaderSource interface {
	InspectMCPServerHeaders(ctx context.Context, projectID uuid.UUID, serverID uuid.UUID) (environments.MCPServerHeaderSnapshot, error)
}

// environmentHeaderSnapshot is the set of environment headers loaded once for
// one logical request, so every exchange built for that request sends the
// same values.
type environmentHeaderSnapshot struct {
	rows []proxy.ConfiguredHeader
}

// errServingConfigurationChanged reports that an MCP server's backend,
// remote URL or environment link changed after the request read it. The
// request is refused rather than sending one configuration's environment
// headers to another configuration's destination.
var errServingConfigurationChanged = errors.New("mcp server configuration changed while serving the request")

// environmentHeaderMisconfigured is the message an MCP client sees when the
// served server's environment headers cannot be sent. It names nothing from
// the environment: callers of a public server are anonymous.
const environmentHeaderMisconfigured = "this MCP server's environment headers are misconfigured; contact the MCP server administrator"

// servingConfigurationChanged is the message an MCP client sees when the
// server's configuration changed mid-request.
const servingConfigurationChanged = "this MCP server's configuration changed while handling the request; retry"

// readEnvironmentHeaders returns the headers mapped from the environment
// linked to server, the row the request was authorized against. remoteURL is
// the remote source URL the request will dial, empty for a tunneled server.
//
// A server with no linked environment has none. Otherwise the server's
// backend, remote URL, link and MCP_HEADER_ entries are read in one snapshot
// and must still match server and remoteURL, so the headers come from the
// configuration whose destination is dialed. A linked environment that is
// unavailable, or holds an MCP_HEADER_ entry that cannot be sent, is an error:
// serving must not fall back to the source's value for a header the
// environment was meant to set. A link with no source wired is an error too,
// never a silent "no headers".
func readEnvironmentHeaders(ctx context.Context, source environmentHeaderSource, projectID uuid.UUID, server *mcpserversrepo.McpServer, remoteURL string) (environmentHeaderSnapshot, error) {
	if !server.EnvironmentID.Valid {
		return environmentHeaderSnapshot{rows: nil}, nil
	}
	if source == nil {
		return environmentHeaderSnapshot{}, errors.New("environment header source is not configured")
	}

	snapshot, err := source.InspectMCPServerHeaders(ctx, projectID, server.ID)
	switch {
	case errors.Is(err, environments.ErrMCPServerUnavailable):
		return environmentHeaderSnapshot{}, errServingConfigurationChanged
	case err != nil:
		return environmentHeaderSnapshot{}, fmt.Errorf("inspect environment headers: %w", err)
	}
	if snapshot.EnvironmentID != server.EnvironmentID ||
		snapshot.RemoteMcpServerID != server.RemoteMcpServerID ||
		snapshot.TunneledMcpServerID != server.TunneledMcpServerID ||
		(server.RemoteMcpServerID.Valid && snapshot.RemoteURL != remoteURL) {
		return environmentHeaderSnapshot{}, errServingConfigurationChanged
	}
	if !snapshot.EnvironmentLive {
		return environmentHeaderSnapshot{}, environments.ErrEnvironmentUnavailable
	}

	rows, err := proxy.EnvironmentHeaderRows(snapshot.Headers)
	if err != nil {
		return environmentHeaderSnapshot{}, fmt.Errorf("map environment headers: %w", err)
	}
	return environmentHeaderSnapshot{rows: rows}, nil
}

// isEnvironmentHeaderConfigError reports whether err is an operator
// configuration problem with the linked environment, as opposed to a database
// or decryption failure.
func isEnvironmentHeaderConfigError(err error) bool {
	return errors.Is(err, environments.ErrEnvironmentUnavailable) || errors.Is(err, proxy.ErrInvalidEnvironmentHeader)
}

// loadEnvironmentHeaders is [readEnvironmentHeaders] with the error shaped for
// an MCP client: a configuration problem is a client-visible refusal logged as
// a warning, anything else an unexpected failure. Logs carry entry names,
// never values.
func loadEnvironmentHeaders(ctx context.Context, logger *slog.Logger, source environmentHeaderSource, projectID uuid.UUID, server *mcpserversrepo.McpServer, remoteURL string) (environmentHeaderSnapshot, error) {
	snapshot, err := readEnvironmentHeaders(ctx, source, projectID, server, remoteURL)
	switch {
	case err == nil:
		return snapshot, nil
	case errors.Is(err, errServingConfigurationChanged):
		return environmentHeaderSnapshot{}, oops.E(oops.CodeConflict, err, servingConfigurationChanged).LogWarn(ctx, logger)
	case isEnvironmentHeaderConfigError(err):
		return environmentHeaderSnapshot{}, oops.E(oops.CodeBadRequest, err, environmentHeaderMisconfigured).LogWarn(ctx, logger)
	default:
		return environmentHeaderSnapshot{}, oops.E(oops.CodeUnexpected, err, "load mcp server environment headers").LogError(ctx, logger)
	}
}
