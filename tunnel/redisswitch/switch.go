// Package redisswitch moves a running process from one Redis instance to
// another without restarting it.
//
// Every process polls two keys on the current instance. While HoldKey exists,
// commands wait. Once StateKey says the instance has moved, the process
// reconnects to the next instance and carries on. An operator copies the data
// during the hold, so exactly one instance accepts writes at any moment.
//
// A process may use the current instance only while it holds a reading of
// StateKey that says so and is under a second old. Anything else (a missing
// key, a value it does not recognise, a read that failed or stalled) makes it
// wait. The operator's job closes every connection once the hold has been
// seen, so a process that fell behind cannot write through an old one, and it
// refuses to open a new one without a fresh reading.
//
// A client follows the state key of one role. A process that keeps different
// workloads on different instances gives each its own client and role, so one
// workload can move while the others stay. The hold key is shared: a copy
// pauses every client of the instance, which is what lets the operator's job
// prove nothing else wrote to it.
package redisswitch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// stateKeyPrefix precedes a role's name in its state key.
	stateKeyPrefix = "gram:redis-switch:state:"

	// DefaultRole is the role of a process that keeps everything on one
	// instance.
	DefaultRole = "all"

	// HoldKey exists, with an expiry, while an operator is copying the data.
	// Its expiry bounds the pause: if the operator's job dies, the key goes and
	// processes resume on the current instance.
	HoldKey = "gram:redis-switch:hold"

	// ClientName is set on every connection a switch-aware process opens, so
	// the operator's job can tell them apart from clients that would ignore a
	// hold.
	ClientName = "gram-redisswitch"

	// EnvNextAddr names the environment variable holding the next instance's
	// address. The switch is inactive when it is empty.
	EnvNextAddr = "GRAM_REDIS_NEXT_ADDR"

	// EnvNextPassword names the environment variable holding the next
	// instance's password.
	EnvNextPassword = "GRAM_REDIS_NEXT_PASSWORD"

	valueCurrent = "current"
	movedPrefix  = "moved:"

	// pollInterval is how often a process reads the keys. It bounds how long a
	// process keeps sending commands after an operator sets the hold.
	pollInterval = 250 * time.Millisecond

	// pollTimeout bounds one read of the keys, so a slow instance cannot stall
	// the poller for longer than two missed reads.
	pollTimeout = 500 * time.Millisecond

	// staleAfter is how long a reading is trusted, measured from when its
	// request was sent. The operator waits longer than staleAfter plus the
	// write timeout before closing connections and copying.
	staleAfter = time.Second

	// startTimeout bounds the first read of the keys at startup.
	startTimeout = 5 * time.Second

	// maxWait is the longest a command waits for a hold to end. The operator's
	// hold expires well inside it, so only an instance that stays unreadable
	// reaches it; the command then fails instead of waiting for ever.
	maxWait = 60 * time.Second

	// keepAlive matches the go-redis default dialer.
	keepAlive = 5 * time.Minute
)

// ErrHolding is returned when a command cannot run because the switch is
// holding: its wait ended, or it needed a new connection.
var ErrHolding = errors.New("redis switch is holding")

// ErrNotArmed is returned at startup when the current instance has no state
// key for the role. An operator must set it before processes are configured
// with a next instance.
var ErrNotArmed = errors.New("redis switch state key is not set on the current instance")

// StateKey names the key that says which instance is authoritative for a role.
// On the current instance its value is "current" until the move, then
// "moved:<run id>". It never expires.
func StateKey(role string) string {
	return stateKeyPrefix + role
}

type state string

const (
	// stateCurrent sends commands to the current instance.
	stateCurrent state = "current"

	// stateHold makes commands wait because HoldKey exists.
	stateHold state = "hold"

	// stateUnknown makes commands wait because the keys could not be read, or
	// StateKey is missing or unrecognised.
	stateUnknown state = "unknown"

	// stateNext sends commands to the next instance. It is final.
	stateNext state = "next"
)

// Target is one Redis instance.
type Target struct {
	// Addr is the instance's host:port.
	Addr string

	// Password is the instance's AUTH string.
	Password string
}

// NextFromEnv reads the next instance from the environment.
func NextFromEnv() Target {
	return Target{
		Addr:     strings.TrimSpace(os.Getenv(EnvNextAddr)),
		Password: os.Getenv(EnvNextPassword),
	}
}

// Switch tracks which instance a process should use for one role.
type Switch struct {
	logger      *slog.Logger
	stateKey    string
	current     Target
	next        Target
	poller      *redis.Client
	dialTimeout time.Duration
	now         func() time.Time

	mu       sync.Mutex
	state    state
	readAt   time.Time
	changed  chan struct{}
	conns    map[*trackedConn]struct{}
	lastSeen state
}

// switches maps a current instance's address to its Switch, so a second client
// built from the first one's options gets the same gate.
var switches sync.Map

// NewClient returns a client for opts. When next names an instance, the client
// follows the role's switch keys on the instance in opts; otherwise it is an
// ordinary client. The poller runs for the life of the process: callers often
// pass a startup context, and a poller that stopped with it would leave the
// process unable to see a hold.
func NewClient(ctx context.Context, logger *slog.Logger, opts *redis.Options, role string, next Target) (*redis.Client, error) {
	if next.Addr == "" {
		return redis.NewClient(opts), nil
	}
	if next.Addr == opts.Addr {
		return nil, errors.New("next redis instance is the current one")
	}

	sw, err := start(context.WithoutCancel(ctx), logger, StateKey(role), Target{Addr: opts.Addr, Password: opts.Password}, next, opts.DialTimeout)
	if err != nil {
		return nil, err
	}
	switches.Store(opts.Addr, sw)

	opts.Dialer = sw.dial
	opts.CredentialsProvider = sw.credentials

	return sw.client(opts), nil
}

// Wrap returns a client for options copied from a client that NewClient built.
// The copy carries the dialer but not the command gate or the retry setting, so
// they are applied again here. For any other options it is redis.NewClient.
func Wrap(opts *redis.Options) *redis.Client {
	sw, ok := switches.Load(opts.Addr)
	if !ok || opts.Dialer == nil {
		return redis.NewClient(opts)
	}
	return sw.(*Switch).client(opts)
}

func (s *Switch) client(opts *redis.Options) *redis.Client {
	// A command interrupted by the move must fail once. Replayed, it could run
	// a second time on the other instance.
	opts.MaxRetries = -1
	opts.DialerRetries = 1
	opts.ClientName = ClientName

	client := redis.NewClient(opts)
	client.AddHook(hook{sw: s})

	return client
}

func start(ctx context.Context, logger *slog.Logger, stateKey string, current, next Target, dialTimeout time.Duration) (*Switch, error) {
	sw := &Switch{
		logger:      logger,
		stateKey:    stateKey,
		current:     current,
		next:        next,
		dialTimeout: dialTimeout,
		now:         time.Now,
		poller: redis.NewClient(&redis.Options{
			Addr:            current.Addr,
			Password:        current.Password,
			ClientName:      ClientName,
			DialTimeout:     pollTimeout,
			ReadTimeout:     pollTimeout,
			WriteTimeout:    pollTimeout,
			MaxRetries:      -1,
			PoolSize:        1,
			DisableIdentity: true,
		}),
		mu:       sync.Mutex{},
		state:    stateUnknown,
		readAt:   time.Time{},
		changed:  make(chan struct{}),
		conns:    make(map[*trackedConn]struct{}),
		lastSeen: stateUnknown,
	}

	if err := sw.readAtStart(ctx); err != nil {
		_ = sw.poller.Close()
		return nil, fmt.Errorf("read redis switch keys: %w", err)
	}

	// A wrong address or password for the next instance must fail this
	// process at startup, not every process at the moment of the switch.
	if err := checkReachable(ctx, next); err != nil {
		_ = sw.poller.Close()
		return nil, fmt.Errorf("check next redis instance: %w", err)
	}

	go sw.run(ctx)

	return sw, nil
}

func checkReachable(ctx context.Context, target Target) error {
	ctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()

	client := redis.NewClient(&redis.Options{
		Addr:            target.Addr,
		Password:        target.Password,
		ClientName:      ClientName,
		DialTimeout:     pollTimeout,
		ReadTimeout:     pollTimeout,
		WriteTimeout:    pollTimeout,
		PoolSize:        1,
		DisableIdentity: true,
	})
	defer func() { _ = client.Close() }()

	if err := client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	return nil
}

// readAtStart retries the first read, so a brief blip does not fail startup,
// and refuses to start when the current instance has no StateKey: without it
// the process could never tell a move from a lost key.
func (s *Switch) readAtStart(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()

	for {
		sentAt := s.now()
		got, missing, err := s.read(ctx)
		if err == nil && missing {
			return ErrNotArmed
		}
		if err == nil {
			s.set(ctx, got, sentAt)
			return nil
		}

		select {
		case <-ctx.Done():
			return err
		case <-time.After(pollInterval):
		}
	}
}

// run polls the keys until the switch reaches the next instance or ctx ends.
func (s *Switch) run(ctx context.Context) {
	defer func() { _ = s.poller.Close() }()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		if current, _ := s.load(); current == stateNext {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		sentAt := s.now()
		// A failed read changes nothing: the last reading simply ages until it
		// is no longer trusted.
		if got, _, err := s.read(ctx); err == nil {
			s.set(ctx, got, sentAt)
		}
	}
}

// read returns the state the keys describe, and whether StateKey is missing.
func (s *Switch) read(ctx context.Context) (state, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()

	values, err := s.poller.MGet(ctx, s.stateKey, HoldKey).Result()
	if err != nil {
		return stateUnknown, false, fmt.Errorf("get switch keys: %w", err)
	}

	value, present := values[0].(string)
	switch {
	case !present:
		return stateUnknown, true, nil
	case strings.HasPrefix(value, movedPrefix):
		return stateNext, false, nil
	case values[1] != nil:
		return stateHold, false, nil
	case value == valueCurrent:
		return stateCurrent, false, nil
	default:
		// An unrecognised value must not let writes through.
		return stateUnknown, false, nil
	}
}

// load returns the state to act on now, and whether a hold is deliberate. A
// reading older than staleAfter is not trusted, whatever it said: this is
// checked here, at the moment of use, so a process that stalled cannot act on
// what it read before it stalled.
func (s *Switch) load() (state, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state == stateNext {
		return stateNext, false
	}
	if s.now().Sub(s.readAt) > staleAfter {
		return stateUnknown, false
	}
	return s.state, s.state == stateHold
}

func (s *Switch) set(ctx context.Context, next state, readAt time.Time) {
	s.mu.Lock()
	previous := s.state
	if previous == stateNext {
		s.mu.Unlock()
		return
	}
	s.state = next
	s.readAt = readAt
	close(s.changed)
	s.changed = make(chan struct{})

	var stale []*trackedConn
	if next == stateNext {
		for conn := range s.conns {
			stale = append(stale, conn)
		}
		s.conns = make(map[*trackedConn]struct{})
	}
	logged := s.lastSeen
	s.lastSeen = next
	s.mu.Unlock()

	// Connections to the instance being left would otherwise keep serving
	// commands from the pool.
	for _, conn := range stale {
		_ = conn.Conn.Close()
	}

	if logged != next {
		s.logger.InfoContext(ctx, "redis switch state changed",
			slog.String("from", string(logged)),
			slog.String("to", string(next)),
			slog.Int("closed_connections", len(stale)),
		)
	}
}

// wait blocks while the switch is holding, for at most maxWait.
func (s *Switch) wait(ctx context.Context) error {
	deadline := time.NewTimer(maxWait)
	defer deadline.Stop()

	for {
		if current, _ := s.load(); current == stateCurrent || current == stateNext {
			return nil
		}

		s.mu.Lock()
		changed := s.changed
		s.mu.Unlock()

		select {
		case <-changed:
		case <-deadline.C:
			return ErrHolding
		case <-ctx.Done():
			return fmt.Errorf("%w: %w", ErrHolding, ctx.Err())
		}
	}
}

// dial connects to whichever instance the switch points at. It refuses unless
// the process holds a fresh reading that allows it, before connecting and
// again after, so a command that slipped past the gate, or a process that
// stalled mid-dial, cannot open a connection to the instance being copied.
func (s *Switch) dial(ctx context.Context, network, _ string) (net.Conn, error) {
	target, _ := s.load()

	var addr string
	switch target {
	case stateCurrent:
		addr = s.current.Addr
	case stateNext:
		addr = s.next.Addr
	case stateHold, stateUnknown:
		return nil, ErrHolding
	}

	dialer := &net.Dialer{Timeout: s.dialTimeout, KeepAlive: keepAlive}
	conn, err := dialer.DialContext(ctx, network, addr)
	if err != nil {
		return nil, fmt.Errorf("dial redis: %w", err)
	}
	if target == stateNext {
		return conn, nil
	}

	tracked := &trackedConn{Conn: conn, sw: s}
	if after, _ := s.load(); after != stateCurrent {
		_ = conn.Close()
		return nil, ErrHolding
	}
	s.mu.Lock()
	s.conns[tracked] = struct{}{}
	s.mu.Unlock()

	return tracked, nil
}

// credentials returns the password of the instance the switch points at. A
// connection whose instance and password disagree fails AUTH and is dropped.
func (s *Switch) credentials() (string, string) {
	if current, _ := s.load(); current == stateNext {
		return "", s.next.Password
	}
	return "", s.current.Password
}

// trackedConn is a connection to the current instance, closed when the switch
// moves to the next one.
type trackedConn struct {
	net.Conn
	sw *Switch
}

func (c *trackedConn) Close() error {
	c.sw.mu.Lock()
	delete(c.sw.conns, c)
	c.sw.mu.Unlock()

	if err := c.Conn.Close(); err != nil {
		return fmt.Errorf("close redis connection: %w", err)
	}
	return nil
}

// SyscallConn exposes the wrapped socket, which the connection pool uses to
// detect a connection the server has already closed.
func (c *trackedConn) SyscallConn() (syscall.RawConn, error) {
	inner, ok := c.Conn.(syscall.Conn)
	if !ok {
		return nil, errors.New("redis connection has no raw socket")
	}

	raw, err := inner.SyscallConn()
	if err != nil {
		return nil, fmt.Errorf("raw redis connection: %w", err)
	}
	return raw, nil
}

type hook struct {
	sw *Switch
}

func (h hook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

func (h hook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		// A deliberate hold must not fail health checks and take pods out of
		// rotation, so the switch answers PING itself. An unreadable key is a
		// real fault and still fails them.
		if ping, ok := cmd.(*redis.StatusCmd); ok && cmd.Name() == "ping" {
			if _, deliberate := h.sw.load(); deliberate {
				ping.SetVal("PONG")
				return nil
			}
		}

		if err := h.sw.wait(ctx); err != nil {
			cmd.SetErr(err)
			return err
		}
		return next(ctx, cmd)
	}
}

func (h hook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		if err := h.sw.wait(ctx); err != nil {
			for _, cmd := range cmds {
				cmd.SetErr(err)
			}
			return err
		}
		return next(ctx, cmds)
	}
}
