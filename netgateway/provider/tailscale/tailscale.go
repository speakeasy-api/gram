// Package tailscale implements the gateway's Provider interface with embedded
// tsnet nodes. It is the only package in the repository that may import
// tailscale.com (enforced by depguard for the server tree).
//
// Durable node identity (machine key, node key, profile) lives in the
// gateway's StateStore, so a node resumes as the same tailnet device after a
// process restart or replica failover with only an ephemeral local directory.
// This behavior — including unclean-death resume in under two seconds — was
// validated by the Phase 0 spike recorded in docs/network-ingress-design.md.
package tailscale

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"

	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tsnet"

	"github.com/speakeasy-api/gram/netgateway/gateway"
)

type Provider struct {
	// ControlURL overrides the Tailscale control plane, e.g. for a Headscale
	// variant or tests. Empty means the public Tailscale control plane.
	ControlURL string

	// APIBase overrides the Tailscale HTTP API used to mint auth keys from
	// OAuth clients. Empty means https://api.tailscale.com.
	APIBase string

	Logger *slog.Logger
}

var _ gateway.Provider = (*Provider)(nil)

func (p *Provider) Name() string { return "tailscale" }

func (p *Provider) NewNode(_ context.Context, cfg gateway.IngressConfig, state gateway.StateStore) (gateway.Node, error) {
	return &node{
		provider: p,
		cfg:      cfg,
		state:    state,
		ts:       nil,
		dir:      "",
		netName:  "",
		dnsName:  "",
		nodeID:   "",
	}, nil
}

type node struct {
	provider *Provider
	cfg      gateway.IngressConfig
	state    gateway.StateStore

	ts  *tsnet.Server
	dir string

	netName string
	dnsName string
	nodeID  string
}

func (n *node) Start(ctx context.Context) error {
	authKey, err := n.resolveAuthKey(ctx)
	if err != nil {
		return err
	}

	// Dir holds only re-obtainable caches (certs, logs); device identity is
	// in the state store. A fresh temp dir per start is deliberate — it is
	// the same ephemeral-disk posture the deployment runs with.
	dir, err := os.MkdirTemp("", "netingress-"+n.cfg.ID.String())
	if err != nil {
		return fmt.Errorf("create tsnet state dir: %w", err)
	}
	n.dir = dir

	logf := func(string, ...any) {}
	if n.provider.Logger != nil && n.provider.Logger.Enabled(ctx, slog.LevelDebug) {
		logger := n.provider.Logger
		logf = func(format string, args ...any) {
			logger.DebugContext(context.Background(), fmt.Sprintf(format, args...))
		}
	}

	n.ts = &tsnet.Server{
		Hostname:   n.cfg.Hostname,
		Dir:        dir,
		Store:      &ipnStore{state: n.state},
		AuthKey:    authKey,
		ControlURL: n.provider.ControlURL,
		Ephemeral:  false,
		Logf:       logf,
	}

	status, err := n.ts.Up(ctx)
	if err != nil {
		n.cleanup()
		return fmt.Errorf("tsnet up: %w", err)
	}
	n.recordStatus(status)
	return nil
}

// resolveAuthKey produces the join key for a fresh device. A node with stored
// identity re-authenticates from the state store and needs no key, so OAuth
// minting is skipped — it would create a stray key in the customer's tailnet
// on every restart.
func (n *node) resolveAuthKey(ctx context.Context) (string, error) {
	if _, err := n.state.Get(ctx, string(ipn.MachineKeyStateKey)); err == nil {
		return "", nil
	} else if !errors.Is(err, gateway.ErrStateNotFound) {
		return "", fmt.Errorf("probe node state: %w", err)
	}

	switch n.cfg.Credential.Kind {
	case gateway.CredentialKindAuthKey:
		if n.cfg.Credential.AuthKey == "" {
			return "", errors.New("auth key credential is empty and no stored node identity exists")
		}
		return n.cfg.Credential.AuthKey, nil
	case gateway.CredentialKindOAuthClient:
		key, err := n.mintAuthKey(ctx)
		if err != nil {
			return "", fmt.Errorf("mint auth key from oauth client: %w", err)
		}
		return key, nil
	default:
		return "", fmt.Errorf("unknown credential kind: %q", n.cfg.Credential.Kind)
	}
}

func (n *node) recordStatus(status *ipnstate.Status) {
	if status == nil {
		return
	}
	if status.CurrentTailnet != nil {
		n.netName = status.CurrentTailnet.Name
	}
	if status.Self != nil {
		n.dnsName = strings.TrimSuffix(status.Self.DNSName, ".")
		n.nodeID = string(status.Self.ID)
	}
}

func (n *node) Listener(_ context.Context) (net.Listener, error) {
	ln, err := n.ts.Listen("tcp", ":80")
	if err != nil {
		return nil, fmt.Errorf("tsnet listen: %w", err)
	}
	return ln, nil
}

func (n *node) Identity(ctx context.Context, remoteAddr string) (*gateway.PeerIdentity, error) {
	lc, err := n.ts.LocalClient()
	if err != nil {
		return nil, fmt.Errorf("tsnet local client: %w", err)
	}
	who, err := lc.WhoIs(ctx, remoteAddr)
	if err != nil {
		return nil, fmt.Errorf("tailscale whois: %w", err)
	}

	identity := &gateway.PeerIdentity{
		Login:       "",
		DisplayName: "",
		Device:      "",
		Tags:        nil,
		Caps:        nil,
	}
	if who.UserProfile != nil {
		identity.Login = who.UserProfile.LoginName
		identity.DisplayName = who.UserProfile.DisplayName
	}
	if who.Node != nil {
		identity.Device = strings.TrimSuffix(who.Node.Name, ".")
		if who.Node.Tags != nil {
			identity.Tags = append(identity.Tags, who.Node.Tags...)
		}
	}
	for cap := range who.CapMap {
		identity.Caps = append(identity.Caps, string(cap))
	}
	if identity.Login == "" && identity.Device == "" {
		return nil, nil
	}
	return identity, nil
}

func (n *node) Status(ctx context.Context) gateway.NodeStatus {
	status := gateway.NodeStatus{
		Online:      false,
		NetworkName: n.netName,
		DNSName:     n.dnsName,
		NodeID:      n.nodeID,
		Err:         "",
	}

	lc, err := n.ts.LocalClient()
	if err != nil {
		status.Err = err.Error()
		return status
	}
	st, err := lc.StatusWithoutPeers(ctx)
	if err != nil {
		status.Err = err.Error()
		return status
	}
	n.recordStatus(st)
	status.Online = st.BackendState == "Running"
	status.NetworkName = n.netName
	status.DNSName = n.dnsName
	status.NodeID = n.nodeID
	return status
}

// Close shuts the node down without logging the device out of the tailnet:
// disable/stop must not force a re-auth, and the device identity stays in
// the state store for the next start. Deletion-time logout ships with the
// delete lifecycle.
func (n *node) Close(_ context.Context) error {
	var err error
	if n.ts != nil {
		err = n.ts.Close()
	}
	n.cleanup()
	if err != nil {
		return fmt.Errorf("tsnet close: %w", err)
	}
	return nil
}

func (n *node) cleanup() {
	if n.dir != "" {
		_ = os.RemoveAll(n.dir)
		n.dir = ""
	}
}

// ipnStore adapts the gateway's StateStore to tsnet's ipn.StateStore.
// ipn.StateStore carries no context, so calls use the background context;
// the underlying Postgres store applies its own timeouts.
type ipnStore struct {
	state gateway.StateStore
}

var _ ipn.StateStore = (*ipnStore)(nil)

func (s *ipnStore) ReadState(id ipn.StateKey) ([]byte, error) {
	v, err := s.state.Get(context.Background(), string(id))
	if errors.Is(err, gateway.ErrStateNotFound) {
		return nil, ipn.ErrStateNotExist
	}
	if err != nil {
		return nil, fmt.Errorf("read tsnet state: %w", err)
	}
	return v, nil
}

func (s *ipnStore) WriteState(id ipn.StateKey, bs []byte) error {
	if err := s.state.Set(context.Background(), string(id), bs); err != nil {
		return fmt.Errorf("write tsnet state: %w", err)
	}
	return nil
}
