package o11y

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"goa.design/clue/health"
)

type ping struct {
	name      string
	timeout   time.Duration
	checkFunc func(ctx context.Context) error
}

func (p ping) Ping(ctx context.Context) error {
	d := p.timeout
	if d == 0 {
		d = 10 * time.Second
	}
	tctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()

	return p.checkFunc(tctx)
}
func (p ping) Name() string {
	return p.name
}

func NewHealthCheckHandler(db *pgxpool.Pool, cache *redis.Client) http.Handler {
	pingers := []health.Pinger{
		ping{name: "database", timeout: 10 * time.Second, checkFunc: db.Ping},
		ping{name: "cache", timeout: 10 * time.Second, checkFunc: func(ctx context.Context) error { return cache.Ping(ctx).Err() }},
	}

	h := health.NewChecker(pingers...)
	return health.Handler(h)
}
