package otel

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/database"
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
}

func newOrgDisabledChecker(db database.DBTX) *orgDisabledChecker {
	return &orgDisabledChecker{db: db, now: time.Now, mu: sync.Mutex{}, entries: make(map[string]orgDisabledEntry)}
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

	disabled := false
	org, err := orgrepo.New(c.db).GetOrganizationMetadata(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return false, fmt.Errorf("load organization metadata: %w", err)
	default:
		disabled = org.DisabledAt.Valid
	}

	c.mu.Lock()
	if len(c.entries) >= orgDisabledMaxEntries {
		clear(c.entries)
	}
	c.entries[organizationID] = orgDisabledEntry{disabled: disabled, expiresAt: now.Add(orgDisabledTTL)}
	c.mu.Unlock()
	return disabled, nil
}
