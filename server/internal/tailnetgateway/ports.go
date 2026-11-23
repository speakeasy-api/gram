package tailnetgateway

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// errNoTsnetPort reports that every port in the pod's range is held by a
// running node. MaxHosts bounds both, so it means a node was not released.
var errNoTsnetPort = errors.New("no tsnet port available")

// portPool hands each tsnet node in this pod its own UDP port from
// [base, base+size). Inside a pod, Kubernetes networking rewrites outbound
// source ports per destination, so a node on a random port has no public
// endpoint a strict-NAT client can reach and falls back to a DERP relay. With
// a fixed port, magicsock also advertises the STUN address with the local
// port, which the pod publishes as a hostPort on its node.
//
// Ports only need to be unique within a pod: every pod is its own node with
// its own address, so pods reuse the same range. A zero base leaves the port
// to tsnet, as before.
type portPool struct {
	mu   sync.Mutex
	base uint16
	used []bool
	next int
}

func newPortPool(base uint16, size int) *portPool {
	if base == 0 {
		return &portPool{mu: sync.Mutex{}, base: 0, used: nil, next: 0}
	}
	return &portPool{mu: sync.Mutex{}, base: base, used: make([]bool, size), next: 0}
}

// acquire returns a free port, or 0 when the pool is disabled and tsnet
// picks its own. It searches from just after the last port handed out, so a
// port released a moment ago is the last to be reused.
func (p *portPool) acquire() (uint16, error) {
	if p.base == 0 {
		return 0, nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.used {
		index := (p.next + i) % len(p.used)
		if !p.used[index] {
			p.used[index] = true
			p.next = index + 1
			return p.base + uint16(index), nil //nolint:gosec // index < size, and Validate keeps base+size within uint16.
		}
	}
	return 0, errNoTsnetPort
}

// release returns port to the pool. Call it only once the node holding it is
// closed, so the socket is gone before another node binds the port.
func (p *portPool) release(port uint16) {
	if p.base == 0 || port < p.base {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if index := int(port - p.base); index < len(p.used) {
		p.used[index] = false
	}
}

// portNode releases its port once the node it wraps has closed, whichever
// path closes it: a normal stop, a drain, or a start that failed after the
// node came up.
type portNode struct {
	Node
	once    sync.Once
	release func()
}

func (n *portNode) Close(ctx context.Context) error {
	err := n.Node.Close(ctx)
	// Close runs tsnet.Server.Close synchronously even when the service
	// listener's shutdown fails, so the UDP socket is gone either way.
	n.once.Do(n.release)
	if err != nil {
		return fmt.Errorf("close tailnet node: %w", err)
	}
	return nil
}
