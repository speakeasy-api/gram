package platformmcp

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	gensigint "github.com/speakeasy-api/gram/server/gen/sigint"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/sigint"
)

func TestSignalAuthoringBundleConfirmationReplayAndLiveRead(t *testing.T) {
	t.Parallel()
	ctx, f := newSignalAuthoringFixture(t)
	input := signalAuthoringInput{ProjectID: f.project.ID.String(), Proposal: sigint.AuthoringInput{Name: new("Support topics"), Mode: new("multi_label"), Instructions: new("Classify topics"), Signals: &[]sigint.SignalReference{{New: &sigint.SignalDefinition{Name: "Billing", Criteria: new("Billing questions")}}}}}

	preview, err := f.service.author(ctx, f.principal, "create_sensor", input)
	require.NoError(t, err)
	require.True(t, preview.Preview)
	require.True(t, preview.Sensor.Ready)
	require.Len(t, preview.Sensor.Signals, 1)

	state, err := f.service.management.ReadAuthoringState(ctx)
	require.NoError(t, err)
	require.Empty(t, state.Sensors)
	require.Empty(t, state.Signals)

	input.Confirmed, input.ExpectedVersion, input.PreviewToken, input.IdempotencyKey = true, preview.Version, preview.PreviewToken, "create-sensor-request"

	created, err := f.service.author(ctx, f.principal, "create_sensor", input)
	require.NoError(t, err)
	require.False(t, created.Preview)
	require.False(t, created.Replayed)
	require.True(t, created.TargetAvailable)
	require.NotEqual(t, preview.Sensor.ID, created.Sensor.ID)

	replayed, err := f.service.author(ctx, f.principal, "create_sensor", input)
	require.NoError(t, err)
	require.True(t, replayed.Replayed)
	require.Equal(t, created.Sensor.ID, replayed.Sensor.ID)
	require.Equal(t, created.ReceiptID, replayed.ReceiptID)

	count, err := audittest.AuditLogCountByAction(ctx, f.service.db, audit.ActionSigintSignalCreate)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)

	_, err = f.service.management.DeleteSensor(ctx, &gensigint.DeleteSensorPayload{ID: created.Sensor.ID})
	require.NoError(t, err)

	replayed, err = f.service.author(ctx, f.principal, "create_sensor", input)
	require.NoError(t, err)
	require.True(t, replayed.Replayed)
	require.False(t, replayed.TargetAvailable)
	require.Nil(t, replayed.Sensor)
}

func TestSignalAuthoringRejectsChangedProposalAndStalePreview(t *testing.T) {
	t.Parallel()
	ctx, f := newSignalAuthoringFixture(t)
	input := signalAuthoringInput{ProjectID: f.project.ID.String(), Proposal: sigint.AuthoringInput{Name: new("Reusable signal")}}

	preview, err := f.service.author(ctx, f.principal, "create_signal", input)
	require.NoError(t, err)

	input.Confirmed, input.ExpectedVersion, input.PreviewToken, input.IdempotencyKey = true, preview.Version, preview.PreviewToken, "signal-request"
	changed := input
	changed.Proposal.Name = new("Different proposal")

	_, err = f.service.author(ctx, f.principal, "create_signal", changed)
	require.Error(t, err)

	_, err = f.service.management.CreateSignal(ctx, &gensigint.CreateSignalPayload{Name: "Concurrent signal"})
	require.NoError(t, err)

	_, err = f.service.author(ctx, f.principal, "create_signal", input)
	require.Error(t, err)
}

func TestSignalAuthoringRechecksPermissionAndFeatureBeforeReplay(t *testing.T) {
	t.Parallel()
	ctx, f := newSignalAuthoringFixture(t)
	input := signalAuthoringInput{ProjectID: f.project.ID.String(), Proposal: sigint.AuthoringInput{Name: new("Signal")}}

	preview, err := f.service.author(ctx, f.principal, "create_signal", input)
	require.NoError(t, err)

	input.Confirmed, input.ExpectedVersion, input.PreviewToken, input.IdempotencyKey = true, preview.Version, preview.PreviewToken, "signal-request"

	_, err = f.service.author(ctx, f.principal, "create_signal", input)
	require.NoError(t, err)

	readOnly := authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, f.project.ID.String())})

	_, err = f.service.author(readOnly, f.principal, "create_signal", input)
	require.Error(t, err)
	require.NoError(t, f.features.SetFeatureEnabled(ctx, f.principal.OrganizationID, productfeatures.FeatureSignalsIntelligence, false))

	_, err = f.service.author(ctx, f.principal, "create_signal", input)
	require.Error(t, err)
}

func TestSignalAuthoringUpdateSharedSignalImpact(t *testing.T) {
	t.Parallel()
	ctx, f := newSignalAuthoringFixture(t)

	signal, err := f.service.management.CreateSignal(ctx, &gensigint.CreateSignalPayload{Name: "Shared signal"})
	require.NoError(t, err)

	sensor, err := f.service.management.CreateSensor(ctx, &gensigint.CreateSensorPayload{Name: "Consumer", Mode: "multi_label", SignalIds: []string{signal.ID}})
	require.NoError(t, err)

	input := signalAuthoringInput{ProjectID: f.project.ID.String(), Proposal: sigint.AuthoringInput{ID: signal.ID, Criteria: new("Updated criteria")}}

	preview, err := f.service.author(ctx, f.principal, "update_signal", input)
	require.NoError(t, err)
	require.Len(t, preview.AffectedSensors, 1)
	require.Equal(t, sensor.ID, preview.AffectedSensors[0].ID)

	input.Confirmed, input.ExpectedVersion, input.PreviewToken, input.IdempotencyKey = true, preview.Version, preview.PreviewToken, "update-signal-request"

	updated, err := f.service.author(ctx, f.principal, "update_signal", input)
	require.NoError(t, err)
	require.Equal(t, "Updated criteria", *updated.Signal.Criteria)

	live, err := f.service.management.ReadAuthoringState(ctx)
	require.NoError(t, err)
	require.Equal(t, "Updated criteria", *live.Signals[0].ClassifierCriteria)
}

func TestSignalToolContractsAndUnavailableParity(t *testing.T) {
	t.Parallel()
	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "live", Version: "1"}, nil))
	unavailable := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "unavailable", Version: "1"}, nil))
	registerSignalTools(live, &SignalAuthoringService{})
	registerSignalTools(unavailable, nil)
	require.Len(t, live.For(AudienceExternal), 8)
	require.Empty(t, live.For(AudienceAssistant))
	for _, descriptor := range live.For(AudienceExternal) {
		other := descriptorByName(t, unavailable, descriptor.Name)
		require.Equal(t, descriptor.Meta, other.Meta)
		require.JSONEq(t, string(descriptor.InputSchema), string(other.InputSchema))
		require.Equal(t, ProjectScopeExplicit, descriptor.Meta.ProjectScope)
		require.Equal(t, ExternalAuthorizationMember, descriptor.Meta.Authorization)
	}
}

func TestSignalToolMatchingPreviewAndExactProject(t *testing.T) {
	t.Parallel()
	ctx, f := newSignalAuthoringFixture(t)
	reg := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "signals", Version: "1"}, nil))
	registerSignalTools(reg, f.service)
	descriptor := descriptorByName(t, reg, "preview_sensor_match")

	data, err := json.Marshal(previewSensorMatchInput{ProjectID: f.project.ID.String(), Expression: `message.role != "assistant"`, Examples: []sensorMatchExample{{Role: new("user")}, {Role: new("assistant")}, {}}})
	require.NoError(t, err)

	result, err := descriptor.Invoke(ctx, data)
	require.NoError(t, err)

	encoded, err := json.Marshal(result)
	require.NoError(t, err)

	var output previewSensorMatchOutput
	require.NoError(t, json.Unmarshal(encoded, &output))
	require.Equal(t, []string{"matched", "not_matched", "evaluation_error"}, output.Results)

	_, _, _, err = f.service.scope(ctx, f.principal, uuid.NewString(), false)
	require.Error(t, err)

	assistant := f.principal
	assistant.Surface = SurfaceProjectAssistant

	_, _, _, err = f.service.scope(ctx, assistant, f.project.ID.String(), false)
	require.Error(t, err)
}

func TestSignalToolDiscoveryAndSensorUpdate(t *testing.T) {
	t.Parallel()
	ctx, f := newSignalAuthoringFixture(t)

	first, err := f.service.management.CreateSignal(ctx, &gensigint.CreateSignalPayload{Name: "First topic"})
	require.NoError(t, err)

	second, err := f.service.management.CreateSignal(ctx, &gensigint.CreateSignalPayload{Name: "Second topic"})
	require.NoError(t, err)

	sensor, err := f.service.management.CreateSensor(ctx, &gensigint.CreateSensorPayload{Name: "Topics", Mode: "multi_label", SignalIds: []string{first.ID}, Instructions: new("Classify the message")})
	require.NoError(t, err)

	reg := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "signals", Version: "1"}, nil))
	registerSignalTools(reg, f.service)
	search := descriptorByName(t, reg, "find_signals")

	data, err := json.Marshal(findSignalsInput{ProjectID: f.project.ID.String(), Query: "topic", Limit: 1})
	require.NoError(t, err)

	value, err := search.Invoke(ctx, data)
	require.NoError(t, err)

	page, ok := value.(findSignalsOutput)
	require.True(t, ok)
	require.Len(t, page.Signals, 1)
	require.Equal(t, first.ID, page.Signals[0].ID)
	require.Equal(t, 1, page.Signals[0].SensorCount)
	require.NotEmpty(t, page.NextCursor)

	data, err = json.Marshal(findSignalsInput{ProjectID: f.project.ID.String(), Query: "topic", Limit: 1, Cursor: page.NextCursor})
	require.NoError(t, err)

	value, err = search.Invoke(ctx, data)
	require.NoError(t, err)

	page, ok = value.(findSignalsOutput)
	require.True(t, ok)
	require.Equal(t, second.ID, page.Signals[0].ID)
	require.Empty(t, page.NextCursor)
	input := signalAuthoringInput{ProjectID: f.project.ID.String(), Proposal: sigint.AuthoringInput{Operation: "update_sensor", ID: sensor.ID, MatchExpression: new(`message.role == "assistant"`), Signals: &[]sigint.SignalReference{{ID: second.ID}, {ID: first.ID}}}}
	mutation := descriptorByName(t, reg, "update_sensor")

	data, err = json.Marshal(input)
	require.NoError(t, err)

	value, err = mutation.Invoke(ctx, data)
	require.NoError(t, err)

	preview, ok := value.(signalAuthoringOutput)
	require.True(t, ok)
	input.Confirmed, input.ExpectedVersion, input.PreviewToken, input.IdempotencyKey = true, preview.Version, preview.PreviewToken, "update-sensor-request"

	data, err = json.Marshal(input)
	require.NoError(t, err)

	_, err = mutation.Invoke(ctx, data)
	require.NoError(t, err)

	data, err = json.Marshal(getSensorInput{ProjectID: f.project.ID.String(), SensorID: sensor.ID})
	require.NoError(t, err)

	value, err = descriptorByName(t, reg, "get_sensor").Invoke(ctx, data)
	require.NoError(t, err)

	live, ok := value.(signalAuthoringOutput)
	require.True(t, ok)
	require.Equal(t, `message.role == "assistant"`, live.Sensor.MatchExpression)
	require.Equal(t, second.ID, live.Sensor.Signals[0].ID)
	require.Equal(t, first.ID, live.Sensor.Signals[1].ID)
	require.True(t, live.Sensor.Ready)

	data, err = json.Marshal(findSignalsInput{ProjectID: f.project.ID.String(), Query: "Topics"})
	require.NoError(t, err)

	value, err = descriptorByName(t, reg, "find_sensors").Invoke(ctx, data)
	require.NoError(t, err)

	summaries, ok := value.(findSensorsOutput)
	require.True(t, ok)
	require.Len(t, summaries.Sensors, 1)
	require.Equal(t, 2, summaries.Sensors[0].SignalCount)
}
