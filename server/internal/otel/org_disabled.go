package otel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/singleflight"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

// orgDisabledTTL bounds how long a transform keeps admitting a newly disabled
// organization's queued telemetry.
const orgDisabledTTL = 30 * time.Second

const orgDisabledMaxEntries = 10_000

type orgDisabledEntry struct {
	disabled  bool
	expiresAt time.Time
}

// orgDisabledChecker reports whether an organization is disabled, caching each
// answer briefly so a busy topic does not read Postgres per record.
type orgDisabledChecker struct {
	db      database.DBTX
	now     func() time.Time
	mu      sync.Mutex
	entries map[string]orgDisabledEntry
	group   singleflight.Group
}

func newOrgDisabledChecker(db database.DBTX) *orgDisabledChecker {
	return &orgDisabledChecker{db: db, now: time.Now, mu: sync.Mutex{}, entries: make(map[string]orgDisabledEntry), group: singleflight.Group{}}
}

// Disabled is false for an unknown organization or a nil checker, so only a
// known disablement drops a record.
func (c *orgDisabledChecker) Disabled(ctx context.Context, organizationID string) (bool, error) {
	if c == nil || c.db == nil || organizationID == "" {
		return false, nil
	}
	now := c.now()

	c.mu.Lock()
	entry, ok := c.entries[organizationID]
	c.mu.Unlock()
	if ok && now.Before(entry.expiresAt) {
		return entry.disabled, nil
	}

	// Concurrent misses for one organization share a single read.
	v, err, _ := c.group.Do(organizationID, func() (any, error) {
		org, err := orgrepo.New(c.db).GetOrganizationMetadata(ctx, organizationID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return false, nil
		case err != nil:
			return false, fmt.Errorf("load organization metadata: %w", err)
		default:
			return org.DisabledAt.Valid, nil
		}
	})
	if err != nil {
		return false, err
	}
	disabled, _ := v.(bool)

	c.mu.Lock()
	if len(c.entries) >= orgDisabledMaxEntries {
		clear(c.entries)
	}
	c.entries[organizationID] = orgDisabledEntry{disabled: disabled, expiresAt: now.Add(orgDisabledTTL)}
	c.mu.Unlock()
	return disabled, nil
}

// Drop reports whether a transform should acknowledge a record without
// publishing it because its organization is disabled.
func (c *orgDisabledChecker) Drop(ctx context.Context, logger *slog.Logger, organizationID string) (bool, error) {
	disabled, err := c.Disabled(ctx, organizationID)
	if err != nil {
		return false, o11y.LogError(ctx, logger, err, "failed to check organization status")
	}
	if disabled {
		logger.DebugContext(ctx, "dropping queued telemetry for disabled organization", attr.SlogOrganizationID(organizationID))
	}
	return disabled, nil
}
