package admin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// organizationKeySyncTimeout bounds the key work that follows an access change.
// Each of the two key types waits for its billing lock and makes one OpenRouter
// call; 30 seconds covers both with the client's own retries, and keeps a stuck
// lock from holding the staff request open.
const organizationKeySyncTimeout = 30 * time.Second

// OrganizationInferenceKeys is the OpenRouter surface that follows an
// organization's access state. Both calls take the per-key billing lock
// themselves and leave the upstream key matching the stored causes, so they
// must run outside the caller's transaction.
type OrganizationInferenceKeys interface {
	// AddAPIKeyDisableCause records cause on the key and disables it upstream.
	// A key that does not exist, or already holds the cause, is left alone.
	AddAPIKeyDisableCause(ctx context.Context, orgID string, keyType openrouter.KeyType, cause openrouter.DisableCause) (openrouter.DisableCauseChange, error)

	// RemoveAPIKeyDisableCause removes cause from the key and enables it
	// upstream when no other cause remains. A nil limit keeps the stored one.
	RemoveAPIKeyDisableCause(ctx context.Context, orgID string, keyType openrouter.KeyType, cause openrouter.DisableCause, limit *int) (int, openrouter.DisableCauseChange, error)
}

// SyncInferenceKeysWithOrganizationAccess makes every platform inference key of
// the organization hold the organization_disabled cause exactly while the
// organization is disabled. It is safe to repeat: a key already in the wanted
// state is not touched.
//
// Call it after the access change has committed. Access to the organization is
// decided by disabled_at alone, so a failure here leaves a disabled
// organization blocked and only its upstream keys out of step. Every key type
// is attempted, and the failures are returned together.
func SyncInferenceKeysWithOrganizationAccess(ctx context.Context, keys OrganizationInferenceKeys, organizationID string, enabled bool) error {
	var failures []error
	for _, keyType := range openrouter.AllKeyTypes {
		var err error
		if enabled {
			_, _, err = keys.RemoveAPIKeyDisableCause(ctx, organizationID, keyType, openrouter.DisableCauseOrganizationDisabled, nil)
		} else {
			_, err = keys.AddAPIKeyDisableCause(ctx, organizationID, keyType, openrouter.DisableCauseOrganizationDisabled)
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("%s key: %w", keyType, err))
		}
	}
	return errors.Join(failures...)
}
