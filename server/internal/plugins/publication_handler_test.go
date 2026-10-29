package plugins

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	publicationv1 "github.com/speakeasy-api/gram/infra/gen/gram/plugins/v1"
	"github.com/speakeasy-api/gram/server/internal/streams"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type recordedPublicationSignal struct {
	projectID uuid.UUID
	sync      bool
}

type recordingPublicationSignaler struct {
	mu      sync.Mutex
	signals []recordedPublicationSignal
}

func (r *recordingPublicationSignaler) SignalPluginPublish(_ context.Context, projectID uuid.UUID, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.signals = append(r.signals, recordedPublicationSignal{projectID: projectID, sync: false})
	return nil
}

func (r *recordingPublicationSignaler) SignalPluginSync(_ context.Context, projectID uuid.UUID, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.signals = append(r.signals, recordedPublicationSignal{projectID: projectID, sync: true})
	return nil
}

// A batch signals each project once. A manual sync in it must win over the
// ordinary requests for the same project, wherever it falls in the batch, or
// the pending changes of plugins whose auto sync is off stay unpublished.
func TestPublicationHandlerKeepsAManualSyncThatCoalescesWithOrdinaryRequests(t *testing.T) {
	t.Parallel()

	const organizationID = "org-1"
	synced, ordinary := uuid.New(), uuid.New()
	request := func(projectID uuid.UUID, includePaused bool) streams.BatchMessage[*publicationv1.PublicationRequested] {
		return streams.BatchMessage[*publicationv1.PublicationRequested]{Message: publicationv1.PublicationRequested_builder{
			OrganizationId:       new(organizationID),
			ProjectId:            new(projectID.String()),
			CreatedByUserId:      new("user-1"),
			IncludePausedPlugins: new(includePaused),
		}.Build()}
	}
	signaler := &recordingPublicationSignaler{}
	handler := &PublicationHandler{
		logger:   testenv.NewLogger(t),
		validate: func(context.Context, string, uuid.UUID) (bool, error) { return true, nil },
		signaler: signaler,
	}

	require.NoError(t, handler.HandleBatchWithResult(t.Context(), []streams.BatchMessage[*publicationv1.PublicationRequested]{
		request(synced, false),
		request(ordinary, false),
		request(synced, true),
		request(ordinary, false),
		request(synced, false),
	}))

	require.ElementsMatch(t, []recordedPublicationSignal{
		{projectID: synced, sync: true},
		{projectID: ordinary, sync: false},
	}, signaler.signals)
}
