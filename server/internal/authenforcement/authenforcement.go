// Package authenforcement reads which organizations have upstream auth
// enforcement enabled: their direct proxied servers are served by the typed
// auth client resolver, and using the chosen auth client requires mcp:auth.
//
// Staff enable it per organization once its auth clients, default auth
// permissions, credential choices and execution profiles are covered and its
// shadow evaluation agrees. An enabled organization is enforced whatever the
// resolver flags say, so a flag change never skips those checks. Disabling it
// is the organization's rollback to the upstream client choice, which stays
// in place until enforcement is stable everywhere.
package authenforcement

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"

	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
)

// cacheTTL bounds how long a request can see an organization's enforcement
// state after staff change it.
const cacheTTL = 30 * time.Second

// cacheSize bounds how many organizations' states one server keeps.
const cacheSize = 4096

// Gate reads organizations' enforcement state, caching it briefly.
type Gate struct {
	db    repo.DBTX
	cache *expirable.LRU[string, bool]
}

// NewGate returns a gate reading db.
func NewGate(db repo.DBTX) *Gate {
	return &Gate{db: db, cache: expirable.NewLRU[string, bool](cacheSize, nil, cacheTTL)}
}

// Enforced reports whether organizationID has enforcement enabled. A nil gate
// enforces nothing.
func (g *Gate) Enforced(ctx context.Context, organizationID string) (bool, error) {
	if g == nil || organizationID == "" {
		return false, nil
	}
	if enforced, ok := g.cache.Get(organizationID); ok {
		return enforced, nil
	}
	enforced, err := repo.New(g.db).IsFeatureEnabled(ctx, repo.IsFeatureEnabledParams{
		OrganizationID: organizationID,
		FeatureName:    string(productfeatures.FeatureUpstreamAuthEnforced),
	})
	if err != nil {
		return false, fmt.Errorf("read auth enforcement: %w", err)
	}
	g.cache.Add(organizationID, enforced)
	return enforced, nil
}
