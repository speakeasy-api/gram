package environments

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// ErrEnvironmentUnavailable reports that an MCP server's linked environment is
// deleted, missing or belongs to another project. Serving refuses the request:
// the link is still set, and ignoring it would send the source's values in
// place of the environment's.
var ErrEnvironmentUnavailable = errors.New("linked environment is unavailable")

// MCPHeaderEnvironment is a live environment and the inspection of its
// MCP_HEADER_ entries, read in one consistent snapshot.
type MCPHeaderEnvironment struct {
	// ID is the environment id.
	ID uuid.UUID

	// Name is the environment's display name.
	Name string

	// Slug is the environment's slug.
	Slug string

	// Headers classifies every MCP_HEADER_ entry, ordered by entry name.
	Headers []proxy.EnvironmentHeaderInspection
}

// InspectMCPHeaders reads the environment environmentID of projectID and
// classifies its MCP_HEADER_ entries. Only those entries are read and
// decrypted; an entry that cannot be decrypted is reported as such rather than
// failing the read, so a report can still describe the others. It returns
// [ErrEnvironmentUnavailable] when the environment is deleted, missing or in
// another project.
func (e *EnvironmentEntries) InspectMCPHeaders(ctx context.Context, projectID uuid.UUID, environmentID uuid.UUID) (MCPHeaderEnvironment, error) {
	rows, err := e.repo.ListMCPHeaderEnvironmentEntries(ctx, repo.ListMCPHeaderEnvironmentEntriesParams{
		EnvironmentID: environmentID,
		ProjectID:     projectID,
	})
	if err != nil {
		return MCPHeaderEnvironment{}, fmt.Errorf("list mcp header environment entries: %w", err)
	}
	if len(rows) == 0 {
		return MCPHeaderEnvironment{}, ErrEnvironmentUnavailable
	}

	entries := make([]proxy.EnvironmentHeaderEntry, 0, len(rows))
	for _, row := range rows {
		if row.EntryName.Valid {
			entries = append(entries, e.revealMCPHeaderEntry(row.EntryName.String, row.EntryValue.String, row.EntryIsSecret.Bool))
		}
	}

	return MCPHeaderEnvironment{
		ID:      rows[0].EnvironmentID,
		Name:    rows[0].EnvironmentName,
		Slug:    rows[0].EnvironmentSlug,
		Headers: proxy.InspectEnvironmentHeaders(entries),
	}, nil
}

// revealMCPHeaderEntry decrypts one MCP_HEADER_ entry for inspection. A value
// that cannot be decrypted is reported, never returned.
func (e *EnvironmentEntries) revealMCPHeaderEntry(name, value string, isSecret bool) proxy.EnvironmentHeaderEntry {
	entry := proxy.EnvironmentHeaderEntry{Name: name, Value: value, Undecryptable: false}
	if isSecret {
		decrypted, err := e.enc.Decrypt(value)
		if err != nil {
			entry.Value = ""
			entry.Undecryptable = true
		} else {
			entry.Value = decrypted
		}
	}
	return entry
}

// ErrMCPServerUnavailable reports that the MCP server is deleted or missing.
var ErrMCPServerUnavailable = errors.New("mcp server is unavailable")

// MCPServerHeaderSnapshot is an MCP server's backend, remote source URL,
// environment link and the inspection of that environment's MCP_HEADER_
// entries, all read at one instant.
type MCPServerHeaderSnapshot struct {
	// EnvironmentID is the server's environment link.
	EnvironmentID uuid.NullUUID

	// EnvironmentLive reports whether the linked environment exists, is not
	// deleted and belongs to the server's project.
	EnvironmentLive bool

	// RemoteMcpServerID is the server's remote source, if any.
	RemoteMcpServerID uuid.NullUUID

	// TunneledMcpServerID is the server's tunneled source, if any.
	TunneledMcpServerID uuid.NullUUID

	// RemoteURL is the remote source's URL, empty for other backends or a
	// deleted source.
	RemoteURL string

	// Headers classifies every MCP_HEADER_ entry of a live linked
	// environment, ordered by entry name.
	Headers []proxy.EnvironmentHeaderInspection
}

// InspectMCPServerHeaders reads the snapshot for the MCP server serverID of
// projectID. It returns [ErrMCPServerUnavailable] when the server is deleted
// or missing.
func (e *EnvironmentEntries) InspectMCPServerHeaders(ctx context.Context, projectID uuid.UUID, serverID uuid.UUID) (MCPServerHeaderSnapshot, error) {
	rows, err := e.repo.GetMCPServerHeaderSnapshot(ctx, repo.GetMCPServerHeaderSnapshotParams{
		McpServerID: serverID,
		ProjectID:   projectID,
	})
	if err != nil {
		return MCPServerHeaderSnapshot{}, fmt.Errorf("get mcp server header snapshot: %w", err)
	}
	if len(rows) == 0 {
		return MCPServerHeaderSnapshot{}, ErrMCPServerUnavailable
	}

	entries := make([]proxy.EnvironmentHeaderEntry, 0, len(rows))
	for _, row := range rows {
		if row.EntryName.Valid {
			entries = append(entries, e.revealMCPHeaderEntry(row.EntryName.String, row.EntryValue.String, row.EntryIsSecret.Bool))
		}
	}

	first := rows[0]
	return MCPServerHeaderSnapshot{
		EnvironmentID:       first.ServerEnvironmentID,
		EnvironmentLive:     first.LiveEnvironmentID.Valid,
		RemoteMcpServerID:   first.ServerRemoteMcpServerID,
		TunneledMcpServerID: first.ServerTunneledMcpServerID,
		RemoteURL:           first.RemoteUrl.String,
		Headers:             proxy.InspectEnvironmentHeaders(entries),
	}, nil
}

// MCPHeaderNearMissNames lists the names, never the values, of entries in the
// environment environmentID of projectID that resemble the MCP_HEADER_ prefix
// without matching it exactly, such as mcp_header_ or HEADER_. They are not
// sent upstream; a report lists them so an operator can spot the typo.
func (e *EnvironmentEntries) MCPHeaderNearMissNames(ctx context.Context, projectID uuid.UUID, environmentID uuid.UUID) ([]string, error) {
	names, err := e.repo.ListMCPHeaderNearMissEntryNames(ctx, repo.ListMCPHeaderNearMissEntryNamesParams{
		EnvironmentID: environmentID,
		ProjectID:     projectID,
	})
	if err != nil {
		return nil, fmt.Errorf("list mcp header near-miss entry names: %w", err)
	}
	return names, nil
}
