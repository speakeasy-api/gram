package tailnetgateway

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"sync"
	"time"

	"github.com/pires/go-proxyproto"
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

	// requestHeaderTimeout bounds reading request headers, and the PROXY
	// header tsnet sends ahead of each connection.
	requestHeaderTimeout = 10 * time.Second

	// proxyProtocolVersion is the PROXY protocol header tsnet prefixes each
	// service connection with, carrying the tailnet peer's address.
	proxyProtocolVersion = 2

	// idleConnTimeout keeps idle keep-alive connections a little longer than
	// the ten-minute MCP session keep-alive so clients are not cut mid-session.
	idleConnTimeout = 620 * time.Second

	// needsLoginProbeTimeout bounds the status read after a failed Up.
	needsLoginProbeTimeout = 5 * time.Second

	// approvalWait is how long Start waits for the tailnet to approve the
	// node as a host of its service. On a real tailnet an auto-approval
	// arrived about 3s after the node was Running, so without this wait the
	// first heartbeat would report a host that is merely about to be
	// approved as awaiting approval. A host still unapproved after the wait
	// is reported as such by Status.
	approvalWait = 10 * time.Second

	// approvalPollInterval is how often Start checks for approval.
	approvalPollInterval = 250 * time.Millisecond
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

	ln, err := srv.ListenService(spec.ServiceName, serviceMode())
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

	approved := waitForApproval(ctx, lc, tailcfg.ServiceName(spec.ServiceName))

	fqdn := ln.FQDN
	cert := newCertificateTracker(fqdn, leasedCertificateFetch(spec.Store, fqdn, lc.CertPair), hostLogger, time.Now)
	if approved {
		cert.Await(ctx, certificateStartWait)
	}

	return &tsnetNode{
		srv:                srv,
		ln:                 ln,
		lc:                 lc,
		service:            tailcfg.ServiceName(spec.ServiceName),
		fqdn:               fqdn,
		cert:               cert,
		logger:             hostLogger,
		shutdownTimeout:    r.shutdownTimeout,
		requestReadTimeout: r.requestReadTimeout,
		server:             nil,
		closeOnce:          sync.Once{},
		mu:                 sync.Mutex{},
		serveErr:           nil,
	}, nil
}

// serviceMode is how a node opens its service: tsnet forwards raw TCP and the
// node terminates TLS itself, with only the certificate its tracker obtained
// under the issuance lease. In HTTPS mode, or with TerminateTLS, tsnet would
// order a certificate for any client that connects before one is stored,
// outside the lease.
func serviceMode() tsnet.ServiceModeTCP {
	return tsnet.ServiceModeTCP{Port: servicePort, TerminateTLS: false, PROXYProtocolVersion: proxyProtocolVersion}
}

// certPairFunc asks tsnet for the certificate for domain, ordering one if
// none is stored; it is local.Client.CertPair.
type certPairFunc func(ctx context.Context, domain string) (certPEM, keyPEM []byte, err error)

// leasedCertificateFetch obtains the certificate for fqdn through certPair.
// When the store shares certificates across the connection's slots, a stored
// one is reused without the lease: that is every start after the first
// issuance, so restarting a pod with many connections takes no leases. A
// missing one is ordered only by the slot holding the issuance lease.
func leasedCertificateFetch(store ipn.StateStore, fqdn string, certPair certPairFunc) certificateFetcher {
	return func(ctx context.Context) (*tls.Certificate, error) {
		if issuer, ok := store.(certificateIssuer); ok {
			if _, _, err := issuer.ReadTLSCertAndKey(fqdn); err != nil {
				release, claimed, err := issuer.ClaimCertificateIssuance(ctx)
				if err != nil {
					return nil, fmt.Errorf("claim tailnet service certificate lease: %w", err)
				}
				if !claimed {
					return nil, errCertificateIssuing
				}
				defer release()
			}
		}
		certPEM, keyPEM, err := certPair(ctx, fqdn)
		if err != nil {
			return nil, fmt.Errorf("obtain tailnet service certificate: %w", err)
		}
		pair, err := tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			return nil, fmt.Errorf("parse tailnet service certificate: %w", err)
		}
		return &pair, nil
	}
}

// waitForApproval gives the tailnet a moment to approve the node as a host
// of service, returning true as soon as it does, or false when approvalWait
// elapses or ctx is done. It never fails: Status reports a host that is
// still unapproved.
func waitForApproval(ctx context.Context, lc *local.Client, service tailcfg.ServiceName) bool {
	waitCtx, cancel := context.WithTimeout(ctx, approvalWait)
	defer cancel()

	ticker := time.NewTicker(approvalPollInterval)
	defer ticker.Stop()
	for {
		if st, err := lc.StatusWithoutPeers(waitCtx); err == nil {
			if approved, err := hostsService(st.Self, service); err == nil && approved {
				return true
			}
		}

		select {
		case <-waitCtx.Done():
			return false
		case <-ticker.C:
		}
	}
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
	cert               *certificateTracker
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
		TLSConfig:         serviceTLSConfig(n.cert),
	}
	// Every connection comes from tsnet's forwarder, which names the tailnet
	// peer in a PROXY header; a connection without one is refused.
	listener := &proxyproto.Listener{
		Listener:          n.ln,
		Policy:            nil,
		ConnPolicy:        func(proxyproto.ConnPolicyOptions) (proxyproto.Policy, error) { return proxyproto.REQUIRE, nil },
		ValidateHeader:    nil,
		ReadHeaderTimeout: requestHeaderTimeout,
		ReadBufferSize:    0,
	}

	go func() {
		// ServeTLS takes the certificate from TLSConfig and offers HTTP/2.
		err := n.server.ServeTLS(listener, "", "")
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

// serviceTLSConfig serves the tracker's certificate, so a handshake before
// the node holds one fails instead of ordering one.
func serviceTLSConfig(cert *certificateTracker) *tls.Config {
	return &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: cert.Certificate,
	}
}

func (n *tsnetNode) Status(ctx context.Context) (NodeState, error) {
	n.mu.Lock()
	serveErr := n.serveErr
	n.mu.Unlock()

	if serveErr != nil {
		return NodeState{Online: false, BackendState: "", NeedsLogin: false, AwaitingApproval: false, AwaitingCertificate: false, CertificateStatus: "", NodeID: "", FQDN: n.fqdn}, fmt.Errorf("service listener stopped: %w", serveErr)
	}

	st, err := n.lc.Status(ctx)
	if err != nil {
		return NodeState{Online: false, BackendState: "", NeedsLogin: false, AwaitingApproval: false, AwaitingCertificate: false, CertificateStatus: "", NodeID: "", FQDN: n.fqdn}, fmt.Errorf("tsnet status: %w", err)
	}

	running := st.BackendState == ipn.Running.String()
	approved, err := hostsService(st.Self, n.service)
	if err != nil {
		// Fail closed, but report why rather than as a plain approval wait.
		return NodeState{Online: false, BackendState: st.BackendState, NeedsLogin: false, AwaitingApproval: false, AwaitingCertificate: false, CertificateStatus: "", NodeID: "", FQDN: n.fqdn}, err
	}
	certified, certificateStatus := false, ""
	if running && approved {
		certified, certificateStatus = n.cert.Poll()
	}
	state := NodeState{
		Online: running && approved && certified, BackendState: st.BackendState,
		NeedsLogin:          st.BackendState == ipn.NeedsLogin.String(),
		AwaitingApproval:    running && !approved,
		AwaitingCertificate: running && approved && !certified,
		CertificateStatus:   certificateStatus,
		NodeID:              "", FQDN: n.fqdn,
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
func hostsService(self *ipnstate.PeerStatus, service tailcfg.ServiceName) (bool, error) {
	if self == nil {
		return false, nil
	}

	mappings, err := tailcfg.UnmarshalNodeCapJSON[tailcfg.ServiceIPMappings](self.CapMap, nodecap.ServiceHost)
	if err != nil {
		return false, fmt.Errorf("read service-host capability: %w", err)
	}
	for _, mapping := range mappings {
		if addrs, ok := mapping[service]; ok && len(addrs) > 0 {
			return true, nil
		}
	}

	return false, nil
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
		n.cert.Close()

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
