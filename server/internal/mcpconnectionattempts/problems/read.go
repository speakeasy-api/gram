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

// Read returns the suspected problems at now, or mcpconnectionattempts.ErrLogsOff.
// Failures before recordedSince (zero for none) cannot be seen to end; app versions need served rows.
func Read(ctx context.Context, ch driver.Conn, db repo.DBTX, logsEnabled func(context.Context, string) (bool, error), organizationID string, recordedSince, now time.Time) ([]Problem, error) {
	if organizationID == "" {
		return nil, errors.New("suspected problems need an organization")
	}
	triedSince := now.Add(-TriedWithin)
	if recordedSince.After(triedSince) {
		triedSince = recordedSince
	}
	episodes, err := mcpconnectionattempts.ReadFailingNow(ctx, ch, logsEnabled, mcpconnectionattempts.FailingNowFilter{
		OrganizationID: organizationID,
		ProviderHost:   "",
		ServerID:       "",
		TriedSince:     triedSince,
	}, now)
	if err != nil {
		return nil, fmt.Errorf("read failing now: %w", err)
	}
	if len(episodes) == 0 {
		return nil, nil
	}
	for i := range episodes {
		if episodes[i].OpenedAt.Before(recordedSince) {
			episodes[i].OpenedAt = recordedSince
		}
	}

	var upstreams map[string]Upstream
	var served []mcpconnectionattempts.ServedByFamily
	eg, egctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		var err error
		upstreams, err = readUpstreams(egctx, db, organizationID, episodes)
		return err
	})
	eg.Go(func() error {
		var err error
		served, err = mcpconnectionattempts.ReadServedByFamily(egctx, ch, organizationID, episodes)
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

// readUpstreams reads the configured upstream of each episode's mcp_servers row.
func readUpstreams(ctx context.Context, db repo.DBTX, organizationID string, episodes []mcpconnectionattempts.FailingPerson) (map[string]Upstream, error) {
	set := map[uuid.UUID]bool{}
	for _, e := range episodes {
		if id, err := uuid.Parse(e.BackingServerID()); err == nil {
			set[id] = true
		}
	}
	if len(set) == 0 {
		return map[string]Upstream{}, nil
	}

	rows, err := repo.New(db).ListServerUpstreams(ctx, repo.ListServerUpstreamsParams{OrganizationID: organizationID, ServerIds: slices.Collect(maps.Keys(set))})
	if err != nil {
		return nil, fmt.Errorf("list server upstreams: %w", err)
	}
	out := make(map[string]Upstream, len(rows))
	for _, r := range rows {
		out[r.ID.String()] = Upstream{URL: r.Url, Issuer: r.Issuer.String}
	}
	return out, nil
}
