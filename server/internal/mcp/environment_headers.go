package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// environmentHeaderSource reads the MCP_HEADER_ entries of an MCP server's
// linked environment.
type environmentHeaderSource interface {
	InspectMCPHeaders(ctx context.Context, projectID uuid.UUID, environmentID uuid.UUID) (environments.MCPHeaderEnvironment, error)
}

// environmentHeaderSnapshot is the set of environment headers loaded once for
// one logical request, so every exchange built for that request sends the
// same values.
type environmentHeaderSnapshot struct {
	rows []proxy.ConfiguredHeader
}

// environmentHeaderMisconfigured is the message an MCP client sees when the
// served server's environment headers cannot be sent. It names nothing from
// the environment: callers of a public server are anonymous.
const environmentHeaderMisconfigured = "this MCP server's environment headers are misconfigured; contact the MCP server administrator"

// readEnvironmentHeaders returns the headers mapped from the environment
// linked to an MCP server. A server with no linked environment has none. A
// linked environment that is unavailable, or holds an MCP_HEADER_ entry that
// cannot be sent, is an error: serving must not fall back to the source's
// value for a header the environment was meant to set. A non-null link with no
// source wired is an error too, never a silent "no headers".
func readEnvironmentHeaders(ctx context.Context, source environmentHeaderSource, projectID uuid.UUID, environmentID uuid.NullUUID) (environmentHeaderSnapshot, error) {
	if !environmentID.Valid {
		return environmentHeaderSnapshot{rows: nil}, nil
	}
	if source == nil {
		return environmentHeaderSnapshot{}, errors.New("environment header source is not configured")
	}

	env, err := source.InspectMCPHeaders(ctx, projectID, environmentID.UUID)
	if err != nil {
		return environmentHeaderSnapshot{}, fmt.Errorf("inspect environment headers: %w", err)
	}
	rows, err := proxy.EnvironmentHeaderRows(env.Headers)
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
func loadEnvironmentHeaders(ctx context.Context, logger *slog.Logger, source environmentHeaderSource, projectID uuid.UUID, environmentID uuid.NullUUID) (environmentHeaderSnapshot, error) {
	snapshot, err := readEnvironmentHeaders(ctx, source, projectID, environmentID)
	switch {
	case err == nil:
		return snapshot, nil
	case isEnvironmentHeaderConfigError(err):
		return environmentHeaderSnapshot{}, oops.E(oops.CodeBadRequest, err, environmentHeaderMisconfigured).LogWarn(ctx, logger)
	default:
		return environmentHeaderSnapshot{}, oops.E(oops.CodeUnexpected, err, "load mcp server environment headers").LogError(ctx, logger)
	}
}
