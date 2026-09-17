package activities

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/server/internal/oktaapplications"
)

type GetOktaApplicationSyncCandidates struct {
	syncer *oktaapplications.Syncer
}

func NewGetOktaApplicationSyncCandidates(syncer *oktaapplications.Syncer) *GetOktaApplicationSyncCandidates {
	return &GetOktaApplicationSyncCandidates{syncer: syncer}
}

type GetOktaApplicationSyncCandidatesInput struct {
	Limit int32
	// ExcludeConnectionIDs are connections already attempted this pass.
	ExcludeConnectionIDs []uuid.UUID
}

func (c *GetOktaApplicationSyncCandidates) Do(ctx context.Context, input GetOktaApplicationSyncCandidatesInput) ([]oktaapplications.SyncCandidate, error) {
	candidates, err := c.syncer.ListCandidates(ctx, input.Limit, input.ExcludeConnectionIDs)
	if err != nil {
		return nil, fmt.Errorf("get okta application sync candidates: %w", err)
	}
	return candidates, nil
}

// RunOktaApplicationSync reconciles one connection. Its input is the
// connection id only; the credential is resolved inside the activity.
type RunOktaApplicationSync struct {
	syncer *oktaapplications.Syncer
}

func NewRunOktaApplicationSync(syncer *oktaapplications.Syncer) *RunOktaApplicationSync {
	return &RunOktaApplicationSync{syncer: syncer}
}

func (r *RunOktaApplicationSync) Do(ctx context.Context, connectionID string) error {
	id, err := uuid.Parse(connectionID)
	if err != nil {
		return fmt.Errorf("parse identity provider connection id: %w", err)
	}

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				activity.RecordHeartbeat(ctx)
			}
		}
	}()

	if err := r.syncer.Run(ctx, id); err != nil {
		return fmt.Errorf("run okta application sync: %w", err)
	}
	return nil
}
