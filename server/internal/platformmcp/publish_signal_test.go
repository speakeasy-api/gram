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

// scriptedPublishSignaler records every post-commit publish signal and answers
// each with err, so a test can drive both the delivered and the failed case.
type scriptedPublishSignaler struct {
	err     error
	signals []pluginPublishSignal
}

func (s *scriptedPublishSignaler) SignalPluginPublish(_ context.Context, projectID uuid.UUID, createdByUserID string) error {
	s.signals = append(s.signals, pluginPublishSignal{projectID: projectID, userID: createdByUserID})
	return s.err
}

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
	publisher   *scriptedPublishSignaler
	nilSignaler bool
	wantSignal  string
	wantSignals int
}

// publishSignalCases is the shared contract: a durable outbox request needs no
// signal, a missing publisher is reported rather than hidden, and a failed
// signal is distinguished from a delivered one. Every non-enqueued outcome is
// signalled, not_configured and emission_disabled alike.
func publishSignalCases() []publishSignalCase {
	return []publishSignalCase{
		{name: "enqueued needs no signal", publication: plugindelivery.ProjectPublicationEnqueued, publisher: &scriptedPublishSignaler{}, wantSignal: "not_requested", wantSignals: 0},
		{name: "enqueued with no publisher is still not requested", publication: plugindelivery.ProjectPublicationEnqueued, nilSignaler: true, wantSignal: "not_requested", wantSignals: 0},
		{name: "no publisher is unavailable", publication: plugindelivery.ProjectPublicationEmissionDisabled, nilSignaler: true, wantSignal: "unavailable", wantSignals: 0},
		{name: "emission disabled is signalled", publication: plugindelivery.ProjectPublicationEmissionDisabled, publisher: &scriptedPublishSignaler{}, wantSignal: "best_effort_requested", wantSignals: 1},
		{name: "not configured is signalled", publication: plugindelivery.ProjectPublicationNotConfigured, publisher: &scriptedPublishSignaler{}, wantSignal: "best_effort_requested", wantSignals: 1},
		{name: "empty outcome is signalled", publication: "", publisher: &scriptedPublishSignaler{}, wantSignal: "best_effort_requested", wantSignals: 1},
		{name: "a failed signal is request_failed", publication: plugindelivery.ProjectPublicationNotConfigured, publisher: &scriptedPublishSignaler{err: errors.New("signal refused")}, wantSignal: "request_failed", wantSignals: 1},
	}
}

func (c publishSignalCase) signaler() plugindelivery.PluginPublishSignaler {
	if c.nilSignaler {
		return nil
	}
	return c.publisher
}

func (c publishSignalCase) requireSignals(t *testing.T, projectID uuid.UUID, userID string) {
	t.Helper()
	if c.publisher == nil {
		return
	}
	require.Len(t, c.publisher.signals, c.wantSignals)
	for _, signal := range c.publisher.signals {
		require.Equal(t, pluginPublishSignal{projectID: projectID, userID: userID}, signal, "the signal names the written project and the calling user")
	}
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
	publisher := &scriptedPublishSignaler{}
	service := &PluginsService{publisher: publisher}
	payload, err := json.Marshal(PluginMetadataReceiptResult{PublicationRequest: string(plugindelivery.ProjectPublicationNotConfigured)})
	require.NoError(t, err)

	output, err := service.finishPluginMetadataMutation(t.Context(), publishSignalPrincipal, ResolvedProject{ID: uuid.New()}, OperationReceipt{ResultPayload: payload, Replayed: true})
	require.NoError(t, err)
	require.Equal(t, "not_requested", output.PublishSignal)
	require.Empty(t, publisher.signals)
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
	publisher := &scriptedPublishSignaler{}
	service := &MCPToolExposureService{publisher: publisher, queries: platformrepo.New(unreachableDBTX{})}

	output := service.finish(t.Context(), publishSignalPrincipal, ResolvedProject{ID: uuid.New()}, uuid.New(),
		toolExposureReceipt{Outcome: "no_op", Publication: string(plugindelivery.ProjectPublicationNotConfigured)}, OperationReceipt{})
	require.Equal(t, "not_requested", output.PublishSignal)
	require.Empty(t, publisher.signals)
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
	publisher := &scriptedPublishSignaler{}
	service := &MCPToolExposureService{publisher: publisher}

	output := service.finishMCPFromFunctions(t.Context(), publishSignalPrincipal, ResolvedProject{ID: uuid.New()},
		mcpFromFunctionsReceipt{AddedToDefaultPlugin: false, Publication: ""}, OperationReceipt{})
	require.Equal(t, "not_requested", output.PublishSignal)
	require.False(t, output.PublicationRequested)
	require.Empty(t, publisher.signals)
}
