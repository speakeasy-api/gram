package o11y

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/hashicorp/go-cleanhttp"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.temporal.io/sdk/client"
	"goa.design/clue/health"
)

type HTTPEndpoint struct {
	URL            *url.URL
	TLSCertificate []byte
}

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
	httpEndpoints []*NamedResource[*HTTPEndpoint],
	databaseClients []*NamedResource[*pgxpool.Pool],
	redisClients []*NamedResource[*redis.Client],
	temporalClients []*NamedResource[client.Client],
) http.Handler {
	pingers := make([]health.Pinger, 0, len(httpEndpoints)+len(databaseClients)+len(redisClients)+len(temporalClients))

	for _, h := range httpEndpoints {
		n := fmt.Sprintf("http:%s", h.Name)
		u := h.Resource.URL

		transport := cleanhttp.DefaultTransport()
		if h.Resource.TLSCertificate != nil {
			certPool := x509.NewCertPool()
			certPool.AppendCertsFromPEM(h.Resource.TLSCertificate)
			transport.TLSClientConfig = &tls.Config{RootCAs: certPool}
		}

		opts := []health.Option{
			health.WithScheme(u.Scheme),
			health.WithPath(u.Path),
			health.WithTimeout(10 * time.Second),
			health.WithTransport(transport),
		}

		pingers = append(pingers, health.NewPinger(n, u.Host, opts...))
	}

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
			if err != nil {
				return fmt.Errorf("temporal health check failed: %w", err)
			}

			return nil
		}})
	}

	h := health.NewChecker(pingers...)
	return health.Handler(h)
}
