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
		if !row.EntryName.Valid {
			continue
		}
		entry := proxy.EnvironmentHeaderEntry{Name: row.EntryName.String, Value: row.EntryValue.String, Undecryptable: false}
		if row.EntryIsSecret.Bool {
			decrypted, derr := e.enc.Decrypt(row.EntryValue.String)
			if derr != nil {
				entry.Value = ""
				entry.Undecryptable = true
			} else {
				entry.Value = decrypted
			}
		}
		entries = append(entries, entry)
	}

	return MCPHeaderEnvironment{
		ID:      rows[0].EnvironmentID,
		Name:    rows[0].EnvironmentName,
		Slug:    rows[0].EnvironmentSlug,
		Headers: proxy.InspectEnvironmentHeaders(entries),
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
