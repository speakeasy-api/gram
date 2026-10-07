package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	plugindelivery "github.com/speakeasy-api/gram/server/internal/plugins"
)

// unreachableDBTX fails every query, standing in for the post-commit reads a
// finish step makes after signalling. Those reads only decorate the output, so
// failing them keeps these tests about the signal alone.
type unreachableDBTX struct{}

var errUnreachableDBTX = errors.New("database unreachable in this test")

func (unreachableDBTX) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errUnreachableDBTX
}

func (unreachableDBTX) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errUnreachableDBTX
}

func (unreachableDBTX) QueryRow(context.Context, string, ...any) pgx.Row {
	return unreachableRow{}
}

type unreachableRow struct{}

func (unreachableRow) Scan(...any) error { return errUnreachableDBTX }

// publishSignalCase is one post-commit situation every write that may change a
// published package has to report on the same way.
type publishSignalCase struct {
	name        string
	publication plugindelivery.ProjectPublicationRequestOutcome
	// publisher is nil for a server with no publisher configured.
	publisher  *recordingPluginPublishSignaler
	wantSignal string
}

// publishSignalCases is the shared contract: a durable outbox request needs no
// signal, a missing publisher is reported rather than hidden, and a failed
// signal is distinguished from a delivered one. Every non-enqueued outcome is
// signalled, not_configured and emission_disabled alike.
func publishSignalCases() []publishSignalCase {
	return []publishSignalCase{
		{name: "enqueued needs no signal", publication: plugindelivery.ProjectPublicationEnqueued, publisher: &recordingPluginPublishSignaler{}, wantSignal: "not_requested"},
		{name: "enqueued with no publisher is still not requested", publication: plugindelivery.ProjectPublicationEnqueued, publisher: nil, wantSignal: "not_requested"},
		{name: "no publisher is unavailable", publication: plugindelivery.ProjectPublicationEmissionDisabled, publisher: nil, wantSignal: "unavailable"},
		{name: "emission disabled is signalled", publication: plugindelivery.ProjectPublicationEmissionDisabled, publisher: &recordingPluginPublishSignaler{}, wantSignal: "best_effort_requested"},
		{name: "not configured is signalled", publication: plugindelivery.ProjectPublicationNotConfigured, publisher: &recordingPluginPublishSignaler{}, wantSignal: "best_effort_requested"},
		{name: "empty outcome is signalled", publication: "", publisher: &recordingPluginPublishSignaler{}, wantSignal: "best_effort_requested"},
		{name: "a failed signal is request_failed", publication: plugindelivery.ProjectPublicationNotConfigured, publisher: &recordingPluginPublishSignaler{err: errors.New("signal refused")}, wantSignal: "request_failed"},
	}
}

// signaler returns the case's publisher as the interface a service holds,
// keeping a missing publisher a nil interface rather than a typed nil.
func (c publishSignalCase) signaler() plugindelivery.PluginPublishSignaler {
	if c.publisher == nil {
		return nil
	}
	return c.publisher
}

// requireSignals asserts that a signal was sent exactly when wantSignal says
// one was attempted, naming the written project and the calling user.
func (c publishSignalCase) requireSignals(t *testing.T, projectID uuid.UUID, userID string) {
	t.Helper()
	if c.publisher == nil {
		return
	}
	attempted := c.wantSignal == "best_effort_requested" || c.wantSignal == "request_failed"
	if !attempted {
		require.Empty(t, c.publisher.recorded())
		return
	}
	require.Equal(t, []pluginPublishSignal{{projectID: projectID, userID: userID}}, c.publisher.recorded())
}

var publishSignalPrincipal = Principal{UserID: "user-publish-signal", OrganizationID: "org-publish-signal"}

func TestConnectionMutationFinishPublishSignal(t *testing.T) {
	t.Parallel()
	for _, tc := range publishSignalCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			projectID := uuid.New()
			service := &MCPConnectionMutationService{publisher: tc.signaler()}

			output, err := service.finish(t.Context(), publishSignalPrincipal, projectID, projectID.String(), "", uuid.New(),
				connectionMutationReceipt{PublicationRequest: string(tc.publication)}, OperationReceipt{})
			require.NoError(t, err)
			require.Equal(t, tc.wantSignal, output.PublishSignal)
			require.Equal(t, string(tc.publication), output.PublicationRequest)
			tc.requireSignals(t, projectID, publishSignalPrincipal.UserID)
		})
	}
}

func TestPluginMetadataFinishPublishSignal(t *testing.T) {
	t.Parallel()
	for _, tc := range publishSignalCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			project := ResolvedProject{ID: uuid.New()}
			service := &PluginsService{publisher: tc.signaler()}
			payload, err := json.Marshal(PluginMetadataReceiptResult{PublicationRequest: string(tc.publication)})
			require.NoError(t, err)

			output, err := service.finishPluginMetadataMutation(t.Context(), publishSignalPrincipal, project, OperationReceipt{ResultPayload: payload})
			require.NoError(t, err)
			require.Equal(t, tc.wantSignal, output.PublishSignal)
			tc.requireSignals(t, project.ID, publishSignalPrincipal.UserID)
		})
	}
}

// A replayed plugin metadata write never signals: the original attempt already
// did, and the replay reports not_requested whatever the stored outcome was.
func TestPluginMetadataFinishDoesNotSignalOnReplay(t *testing.T) {
	t.Parallel()
	publisher := &recordingPluginPublishSignaler{}
	service := &PluginsService{publisher: publisher}
	payload, err := json.Marshal(PluginMetadataReceiptResult{PublicationRequest: string(plugindelivery.ProjectPublicationNotConfigured)})
	require.NoError(t, err)

	output, err := service.finishPluginMetadataMutation(t.Context(), publishSignalPrincipal, ResolvedProject{ID: uuid.New()}, OperationReceipt{ResultPayload: payload, Replayed: true})
	require.NoError(t, err)
	require.Equal(t, "not_requested", output.PublishSignal)
	require.Empty(t, publisher.recorded())
}

func TestToolExposureFinishPublishSignal(t *testing.T) {
	t.Parallel()
	for _, tc := range publishSignalCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			project := ResolvedProject{ID: uuid.New()}
			service := &MCPToolExposureService{publisher: tc.signaler(), queries: platformrepo.New(unreachableDBTX{})}

			output := service.finish(t.Context(), publishSignalPrincipal, project, uuid.New(),
				toolExposureReceipt{Outcome: "applied", Publication: string(tc.publication)}, OperationReceipt{})
			require.Equal(t, tc.wantSignal, output.PublishSignal)
			tc.requireSignals(t, project.ID, publishSignalPrincipal.UserID)
		})
	}
}

// Only an applied change can alter a package, so a no-op never signals, even
// with a publisher ready and no durable request recorded.
func TestToolExposureFinishDoesNotSignalANoOp(t *testing.T) {
	t.Parallel()
	publisher := &recordingPluginPublishSignaler{}
	service := &MCPToolExposureService{publisher: publisher, queries: platformrepo.New(unreachableDBTX{})}

	output := service.finish(t.Context(), publishSignalPrincipal, ResolvedProject{ID: uuid.New()}, uuid.New(),
		toolExposureReceipt{Outcome: "no_op", Publication: string(plugindelivery.ProjectPublicationNotConfigured)}, OperationReceipt{})
	require.Equal(t, "not_requested", output.PublishSignal)
	require.Empty(t, publisher.recorded())
}

func TestMCPFromFunctionsFinishPublishSignal(t *testing.T) {
	t.Parallel()
	for _, tc := range publishSignalCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			project := ResolvedProject{ID: uuid.New()}
			service := &MCPToolExposureService{publisher: tc.signaler()}

			output := service.finishMCPFromFunctions(t.Context(), publishSignalPrincipal, project,
				mcpFromFunctionsReceipt{AddedToDefaultPlugin: true, Publication: string(tc.publication)}, OperationReceipt{})
			require.Equal(t, tc.wantSignal, output.PublishSignal)
			// publication_requested is true exactly when a publish is on its
			// way: a durable request, or a signal that was delivered.
			require.Equal(t, tc.publication == plugindelivery.ProjectPublicationEnqueued || tc.wantSignal == "best_effort_requested", output.PublicationRequested)
			tc.requireSignals(t, project.ID, publishSignalPrincipal.UserID)
		})
	}
}

// A server that was not added to the default plugin is in no package yet, so
// creating it neither signals nor claims a publication.
func TestMCPFromFunctionsFinishDoesNotSignalOutsideTheDefaultPlugin(t *testing.T) {
	t.Parallel()
	publisher := &recordingPluginPublishSignaler{}
	service := &MCPToolExposureService{publisher: publisher}

	output := service.finishMCPFromFunctions(t.Context(), publishSignalPrincipal, ResolvedProject{ID: uuid.New()},
		mcpFromFunctionsReceipt{AddedToDefaultPlugin: false, Publication: ""}, OperationReceipt{})
	require.Equal(t, "not_requested", output.PublishSignal)
	require.False(t, output.PublicationRequested)
	require.Empty(t, publisher.recorded())
}
