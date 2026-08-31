package metering_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/metering/chrepo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type captureReadingInserter struct {
	rows []chrepo.ReadingRow
	err  error
}

func (c *captureReadingInserter) InsertReadings(_ context.Context, rows []chrepo.ReadingRow) error {
	c.rows = append(c.rows, rows...)
	return c.err
}

type failingMeteringDB struct {
	err error
}

func (f failingMeteringDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag(""), f.err
}

func (f failingMeteringDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, f.err
}

func (f failingMeteringDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return nil
}

func usageMessage(t *testing.T, input metering.UsageInput) (*meteringv1.MeterReading, metering.Reading) {
	t.Helper()
	reading, err := metering.NewUsage(input)
	require.NoError(t, err)
	message := commonMessage(reading.ID(), input.Scope, input.OperationID, input.Value, input.OccurredAt, input.ProducedAt, input.Source, input.Attributes)
	message.SetKind(meteringv1.MeterReading_KIND_USAGE)
	return message, reading
}

func adjustmentMessage(t *testing.T, input metering.AdjustmentInput) (*meteringv1.MeterReading, metering.Reading) {
	t.Helper()
	reading, err := metering.NewAdjustment(input)
	require.NoError(t, err)
	message := commonMessage(reading.ID(), input.Scope, input.OperationID, input.Value, input.OccurredAt, input.ProducedAt, input.Source, input.Attributes)
	message.SetKind(meteringv1.MeterReading_KIND_ADJUSTMENT)
	message.SetCorrectsReadingId(input.CorrectsReadingID.String())
	message.SetAdjustmentReason(input.Reason)
	return message, reading
}

func commonMessage(
	id uuid.UUID,
	scope metering.Scope,
	operationID string,
	value int64,
	occurredAt time.Time,
	producedAt time.Time,
	source string,
	attributes map[string]string,
) *meteringv1.MeterReading {
	message := &meteringv1.MeterReading{}
	message.SetId(id.String())
	message.SetOrganizationId(scope.OrganizationID())
	if projectID, ok := scope.ProjectID(); ok {
		message.SetProjectId(projectID.String())
	}
	message.SetMeterId(string(metering.MeterAgentSessionStorage))
	message.SetOperationId(operationID)
	message.SetUnit(string(metering.UnitSTokens))
	message.SetValue(value)
	message.SetOccurredAt(occurredAt.Format(time.RFC3339Nano))
	message.SetAttributes(attributes)
	message.SetMeterVersion(1)
	message.SetProducedAt(producedAt.Format(time.RFC3339Nano))
	message.SetMeasurementMethod(string(metering.MeasurementTiktokenO200kBase))
	message.SetSource(source)
	return message
}

func TestMeterReadingCHWriterSkipsPoisonAndDeduplicatesBatch(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	input := metering.UsageInput{
		Meter:       metering.AgentSessionStorage(),
		Scope:       metering.ProjectScope("org-"+uuid.NewString(), uuid.New()),
		OperationID: "chat_message:" + uuid.NewString(),
		Value:       7,
		OccurredAt:  now,
		ProducedAt:  now,
		Source:      "test",
		Attributes:  nil,
	}
	valid, reading := usageMessage(t, input)
	updatedInput := input
	updatedInput.ProducedAt = now.Add(time.Second)
	updatedInput.Attributes = map[string]string{"revision": "native-promotion"}
	updated, _ := usageMessage(t, updatedInput)
	refreshedInput := updatedInput
	refreshedInput.Attributes = map[string]string{"revision": "refreshed"}
	refreshed, _ := usageMessage(t, refreshedInput)
	cloned := proto.Clone(valid)
	invalid, ok := cloned.(*meteringv1.MeterReading)
	require.True(t, ok)
	invalid.SetId("not-a-uuid")
	capture := &captureReadingInserter{rows: nil, err: nil}
	writer := metering.NewMeterReadingCHWriter(testenv.NewLogger(t), nil, capture)

	require.NoError(t, writer.HandleBatch(t.Context(), []*meteringv1.MeterReading{nil, invalid, valid, updated, valid, refreshed}, nil))
	require.Len(t, capture.rows, 1)
	require.Equal(t, reading.ID(), capture.rows[0].ID)
	require.Equal(t, input.Value, capture.rows[0].Value)
	require.Equal(t, string(metering.MeasurementTiktokenO200kBase), capture.rows[0].Attributes["codec"])
	require.Equal(t, "refreshed", capture.rows[0].Attributes["revision"])
	require.Equal(t, updatedInput.ProducedAt, capture.rows[0].ProducedAt)
}

func TestMeterReadingCHWriterPropagatesInsertFailure(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	message, _ := usageMessage(t, metering.UsageInput{
		Meter:       metering.AgentSessionStorage(),
		Scope:       metering.ProjectScope("org-"+uuid.NewString(), uuid.New()),
		OperationID: "chat_message:" + uuid.NewString(),
		Value:       4,
		OccurredAt:  now,
		ProducedAt:  now,
		Source:      "test",
		Attributes:  nil,
	})
	capture := &captureReadingInserter{rows: nil, err: errors.New("clickhouse unavailable")}
	writer := metering.NewMeterReadingCHWriter(testenv.NewLogger(t), nil, capture)
	require.Error(t, writer.HandleBatch(t.Context(), []*meteringv1.MeterReading{message}, nil))
}

func TestMeterReadingCHWriterRedeliveryConvergesAndPreservesAdjustment(t *testing.T) {
	t.Parallel()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	definition := metering.AgentSessionStorage()
	scope := metering.ProjectScope("org-"+uuid.NewString(), uuid.New())
	now := time.Now().UTC()
	staleInput := metering.UsageInput{
		Meter:       definition,
		Scope:       scope,
		OperationID: "chat_message:" + uuid.NewString(),
		Value:       11,
		OccurredAt:  now,
		ProducedAt:  now,
		Source:      "test",
		Attributes:  map[string]string{"attribution": "stale"},
	}
	staleEvent, usage := usageMessage(t, staleInput)
	newerInput := staleInput
	newerInput.ProducedAt = now.Add(time.Second)
	newerInput.Attributes = map[string]string{"attribution": "newer"}
	newerEvent, newerUsage := usageMessage(t, newerInput)
	require.Equal(t, usage.ID(), newerUsage.ID())
	equalVersionInput := newerInput
	equalVersionInput.Attributes = map[string]string{"attribution": "refreshed"}
	equalVersionEvent, equalVersionUsage := usageMessage(t, equalVersionInput)
	require.Equal(t, usage.ID(), equalVersionUsage.ID())
	adjustmentEvent, _ := adjustmentMessage(t, metering.AdjustmentInput{
		Meter:             definition,
		Scope:             scope,
		OperationID:       staleInput.OperationID + ":adjustment",
		Value:             -5,
		OccurredAt:        now,
		ProducedAt:        now.Add(2 * time.Second),
		CorrectsReadingID: usage.ID(),
		Reason:            "source_reconciliation",
		Source:            "test",
		Attributes:        nil,
	})
	writer := metering.NewMeterReadingCHWriter(testenv.NewLogger(t), nil, chrepo.New(conn))
	require.NoError(t, writer.HandleBatch(t.Context(), []*meteringv1.MeterReading{newerEvent}, nil))
	require.NoError(t, writer.HandleBatch(t.Context(), []*meteringv1.MeterReading{staleEvent}, nil))

	projectID, ok := scope.ProjectID()
	require.True(t, ok)
	var storedProducedAt time.Time
	var attributes map[string]string
	require.NoError(t, conn.QueryRow(t.Context(), `
		SELECT produced_at, attributes
		FROM billing_meter_readings FINAL
		WHERE organization_id = ? AND project_id = ? AND meter_id = ? AND id = ?
	`, scope.OrganizationID(), projectID, string(metering.MeterAgentSessionStorage), usage.ID()).Scan(&storedProducedAt, &attributes))
	require.Equal(t, newerInput.ProducedAt, storedProducedAt)
	require.Equal(t, "newer", attributes["attribution"])

	require.NoError(t, writer.HandleBatch(t.Context(), []*meteringv1.MeterReading{equalVersionEvent}, nil))
	require.NoError(t, writer.HandleBatch(t.Context(), []*meteringv1.MeterReading{adjustmentEvent}, nil))
	require.NoError(t, conn.QueryRow(t.Context(), `
		SELECT produced_at, attributes
		FROM billing_meter_readings FINAL
		WHERE organization_id = ? AND project_id = ? AND meter_id = ? AND id = ?
	`, scope.OrganizationID(), projectID, string(metering.MeterAgentSessionStorage), usage.ID()).Scan(&storedProducedAt, &attributes))
	require.Equal(t, equalVersionInput.ProducedAt, storedProducedAt)
	require.Equal(t, "refreshed", attributes["attribution"])

	var count uint64
	var net int64
	require.NoError(t, conn.QueryRow(t.Context(), `
		SELECT count(), sum(value)
		FROM billing_meter_readings FINAL
		WHERE organization_id = ? AND project_id = ? AND meter_id = ?
	`, scope.OrganizationID(), projectID, string(metering.MeterAgentSessionStorage)).Scan(&count, &net))
	require.Equal(t, uint64(2), count)
	require.Equal(t, int64(6), net)
}

func TestMeterReadingCHWriterEnrichesMessageUserAndChatOwnerIndependently(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, organizationID := newMeteringPostgres(t)
	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Metering Enrichment Project",
		Slug:           "metering-enrichment-" + uuid.NewString()[:8],
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	messageUserID := "message-user-" + uuid.NewString()
	ownerUserID := "owner-user-" + uuid.NewString()
	seedMeteringAccount(t, conn, organizationID, messageUserID, "message-account@example.test")
	seedMeteringDirectoryUser(t, conn, organizationID, messageUserID, "message-account@example.test", "Stale Message Division", "Stale Message Department", true)
	seedMeteringDirectoryUser(t, conn, organizationID, messageUserID, "message-account@example.test", "Message Division", "Message Department", true)
	seedMeteringUser(t, conn, organizationID, ownerUserID, "owner-account@example.test", "Owner Division", "Owner Department", false)
	emailCollisionUserID := "email-collision-user-" + uuid.NewString()
	seedMeteringAccount(t, conn, organizationID, emailCollisionUserID, "email-collision-account@example.test")
	seedMeteringDirectoryUser(t, conn, organizationID, emailCollisionUserID, "owner-account@example.test", "Wrong Owner Division", "Wrong Owner Department", true)
	seedMeteringRole(t, conn, organizationID, messageUserID, "zeta-message", false)
	seedMeteringRole(t, conn, organizationID, messageUserID, "alpha-message", true)
	seedMeteringRole(t, conn, organizationID, ownerUserID, "member-owner", false)
	seedMeteringRole(t, conn, organizationID, ownerUserID, "admin-owner", true)

	chatID := uuid.New()
	_, err = chatrepo.New(conn).UpsertChat(ctx, chatrepo.UpsertChatParams{
		ID:             chatID,
		ProjectID:      project.ID,
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(ownerUserID),
		ExternalUserID: conv.ToPGText("owner-provider-id"),
		Title:          conv.ToPGText("Independent identities"),
	})
	require.NoError(t, err)

	now := time.Now().UTC()
	attributes := map[string]string{
		metering.AttributeChatID:                    chatID.String(),
		metering.AttributeModel:                     "gpt-5",
		metering.AttributeHookSource:                "codex",
		metering.AttributeMessageUserID:             messageUserID,
		metering.AttributeMessageExternalUserID:     "opaque-message-provider-id",
		metering.AttributeMessageUserEmail:          "reported-message@example.test",
		metering.AttributeMessageUserAccountEmail:   "stale-message@example.test",
		metering.AttributeChatOwnerUserEmail:        "stale-owner@example.test",
		metering.AttributeChatOwnerExternalUserID:   "stale-owner-provider-id",
		metering.AttributeMessageUserDepartmentName: "Stale Message Department",
		metering.AttributeChatOwnerDepartmentName:   "Stale Owner Department",
		metering.AttributeMessageUserDirectoryMatch: "stale",
		metering.AttributeChatOwnerDirectoryMatch:   "stale",
		metering.AttributeMessageUserRBACRoles:      `["stale"]`,
		metering.AttributeChatOwnerRBACRoles:        `["stale"]`,
		metering.AttributeMessageUserDivisionName:   "Stale Message Division",
		metering.AttributeChatOwnerDivisionName:     "Stale Owner Division",
		metering.AttributeChatOwnerUserID:           "stale-owner-user",
	}
	message, _ := usageMessage(t, metering.UsageInput{
		Meter:       metering.AgentSessionStorage(),
		Scope:       metering.ProjectScope(organizationID, project.ID),
		OperationID: "chat_message:" + uuid.NewString(),
		Value:       9,
		OccurredAt:  now,
		ProducedAt:  now,
		Source:      "chat_message_writer",
		Attributes:  attributes,
	})
	capture := &captureReadingInserter{rows: nil, err: nil}
	writer := metering.NewMeterReadingCHWriter(testenv.NewLogger(t), conn, capture)

	require.NoError(t, writer.HandleBatch(ctx, []*meteringv1.MeterReading{message}, nil))
	require.Equal(t, "stale-owner@example.test", message.GetAttributes()[metering.AttributeChatOwnerUserEmail])
	require.NotContains(t, message.GetAttributes(), "codec")
	require.Len(t, capture.rows, 1)
	require.Equal(t, map[string]string{
		"codec":                                     string(metering.MeasurementTiktokenO200kBase),
		metering.AttributeChatID:                    chatID.String(),
		metering.AttributeModel:                     "gpt-5",
		metering.AttributeHookSource:                "codex",
		metering.AttributeMessageUserID:             messageUserID,
		metering.AttributeMessageExternalUserID:     "opaque-message-provider-id",
		metering.AttributeMessageUserEmail:          "reported-message@example.test",
		metering.AttributeMessageUserAccountEmail:   "message-account@example.test",
		metering.AttributeMessageUserDivisionName:   "Message Division",
		metering.AttributeMessageUserDepartmentName: "Message Department",
		metering.AttributeMessageUserDirectoryMatch: "user_id",
		metering.AttributeMessageUserRBACRoles:      `["alpha-message","zeta-message"]`,
		metering.AttributeChatOwnerUserID:           ownerUserID,
		metering.AttributeChatOwnerExternalUserID:   "owner-provider-id",
		metering.AttributeChatOwnerUserEmail:        "owner-account@example.test",
		metering.AttributeChatOwnerDivisionName:     "Owner Division",
		metering.AttributeChatOwnerDepartmentName:   "Owner Department",
		metering.AttributeChatOwnerDirectoryMatch:   "email",
		metering.AttributeChatOwnerRBACRoles:        `["admin-owner","member-owner"]`,
	}, capture.rows[0].Attributes)
}

func TestMeterReadingCHWriterPreservesIdentityBoundaries(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, organizationID := newMeteringPostgres(t)
	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Metering Boundaries Project",
		Slug:           "metering-boundaries-" + uuid.NewString()[:8],
		OrganizationID: organizationID,
	})
	require.NoError(t, err)
	otherProject, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Other Metering Project",
		Slug:           "other-metering-" + uuid.NewString()[:8],
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	ownerOnlyUserID := "owner-only-" + uuid.NewString()
	seedMeteringUser(t, conn, organizationID, ownerOnlyUserID, "owner-only@example.test", "Owner Only Division", "Owner Only Department", true)
	seedMeteringRole(t, conn, organizationID, ownerOnlyUserID, "owner-only-role", false)
	sameUserID := "same-user-" + uuid.NewString()
	seedMeteringUser(t, conn, organizationID, sameUserID, "same-user@example.test", "Same Division", "Same Department", true)
	seedMeteringRole(t, conn, organizationID, sameUserID, "same-role", false)
	plainUserID := "plain-user-" + uuid.NewString()
	seedMeteringAccount(t, conn, organizationID, plainUserID, "plain-user@example.test")
	upsertMeteringDirectoryUser(t, conn, organizationID, plainUserID, "plain-user@example.test", map[string]any{
		"division_name":    42,
		"department_name":  true,
		"job_title":        map[string]string{"invalid": "object"},
		"employee_type":    []string{"invalid", "array"},
		"cost_center_name": nil,
	}, true)
	ambiguousUserID := "ambiguous-user-" + uuid.NewString()
	ambiguousEmail := "ambiguous-user@example.test"
	seedMeteringAccount(t, conn, organizationID, ambiguousUserID, ambiguousEmail)
	seedMeteringDirectoryUser(t, conn, organizationID, ambiguousUserID, ambiguousEmail, "First Ambiguous Division", "First Ambiguous Department", false)
	seedMeteringDirectoryUser(t, conn, organizationID, ambiguousUserID, ambiguousEmail, "Second Ambiguous Division", "Second Ambiguous Department", false)

	createChat := func(chatID uuid.UUID, projectID uuid.UUID, ownerUserID string) {
		t.Helper()
		_, createErr := chatrepo.New(conn).UpsertChat(ctx, chatrepo.UpsertChatParams{
			ID:             chatID,
			ProjectID:      projectID,
			OrganizationID: organizationID,
			UserID:         conv.ToPGTextEmpty(ownerUserID),
			ExternalUserID: conv.ToPGTextEmpty(""),
			Title:          conv.ToPGText("Boundary chat"),
		})
		require.NoError(t, createErr)
	}
	ownerOnlyChatID := uuid.New()
	sameUserChatID := uuid.New()
	anonymousChatID := uuid.New()
	plainUserChatID := uuid.New()
	ambiguousChatID := uuid.New()
	foreignChatID := uuid.New()
	deletedChatID := uuid.New()
	createChat(ownerOnlyChatID, project.ID, ownerOnlyUserID)
	createChat(sameUserChatID, project.ID, sameUserID)
	createChat(anonymousChatID, project.ID, "")
	createChat(plainUserChatID, project.ID, plainUserID)
	createChat(ambiguousChatID, project.ID, ambiguousUserID)
	createChat(foreignChatID, otherProject.ID, ownerOnlyUserID)
	createChat(deletedChatID, project.ID, ownerOnlyUserID)
	deleted, err := chatrepo.New(conn).SoftDeleteChat(ctx, chatrepo.SoftDeleteChatParams{ProjectID: project.ID, ID: deletedChatID})
	require.NoError(t, err)
	require.True(t, deleted.Deleted)

	now := time.Now().UTC()
	newMessage := func(operationID string, attributes map[string]string) *meteringv1.MeterReading {
		t.Helper()
		message, _ := usageMessage(t, metering.UsageInput{
			Meter:       metering.AgentSessionStorage(),
			Scope:       metering.ProjectScope(organizationID, project.ID),
			OperationID: operationID,
			Value:       1,
			OccurredAt:  now,
			ProducedAt:  now,
			Source:      "chat_message_writer",
			Attributes:  attributes,
		})
		return message
	}
	messages := []*meteringv1.MeterReading{
		newMessage("owner-only", map[string]string{
			metering.AttributeChatID:     ownerOnlyChatID.String(),
			metering.AttributeModel:      "owner-only-model",
			metering.AttributeHookSource: "codex",
		}),
		newMessage("same-user", map[string]string{
			metering.AttributeChatID:        sameUserChatID.String(),
			metering.AttributeMessageUserID: sameUserID,
		}),
		newMessage("explicit-email", map[string]string{
			metering.AttributeChatID:                anonymousChatID.String(),
			metering.AttributeModel:                 "anonymous-model",
			metering.AttributeHookSource:            "chatgpt",
			metering.AttributeMessageExternalUserID: "opaque-provider-id",
			metering.AttributeMessageUserEmail:      "observed-only@example.test",
		}),
		newMessage("anonymous", map[string]string{
			metering.AttributeChatID:     anonymousChatID.String(),
			metering.AttributeModel:      "anonymous-model",
			metering.AttributeHookSource: "codex",
		}),
		newMessage("no-directory-or-roles", map[string]string{
			metering.AttributeChatID:        plainUserChatID.String(),
			metering.AttributeMessageUserID: plainUserID,
		}),
		newMessage("ambiguous-email", map[string]string{
			metering.AttributeChatID:        ambiguousChatID.String(),
			metering.AttributeMessageUserID: ambiguousUserID,
		}),
		newMessage("tenant-mismatch", map[string]string{
			metering.AttributeChatID:                  foreignChatID.String(),
			metering.AttributeModel:                   "foreign-model",
			metering.AttributeMessageUserAccountEmail: "stale@example.test",
			metering.AttributeChatOwnerUserID:         "stale-owner",
		}),
		newMessage("deleted-chat", map[string]string{
			metering.AttributeChatID:               deletedChatID.String(),
			metering.AttributeHookSource:           "deleted-source",
			metering.AttributeChatOwnerUserEmail:   "stale-owner@example.test",
			metering.AttributeMessageUserRBACRoles: `["stale"]`,
		}),
		newMessage("missing-chat-id", map[string]string{
			metering.AttributeModel:                    "missing-chat-model",
			metering.AttributeMessageUserAccountEmail:  "stale@example.test",
			metering.AttributeMessageUserJobTitle:      "Stale Job",
			metering.AttributeChatOwnerUserID:          "stale-owner",
			metering.AttributeChatOwnerDirectoryGroups: `["stale"]`,
		}),
	}
	capture := &captureReadingInserter{rows: nil, err: nil}
	writer := metering.NewMeterReadingCHWriter(testenv.NewLogger(t), conn, capture)

	require.NoError(t, writer.HandleBatch(ctx, messages, nil))
	require.Len(t, capture.rows, len(messages))
	rows := make(map[string]chrepo.ReadingRow, len(capture.rows))
	for _, row := range capture.rows {
		rows[row.OperationID] = row
	}

	ownerOnly := rows["owner-only"].Attributes
	require.Equal(t, ownerOnlyUserID, ownerOnly[metering.AttributeChatOwnerUserID])
	require.Equal(t, "owner-only@example.test", ownerOnly[metering.AttributeChatOwnerUserEmail])
	require.Equal(t, "Owner Only Division", ownerOnly[metering.AttributeChatOwnerDivisionName])
	require.Equal(t, `["owner-only-role"]`, ownerOnly[metering.AttributeChatOwnerRBACRoles])
	require.NotContains(t, ownerOnly, metering.AttributeMessageUserID)
	require.NotContains(t, ownerOnly, metering.AttributeMessageUserAccountEmail)
	require.NotContains(t, ownerOnly, metering.AttributeMessageUserDirectoryMatch)
	require.NotContains(t, ownerOnly, metering.AttributeMessageUserRBACRoles)

	sameUser := rows["same-user"].Attributes
	require.Equal(t, sameUserID, sameUser[metering.AttributeMessageUserID])
	require.Equal(t, sameUserID, sameUser[metering.AttributeChatOwnerUserID])
	require.Equal(t, "same-user@example.test", sameUser[metering.AttributeMessageUserAccountEmail])
	require.Equal(t, "same-user@example.test", sameUser[metering.AttributeChatOwnerUserEmail])
	require.Equal(t, "Same Division", sameUser[metering.AttributeMessageUserDivisionName])
	require.Equal(t, "Same Division", sameUser[metering.AttributeChatOwnerDivisionName])
	require.Equal(t, `["same-role"]`, sameUser[metering.AttributeMessageUserRBACRoles])
	require.Equal(t, `["same-role"]`, sameUser[metering.AttributeChatOwnerRBACRoles])

	require.Equal(t, map[string]string{
		"codec":                                 string(metering.MeasurementTiktokenO200kBase),
		metering.AttributeChatID:                anonymousChatID.String(),
		metering.AttributeModel:                 "anonymous-model",
		metering.AttributeHookSource:            "chatgpt",
		metering.AttributeMessageExternalUserID: "opaque-provider-id",
		metering.AttributeMessageUserEmail:      "observed-only@example.test",
	}, rows["explicit-email"].Attributes)
	require.Equal(t, map[string]string{
		"codec":                      string(metering.MeasurementTiktokenO200kBase),
		metering.AttributeChatID:     anonymousChatID.String(),
		metering.AttributeModel:      "anonymous-model",
		metering.AttributeHookSource: "codex",
	}, rows["anonymous"].Attributes)

	plain := rows["no-directory-or-roles"].Attributes
	require.Equal(t, plainUserID, plain[metering.AttributeMessageUserID])
	require.Equal(t, plainUserID, plain[metering.AttributeChatOwnerUserID])
	require.Equal(t, "plain-user@example.test", plain[metering.AttributeMessageUserAccountEmail])
	require.Equal(t, "plain-user@example.test", plain[metering.AttributeChatOwnerUserEmail])
	require.Equal(t, "user_id", plain[metering.AttributeMessageUserDirectoryMatch])
	require.Equal(t, "user_id", plain[metering.AttributeChatOwnerDirectoryMatch])
	for _, key := range []string{
		metering.AttributeMessageUserDivisionName,
		metering.AttributeMessageUserDepartmentName,
		metering.AttributeMessageUserJobTitle,
		metering.AttributeMessageUserEmployeeType,
		metering.AttributeMessageUserCostCenterName,
		metering.AttributeChatOwnerDivisionName,
		metering.AttributeChatOwnerDepartmentName,
		metering.AttributeChatOwnerJobTitle,
		metering.AttributeChatOwnerEmployeeType,
		metering.AttributeChatOwnerCostCenterName,
	} {
		require.NotContains(t, plain, key)
	}
	require.NotContains(t, plain, metering.AttributeMessageUserRBACRoles)
	require.NotContains(t, plain, metering.AttributeChatOwnerRBACRoles)
	require.Equal(t, map[string]string{
		"codec":                                   string(metering.MeasurementTiktokenO200kBase),
		metering.AttributeChatID:                  ambiguousChatID.String(),
		metering.AttributeMessageUserID:           ambiguousUserID,
		metering.AttributeMessageUserAccountEmail: ambiguousEmail,
		metering.AttributeChatOwnerUserID:         ambiguousUserID,
		metering.AttributeChatOwnerUserEmail:      ambiguousEmail,
	}, rows["ambiguous-email"].Attributes)

	require.Equal(t, map[string]string{
		"codec":                  string(metering.MeasurementTiktokenO200kBase),
		metering.AttributeChatID: foreignChatID.String(),
		metering.AttributeModel:  "foreign-model",
	}, rows["tenant-mismatch"].Attributes)
	require.Equal(t, map[string]string{
		"codec":                      string(metering.MeasurementTiktokenO200kBase),
		metering.AttributeChatID:     deletedChatID.String(),
		metering.AttributeHookSource: "deleted-source",
	}, rows["deleted-chat"].Attributes)
	require.Equal(t, map[string]string{
		"codec":                 string(metering.MeasurementTiktokenO200kBase),
		metering.AttributeModel: "missing-chat-model",
	}, rows["missing-chat-id"].Attributes)
}

func TestMeterReadingCHWriterDoesNotEnrichAcrossOrganizations(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, organizationID := newMeteringPostgres(t)
	foreignOrganizationID := "org_" + uuid.NewString()
	seedMeteringOrganization(t, conn, foreignOrganizationID)

	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Local Tenant Project",
		Slug:           "local-tenant-" + uuid.NewString()[:8],
		OrganizationID: organizationID,
	})
	require.NoError(t, err)
	foreignProject, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Foreign Tenant Project",
		Slug:           "foreign-tenant-" + uuid.NewString()[:8],
		OrganizationID: foreignOrganizationID,
	})
	require.NoError(t, err)

	localUserID := "local-user-" + uuid.NewString()
	localEmail := "local-user-" + uuid.NewString() + "@example.test"
	seedMeteringAccount(t, conn, organizationID, localUserID, localEmail)
	seedMeteringRole(t, conn, organizationID, localUserID, "local-role", false)
	seedMeteringDirectoryUser(t, conn, foreignOrganizationID, localUserID, localEmail, "Foreign Division", "Foreign Department", false)
	seedMeteringRole(t, conn, foreignOrganizationID, localUserID, "foreign-local-role", false)

	foreignUserID := "foreign-user-" + uuid.NewString()
	foreignEmail := "foreign-user-" + uuid.NewString() + "@example.test"
	seedMeteringUser(t, conn, foreignOrganizationID, foreignUserID, foreignEmail, "Foreign User Division", "Foreign User Department", true)
	seedMeteringRole(t, conn, foreignOrganizationID, foreignUserID, "foreign-user-role", false)

	localChatID := uuid.New()
	_, err = chatrepo.New(conn).UpsertChat(ctx, chatrepo.UpsertChatParams{
		ID:             localChatID,
		ProjectID:      project.ID,
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(localUserID),
		ExternalUserID: conv.ToPGText("local-owner-external"),
		Title:          conv.ToPGText("Local tenant chat"),
	})
	require.NoError(t, err)
	foreignChatID := uuid.New()
	_, err = chatrepo.New(conn).UpsertChat(ctx, chatrepo.UpsertChatParams{
		ID:             foreignChatID,
		ProjectID:      foreignProject.ID,
		OrganizationID: foreignOrganizationID,
		UserID:         conv.ToPGText(foreignUserID),
		ExternalUserID: conv.ToPGText("foreign-owner-external"),
		Title:          conv.ToPGText("Foreign tenant chat"),
	})
	require.NoError(t, err)

	now := time.Now().UTC()
	newMessage := func(operationID string, scope metering.Scope, attributes map[string]string) *meteringv1.MeterReading {
		t.Helper()
		message, _ := usageMessage(t, metering.UsageInput{
			Meter:       metering.AgentSessionStorage(),
			Scope:       scope,
			OperationID: operationID,
			Value:       1,
			OccurredAt:  now,
			ProducedAt:  now,
			Source:      "chat_message_writer",
			Attributes:  attributes,
		})
		return message
	}
	messages := []*meteringv1.MeterReading{
		newMessage("foreign-chat", metering.ProjectScope(organizationID, project.ID), map[string]string{
			metering.AttributeChatID:                  foreignChatID.String(),
			metering.AttributeModel:                   "foreign-chat-model",
			metering.AttributeMessageUserID:           foreignUserID,
			metering.AttributeMessageUserAccountEmail: foreignEmail,
			metering.AttributeChatOwnerUserEmail:      foreignEmail,
			metering.AttributeMessageUserRBACRoles:    `["foreign-user-role"]`,
		}),
		newMessage("foreign-project", metering.ProjectScope(organizationID, foreignProject.ID), map[string]string{
			metering.AttributeChatID:     foreignChatID.String(),
			metering.AttributeHookSource: "foreign-project-source",
		}),
		newMessage("foreign-message-user", metering.ProjectScope(organizationID, project.ID), map[string]string{
			metering.AttributeChatID:        localChatID.String(),
			metering.AttributeMessageUserID: foreignUserID,
		}),
		newMessage("local-principals", metering.ProjectScope(organizationID, project.ID), map[string]string{
			metering.AttributeChatID:        localChatID.String(),
			metering.AttributeMessageUserID: localUserID,
		}),
	}

	capture := &captureReadingInserter{rows: nil, err: nil}
	writer := metering.NewMeterReadingCHWriter(testenv.NewLogger(t), conn, capture)
	require.NoError(t, writer.HandleBatch(ctx, messages, nil))
	require.Len(t, capture.rows, len(messages))

	rows := make(map[string]chrepo.ReadingRow, len(capture.rows))
	for _, row := range capture.rows {
		rows[row.OperationID] = row
	}

	require.Equal(t, map[string]string{
		"codec":                         string(metering.MeasurementTiktokenO200kBase),
		metering.AttributeChatID:        foreignChatID.String(),
		metering.AttributeModel:         "foreign-chat-model",
		metering.AttributeMessageUserID: foreignUserID,
	}, rows["foreign-chat"].Attributes)
	require.Equal(t, map[string]string{
		"codec":                      string(metering.MeasurementTiktokenO200kBase),
		metering.AttributeChatID:     foreignChatID.String(),
		metering.AttributeHookSource: "foreign-project-source",
	}, rows["foreign-project"].Attributes)

	foreignMessageUser := rows["foreign-message-user"].Attributes
	require.Equal(t, foreignUserID, foreignMessageUser[metering.AttributeMessageUserID])
	require.NotContains(t, foreignMessageUser, metering.AttributeMessageUserAccountEmail)
	require.NotContains(t, foreignMessageUser, metering.AttributeMessageUserDivisionName)
	require.NotContains(t, foreignMessageUser, metering.AttributeMessageUserDepartmentName)
	require.NotContains(t, foreignMessageUser, metering.AttributeMessageUserDirectoryMatch)
	require.NotContains(t, foreignMessageUser, metering.AttributeMessageUserRBACRoles)
	require.Equal(t, localUserID, foreignMessageUser[metering.AttributeChatOwnerUserID])
	require.Equal(t, localEmail, foreignMessageUser[metering.AttributeChatOwnerUserEmail])
	require.Equal(t, "local-owner-external", foreignMessageUser[metering.AttributeChatOwnerExternalUserID])
	require.Equal(t, `["local-role"]`, foreignMessageUser[metering.AttributeChatOwnerRBACRoles])
	require.NotContains(t, foreignMessageUser, metering.AttributeChatOwnerDirectoryMatch)

	localPrincipals := rows["local-principals"].Attributes
	require.Equal(t, localEmail, localPrincipals[metering.AttributeMessageUserAccountEmail])
	require.Equal(t, localEmail, localPrincipals[metering.AttributeChatOwnerUserEmail])
	require.Equal(t, `["local-role"]`, localPrincipals[metering.AttributeMessageUserRBACRoles])
	require.Equal(t, `["local-role"]`, localPrincipals[metering.AttributeChatOwnerRBACRoles])
	require.NotContains(t, localPrincipals, metering.AttributeMessageUserDivisionName)
	require.NotContains(t, localPrincipals, metering.AttributeMessageUserDepartmentName)
	require.NotContains(t, localPrincipals, metering.AttributeMessageUserDirectoryMatch)
	require.NotContains(t, localPrincipals, metering.AttributeChatOwnerDivisionName)
	require.NotContains(t, localPrincipals, metering.AttributeChatOwnerDepartmentName)
	require.NotContains(t, localPrincipals, metering.AttributeChatOwnerDirectoryMatch)
}

func TestMeterReadingCHWriterAbortsBeforeClickHouseInsertOnPostgresFailure(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	message, _ := usageMessage(t, metering.UsageInput{
		Meter:       metering.AgentSessionStorage(),
		Scope:       metering.ProjectScope("org-"+uuid.NewString(), uuid.New()),
		OperationID: "chat_message:" + uuid.NewString(),
		Value:       1,
		OccurredAt:  now,
		ProducedAt:  now,
		Source:      "chat_message_writer",
		Attributes: map[string]string{
			metering.AttributeChatID: uuid.NewString(),
		},
	})
	capture := &captureReadingInserter{rows: nil, err: nil}
	writer := metering.NewMeterReadingCHWriter(testenv.NewLogger(t), failingMeteringDB{err: errors.New("postgres unavailable")}, capture)

	err := writer.HandleBatch(t.Context(), []*meteringv1.MeterReading{message}, nil)

	require.ErrorContains(t, err, "resolve agent session storage attributes")
	require.Empty(t, capture.rows)
}
