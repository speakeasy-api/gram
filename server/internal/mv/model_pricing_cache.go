package mv

import (
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/cache"
)

type ModelPricing struct {
	ID         string
	Prompt     string
	Completion string
	Request    string
	Image      string
}

var _ cache.CacheableObject[ModelPricing] = (*ModelPricing)(nil)

func (m ModelPricing) CacheKey() string {
	return ModelPricingCacheKey(m.ID)
}

func ModelPricingCacheKey(id string) string {
	return fmt.Sprintf("openrouter:model:pricing:%s", id)
}

func (m ModelPricing) TTL() time.Duration {
	return 72 * time.Hour
}

func (m ModelPricing) AdditionalCacheKeys() []string {
	return []string{}
}
