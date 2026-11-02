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
	actor     string
}

type recordingPublicationSignaler struct {
	mu      sync.Mutex
	signals []recordedPublicationSignal
}

func (r *recordingPublicationSignaler) SignalPluginPublish(_ context.Context, projectID uuid.UUID, createdByUserID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.signals = append(r.signals, recordedPublicationSignal{projectID: projectID, sync: false, actor: createdByUserID})
	return nil
}

func (r *recordingPublicationSignaler) SignalPluginSync(_ context.Context, projectID uuid.UUID, createdByUserID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.signals = append(r.signals, recordedPublicationSignal{projectID: projectID, sync: true, actor: createdByUserID})
	return nil
}

// A batch signals each project once. A manual sync in it must win over the
// ordinary requests for the same project, wherever it falls in the batch, or
// the pending changes of plugins whose auto sync is off stay unpublished. The
// sync is attributed to the user who asked for it, not to whoever's ordinary
// request came first.
func TestPublicationHandlerKeepsAManualSyncThatCoalescesWithOrdinaryRequests(t *testing.T) {
	t.Parallel()

	const organizationID = "org-1"
	synced, ordinary := uuid.New(), uuid.New()
	request := func(projectID uuid.UUID, includePaused bool, actor string) streams.BatchMessage[*publicationv1.PublicationRequested] {
		return streams.BatchMessage[*publicationv1.PublicationRequested]{Message: publicationv1.PublicationRequested_builder{
			OrganizationId:       new(organizationID),
			ProjectId:            new(projectID.String()),
			CreatedByUserId:      new(actor),
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
		request(synced, false, "automatic-editor"),
		request(ordinary, false, "ordinary-editor"),
		request(synced, true, "manual-syncer"),
		request(ordinary, false, "later-editor"),
		request(synced, true, "second-syncer"),
	}))

	require.ElementsMatch(t, []recordedPublicationSignal{
		{projectID: synced, sync: true, actor: "manual-syncer"},
		{projectID: ordinary, sync: false, actor: "ordinary-editor"},
	}, signaler.signals)
}
