package mcpapproval_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	mcpeventsv1 "github.com/speakeasy-api/gram/infra/gen/gram/mcpevents/v1"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
	"github.com/speakeasy-api/gram/server/internal/mcpevents"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestReviewEventsTrackDemandAndFreezeDecisionRecipients(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	request, err := ti.service.CreateRequest(ctx, createPayload("server_url", "https://events.example.test/mcp", "private justification"))
	require.NoError(t, err)
	_, err = ti.service.CreateRequest(ctx, createPayload("server_url", "https://events.example.test/mcp", "repeat"))
	require.NoError(t, err)
	other := *ti.authContext
	other.UserID = "second-event-requester"
	otherCtx := contextvalues.SetAuthContext(ctx, &other)
	_, err = ti.service.CreateRequest(otherCtx, createPayload("server_url", "https://events.example.test/mcp", "second request"))
	require.NoError(t, err)
	decision, err := ti.service.RecordDecision(ctx, decisionPayload(request.ID, "denied"))
	require.NoError(t, err)
	rows, err := testrepo.New(ti.conn).ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	events := []*mcpeventsv1.ReviewChanged{}
	for _, row := range rows {
		if row.Topic != "gram.mcpevents.v1.ReviewChanged" {
			continue
		}
		event := new(mcpeventsv1.ReviewChanged)
		require.NoError(t, proto.Unmarshal(row.Message, event))
		events = append(events, event)
		require.Equal(t, ti.projectID.String(), event.GetProjectId())
		require.Equal(t, request.ID, event.GetRequestId())
		require.NotContains(t, string(row.Message), "private justification")
		require.NotContains(t, string(row.Message), "recorded in a test")
	}
	require.Len(t, events, 3)
	require.Equal(t, mcpevents.ReviewRequested, events[0].GetEventName())
	require.Equal(t, mcpevents.ReviewRequested, events[1].GetEventName())
	require.NotEqual(t, events[0].GetEventId(), events[1].GetEventId())
	require.Equal(t, mcpevents.ReviewDecided, events[2].GetEventName())
	require.Equal(t, decision.ID, events[2].GetDecisionId())
	require.ElementsMatch(t, []string{ti.authContext.UserID, other.UserID}, events[2].GetRecipientUserIds())
	_, err = ti.service.CreateRequest(ctx, createPayload("server_url", "https://events.example.test/mcp", "reopen"))
	require.NoError(t, err)
	count, err := testrepo.New(ti.conn).CountPublishOutboxRowsByTopic(ctx, testrepo.CountPublishOutboxRowsByTopicParams{OrganizationID: ti.organizationID, Topic: "gram.mcpevents.v1.ReviewChanged"})
	require.NoError(t, err)
	require.EqualValues(t, 4, count)
}

func TestReviewDecisionEventRollsBackWithDecision(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "https://rollback-event.example.test", status: "requested", evidence: `{}`, version: 1})
	tx := testenv.BeginTx(t, ctx, ti.conn)
	_, err := ti.service.DecideInTransaction(ctx, tx, mcpapproval.DecisionCommandInput{
		OrganizationID: ti.organizationID, ProjectID: ti.projectID, RequestID: requestID,
		Decision: "denied", Rationale: "private rationale", GrantedPrincipalURNs: []string{},
		ResearchReportID: uuid.NullUUID{}, ActorUserID: ti.authContext.UserID, ActorEmail: ti.authContext.Email,
	})
	require.NoError(t, err)
	count, err := testrepo.New(tx).CountPublishOutboxRowsByTopic(ctx, testrepo.CountPublishOutboxRowsByTopicParams{OrganizationID: ti.organizationID, Topic: "gram.mcpevents.v1.ReviewChanged"})
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	require.NoError(t, tx.Rollback(ctx))
	count, err = testrepo.New(ti.conn).CountPublishOutboxRowsByTopic(ctx, testrepo.CountPublishOutboxRowsByTopicParams{OrganizationID: ti.organizationID, Topic: "gram.mcpevents.v1.ReviewChanged"})
	require.NoError(t, err)
	require.Zero(t, count)
	require.Equal(t, "requested", requestStatus(t, ctx, ti, ti.projectID, requestID))
}
