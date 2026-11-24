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
// wait. Where the instance allows it, the operator's job closes every
// connection once the hold has been seen, so a process that fell behind cannot
// write through an old one; an instance without the CLIENT command does not
// allow it. A process refuses to open a new connection without a fresh reading.
//
// A client follows the state key of one role. A process that keeps different
// workloads on different instances gives each its own client and role, each
// role with its own next instance, so one workload can move while the others
// stay. The hold key is shared: a copy pauses every client of the instance,
// which is what lets the operator's job prove nothing else wrote to it.
//
// The current and next instances may differ in how they are reached: TLS or
// plaintext, a password or an IAM token. Each new socket is encrypted and
// authenticated inside the switch's dialer with the credentials of the
// instance that dialer chose, and the client itself carries none. A move that
// lands while a socket is being opened therefore cannot send one instance's
// credentials to the other.
package redisswitch

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/oauth2"

	"github.com/speakeasy-api/gram/tunnel/redisconn"
)

const (
	// stateKeyPrefix precedes a role's name in its state key.
	stateKeyPrefix = "gram:redis-switch:state:"

	// DefaultRole is the role of a process that keeps everything on one
	// instance.
	DefaultRole = "all"

	// RoleAuth is the role of the authentication and session state.
	RoleAuth = "auth"

	// RoleOps is the role of the operational state: queues, routes, rate
	// limits and the like.
	RoleOps = "ops"

	// HoldKey exists, with an expiry, while an operator is copying the data.
	// Its expiry bounds the pause: if the operator's job dies, the key goes and
	// processes resume on the current instance.
	HoldKey = "gram:redis-switch:hold"

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

	// maxWait is the longest a command waits for a hold to end. It is longer
	// than the longest hold the operator's job sets (120 s in prod), so a held
	// command waits for the hold to end rather than failing; only an instance
	// that stays unreadable reaches it, and the command then fails instead of
	// waiting for ever. A request's own context can end the wait sooner.
	maxWait = 150 * time.Second

	// keepAlive matches the go-redis default dialer.
	keepAlive = 5 * time.Minute

	// defaultDialTimeout applies when the caller's options leave the dial
	// timeout at zero. It is the go-redis default; without it a dial to an
	// unreachable instance would wait for the operating system to give up.
	defaultDialTimeout = 5 * time.Second

	// moveJitterWindow is how long after a move new connections to the next
	// instance are spread out. Every process sees the move within a poll
	// interval and refills its pool at once; by the end of the window those
	// pools are full again.
	moveJitterWindow = 10 * time.Second

	// maxMoveJitter is the longest a dial waits inside moveJitterWindow. It
	// spreads a fleet's TLS handshakes and token checks over a quarter of a
	// second while staying well inside the dial timeout.
	maxMoveJitter = 250 * time.Millisecond
)

// ErrHolding is returned when a command cannot run because the switch is
// holding: its wait ended, or it needed a new connection.
var ErrHolding = errors.New("redis switch is holding")

// ErrNotArmed is returned at startup when the current instance has no state
// key for the role. An operator must set it before processes are configured
// with a next instance.
var ErrNotArmed = errors.New("redis switch state key is not set on the current instance")

// ErrAmbiguousRole is returned by a client that Wrap built for options whose
// instance has switches for more than one role: Wrap cannot tell which one the
// options came from. Use WrapRole.
var ErrAmbiguousRole = errors.New("redis switch follows several roles on this instance; name the role")

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

// Binding names the role a client follows and the instances it moves between.
type Binding struct {
	// Role names the state key the client follows on the current instance.
	Role string

	// Current is the instance the role uses until it moves. Its switch keys
	// live there.
	Current redisconn.Endpoint

	// Next is the instance the role moves to. The switch is inactive when its
	// address is empty or equal to Current's.
	Next redisconn.Endpoint

	// Tokens supplies access tokens for an IAM endpoint. It may be nil when
	// neither endpoint uses IAM.
	Tokens oauth2.TokenSource
}

// NextFromEnv reads a next instance that takes a password, or nothing, from
// the environment. Its address is empty when the switch is not configured.
func NextFromEnv() redisconn.Endpoint {
	addr := strings.TrimSpace(os.Getenv(EnvNextAddr))
	if addr == "" {
		return redisconn.Endpoint{Addr: "", Auth: redisconn.AuthNone, Password: "", CACertPEM: "", ServerName: ""}
	}
	return redisconn.PasswordEndpoint(addr, os.Getenv(EnvNextPassword))
}

// Switch tracks which instance a process should use for one role.
type Switch struct {
	logger      *slog.Logger
	stateKey    string
	current     *redisconn.Connector
	next        *redisconn.Connector
	poller      *redis.Client
	dialTimeout time.Duration
	now         func() time.Time

	// dialTCP opens the raw socket for a dial. Tests replace it to stall or
	// observe a dial.
	dialTCP func(ctx context.Context, network, addr string, timeout time.Duration) (net.Conn, error)

	// jitter picks how long a dial waits shortly after a move.
	jitter func() time.Duration

	mu       sync.Mutex
	state    state
	readAt   time.Time
	movedAt  time.Time
	changed  chan struct{}
	conns    map[*trackedConn]struct{}
	lastSeen state
}

// registryKey identifies a switch: the current instance it polls and the role
// whose state key it follows there.
type registryKey struct {
	addr string
	role string
}

// switches maps a registryKey to its *entry, so a second client built from the
// first one's options gets the same gate. Connections carry no name to tie a
// copy to its parent: an instance may reject the CLIENT command, and a named
// client then cannot connect to it. One process therefore follows each role
// of an instance with one switch.
var switches sync.Map

// NewClient returns a client for opts and binding. Binding's endpoints replace
// the address, TLS configuration and credentials in opts; the rest of opts
// (pool size, timeouts, database) is kept. When binding names a next instance,
// the client follows the role's switch keys on the current instance;
// otherwise it is an ordinary client of the current instance. The poller runs
// for the life of the process: callers often pass a startup context, and a
// poller that stopped with it would leave the process unable to see a hold.
func NewClient(ctx context.Context, logger *slog.Logger, opts *redis.Options, binding Binding) (*redis.Client, error) {
	// A next instance equal to the current one means the move is over.
	if binding.Next.Addr == "" || binding.Next.Addr == binding.Current.Addr {
		return ordinaryClient(opts, binding)
	}

	current, err := redisconn.NewConnector(binding.Current, binding.Tokens)
	if err != nil {
		return nil, fmt.Errorf("current redis instance: %w", err)
	}

	if binding.Role == "" {
		return nil, errors.New("redis switch needs a role")
	}
	next, err := redisconn.NewConnector(binding.Next, binding.Tokens)
	if err != nil {
		return nil, fmt.Errorf("next redis instance: %w", err)
	}

	// Reserved before starting, so two callers cannot both build a switch for
	// the same instance and role.
	key := registryKey{addr: current.Addr(), role: binding.Role}
	reserved := &entry{ready: make(chan struct{}), sw: nil}
	if found, taken := switches.LoadOrStore(key, reserved); taken {
		// Only a switch that failed to start may be replaced. Its entry stays in
		// the registry until then, so Wrap keeps refusing its address.
		previous, ok := found.(*entry)
		if !ok || !previous.failed() || !switches.CompareAndSwap(key, found, reserved) {
			return nil, fmt.Errorf("a redis switch already follows role %q on this instance in this process", binding.Role)
		}
	}

	sw, err := start(context.WithoutCancel(ctx), logger, binding.Role, current, next, opts.DialTimeout)
	if err != nil {
		close(reserved.ready)
		return nil, err
	}
	reserved.sw = sw
	close(reserved.ready)

	return sw.client(opts), nil
}

// ordinaryClient returns a client of the current instance that follows no
// switch. A plaintext instance's address and password go to go-redis as given,
// so an unconfigured address keeps go-redis's default; an instance that needs
// TLS or an IAM token is checked and connected through redisconn.
func ordinaryClient(opts *redis.Options, binding Binding) (*redis.Client, error) {
	if !binding.Current.TLS() && binding.Current.Auth != redisconn.AuthIAM {
		plain := *opts
		plain.Addr = binding.Current.Addr
		plain.Password = binding.Current.Password
		return redis.NewClient(&plain), nil
	}

	current, err := redisconn.NewConnector(binding.Current, binding.Tokens)
	if err != nil {
		return nil, fmt.Errorf("current redis instance: %w", err)
	}
	return redis.NewClient(current.Options(opts)), nil
}

// entry is a role's place in the registry. It is stored before its switch has
// started, so sw may only be read once ready is closed, and is nil if the
// switch failed to start.
type entry struct {
	// ready is closed when the switch has started or failed to.
	ready chan struct{}

	// sw is the started switch, or nil if starting it failed.
	sw *Switch
}

// failed reports whether the switch finished starting and did not succeed.
func (e *entry) failed() bool {
	select {
	case <-e.ready:
		return e.sw == nil
	default:
		return false
	}
}

// ErrNotStarted is returned by a client that Wrap built for a switch that
// failed to start.
var ErrNotStarted = errors.New("redis switch for this client failed to start")

// Wrap returns a client for options copied from a client that NewClient built.
// A copy does not carry the command gate or the retry setting, so the whole
// binding is applied again here. Options that did not come from such a client
// (their address matches no switch) get redis.NewClient.
//
// The switch is found by address. When the process follows more than one role
// on that instance the returned client cannot connect (ErrAmbiguousRole); use
// WrapRole.
//
// If the matching switch is still starting, Wrap waits for it. If it failed to
// start, the returned client cannot connect: an ordinary client would not
// follow a move.
func Wrap(opts *redis.Options) *redis.Client {
	var found []*entry
	switches.Range(func(key, value any) bool {
		if k, ok := key.(registryKey); ok && k.addr == opts.Addr {
			if e, ok := value.(*entry); ok {
				found = append(found, e)
			}
		}
		return true
	})

	switch len(found) {
	case 0:
		return redis.NewClient(opts)
	case 1:
		return wrapEntry(opts, found[0])
	default:
		return refusedClient(opts, ErrAmbiguousRole)
	}
}

// WrapRole is Wrap for options copied from the client of a named role.
func WrapRole(opts *redis.Options, role string) *redis.Client {
	found, ok := switches.Load(registryKey{addr: opts.Addr, role: role})
	if !ok {
		return redis.NewClient(opts)
	}
	reserved, ok := found.(*entry)
	if !ok {
		return redis.NewClient(opts)
	}
	return wrapEntry(opts, reserved)
}

func wrapEntry(opts *redis.Options, reserved *entry) *redis.Client {
	<-reserved.ready
	if reserved.sw == nil {
		return refusedClient(opts, ErrNotStarted)
	}
	return reserved.sw.client(opts)
}

// refusedClient returns a client whose every dial fails with err.
func refusedClient(opts *redis.Options, err error) *redis.Client {
	refused := *opts
	refused.Dialer = func(context.Context, string, string) (net.Conn, error) { return nil, err }
	refused.MaxRetries = -1
	refused.DialerRetries = 1
	return redis.NewClient(&refused)
}

// client builds a client bound to this switch. The gate and the dialer are
// always set together: a client with the gate but an ordinary dialer would
// wait through a hold and then write to the instance being left. The dialer
// encrypts and authenticates each socket for the instance it chose, so the
// client carries no credentials and no TLS configuration of its own. The
// caller's options are copied, not changed.
func (s *Switch) client(opts *redis.Options) *redis.Client {
	bound := *opts
	bound.Addr = s.current.Addr()
	bound.Dialer = s.dial
	bound.TLSConfig = nil
	bound.Username = ""
	bound.Password = ""
	bound.CredentialsProvider = nil
	bound.CredentialsProviderContext = nil
	bound.StreamingCredentialsProvider = nil
	// A command interrupted by the move must fail once. Replayed, it could run
	// a second time on the other instance.
	bound.MaxRetries = -1
	bound.DialerRetries = 1
	// A named connection sends CLIENT SETNAME, which an instance without the
	// CLIENT command refuses, and the connection then fails.
	bound.ClientName = ""

	client := redis.NewClient(&bound)
	client.AddHook(hook{sw: s})

	return client
}

func start(ctx context.Context, logger *slog.Logger, role string, current, next *redisconn.Connector, dialTimeout time.Duration) (*Switch, error) {
	if dialTimeout <= 0 {
		dialTimeout = defaultDialTimeout
	}

	sw := &Switch{
		logger:      logger,
		stateKey:    StateKey(role),
		current:     current,
		next:        next,
		dialTimeout: dialTimeout,
		now:         time.Now,
		dialTCP:     dialTCP,
		jitter:      func() time.Duration { return rand.N(maxMoveJitter) },
		poller: redis.NewClient(current.Options(&redis.Options{
			DialTimeout:     pollTimeout,
			ReadTimeout:     pollTimeout,
			WriteTimeout:    pollTimeout,
			MaxRetries:      -1,
			PoolSize:        1,
			DisableIdentity: true,
		})),
		mu:       sync.Mutex{},
		state:    stateUnknown,
		readAt:   time.Time{},
		movedAt:  time.Time{},
		changed:  make(chan struct{}),
		conns:    make(map[*trackedConn]struct{}),
		lastSeen: stateUnknown,
	}

	if err := sw.readAtStart(ctx); err != nil {
		_ = sw.poller.Close()
		return nil, fmt.Errorf("read redis switch keys: %w", err)
	}

	// A wrong address or credentials for the next instance must fail this
	// process at startup, not every process at the moment of the switch.
	if err := checkReachable(ctx, next); err != nil {
		_ = sw.poller.Close()
		return nil, fmt.Errorf("check next redis instance: %w", err)
	}

	go sw.run(ctx)

	return sw, nil
}

func dialTCP(ctx context.Context, network, addr string, timeout time.Duration) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: timeout, KeepAlive: keepAlive}
	conn, err := dialer.DialContext(ctx, network, addr)
	if err != nil {
		return nil, fmt.Errorf("dial redis: %w", err)
	}
	return conn, nil
}

func checkReachable(ctx context.Context, target *redisconn.Connector) error {
	ctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()

	client := redis.NewClient(target.Options(&redis.Options{
		DialTimeout:     startTimeout,
		ReadTimeout:     pollTimeout,
		WriteTimeout:    pollTimeout,
		PoolSize:        1,
		DisableIdentity: true,
	}))
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
	// A move always names the run that made it; a bare prefix is not one.
	case strings.HasPrefix(value, movedPrefix) && len(value) > len(movedPrefix):
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

	current := s.loadLocked()
	return current, current == stateHold
}

// loadLocked is load for a caller that holds s.mu.
func (s *Switch) loadLocked() state {
	if s.state == stateNext {
		return stateNext
	}
	if s.now().Sub(s.readAt) > staleAfter {
		return stateUnknown
	}
	return s.state
}

func (s *Switch) set(ctx context.Context, next state, readAt time.Time) {
	s.mu.Lock()
	if s.state == stateNext {
		s.mu.Unlock()
		return
	}

	var stale []*trackedConn
	if next == stateNext {
		// Every connection to the instance being left is closed before any
		// command learns of the move. Until that is done commands wait and
		// dials are refused, so nothing can use or add such a connection.
		s.state = stateUnknown
		for conn := range s.conns {
			stale = append(stale, conn)
		}
		s.conns = make(map[*trackedConn]struct{})
		s.mu.Unlock()

		for _, conn := range stale {
			_ = conn.closeSocket()
		}

		s.mu.Lock()
		s.movedAt = s.now()
	}

	s.state = next
	s.readAt = readAt
	close(s.changed)
	s.changed = make(chan struct{})
	logged := s.lastSeen
	s.lastSeen = next
	s.mu.Unlock()

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
		// The state and the channel that signals its next change are read
		// together. Read apart, a move between the two would leave this waiter
		// on a channel nothing closes again.
		s.mu.Lock()
		current, changed := s.loadLocked(), s.changed
		s.mu.Unlock()

		if current == stateCurrent || current == stateNext {
			return nil
		}

		select {
		case <-changed:
		case <-deadline.C:
			return ErrHolding
		case <-ctx.Done():
			return fmt.Errorf("%w: %w", ErrHolding, ctx.Err())
		}
	}
}

// dial connects to whichever instance the switch points at. The instance is
// chosen once, before connecting, and the socket is encrypted and
// authenticated for that instance alone: a move while the dial is in progress
// cannot make it send the other instance's credentials.
//
// A connection to the current instance is handed over only if three checks
// pass: the process holds a fresh reading that allows it before connecting;
// the instance itself, asked on the new socket, says it is current and not
// held; and the reading still allows it when the socket is registered. A
// command that slipped past the gate, a process that stalled mid-dial, or a
// socket whose handshake was delayed past the operator's connection close
// therefore cannot write to the instance being copied.
func (s *Switch) dial(ctx context.Context, network, _ string) (net.Conn, error) {
	target, _ := s.load()

	var endpoint *redisconn.Connector
	switch target {
	case stateCurrent:
		endpoint = s.current
	case stateNext:
		endpoint = s.next
		if err := s.spreadAfterMove(ctx); err != nil {
			return nil, err
		}
	case stateHold, stateUnknown:
		return nil, ErrHolding
	}

	conn, err := s.open(ctx, network, endpoint)
	if err != nil {
		return nil, err
	}

	if target == stateNext {
		if err := handshake(ctx, conn, endpoint, s.dialTimeout, ping); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("authenticate to next redis instance: %w", err)
		}
		return conn, nil
	}

	if err := s.verifyCurrent(ctx, conn); err != nil {
		_ = conn.Close()
		return nil, err
	}

	// Checked and registered under one lock: a move cannot fall between the
	// two and leave an unregistered connection to the instance being left.
	tracked, handed := s.track(conn)
	s.mu.Lock()
	allowed := s.loadLocked() == stateCurrent
	if allowed {
		s.conns[tracked] = struct{}{}
	}
	s.mu.Unlock()

	if !allowed {
		_ = conn.Close()
		return nil, ErrHolding
	}
	return handed, nil
}

// spreadAfterMove waits a random moment when a dial to the next instance
// comes soon after the move, so a fleet that moved together does not open
// every connection at the same instant.
func (s *Switch) spreadAfterMove(ctx context.Context) error {
	s.mu.Lock()
	movedAt := s.movedAt
	s.mu.Unlock()

	if s.now().Sub(movedAt) >= moveJitterWindow {
		return nil
	}
	delay := s.jitter()
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait before dialing next redis instance: %w", ctx.Err())
	}
}

// open dials endpoint and, when it uses TLS, completes the handshake, both
// within the dial timeout.
func (s *Switch) open(ctx context.Context, network string, endpoint *redisconn.Connector) (net.Conn, error) {
	conn, err := s.dialTCP(ctx, network, endpoint.Addr(), s.dialTimeout)
	if err != nil {
		return nil, err
	}

	config := endpoint.TLSConfig()
	if config == nil {
		return conn, nil
	}

	ctx, cancel := context.WithTimeout(ctx, s.dialTimeout)
	defer cancel()

	encrypted := tls.Client(conn, config)
	if err := encrypted.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("tls handshake with redis: %w", err)
	}
	return encrypted, nil
}

// verifyCurrent authenticates a socket just opened to the current instance,
// reads the switch keys on it, and fails unless the instance says it is
// current and not held. It is asked on this socket, not through the poller,
// so the answer is about the instance this socket reached and is as fresh as
// the socket.
func (s *Switch) verifyCurrent(ctx context.Context, conn net.Conn) error {
	return handshake(ctx, conn, s.current, pollTimeout, func(ctx context.Context, probe *redis.Client) error {
		values, err := probe.MGet(ctx, s.stateKey, HoldKey).Result()
		if err != nil {
			return fmt.Errorf("read switch keys on new connection: %w", err)
		}
		if value, _ := values[0].(string); value != valueCurrent || values[1] != nil {
			return ErrHolding
		}
		return nil
	})
}

func ping(ctx context.Context, probe *redis.Client) error {
	if err := probe.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	return nil
}

// handshake authenticates conn with endpoint's credentials and runs check over
// it, both through a throwaway client that leaves the socket open. Later
// commands on the socket run as the authenticated user, so the client that
// receives it sends no credentials of its own.
func handshake(ctx context.Context, conn net.Conn, endpoint *redisconn.Connector, timeout time.Duration, check func(context.Context, *redis.Client) error) error {
	opts := endpoint.Options(&redis.Options{
		Dialer:          func(context.Context, string, string) (net.Conn, error) { return keepOpen{Conn: conn}, nil },
		Protocol:        2,
		ReadTimeout:     timeout,
		WriteTimeout:    timeout,
		MaxRetries:      -1,
		DialerRetries:   1,
		PoolSize:        1,
		DisableIdentity: true,
	})
	// The socket is already encrypted.
	opts.TLSConfig = nil

	probe := redis.NewClient(opts)
	defer func() { _ = probe.Close() }()

	if err := check(ctx, probe); err != nil {
		return err
	}

	// The probe set deadlines for its own commands; the caller sets its own.
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return fmt.Errorf("clear deadline on new connection: %w", err)
	}
	return nil
}

// keepOpen is a socket whose Close does nothing, lent to a throwaway client.
type keepOpen struct {
	net.Conn
}

func (keepOpen) Close() error {
	return nil
}

// track wraps a connection to the current instance so the switch can close it
// at the move. It returns the registry entry and the connection to hand to the
// pool.
//
// Before reusing an idle connection the pool checks it through
// syscall.Conn, and discards it when that check fails. A plain socket is
// handed over with its own raw socket, so the pool sees a connection the
// server closed. A connection without one, such as TLS, cannot be checked
// that way (a raw read would see TLS records), so it is handed over with a
// stand-in that fails only once the switch has closed the connection: an open
// TLS connection is reused, and one the move closed is dropped rather than
// failing the next command.
func (s *Switch) track(conn net.Conn) (*trackedConn, net.Conn) {
	tracked := &trackedConn{Conn: conn, sw: s, closed: atomic.Bool{}}
	if raw, ok := conn.(syscall.Conn); ok {
		return tracked, &rawTrackedConn{trackedConn: tracked, raw: raw}
	}
	return tracked, &checkedTrackedConn{trackedConn: tracked}
}

// trackedConn is a connection to the current instance, closed when the switch
// moves to the next one.
type trackedConn struct {
	net.Conn
	sw *Switch

	// closed is set once the connection has been closed, by its owner or by
	// the move.
	closed atomic.Bool
}

func (c *trackedConn) Close() error {
	c.sw.mu.Lock()
	delete(c.sw.conns, c)
	c.sw.mu.Unlock()

	return c.closeSocket()
}

func (c *trackedConn) closeSocket() error {
	c.closed.Store(true)
	if err := c.Conn.Close(); err != nil {
		return fmt.Errorf("close redis connection: %w", err)
	}
	return nil
}

// checkedTrackedConn is a trackedConn over a connection without a raw socket.
type checkedTrackedConn struct {
	*trackedConn
}

func (c *checkedTrackedConn) SyscallConn() (syscall.RawConn, error) {
	return closedCheck{conn: c.trackedConn}, nil
}

// closedCheck stands in for a raw socket in the pool's liveness check. It never
// touches the socket: it fails once the connection is closed and otherwise
// reports nothing to read.
type closedCheck struct {
	conn *trackedConn
}

func (r closedCheck) err() error {
	if r.conn.closed.Load() {
		return net.ErrClosed
	}
	return nil
}

func (r closedCheck) Control(func(fd uintptr)) error { return r.err() }

func (r closedCheck) Read(func(fd uintptr) bool) error { return r.err() }

func (r closedCheck) Write(func(fd uintptr) bool) error { return r.err() }

// rawTrackedConn is a trackedConn over a plain socket. It exposes the socket,
// which the connection pool uses to detect a connection the server has already
// closed.
type rawTrackedConn struct {
	*trackedConn
	raw syscall.Conn
}

func (c *rawTrackedConn) SyscallConn() (syscall.RawConn, error) {
	raw, err := c.raw.SyscallConn()
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
