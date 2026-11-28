package auth

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgprovision"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type capturedLog struct {
	level   slog.Level
	message string
	attrs   map[string]slog.Value
}

type capturingLogHandler struct {
	mu      sync.Mutex
	records []capturedLog
}

func (h *capturingLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *capturingLogHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := make(map[string]slog.Value, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value
		return true
	})
	h.mu.Lock()
	h.records = append(h.records, capturedLog{
		level:   r.Level,
		message: r.Message,
		attrs:   attrs,
	})
	h.mu.Unlock()
	return nil
}

func (h *capturingLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *capturingLogHandler) WithGroup(string) slog.Handler      { return h }

func (h *capturingLogHandler) snapshot() []capturedLog {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]capturedLog(nil), h.records...)
}

func TestAllowOrgCreation_LogsStructuredWarnings(t *testing.T) {
	t.Parallel()

	env, cleanup, err := testenv.Launch(t.Context(), testenv.LaunchOptions{Redis: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })

	redisClient, err := env.NewRedisClient(t, 0)
	require.NoError(t, err)

	handler := &capturingLogHandler{}
	svc := &Service{
		logger: slog.New(handler),
		orgCreationLimiter: ratelimit.New(
			ratelimit.NewRedisStore(redisClient),
			"test-log-rl-"+uuid.NewString(),
			ratelimit.PerHour(1),
		),
	}

	userID := "user-log-" + uuid.NewString()
	require.NoError(t, svc.allowOrgCreation(t.Context(), userID, orgprovision.SourceSignup))
	require.Empty(t, handler.snapshot())

	err = svc.allowOrgCreation(t.Context(), userID, orgprovision.SourceSignup)
	require.ErrorIs(t, err, ErrOrgCreationRateLimited)

	logs := handler.snapshot()
	require.Len(t, logs, 1)
	require.Equal(t, slog.LevelWarn, logs[0].level)
	require.Equal(t, "self-serve organization creation rate limited", logs[0].message)
	require.Equal(t, userID, logs[0].attrs[string(attr.UserIDKey)].String())
	require.Equal(t, orgprovision.SourceSignup, logs[0].attrs[string(attr.ReasonKey)].String())
	require.Greater(t, logs[0].attrs[string(attr.RetryWaitKey)].Duration(), time.Duration(0))

	// Redis unavailability fails open with a warning log.
	down := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: -1, DialTimeout: 100 * time.Millisecond})
	t.Cleanup(func() { _ = down.Close() })

	downHandler := &capturingLogHandler{}
	downSvc := &Service{
		logger: slog.New(downHandler),
		orgCreationLimiter: ratelimit.New(
			ratelimit.NewRedisStore(down),
			"test-log-down-rl-"+uuid.NewString(),
			ratelimit.PerHour(1),
		),
	}
	require.NoError(t, downSvc.allowOrgCreation(t.Context(), userID, orgprovision.SourcePlatformMCP))
	downLogs := downHandler.snapshot()
	require.Len(t, downLogs, 1)
	require.Equal(t, slog.LevelWarn, downLogs[0].level)
	require.Equal(t, "organization creation rate limiter unavailable; allowing", downLogs[0].message)
	require.Equal(t, userID, downLogs[0].attrs[string(attr.UserIDKey)].String())
}
