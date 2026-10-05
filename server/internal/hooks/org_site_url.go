package hooks

import (
	"context"
	"net/url"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	organizationsRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

const (
	// orgDefaultHostCacheTTL bounds how long a deny link keeps an organization's
	// default host after it changes. Hosts change rarely and only by operators.
	orgDefaultHostCacheTTL = 5 * time.Minute
	// orgDefaultHostLookupTimeout caps the one database read a cache miss costs
	// a deny response, so a slow database cannot delay the block.
	orgDefaultHostLookupTimeout = 250 * time.Millisecond
)

// orgDefaultHostCache remembers each organization's stored default host for
// orgDefaultHostCacheTTL, so consecutive deny responses for an organization do
// not each read its row.
type orgDefaultHostCache struct {
	mu      sync.Mutex
	entries map[string]orgDefaultHostEntry
}

type orgDefaultHostEntry struct {
	defaultHost pgtype.Text
	expiresAt   time.Time
}

func newOrgDefaultHostCache() *orgDefaultHostCache {
	return &orgDefaultHostCache{mu: sync.Mutex{}, entries: map[string]orgDefaultHostEntry{}}
}

func (c *orgDefaultHostCache) get(organizationID string, now time.Time) (pgtype.Text, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[organizationID]
	if !ok {
		return pgtype.Text{String: "", Valid: false}, false
	}
	if now.After(entry.expiresAt) {
		delete(c.entries, organizationID)
		return pgtype.Text{String: "", Valid: false}, false
	}
	return entry.defaultHost, true
}

// put records an organization's host and sweeps out every expired entry, so
// organizations that stop hitting denies don't stay in the map. Puts only
// happen on a cache miss, and the map holds at most the organizations seen
// within the TTL, so the sweep stays cheap.
func (c *orgDefaultHostCache) put(organizationID string, defaultHost pgtype.Text, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, entry := range c.entries {
		if now.After(entry.expiresAt) {
			delete(c.entries, id)
		}
	}
	c.entries[organizationID] = orgDefaultHostEntry{defaultHost: defaultHost, expiresAt: now.Add(orgDefaultHostCacheTTL)}
}

// orgSiteURL returns the dashboard base URL of the links a deny response hands
// to an organization's users: the block page, the policy acknowledgement and
// the policy bypass request. It follows the organization's default host, and
// an organization without one (or whose row cannot be read in time) gets the
// legacy host.
//
// Deny responses must stay immediate, so the stored host is cached per
// organization and a cache miss reads the row under a short timeout. A failed
// read is not cached, so the next deny retries it.
func (s *Service) orgSiteURL(ctx context.Context, organizationID string) *url.URL {
	return s.orgHosts.SiteURL(s.orgDefaultHost(ctx, organizationID))
}

func (s *Service) orgDefaultHost(ctx context.Context, organizationID string) pgtype.Text {
	none := pgtype.Text{String: "", Valid: false}
	if organizationID == "" {
		return none
	}
	now := time.Now()
	if defaultHost, ok := s.orgHostCache.get(organizationID, now); ok {
		return defaultHost
	}
	lookupCtx, cancel := context.WithTimeout(ctx, orgDefaultHostLookupTimeout)
	defer cancel()
	org, err := organizationsRepo.New(s.db).GetOrganizationMetadata(lookupCtx, organizationID)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to read organization default host for deny links; using legacy host",
			attr.SlogError(err),
			attr.SlogOrganizationID(organizationID),
		)
		return none
	}
	s.orgHostCache.put(organizationID, org.DefaultHost, now)
	return org.DefaultHost
}
