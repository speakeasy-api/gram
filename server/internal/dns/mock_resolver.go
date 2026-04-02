package dns

import (
	"context"
	"errors"
	"net"
	"net/netip"
)

// MockResolverConfig configures the behavior of a [MockResolver]. Each function
// field corresponds to a [Resolver] method. Nil fields receive defaults that
// return a "not implemented" error.
type MockResolverConfig struct {
	LookupAddrFunc   func(ctx context.Context, addr string) ([]string, error)
	LookupCNAMEFunc  func(ctx context.Context, host string) (string, error)
	LookupHostFunc   func(ctx context.Context, host string) ([]string, error)
	LookupIPFunc     func(ctx context.Context, network, host string) ([]net.IP, error)
	LookupIPAddrFunc func(ctx context.Context, host string) ([]net.IPAddr, error)
	LookupMXFunc     func(ctx context.Context, name string) ([]*net.MX, error)
	LookupNetIPFunc  func(ctx context.Context, network, host string) ([]netip.Addr, error)
	LookupNSFunc     func(ctx context.Context, name string) ([]*net.NS, error)
	LookupSRVFunc    func(ctx context.Context, service, proto, name string) (string, []*net.SRV, error)
	LookupTXTFunc    func(ctx context.Context, name string) ([]string, error)
}

// MockResolver implements [Resolver] with configurable function fields for
// testing. Create one with [NewMockResolver].
type MockResolver struct {
	lookupAddrFunc   func(ctx context.Context, addr string) ([]string, error)
	lookupCNAMEFunc  func(ctx context.Context, host string) (string, error)
	lookupHostFunc   func(ctx context.Context, host string) ([]string, error)
	lookupIPFunc     func(ctx context.Context, network, host string) ([]net.IP, error)
	lookupIPAddrFunc func(ctx context.Context, host string) ([]net.IPAddr, error)
	lookupMXFunc     func(ctx context.Context, name string) ([]*net.MX, error)
	lookupNetIPFunc  func(ctx context.Context, network, host string) ([]netip.Addr, error)
	lookupNSFunc     func(ctx context.Context, name string) ([]*net.NS, error)
	lookupSRVFunc    func(ctx context.Context, service, proto, name string) (string, []*net.SRV, error)
	lookupTXTFunc    func(ctx context.Context, name string) ([]string, error)
}

// NewMockResolver creates a [MockResolver] from the given configuration. Any
// nil function fields in cfg are replaced with safe no-op defaults.
func NewMockResolver(cfg MockResolverConfig) *MockResolver {
	m := &MockResolver{
		lookupAddrFunc:   cfg.LookupAddrFunc,
		lookupCNAMEFunc:  cfg.LookupCNAMEFunc,
		lookupHostFunc:   cfg.LookupHostFunc,
		lookupIPFunc:     cfg.LookupIPFunc,
		lookupIPAddrFunc: cfg.LookupIPAddrFunc,
		lookupMXFunc:     cfg.LookupMXFunc,
		lookupNetIPFunc:  cfg.LookupNetIPFunc,
		lookupNSFunc:     cfg.LookupNSFunc,
		lookupSRVFunc:    cfg.LookupSRVFunc,
		lookupTXTFunc:    cfg.LookupTXTFunc,
	}

	if m.lookupAddrFunc == nil {
		m.lookupAddrFunc = func(context.Context, string) ([]string, error) {
			return nil, errors.New("MockResolver: LookupAddrFunc not implemented")
		}
	}
	if m.lookupCNAMEFunc == nil {
		m.lookupCNAMEFunc = func(context.Context, string) (string, error) {
			return "", errors.New("MockResolver: LookupCNAMEFunc not implemented")
		}
	}
	if m.lookupHostFunc == nil {
		m.lookupHostFunc = func(context.Context, string) ([]string, error) {
			return nil, errors.New("MockResolver: LookupHostFunc not implemented")
		}
	}
	if m.lookupIPFunc == nil {
		m.lookupIPFunc = func(context.Context, string, string) ([]net.IP, error) {
			return nil, errors.New("MockResolver: LookupIPFunc not implemented")
		}
	}
	if m.lookupIPAddrFunc == nil {
		m.lookupIPAddrFunc = func(context.Context, string) ([]net.IPAddr, error) {
			return nil, errors.New("MockResolver: LookupIPAddrFunc not implemented")
		}
	}
	if m.lookupMXFunc == nil {
		m.lookupMXFunc = func(context.Context, string) ([]*net.MX, error) {
			return nil, errors.New("MockResolver: LookupMXFunc not implemented")
		}
	}
	if m.lookupNetIPFunc == nil {
		m.lookupNetIPFunc = func(context.Context, string, string) ([]netip.Addr, error) {
			return nil, errors.New("MockResolver: LookupNetIPFunc not implemented")
		}
	}
	if m.lookupNSFunc == nil {
		m.lookupNSFunc = func(context.Context, string) ([]*net.NS, error) {
			return nil, errors.New("MockResolver: LookupNSFunc not implemented")
		}
	}
	if m.lookupSRVFunc == nil {
		m.lookupSRVFunc = func(context.Context, string, string, string) (string, []*net.SRV, error) {
			return "", nil, errors.New("MockResolver: LookupSRVFunc not implemented")
		}
	}
	if m.lookupTXTFunc == nil {
		m.lookupTXTFunc = func(context.Context, string) ([]string, error) {
			return nil, errors.New("MockResolver: LookupTXTFunc not implemented")
		}
	}

	return m
}

// Resolver returns a [net.Resolver] whose Dial function routes DNS queries
// through the configured mock functions.
func (m *MockResolver) Resolver() *net.Resolver {
	return &net.Resolver{
		PreferGo:     true,
		StrictErrors: false,
		Dial:         m.dial,
	}
}

func (m *MockResolver) LookupAddr(ctx context.Context, addr string) ([]string, error) {
	return m.lookupAddrFunc(ctx, addr)
}

func (m *MockResolver) LookupCNAME(ctx context.Context, host string) (string, error) {
	return m.lookupCNAMEFunc(ctx, host)
}

func (m *MockResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	return m.lookupHostFunc(ctx, host)
}

func (m *MockResolver) LookupIP(ctx context.Context, network, host string) ([]net.IP, error) {
	return m.lookupIPFunc(ctx, network, host)
}

func (m *MockResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return m.lookupIPAddrFunc(ctx, host)
}

func (m *MockResolver) LookupMX(ctx context.Context, name string) ([]*net.MX, error) {
	return m.lookupMXFunc(ctx, name)
}

func (m *MockResolver) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return m.lookupNetIPFunc(ctx, network, host)
}

func (m *MockResolver) LookupNS(ctx context.Context, name string) ([]*net.NS, error) {
	return m.lookupNSFunc(ctx, name)
}

func (m *MockResolver) LookupSRV(ctx context.Context, service, proto, name string) (string, []*net.SRV, error) {
	return m.lookupSRVFunc(ctx, service, proto, name)
}

func (m *MockResolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	return m.lookupTXTFunc(ctx, name)
}
