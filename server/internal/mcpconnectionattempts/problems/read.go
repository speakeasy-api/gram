package problems

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/problems/repo"
)

// ReadScope is whose episodes are grouped.
type ReadScope struct {
	// OrganizationID is the organization. Required.
	OrganizationID string

	// ProjectID keeps one project's episodes. Required unless WholeOrganization.
	ProjectID uuid.UUID

	// WholeOrganization reads every project and organization gateway instead of one project.
	WholeOrganization bool

	// IssuerID, when set, keeps one MCP authentication provider's failures, attributed as Connection Logs does.
	IssuerID uuid.UUID

	// IssuerServerIDs are the servers and gateways attached to IssuerID now.
	IssuerServerIDs []string
}

// Read returns the suspected problems of the scope at now, or mcpconnectionattempts.ErrLogsOff.
// Episodes count from recordedSince at the earliest; app-version problems need served rows.
// Callers pass recordedSince no earlier than when the organization's logs were last turned back on.
func Read(ctx context.Context, ch driver.Conn, db repo.DBTX, logsEnabled func(context.Context, string) (bool, error), scope ReadScope, recordedSince, now time.Time) ([]Problem, error) {
	switch {
	case scope.OrganizationID == "":
		return nil, errors.New("suspected problems need an organization")
	case scope.ProjectID == uuid.Nil && !scope.WholeOrganization:
		return nil, errors.New("suspected problems need a project")
	case scope.ProjectID != uuid.Nil && scope.WholeOrganization:
		return nil, errors.New("suspected problems need a project or the whole organization, not both")
	}
	organizationID := scope.OrganizationID
	triedSince := now.Add(-triedWithin)
	if recordedSince.After(triedSince) {
		triedSince = recordedSince
	}
	episodes, err := mcpconnectionattempts.ReadFailingNow(ctx, ch, logsEnabled, mcpconnectionattempts.FailingNowFilter{
		OrganizationID:  organizationID,
		ProjectID:       scope.ProjectID,
		IssuerID:        scope.IssuerID,
		IssuerServerIDs: scope.IssuerServerIDs,
		ProviderHost:    "",
		ServerID:        "",
		TriedSince:      triedSince,
	}, now)
	if err != nil {
		return nil, fmt.Errorf("read failing now: %w", err)
	}
	episodes = sinceRecording(episodes, triedSince, recordedSince, now)
	if len(episodes) == 0 {
		return nil, nil
	}

	var upstreams map[string]Upstream
	var served []mcpconnectionattempts.ServedByFamily
	eg, egctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		var err error
		upstreams, err = readUpstreams(egctx, db, organizationID, scope.ProjectID, episodes)
		return err
	})
	eg.Go(func() error {
		var err error
		served, err = mcpconnectionattempts.ReadServedByFamily(egctx, ch, organizationID, scope.ProjectID, episodes)
		if err != nil {
			return fmt.Errorf("read served by family: %w", err)
		}
		return nil
	})
	if err := eg.Wait(); err != nil {
		return nil, fmt.Errorf("read suspected problem inputs: %w", err)
	}
	return Group(organizationID, episodes, upstreams, served, now), nil
}

// sinceRecording drops episodes last failing before triedSince or still in grace, counting from recordedSince.
func sinceRecording(episodes []mcpconnectionattempts.FailingPerson, triedSince, recordedSince, now time.Time) []mcpconnectionattempts.FailingPerson {
	out := episodes[:0]
	for _, e := range episodes {
		if e.LastFailureAt.Before(triedSince) {
			continue
		}
		if e.OpenedAt.Before(recordedSince) {
			e.OpenedAt = recordedSince
			if now.Sub(e.OpenedAt) < mcpconnectionattempts.GraceOf(e.Reason) {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}

// readUpstreams reads the configured upstream of each episode's mcp_servers row.
func readUpstreams(ctx context.Context, db repo.DBTX, organizationID string, projectID uuid.UUID, episodes []mcpconnectionattempts.FailingPerson) (map[string]Upstream, error) {
	set := map[uuid.UUID]bool{}
	for _, e := range episodes {
		if id, err := uuid.Parse(e.BackingServerID()); err == nil {
			set[id] = true
		}
	}
	if len(set) == 0 {
		return map[string]Upstream{}, nil
	}

	rows, err := repo.New(db).ListServerUpstreams(ctx, repo.ListServerUpstreamsParams{OrganizationID: organizationID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: projectID != uuid.Nil}, ServerIds: slices.Collect(maps.Keys(set))})
	if err != nil {
		return nil, fmt.Errorf("list server upstreams: %w", err)
	}
	out := make(map[string]Upstream, len(rows))
	for _, r := range rows {
		out[r.ID.String()] = Upstream{URL: r.Url.String, Issuer: r.Issuer.String}
	}
	return out, nil
}
