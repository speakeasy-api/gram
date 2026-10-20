package feature

import "context"

// AuthClientResolverMode is how an organization's requests use the typed auth
// client resolver.
type AuthClientResolverMode int

const (
	// AuthClientResolverOff keeps the upstream client choice alone.
	AuthClientResolverOff AuthClientResolverMode = iota
	// AuthClientResolverShadow runs the resolver beside the choice and only
	// reports disagreement.
	AuthClientResolverShadow
	// AuthClientResolverEnforced makes the resolver authoritative.
	AuthClientResolverEnforced
)

// AuthClientResolver reports the organization's resolver mode from locally
// evaluated flags. Any evaluation failure keeps it off.
func AuthClientResolver(ctx context.Context, flags Provider, organizationID string) AuthClientResolverMode {
	if flags == nil || organizationID == "" {
		return AuthClientResolverOff
	}
	if enabled, err := flags.IsFlagEnabledLocal(ctx, FlagAuthClientResolver, organizationID, nil, nil); err == nil && enabled {
		return AuthClientResolverEnforced
	}
	if enabled, err := flags.IsFlagEnabledLocal(ctx, FlagAuthClientResolverShadow, organizationID, nil, nil); err == nil && enabled {
		return AuthClientResolverShadow
	}
	return AuthClientResolverOff
}
