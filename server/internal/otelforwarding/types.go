package otelforwarding

import (
	"time"
)

const cacheTTL = 30 * time.Second

// CachedConfig holds the decrypted forwarding config for one org.
// An empty struct (URL == "") means "no config" — cached to avoid hitting
// the DB on every OTEL request for orgs that haven't configured forwarding.
type CachedConfig struct {
	OrganizationID string            `json:"organization_id"`
	URL            string            `json:"url"`
	Headers        map[string]string `json:"headers"`
	Enabled        bool              `json:"enabled"`
}

func (c CachedConfig) CacheKey() string {
	return "otel-forwarding:" + c.OrganizationID
}

func (c CachedConfig) AdditionalCacheKeys() []string {
	return nil
}

func (c CachedConfig) TTL() time.Duration {
	return cacheTTL
}

// IsConfigured reports whether the org has a non-deleted forwarding config.
// Note: a configured-but-disabled config still returns true here; callers
// check Enabled separately.
func (c CachedConfig) IsConfigured() bool {
	return c.URL != ""
}

// emptyCachedConfig returns an exhaustruct-friendly zero value scoped to a
// specific org. Used as the cache sentinel for orgs with no configured
// forwarding and as the return value on error paths.
func emptyCachedConfig(orgID string) CachedConfig {
	return CachedConfig{
		OrganizationID: orgID,
		URL:            "",
		Headers:        nil,
		Enabled:        false,
	}
}
