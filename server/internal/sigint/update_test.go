package sigint_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	srv "github.com/speakeasy-api/gram/server/gen/http/sigint/server"
	gen "github.com/speakeasy-api/gram/server/gen/sigint"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

func TestUpdateSensorHTTPDistinguishesOmittedNullAndEmptyMembership(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	first := createSignal(t, ctx, ti, "calm")
	second := createSignal(t, ctx, ti, "angry")
	sensor := createSensor(t, ctx, ti, "severity", "ordered_score", first.ID, second.ID)
	handler := srv.NewUpdateSensorHandler(
		func(ctx context.Context, request any) (any, error) {
			payload, ok := request.(*gen.UpdateSensorPayload)
			require.True(t, ok)
			return ti.service.UpdateSensor(ctx, payload)
		}, nil, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil,
	)

	request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/rpc/sigint.updateSensor", strings.NewReader(fmt.Sprintf(`{"id":%q,"name":"renamed"}`, sensor.ID)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Gram-Session", "transport-test")
	request.Header.Set("Gram-Project", "transport-test")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	stored := getSensor(t, ctx, ti, sensor.ID)
	require.Equal(t, "renamed", stored.Name)
	require.Equal(t, []string{first.ID, second.ID}, stored.SignalIds)

	request = httptest.NewRequestWithContext(ctx, http.MethodPost, "/rpc/sigint.updateSensor", strings.NewReader(fmt.Sprintf(`{"id":%q,"signal_ids":null}`, sensor.ID)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Gram-Session", "transport-test")
	request.Header.Set("Gram-Project", "transport-test")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, []string{first.ID, second.ID}, getSensor(t, ctx, ti, sensor.ID).SignalIds)

	request = httptest.NewRequestWithContext(ctx, http.MethodPost, "/rpc/sigint.updateSensor", strings.NewReader(fmt.Sprintf(`{"id":%q,"signal_ids":[]}`, sensor.ID)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Gram-Session", "transport-test")
	request.Header.Set("Gram-Project", "transport-test")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var body map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.JSONEq(t, `[]`, string(body["signal_ids"]))
	require.Empty(t, getSensor(t, ctx, ti, sensor.ID).SignalIds)
}

func TestUpdateSensorRejectsForeignMembershipAtomically(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	own := createSignal(t, ctx, ti, "own")
	sensor := createSensor(t, ctx, ti, "unchanged", "multi_label", own.ID)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	project, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: "Other project", Slug: "other-project", OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	otherAuth := *authCtx
	otherAuth.ProjectID = &project.ID
	otherAuth.ProjectSlug = &project.Slug
	otherCtx := contextvalues.SetAuthContext(ctx, &otherAuth)
	foreign := createSignal(t, otherCtx, ti, "foreign")
	changedName := "must not persist"
	_, err = ti.service.UpdateSensor(ctx, &gen.UpdateSensorPayload{
		ID: sensor.ID, Name: &changedName, Description: nil, Instructions: nil, Mode: nil,
		SignalIds: []string{own.ID, foreign.ID}, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
	stored := getSensor(t, ctx, ti, sensor.ID)
	require.Equal(t, sensor.Name, stored.Name)
	require.Equal(t, []string{own.ID}, stored.SignalIds)
	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSigintSensorUpdate)
	require.NoError(t, err)
	require.Zero(t, count)
	_, err = ti.service.GetSignal(ctx, &gen.GetSignalPayload{
		ID: foreign.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = ti.service.GetSensor(otherCtx, &gen.GetSensorPayload{
		ID: sensor.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestUpdateSensorModeChangeValidatesFinalMembership(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ids := make([]string, 11)
	for i := range ids {
		ids[i] = createSignal(t, ctx, ti, fmt.Sprintf("level %d", i)).ID
	}
	sensor := createSensor(t, ctx, ti, "levels", "multi_label", ids...)
	mode := types.SigintSensorMode("ordered_score")
	_, err := ti.service.UpdateSensor(ctx, &gen.UpdateSensorPayload{
		ID: sensor.ID, Name: nil, Description: nil, Instructions: nil, Mode: &mode, SignalIds: nil,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.Equal(t, types.SigintSensorMode("multi_label"), getSensor(t, ctx, ti, sensor.ID).Mode)

	reversed := []string{ids[9], ids[8], ids[7], ids[6], ids[5], ids[4], ids[3], ids[2], ids[1], ids[0]}
	_, err = ti.service.UpdateSensor(ctx, &gen.UpdateSensorPayload{
		ID: sensor.ID, Name: nil, Description: nil, Instructions: nil, Mode: &mode, SignalIds: reversed,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	stored := getSensor(t, ctx, ti, sensor.ID)
	require.Equal(t, mode, stored.Mode)
	require.Equal(t, reversed, stored.SignalIds)
}

func TestUpdateSensorRejectsDuplicateUUIDsAtomically(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	signal := createSignal(t, ctx, ti, "api")
	sensor := createSensor(t, ctx, ti, "unchanged", "exclusive", signal.ID)
	name := "must not persist"
	_, err := ti.service.UpdateSensor(ctx, &gen.UpdateSensorPayload{
		ID: sensor.ID, Name: &name, Description: nil, Instructions: nil, Mode: nil,
		SignalIds:    []string{signal.ID, strings.ToUpper(signal.ID)},
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
	stored := getSensor(t, ctx, ti, sensor.ID)
	require.Equal(t, sensor.Name, stored.Name)
	require.Equal(t, []string{signal.ID}, stored.SignalIds)
}
