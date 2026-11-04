package authz

import (
	"context"
	"errors"
)

// EvaluateGrantsForStoredCheck answers EvaluateGrants for a check replayed
// from storage, such as a recorded challenge, whose scope and selector were
// captured before typed operations existed. A check that a registered
// permission answers is routed like EvaluateGrants. One whose scope no
// registered permission checks is answered by the engine's grant matching,
// which is how every such check is decided today.
func (c *Client) EvaluateGrantsForStoredCheck(ctx context.Context, grants []Grant, check Check) (bool, error) {
	op, err := operationForCheck(c.registry, check)
	if errors.Is(err, ErrUnknownPermission) {
		return GrantsAuthorize(grants, check)
	}
	if err != nil {
		return false, err
	}
	return c.EvaluateGrants(ctx, grants, op)
}
