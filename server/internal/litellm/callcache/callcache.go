package callcache

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/cache"
)

const ttl = 24 * time.Hour

type Record struct {
	ProjectID         uuid.UUID
	CallID            string
	TraceID           string
	SessionID         string
	UserID            string
	Email             string
	OriginatingClient string
}

type Cache struct {
	cache cache.Cache
}

func New(cacheImpl cache.Cache) *Cache {
	return &Cache{cache: cacheImpl}
}

func (c *Cache) Store(ctx context.Context, record Record) error {
	if err := c.cache.Set(ctx, key(record.ProjectID, record.CallID), record, ttl); err != nil {
		return fmt.Errorf("store LiteLLM call: %w", err)
	}
	return nil
}

func (c *Cache) Get(ctx context.Context, projectID uuid.UUID, callID string) (Record, error) {
	var record Record
	if err := c.cache.Get(ctx, key(projectID, callID), &record); err != nil {
		return Record{
			ProjectID:         uuid.Nil,
			CallID:            "",
			TraceID:           "",
			SessionID:         "",
			UserID:            "",
			Email:             "",
			OriginatingClient: "",
		}, fmt.Errorf("get LiteLLM call: %w", err)
	}
	return record, nil
}

func IsMiss(err error) bool {
	return errors.Is(err, redisCache.ErrCacheMiss)
}

func key(projectID uuid.UUID, callID string) string {
	return "litellm:call:" + projectID.String() + ":" + fmt.Sprintf("%x", sha256.Sum256([]byte(callID)))
}
