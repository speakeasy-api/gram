package externalmcp

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/cache"
)

const registryCacheTTL = 24 * time.Hour

// registryCacheSchemaVersion is baked into every registry cache key. Bump it
// whenever the cached shape changes (e.g. a new field on ExternalMCPServerEntry)
// so a deploy can't read blobs written by an older binary. The cache serializes
// structs as field-name-keyed msgpack maps, so a missing field silently decodes
// to its zero value rather than erroring — without this version, adding
// `supports_dcr` made every cached server read back as supports_dcr=false (i.e.
// "manual") for up to the 24h TTL.
const registryCacheSchemaVersion = "v2"

// CachedListServersResponse wraps a list of external MCP server summaries for caching.
type CachedListServersResponse struct {
	Key        string
	Servers    []*types.ExternalMCPServerEntry
	NextCursor *string
}

var _ cache.CacheableObject[CachedListServersResponse] = (*CachedListServersResponse)(nil)

func (c CachedListServersResponse) CacheKey() string              { return c.Key }
func (c CachedListServersResponse) AdditionalCacheKeys() []string { return []string{} }
func (c CachedListServersResponse) TTL() time.Duration            { return registryCacheTTL }

// CachedServerDetailsResponse wraps server details for caching.
type CachedServerDetailsResponse struct {
	Key     string
	Details *ServerDetails
}

var _ cache.CacheableObject[CachedServerDetailsResponse] = (*CachedServerDetailsResponse)(nil)

func (c CachedServerDetailsResponse) CacheKey() string              { return c.Key }
func (c CachedServerDetailsResponse) AdditionalCacheKeys() []string { return []string{} }
func (c CachedServerDetailsResponse) TTL() time.Duration            { return registryCacheTTL }

// registryCacheKey builds a cache key from a prefix and the request's URL + headers.
// Headers are sorted and hashed with SHA-256 to capture tenant/auth identity.
func registryCacheKey(prefix string, req *http.Request) string {
	// Sort header keys for deterministic hashing
	keys := make([]string, 0, len(req.Header))
	for k := range req.Header {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h := sha256.New()
	for _, k := range keys {
		vals := make([]string, len(req.Header[k]))
		copy(vals, req.Header[k])
		sort.Strings(vals)
		_, _ = fmt.Fprintf(h, "%s=%s\n", k, strings.Join(vals, ","))
	}

	return fmt.Sprintf("registry:%s:%s:%s:%x", prefix, registryCacheSchemaVersion, req.URL.String(), h.Sum(nil))
}
