package mcp

import (
	"context"
	"errors"
	"fmt"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// The IDP callback rotates challenge IDs. Resolve the committed grant's actual
// parent and use its server-stamped attempt reference, never the initial ID or
// a parameter supplied by the returning browser.
func (s *Service) recordConnectionPopupGrant(ctx context.Context, grant remotesessions.RemoteGrant) error {
	if !grant.ConnectionPopup {
		return nil
	}
	// Consent updates may race the callback; reload before retrying so those
	// fields are preserved instead of overwriting the newer challenge.
	const maxGrantPublicationAttempts = 3
	for range maxGrantPublicationAttempts {
		parent, err := s.authnChallengeCache.Get(ctx, "authnChallenge:"+grant.ParentChallengeID)
		if err != nil {
			if errors.Is(err, redisCache.ErrCacheMiss) {
				return nil
			}
			return fmt.Errorf("load popup grant parent: %w", err)
		}
		if !parent.FirstParty || parent.Subject == nil || *parent.Subject != grant.Subject || parent.UserSessionIssuerID != grant.UserSessionIssuerID {
			return nil
		}
		if parent.BindingRefreshAttemptID != "" && s.bindingRefresh != nil {
			if err := s.bindingRefresh.Record(ctx, parent.BindingRefreshAttemptID, grant); err != nil {
				return fmt.Errorf("record attachment popup grant: %w", err)
			}
		}
		if parent.ReauthenticateClientID == grant.RemoteSessionClientID {
			completed := parent
			completed.ReauthenticateClientID = uuid.Nil
			swapped, err := s.authnChallengeCache.CompareAndSwap(ctx, parent, completed)
			if err != nil {
				return fmt.Errorf("complete requested reconnect: %w", err)
			}
			if !swapped {
				continue
			}
		}
		return nil
	}
	return oops.E(oops.CodeConflict, nil, "connection changed while completing reconnect; retry sign-in")
}
