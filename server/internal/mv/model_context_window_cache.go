package mv

import (
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/cache"
)

type ModelContextWindow struct {
	ID     string
	Tokens int
}

var _ cache.CacheableObject[ModelContextWindow] = (*ModelContextWindow)(nil)

func (m ModelContextWindow) CacheKey() string {
	return ModelContextWindowCacheKey(m.ID)
}

func ModelContextWindowCacheKey(id string) string {
	return fmt.Sprintf("openrouter:model:context_window:%s", id)
}

func (m ModelContextWindow) TTL() time.Duration {
	return 72 * time.Hour
}

func (m ModelContextWindow) AdditionalCacheKeys() []string {
	return []string{}
}
