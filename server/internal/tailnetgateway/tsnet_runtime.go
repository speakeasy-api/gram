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
	"tailscale.com/tsnet"
	"tailscale.com/types/logger"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	// servicePort is the only port the gateway advertises on each service.
	servicePort = 443

	// stateDirMode keeps tsnet's scratch files private to the pod user.
	stateDirMode = 0o700

	// serveShutdownTimeout bounds how long Close waits for in-flight requests
	// when the caller's context has no deadline.
	serveShutdownTimeout = 20 * time.Second
)

// TsnetRuntime starts real tailnet nodes with tsnet.
type TsnetRuntime struct {
	// Logger receives tsnet's user-facing messages such as login prompts.
	Logger *slog.Logger
}

var _ NodeRuntime = (*TsnetRuntime)(nil)

// Start registers or reconnects the node, waits until it is running, and
// advertises the service.
func (r *TsnetRuntime) Start(ctx context.Context, spec NodeSpec) (Node, error) {
	if spec.Store == nil || spec.Hostname == "" || spec.ServiceName == "" || spec.Tag == "" || spec.StateDir == "" {
		return nil, errors.New("tsnet node spec is incomplete")
	}
	if err := os.MkdirAll(spec.StateDir, stateDirMode); err != nil {
		return nil, fmt.Errorf("create tsnet state dir: %w", err)
	}
	userLogf := logger.Discard
	if r.Logger != nil {
		hostLogger := r.Logger.With(attr.SlogNetworkIngressID(spec.IngressID.String()), attr.SlogTailnetGatewaySlot(spec.Slot))
		userLogf = func(format string, args ...any) {
			hostLogger.InfoContext(ctx, fmt.Sprintf(format, args...))
		}
	}
	//nolint:exhaustruct // tsnet.Server documents its remaining fields as optional.
	srv := &tsnet.Server{
		Dir:           spec.StateDir,
		Store:         spec.Store,
		Hostname:      spec.Hostname,
		UserLogf:      userLogf,
		Logf:          logger.Discard,
		Ephemeral:     false,
		AuthKey:       spec.AuthKey,
		AdvertiseTags: []string{spec.Tag},
		ControlURL:    spec.ControlURL,
	}
	if err := srv.Start(); err != nil {
		return nil, fmt.Errorf("start tsnet node: %w", err)
	}

	timeout := spec.StartTimeout
	if timeout <= 0 {
		timeout = time.Minute
	}
	upCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if _, err := srv.Up(upCtx); err != nil {
		_ = srv.Close()
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
	return &tsnetNode{srv: srv, ln: ln, lc: lc, fqdn: ln.FQDN, server: nil, once: sync.Once{}}, nil
}

type tsnetNode struct {
	srv    *tsnet.Server
	ln     *tsnet.ServiceListener
	lc     *local.Client
	fqdn   string
	server *http.Server
	once   sync.Once
}

func (n *tsnetNode) Serve(handler http.Handler) error {
	if n.server != nil {
		return errors.New("tsnet node is already serving")
	}
	n.server = &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       620 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return context.Background() },
	}
	go func() {
		if err := n.server.Serve(n.ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			_ = err
		}
	}()
	return nil
}

func (n *tsnetNode) Status(ctx context.Context) (NodeState, error) {
	st, err := n.lc.Status(ctx)
	if err != nil {
		return NodeState{Online: false, BackendState: "", NodeID: "", FQDN: n.fqdn}, fmt.Errorf("tsnet status: %w", err)
	}
	state := NodeState{Online: st.BackendState == ipn.Running.String(), BackendState: st.BackendState, NodeID: "", FQDN: n.fqdn}
	if st.Self != nil {
		state.NodeID = string(st.Self.ID)
	}
	return state, nil
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
	n.once.Do(func() {
		if n.server != nil {
			shutdownCtx := ctx
			if _, ok := ctx.Deadline(); !ok {
				var cancel context.CancelFunc
				shutdownCtx, cancel = context.WithTimeout(ctx, serveShutdownTimeout)
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
