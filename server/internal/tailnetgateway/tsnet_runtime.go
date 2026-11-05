package tailnetgateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"sync"
	"time"

	"tailscale.com/client/local"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/tailcfg/nodecap"
	"tailscale.com/tsnet"
	"tailscale.com/types/logger"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	// servicePort is the only port the gateway advertises on each service. It
	// must match the tcp:443 endpoint the reconciler defines on the Tailscale
	// Service, so it is fixed rather than configurable.
	servicePort = 443

	// stateDirMode keeps tsnet's scratch files private to the pod user.
	stateDirMode = 0o700

	// defaultShutdownTimeout bounds how long Close waits for in-flight
	// requests when the caller's context has no deadline.
	defaultShutdownTimeout = 20 * time.Second

	// defaultRequestReadTimeout bounds reading one request including its
	// body, so a peer cannot hold a connection by trickling bytes. MCP request
	// bodies are small; streaming happens on the response side, which this
	// does not limit.
	defaultRequestReadTimeout = 2 * time.Minute

	// requestHeaderTimeout bounds reading request headers.
	requestHeaderTimeout = 10 * time.Second

	// idleConnTimeout keeps idle keep-alive connections a little longer than
	// the ten-minute MCP session keep-alive so clients are not cut mid-session.
	idleConnTimeout = 620 * time.Second

	// needsLoginProbeTimeout bounds the status read after a failed Up.
	needsLoginProbeTimeout = 5 * time.Second
)

// TsnetRuntime starts real tailnet nodes with tsnet. Build it with
// NewTsnetRuntime.
type TsnetRuntime struct {
	logger             *slog.Logger
	shutdownTimeout    time.Duration
	requestReadTimeout time.Duration
}

var _ NodeRuntime = (*TsnetRuntime)(nil)

// TsnetRuntimeOption configures a TsnetRuntime.
type TsnetRuntimeOption func(*TsnetRuntime)

// WithLogger routes tsnet's user-facing messages, such as login prompts, and
// listener failures to logger. Without it they are discarded.
func WithLogger(logger *slog.Logger) TsnetRuntimeOption {
	return func(r *TsnetRuntime) {
		r.logger = logger
	}
}

// WithShutdownTimeout bounds how long Close waits for in-flight requests when
// the caller's context has no deadline. Non-positive values keep the default.
func WithShutdownTimeout(timeout time.Duration) TsnetRuntimeOption {
	return func(r *TsnetRuntime) {
		if timeout > 0 {
			r.shutdownTimeout = timeout
		}
	}
}

// WithRequestReadTimeout bounds reading one request including its body.
// Non-positive values keep the default.
func WithRequestReadTimeout(timeout time.Duration) TsnetRuntimeOption {
	return func(r *TsnetRuntime) {
		if timeout > 0 {
			r.requestReadTimeout = timeout
		}
	}
}

// NewTsnetRuntime returns a runtime with the default timeouts and a discarding
// logger, adjusted by opts.
func NewTsnetRuntime(opts ...TsnetRuntimeOption) *TsnetRuntime {
	r := &TsnetRuntime{
		logger:             slog.New(slog.DiscardHandler),
		shutdownTimeout:    defaultShutdownTimeout,
		requestReadTimeout: defaultRequestReadTimeout,
	}
	for _, opt := range opts {
		opt(r)
	}
	if r.logger == nil {
		r.logger = slog.New(slog.DiscardHandler)
	}

	return r
}

// Start registers or reconnects the node, waits until it is running, and
// advertises the service.
func (r *TsnetRuntime) Start(ctx context.Context, spec NodeSpec) (Node, error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(spec.StateDir, stateDirMode); err != nil {
		return nil, fmt.Errorf("create tsnet state dir: %w", err)
	}

	hostLogger := r.logger.With(attr.SlogNetworkIngressID(spec.IngressID.String()), attr.SlogTailnetGatewaySlot(spec.Slot))
	srv := &tsnet.Server{
		Dir:      spec.StateDir,
		Store:    spec.Store,
		Hostname: spec.Hostname,
		UserLogf: func(format string, args ...any) {
			hostLogger.InfoContext(ctx, fmt.Sprintf(format, args...))
		},
		Logf:          logger.Discard,
		Ephemeral:     false,
		AuthKey:       spec.AuthKey,
		AdvertiseTags: []string{spec.Tag},
		ControlURL:    spec.ControlURL,
	}
	if err := srv.Start(); err != nil {
		return nil, fmt.Errorf("start tsnet node: %w", err)
	}

	upCtx, cancel := context.WithTimeout(ctx, spec.StartTimeout)
	defer cancel()

	if _, err := srv.Up(upCtx); err != nil {
		needsLogin := backendNeedsLogin(ctx, srv)
		_ = srv.Close()
		if needsLogin {
			return nil, fmt.Errorf("bring tsnet node up: %w: %w", ErrNodeNeedsLogin, err)
		}
		return nil, fmt.Errorf("bring tsnet node up: %w", err)
	}

	ln, err := srv.ListenService(spec.ServiceName, tsnet.ServiceModeHTTP{Port: servicePort, HTTPS: true, AcceptAppCaps: nil, PROXYProtocol: 0})
	if err != nil {
		_ = srv.Close()
		return nil, fmt.Errorf("advertise tailscale service: %w", err)
	}

	lc, err := srv.LocalClient()
	if err != nil {
		_ = ln.Close()
		_ = srv.Close()
		return nil, fmt.Errorf("tsnet local client: %w", err)
	}

	return &tsnetNode{
		srv:                srv,
		ln:                 ln,
		lc:                 lc,
		service:            tailcfg.ServiceName(spec.ServiceName),
		fqdn:               ln.FQDN,
		logger:             hostLogger,
		shutdownTimeout:    r.shutdownTimeout,
		requestReadTimeout: r.requestReadTimeout,
		server:             nil,
		closeOnce:          sync.Once{},
		mu:                 sync.Mutex{},
		serveErr:           nil,
	}, nil
}

// backendNeedsLogin reports whether a node that failed to come up is stuck
// waiting for login. Up's own context has expired by then, so this uses a
// short one of its own.
func backendNeedsLogin(ctx context.Context, srv *tsnet.Server) bool {
	lc, err := srv.LocalClient()
	if err != nil {
		return false
	}

	statusCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), needsLoginProbeTimeout)
	defer cancel()

	st, err := lc.StatusWithoutPeers(statusCtx)
	return err == nil && st.BackendState == ipn.NeedsLogin.String()
}

// validate names every field Start needs that the spec leaves unset.
func (s NodeSpec) validate() error {
	var errs []error
	if s.Store == nil {
		errs = append(errs, errors.New("spec.Store is required"))
	}
	if s.Hostname == "" {
		errs = append(errs, errors.New("spec.Hostname is required"))
	}
	if s.ServiceName == "" {
		errs = append(errs, errors.New("spec.ServiceName is required"))
	}
	if s.Tag == "" {
		errs = append(errs, errors.New("spec.Tag is required"))
	}
	if s.StateDir == "" {
		errs = append(errs, errors.New("spec.StateDir is required"))
	}
	if s.StartTimeout <= 0 {
		errs = append(errs, fmt.Errorf("spec.StartTimeout must be positive, got %s", s.StartTimeout))
	}
	if len(errs) > 0 {
		return fmt.Errorf("invalid tsnet node spec: %w", errors.Join(errs...))
	}

	return nil
}

type tsnetNode struct {
	srv                *tsnet.Server
	ln                 *tsnet.ServiceListener
	lc                 *local.Client
	service            tailcfg.ServiceName
	fqdn               string
	logger             *slog.Logger
	shutdownTimeout    time.Duration
	requestReadTimeout time.Duration
	server             *http.Server

	// closeOnce makes Close idempotent.
	closeOnce sync.Once

	// mu guards serveErr, written by the serve goroutine and read by Status.
	mu       sync.Mutex
	serveErr error
}

func (n *tsnetNode) Serve(handler http.Handler) error {
	if n.server != nil {
		return errors.New("tsnet node is already serving")
	}

	n.server = &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: requestHeaderTimeout,
		ReadTimeout:       n.requestReadTimeout,
		IdleTimeout:       idleConnTimeout,
		BaseContext:       func(net.Listener) context.Context { return context.Background() },
	}

	go func() {
		err := n.server.Serve(n.ln)
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return
		}

		// The node stays connected, but nothing answers the service. Status
		// reports it so the gateway marks the host errored on its next poll.
		n.mu.Lock()
		n.serveErr = err
		n.mu.Unlock()

		n.logger.ErrorContext(context.Background(), "tailnet service listener stopped", attr.SlogError(err), attr.SlogHostName(n.fqdn))
	}()

	return nil
}

func (n *tsnetNode) Status(ctx context.Context) (NodeState, error) {
	n.mu.Lock()
	serveErr := n.serveErr
	n.mu.Unlock()

	if serveErr != nil {
		return NodeState{Online: false, BackendState: "", NeedsLogin: false, AwaitingApproval: false, NodeID: "", FQDN: n.fqdn}, fmt.Errorf("service listener stopped: %w", serveErr)
	}

	st, err := n.lc.Status(ctx)
	if err != nil {
		return NodeState{Online: false, BackendState: "", NeedsLogin: false, AwaitingApproval: false, NodeID: "", FQDN: n.fqdn}, fmt.Errorf("tsnet status: %w", err)
	}

	running := st.BackendState == ipn.Running.String()
	approved := hostsService(st.Self, n.service)
	state := NodeState{
		Online: running && approved, BackendState: st.BackendState,
		NeedsLogin:       st.BackendState == ipn.NeedsLogin.String(),
		AwaitingApproval: running && !approved,
		NodeID:           "", FQDN: n.fqdn,
	}
	if st.Self != nil {
		state.NodeID = string(st.Self.ID)
	}

	return state, nil
}

// hostsService reports whether control has approved this node to host the
// service. Advertising a service is not enough: control only assigns the
// service's addresses, through the service-host capability, once the policy
// auto-approves the host or an admin approves it, and it withdraws them when
// approval is revoked. A Running node that advertises a missing, misnamed, or
// unapproved service gets no error and no traffic, so this is the only signal.
func hostsService(self *ipnstate.PeerStatus, service tailcfg.ServiceName) bool {
	if self == nil {
		return false
	}

	mappings, err := tailcfg.UnmarshalNodeCapJSON[tailcfg.ServiceIPMappings](self.CapMap, nodecap.ServiceHost)
	if err != nil {
		return false
	}
	for _, mapping := range mappings {
		if addrs, ok := mapping[service]; ok && len(addrs) > 0 {
			return true
		}
	}

	return false
}

func (n *tsnetNode) WhoIs(ctx context.Context, peer netip.Addr) (*apitype.WhoIsResponse, error) {
	who, err := n.lc.WhoIs(ctx, peer.String())
	if err != nil {
		return nil, fmt.Errorf("tsnet whois: %w", err)
	}

	return who, nil
}

func (n *tsnetNode) Close(ctx context.Context) error {
	var errs []error
	n.closeOnce.Do(func() {
		if n.server != nil {
			shutdownCtx := ctx
			if _, ok := ctx.Deadline(); !ok {
				var cancel context.CancelFunc
				shutdownCtx, cancel = context.WithTimeout(ctx, n.shutdownTimeout)
				defer cancel()
			}

			if err := n.server.Shutdown(shutdownCtx); err != nil {
				errs = append(errs, fmt.Errorf("stop service listener: %w", err))
			}
		}

		if err := n.ln.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close service listener: %w", err))
		}

		if err := n.srv.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close tsnet node: %w", err))
		}
	})

	return errors.Join(errs...)
}
