package route

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// authTunnelRoutesKeyPrefix precedes the auth role's copy of a tunnel's live
// routes. Only tunnels that carry OAuth back-channel calls are copied there,
// so the set stays small, and the gateway writes it without going through the
// operations instance, so an operations outage or move does not break
// tunneled OAuth.
const authTunnelRoutesKeyPrefix = "auth_tunnel_routes:"

// AuthRouteKey returns the Redis key for a tunnel's route on the auth role.
func AuthRouteKey(tunnelID string) string {
	return authTunnelRoutesKeyPrefix + tunnelID
}

// NewAuthMirror returns the auth role's route store. It has the same shape
// and expiry as the bulk route store and keeps no connection snapshots.
func NewAuthMirror(client redis.UniversalClient) Store {
	return &authMirror{routes: &Redis{client: client, routeKey: AuthRouteKey}}
}

// authMirror narrows Redis to the route methods, so the mirror can never be
// asked to hold connection snapshots.
type authMirror struct {
	routes *Redis
}

func (m *authMirror) Publish(ctx context.Context, tunnelID, addr string, ttl time.Duration) error {
	if err := m.routes.Publish(ctx, tunnelID, addr, ttl); err != nil {
		return fmt.Errorf("auth route mirror: %w", err)
	}
	return nil
}

func (m *authMirror) Candidates(ctx context.Context, tunnelID string) ([]string, error) {
	candidates, err := m.routes.Candidates(ctx, tunnelID)
	if err != nil {
		return nil, fmt.Errorf("auth route mirror: %w", err)
	}
	return candidates, nil
}

func (m *authMirror) Unpublish(ctx context.Context, tunnelID, addr string) error {
	if err := m.routes.Unpublish(ctx, tunnelID, addr); err != nil {
		return fmt.Errorf("auth route mirror: %w", err)
	}
	return nil
}

func (m *authMirror) Delete(ctx context.Context, tunnelID string) error {
	if err := m.routes.Delete(ctx, tunnelID); err != nil {
		return fmt.Errorf("auth route mirror: %w", err)
	}
	return nil
}

// MirrorReader reads a tunnel's routes from the auth role's mirror first and
// falls back to the bulk routes. A tunnel the mirror holds is served without
// touching the bulk store, so callers of mirrored tunnels keep working while
// the operations instance is down, full or being moved.
type MirrorReader struct {
	mirror Store
	bulk   Store
}

// NewMirrorReader returns a reader over the auth role's mirror and the bulk
// route store.
func NewMirrorReader(mirror, bulk Store) *MirrorReader {
	return &MirrorReader{mirror: mirror, bulk: bulk}
}

// Candidates returns the mirror's routes when it has any. Otherwise it returns
// the bulk store's, and reports the mirror's error only when the bulk store
// failed too.
func (r *MirrorReader) Candidates(ctx context.Context, tunnelID string) ([]string, error) {
	mirrored, mirrorErr := r.mirror.Candidates(ctx, tunnelID)
	if mirrorErr == nil && len(mirrored) > 0 {
		return mirrored, nil
	}

	candidates, err := r.bulk.Candidates(ctx, tunnelID)
	if err != nil {
		return nil, errors.Join(err, mirrorErr)
	}
	return candidates, nil
}

// Unpublish removes a dead gateway from both stores, so neither hands it out
// again. The mirror goes first: it is what tunneled OAuth reads.
func (r *MirrorReader) Unpublish(ctx context.Context, tunnelID, addr string) error {
	return errors.Join(r.mirror.Unpublish(ctx, tunnelID, addr), r.bulk.Unpublish(ctx, tunnelID, addr))
}

// Publish writes both stores. Readers do not publish; the method exists so the
// reader satisfies Store.
func (r *MirrorReader) Publish(ctx context.Context, tunnelID, addr string, ttl time.Duration) error {
	return errors.Join(r.mirror.Publish(ctx, tunnelID, addr, ttl), r.bulk.Publish(ctx, tunnelID, addr, ttl))
}

// Delete removes the tunnel from both stores.
func (r *MirrorReader) Delete(ctx context.Context, tunnelID string) error {
	return errors.Join(r.mirror.Delete(ctx, tunnelID), r.bulk.Delete(ctx, tunnelID))
}

var _ Store = (*authMirror)(nil)
var _ Store = (*MirrorReader)(nil)
