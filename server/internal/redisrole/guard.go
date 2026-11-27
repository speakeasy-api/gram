package redisrole

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// GuardMode selects what the guard does with a key sent to the wrong role.
// Its field is unexported, so a mode is GuardReport, GuardEnforce or the zero
// value, which reports; no other mode can reach a guard.
type GuardMode struct {
	enforce bool
}

var (
	// GuardReport lets the command run, logs it and counts it.
	GuardReport = GuardMode{enforce: false}

	// GuardEnforce refuses the command with ErrWrongRole without sending it.
	GuardEnforce = GuardMode{enforce: true}
)

const (
	guardReportName  = "report"
	guardEnforceName = "enforce"
)

// String returns the mode's name as ParseGuardMode reads it.
func (m GuardMode) String() string {
	if m.enforce {
		return guardEnforceName
	}
	return guardReportName
}

// ErrWrongRole is returned in enforce mode for a key sent to the client or
// adapter of another role, or a key the manifest does not classify.
var ErrWrongRole = errors.New("redis key sent to the wrong role")

const (
	meterWrongRole = "gram.redis.role.wrong_role_commands"

	// reportLogInterval bounds how often report mode logs a violation for the
	// same role and prefix. The counter records every violation; the log only
	// needs to show that one is happening and an example key.
	reportLogInterval = time.Minute
)

// ParseGuardMode reads a guard mode name. An empty value selects report.
func ParseGuardMode(value string) (GuardMode, error) {
	switch value {
	case "", guardReportName:
		return GuardReport, nil
	case guardEnforceName:
		return GuardEnforce, nil
	default:
		return GuardReport, fmt.Errorf("unknown redis role guard mode %q", value)
	}
}

// Guard checks that keys reach the client or adapter of their role.
type Guard struct {
	mode      GuardMode
	logger    *slog.Logger
	wrongRole metric.Int64Counter
	now       func() time.Time

	mu       sync.Mutex
	lastLogs map[string]time.Time
}

// NewGuard returns a guard in mode that logs to logger and counts violations
// on a meter from meterProvider.
func NewGuard(logger *slog.Logger, meterProvider metric.MeterProvider, mode GuardMode) *Guard {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/redisrole")
	wrongRole, err := meter.Int64Counter(
		meterWrongRole,
		metric.WithDescription("Redis commands whose key belongs to another role or to no declared family, by client role and matched prefix"),
		metric.WithUnit("{command}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterWrongRole), attr.SlogError(err))
	}

	return &Guard{
		mode:      mode,
		logger:    logger.With(attr.SlogComponent("redisrole")),
		wrongRole: wrongRole,
		now:       time.Now,
		mu:        sync.Mutex{},
		lastLogs:  map[string]time.Time{},
	}
}

// Mode reports the guard's mode.
func (g *Guard) Mode() GuardMode {
	return g.mode
}

// Check reports whether key may be sent through a client or adapter of role.
//
// A key of another role, and an unclassified key on any role, is counted with
// the client's role and the matched prefix (UnclassifiedPrefix for an
// unclassified key). In enforce mode Check then returns ErrWrongRole, so tests
// and local development refuse a key the manifest does not declare. In report
// mode it is logged and Check returns nil: production stores an unclassified
// key on whichever client the code used, which for code wired by role is the
// ops client, and the counter raises the alarm.
func (g *Guard) Check(ctx context.Context, role Role, command, key string) error {
	c := Classify(key)
	if c.Control || (c.Classified && c.Role == role) {
		return nil
	}

	if g.wrongRole != nil {
		g.wrongRole.Add(ctx, 1, metric.WithAttributes(
			attr.RedisRole(string(role)),
			attr.RedisKeyPrefix(c.Prefix),
			attr.RedisCommand(command),
		))
	}

	if g.mode.enforce {
		if !c.Classified {
			return fmt.Errorf("%w: unclassified key %q on the %s role; declare its family in redisrole's manifest", ErrWrongRole, keyHead(key), role)
		}
		return fmt.Errorf("%w: %s key %q on the %s role", ErrWrongRole, c.Role, keyHead(key), role)
	}

	if g.shouldLog(role, c.Prefix) {
		g.logger.WarnContext(ctx, "redis key sent to the wrong role",
			attr.SlogRedisRole(string(role)),
			attr.SlogRedisKeyPrefix(c.Prefix),
			attr.SlogRedisCommand(command),
			attr.SlogCacheNamespace(keyHead(key)),
		)
	}
	return nil
}

// keyHead returns key up to and including its first ":", or "" when it has
// none. Auth keys can carry live nonces and codes after the first segment, so
// logs and errors carry only the head.
func keyHead(key string) string {
	head, _, found := strings.Cut(key, ":")
	if !found {
		return ""
	}
	return head + ":"
}

func (g *Guard) shouldLog(role Role, prefix string) bool {
	id := string(role) + "|" + prefix
	now := g.now()

	g.mu.Lock()
	defer g.mu.Unlock()

	last, seen := g.lastLogs[id]
	if seen && now.Sub(last) < reportLogInterval {
		return false
	}
	g.lastLogs[id] = now
	return true
}

func (g *Guard) checkCommand(ctx context.Context, role Role, cmd redis.Cmder) error {
	for _, key := range commandKeys(cmd.Args()) {
		if err := g.Check(ctx, role, cmd.Name(), key); err != nil {
			return err
		}
	}
	return nil
}

// Hook returns a go-redis hook that checks every command a client of role
// sends, including each command of a pipeline or transaction.
func (g *Guard) Hook(role Role) redis.Hook {
	return guardHook{guard: g, role: role}
}

type guardHook struct {
	guard *Guard
	role  Role
}

func (h guardHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return next(ctx, network, addr)
	}
}

func (h guardHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if err := h.guard.checkCommand(ctx, h.role, cmd); err != nil {
			cmd.SetErr(err)
			return err
		}
		return next(ctx, cmd)
	}
}

func (h guardHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		for _, cmd := range cmds {
			if err := h.guard.checkCommand(ctx, h.role, cmd); err != nil {
				for _, failed := range cmds {
					failed.SetErr(err)
				}
				return err
			}
		}
		return next(ctx, cmds)
	}
}
