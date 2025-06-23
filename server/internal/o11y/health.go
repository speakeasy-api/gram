package o11y

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.temporal.io/sdk/client"
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

type NamedResource[T any] struct {
	Name     string
	Resource T
}

func NewHealthCheckHandler(
	databaseClients []*NamedResource[*pgxpool.Pool],
	redisClients []*NamedResource[*redis.Client],
	temporalClients []*NamedResource[client.Client],
) http.Handler {
	pingers := make([]health.Pinger, 0, len(databaseClients)+len(redisClients)+len(temporalClients))
	for _, db := range databaseClients {
		n := fmt.Sprintf("postgres:%s", db.Name)
		pingers = append(pingers, ping{name: n, timeout: 10 * time.Second, checkFunc: db.Resource.Ping})
	}

	for _, rc := range redisClients {
		n := fmt.Sprintf("redis:%s", rc.Name)
		pingers = append(pingers, ping{name: n, timeout: 10 * time.Second, checkFunc: func(ctx context.Context) error {
			err := rc.Resource.Ping(ctx).Err()
			if err != nil {
				return fmt.Errorf("redis health check failed: %w", err)
			}

			return nil
		}})
	}

	for _, tc := range temporalClients {
		n := fmt.Sprintf("temporal:%s", tc.Name)
		pingers = append(pingers, ping{name: n, timeout: 10 * time.Second, checkFunc: func(ctx context.Context) error {
			_, err := tc.Resource.CheckHealth(ctx, &client.CheckHealthRequest{})
			return fmt.Errorf("temporal health check failed: %w", err)
		}})
	}

	h := health.NewChecker(pingers...)
	return health.Handler(h)
}
