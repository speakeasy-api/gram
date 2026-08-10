// Package exposure answers "are we already exposed?" for a requested MCP
// server: whether this organization is already talking to it, since when, how
// much, and by how many people. The shadow inventory is keyed by project, so
// the org-wide answer reads across every project the organization owns.
//
// This is the one part of an approval request's evidence that is observed
// rather than declared. Every other signal in the approval workflow is
// something the server or its registry says about itself; these come from
// traffic this organization actually produced.
//
// It is also what tells the admin what a denial costs. "Fourteen people have
// been using this for three months" and "nobody here has ever touched this"
// are opposite decisions, and neither is visible from the server itself.
package exposure

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// usageLookupLimit fills the required limit parameter of the usage query. The
// query only applies its limit on the unfiltered listing path; when canonical
// URLs are supplied, as here, it filters on those instead and the limit is
// unused — usage for the one URL asked about is always aggregated in full.
const usageLookupLimit = 1

// Status distinguishes the three outcomes a lookup can have.
//
// The distinction matters more here than in most evidence: "we looked and this
// server is new to us" and "we could not look" are very different inputs to a
// decision, and collapsing them into a single empty result would let the
// second read as the first.
type Status string

const (
	// StatusUnaddressable means the request names something this signal
	// cannot be looked up by — an stdio launch command, or a string that is
	// not a URL. The inventory is keyed by canonical URL, so there is nothing
	// to ask. It does NOT mean the server is unused here.
	StatusUnaddressable Status = "unaddressable"

	// StatusUnseen means the lookup ran and this project has no record of the
	// server. This is a real finding: nothing here uses it, so denying costs
	// nobody anything.
	StatusUnseen Status = "unseen"

	// StatusSeen means this project is already talking to the server.
	StatusSeen Status = "seen"
)

// Signals is what this project's own traffic says about a requested server.
type Signals struct {
	// Status is which of the three outcomes the lookup had. Read this before
	// anything else: the remaining fields are only meaningful when it is
	// StatusSeen.
	Status Status

	// CanonicalURL is the inventory key the lookup used. Empty when the target
	// was unaddressable. Surfaced so a reviewer can see what was actually
	// searched for, rather than trusting that the request matched.
	CanonicalURL string

	// URLHost is the host the canonical URL resolved to.
	URLHost string

	// ServerName is the name this project's own inventory records for the
	// server. It comes from observed traffic, so it can differ from the name
	// the server or a registry declares — a difference worth showing.
	ServerName string

	// FirstSeen is when this project first saw the server in its inventory.
	FirstSeen time.Time

	// LastSeen is the most recent inventory sighting.
	LastSeen time.Time

	// FirstCalled is the earliest recorded tool call against the server.
	// Zero when the server is in the inventory but has no recorded calls,
	// which is itself worth showing: it was discovered but never used.
	FirstCalled time.Time

	// LastCalled is the most recent recorded tool call.
	LastCalled time.Time

	// CallCount is how many calls this project has made to the server.
	CallCount uint64

	// UserCount is how many distinct people here have called it. This is the
	// number that decides what a denial costs.
	UserCount uint64
}

// InUse reports whether anyone here has actually called the server, as opposed
// to it merely appearing in the inventory.
func (s Signals) InUse() bool {
	return s.Status == StatusSeen && s.CallCount > 0
}

// Reader is the slice of the telemetry repo this package needs.
//
// Narrow so callers can substitute a fake: the real implementation is
// *telemetryrepo.Queries, which reaches ClickHouse.
type Reader interface {
	GetShadowMCPInventoryURLAcrossProjects(ctx context.Context, arg telemetryrepo.GetShadowMCPInventoryURLAcrossProjectsParams) (*telemetryrepo.ShadowMCPInventoryURLRow, error)
	ListShadowMCPInventoryUsageAcrossProjects(ctx context.Context, arg telemetryrepo.ListShadowMCPInventoryUsageAcrossProjectsParams) ([]telemetryrepo.ShadowMCPInventoryUsageRow, error)
}

var _ Reader = (*telemetryrepo.Queries)(nil)

// Assess looks up what this project's traffic says about the requested server.
//
// target should be the server's URL after identity resolution, not the raw
// request string: a stdio command that proxies through mcp-remote resolves to
// the URL it targets, and passing the raw command here would report a server
// the org already uses as unaddressable. Whatever is passed is canonicalized
// with the same function that writes the inventory, so a lookup cannot miss
// because the two normalized a URL differently.
//
// projectID bounds every read. The inventory is project-scoped and this
// function is the only thing that decides which project is asked about, so it
// takes the id rather than reading one from anywhere ambient.
func Assess(ctx context.Context, reader Reader, projectIDs []uuid.UUID, target string) (Signals, error) {
	inventoryURL, ok := shadowmcp.CanonicalizeInventoryURL(target)
	if !ok {
		return Signals{
			Status: StatusUnaddressable, CanonicalURL: "", URLHost: "",
			ServerName: "", FirstSeen: time.Time{}, LastSeen: time.Time{},
			FirstCalled: time.Time{}, LastCalled: time.Time{},
			CallCount: 0, UserCount: 0,
		}, nil
	}

	signals := Signals{
		Status: StatusUnseen, CanonicalURL: inventoryURL.CanonicalURL, URLHost: inventoryURL.URLHost,
		ServerName: "", FirstSeen: time.Time{}, LastSeen: time.Time{},
		FirstCalled: time.Time{}, LastCalled: time.Time{},
		CallCount: 0, UserCount: 0,
	}

	// No projects means the caller could not resolve the organization's
	// project set — the lookup cannot run, and reporting unseen would let a
	// failed resolution read as "nobody here uses this".
	if len(projectIDs) == 0 {
		return Signals{}, fmt.Errorf("no projects to read exposure from")
	}

	ids := make([]string, 0, len(projectIDs))
	for _, projectID := range projectIDs {
		ids = append(ids, projectID.String())
	}

	row, err := reader.GetShadowMCPInventoryURLAcrossProjects(ctx, telemetryrepo.GetShadowMCPInventoryURLAcrossProjectsParams{
		GramProjectIDs:     ids,
		CanonicalServerURL: inventoryURL.CanonicalURL,
	})
	if err != nil {
		return Signals{}, fmt.Errorf("read shadow mcp inventory: %w", err)
	}

	// A server absent from the inventory can still be absent from usage, so
	// the usage lookup is skipped rather than run against a URL nothing here
	// has ever recorded.
	if row == nil {
		return signals, nil
	}

	signals.Status = StatusSeen
	signals.ServerName = serverName(*row)
	signals.FirstSeen = row.FirstSeen
	signals.LastSeen = row.LastSeen

	usage, err := reader.ListShadowMCPInventoryUsageAcrossProjects(ctx, telemetryrepo.ListShadowMCPInventoryUsageAcrossProjectsParams{
		GramProjectIDs:      ids,
		CanonicalServerURLs: []string{inventoryURL.CanonicalURL},
		Limit:               usageLookupLimit,
	})
	if err != nil {
		return Signals{}, fmt.Errorf("read shadow mcp inventory usage: %w", err)
	}

	for _, candidate := range usage {
		// The query is asked about one URL, but it is a list query: match on
		// the key rather than taking the first row, so a widened result set
		// can never attribute another server's usage to this one.
		if candidate.CanonicalServerURL != inventoryURL.CanonicalURL {
			continue
		}

		signals.CallCount = candidate.CallCount
		signals.UserCount = candidate.UserCount
		if candidate.FirstCalled != nil {
			signals.FirstCalled = *candidate.FirstCalled
		}
		if candidate.LastCalled != nil {
			signals.LastCalled = *candidate.LastCalled
		}
		if signals.ServerName == "" {
			signals.ServerName = candidate.ServerName
		}

		break
	}

	return signals, nil
}

// serverName prefers the name an admin set over the one observed in traffic.
func serverName(row telemetryrepo.ShadowMCPInventoryURLRow) string {
	if row.ServerNameOverride != "" {
		return row.ServerNameOverride
	}

	return row.ServerName
}
