package remotesessions

import (
	"context"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Registration locks span upstream HTTP. Admit before borrowing a connection,
// including while waiting for another registration's session lock. Sharing the
// budget by pool (not service or issuer) keeps distinct clients from consuming
// every connection. Half the pool remains available for ordinary database work.
// A one-connection pool permits one registration for compatibility, but cannot
// reserve capacity; production pools must have at least two connections.
var registrationAdmissions = struct {
	sync.Mutex
	pools map[*pgxpool.Pool]*registrationAdmission
}{Mutex: sync.Mutex{}, pools: make(map[*pgxpool.Pool]*registrationAdmission)}

type registrationAdmission struct {
	slots chan struct{}
	users int // holders and waiters; guarded by registrationAdmissions
}

func admitRegistration(ctx context.Context, pool *pgxpool.Pool) (func(), error) {
	registrationAdmissions.Lock()
	admission := registrationAdmissions.pools[pool]
	if admission == nil {
		admission = &registrationAdmission{users: 0, slots: make(chan struct{}, max(1, pool.Config().MaxConns/2))}
		registrationAdmissions.pools[pool] = admission
	}
	admission.users++
	registrationAdmissions.Unlock()
	drop := func() {
		registrationAdmissions.Lock()
		defer registrationAdmissions.Unlock()
		admission.users--
		if admission.users == 0 {
			delete(registrationAdmissions.pools, pool)
		}
	}
	select {
	case admission.slots <- struct{}{}:
		// Prefer cancellation even when capacity and cancellation become ready together.
		if err := ctx.Err(); err != nil {
			<-admission.slots
			drop()
			return nil, fmt.Errorf("wait for registration admission: %w", err)
		}
		return sync.OnceFunc(func() { <-admission.slots; drop() }), nil
	case <-ctx.Done():
		drop()
		return nil, fmt.Errorf("wait for registration admission: %w", ctx.Err())
	}
}
