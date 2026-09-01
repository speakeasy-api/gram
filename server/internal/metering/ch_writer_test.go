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
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/metering/chrepo"
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

func TestMeterReadingCHWriterEnrichesBillingUserAndPreservesMessageProvenance(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, organizationID := newMeteringPostgres(t)

	billingUserID := "billing-user-" + uuid.NewString()
	billingEmail := "billing-user@example.test"
	seedMeteringFacetUser(t, conn, organizationID, billingUserID, billingEmail, meteringDirectoryFacets{
		DivisionName:   "Billing Division",
		DepartmentName: "Billing Department",
		JobTitle:       "Billing Job",
		EmployeeType:   "Billing Employee Type",
		CostCenterName: "Billing Cost Center",
		Groups:         []string{"zeta-billing-group", "alpha-billing-group"},
	}, true)
	seedMeteringRole(t, conn, organizationID, billingUserID, "zeta-billing-role", false)
	seedMeteringRole(t, conn, organizationID, billingUserID, "alpha-billing-role", true)

	messageUserID := "message-user-" + uuid.NewString()
	now := time.Now().UTC()
	scope := metering.ProjectScope(organizationID, uuid.New())
	attributes := map[string]string{
		metering.AttributeChatID:                     uuid.NewString(),
		metering.AttributeBillingUserID:              billingUserID,
		metering.AttributeMessageUserID:              messageUserID,
		metering.AttributeMessageExternalUserID:      "opaque-message-provider-id",
		metering.AttributeMessageUserEmail:           "observed-message@example.test",
		metering.AttributeBillingUserAccountEmail:    "stale@example.test",
		metering.AttributeBillingUserDivisionName:    "Stale Division",
		metering.AttributeBillingUserDepartmentName:  "Stale Department",
		metering.AttributeBillingUserJobTitle:        "Stale Job",
		metering.AttributeBillingUserEmployeeType:    "Stale Employee Type",
		metering.AttributeBillingUserCostCenterName:  "Stale Cost Center",
		metering.AttributeBillingUserDirectoryGroups: `["stale"]`,
		metering.AttributeBillingUserDirectoryMatch:  "stale",
		metering.AttributeBillingUserRBACRoles:       `["stale"]`,
	}
	usageMessage, usage := usageMessage(t, metering.UsageInput{
		Meter:       metering.AgentSessionStorage(),
		Scope:       scope,
		OperationID: "billing-user-usage",
		Value:       9,
		OccurredAt:  now,
		ProducedAt:  now,
		Source:      "chat_message_writer",
		Attributes:  attributes,
	})
	adjustmentMessage, _ := adjustmentMessage(t, metering.AdjustmentInput{
		Meter:             metering.AgentSessionStorage(),
		Scope:             scope,
		OperationID:       "billing-user-adjustment",
		Value:             -2,
		OccurredAt:        now,
		ProducedAt:        now,
		CorrectsReadingID: usage.ID(),
		Reason:            "source_reconciliation",
		Source:            "chat_message_writer",
		Attributes: map[string]string{
			metering.AttributeBillingUserID:           billingUserID,
			metering.AttributeBillingUserAccountEmail: "spoofed@example.test",
		},
	})
	capture := &captureReadingInserter{rows: nil, err: nil}
	writer := metering.NewMeterReadingCHWriter(testenv.NewLogger(t), conn, capture)

	require.NoError(t, writer.HandleBatch(ctx, []*meteringv1.MeterReading{usageMessage, adjustmentMessage}, nil))
	require.Equal(t, "stale@example.test", usageMessage.GetAttributes()[metering.AttributeBillingUserAccountEmail])
	require.Len(t, capture.rows, 2)
	rows := make(map[string]chrepo.ReadingRow, len(capture.rows))
	for _, row := range capture.rows {
		rows[row.OperationID] = row
	}

	usageAttributes := rows["billing-user-usage"].Attributes
	require.Equal(t, billingUserID, usageAttributes[metering.AttributeBillingUserID])
	require.Equal(t, messageUserID, usageAttributes[metering.AttributeMessageUserID])
	require.Equal(t, "opaque-message-provider-id", usageAttributes[metering.AttributeMessageExternalUserID])
	require.Equal(t, "observed-message@example.test", usageAttributes[metering.AttributeMessageUserEmail])
	require.Equal(t, billingEmail, usageAttributes[metering.AttributeBillingUserAccountEmail])
	require.Equal(t, "Billing Division", usageAttributes[metering.AttributeBillingUserDivisionName])
	require.Equal(t, "Billing Department", usageAttributes[metering.AttributeBillingUserDepartmentName])
	require.Equal(t, "Billing Job", usageAttributes[metering.AttributeBillingUserJobTitle])
	require.Equal(t, "Billing Employee Type", usageAttributes[metering.AttributeBillingUserEmployeeType])
	require.Equal(t, "Billing Cost Center", usageAttributes[metering.AttributeBillingUserCostCenterName])
	require.Equal(t, `["alpha-billing-group","zeta-billing-group"]`, usageAttributes[metering.AttributeBillingUserDirectoryGroups])
	require.Equal(t, "user_id", usageAttributes[metering.AttributeBillingUserDirectoryMatch])
	require.Equal(t, `["alpha-billing-role","zeta-billing-role"]`, usageAttributes[metering.AttributeBillingUserRBACRoles])

	adjustmentAttributes := rows["billing-user-adjustment"].Attributes
	require.Equal(t, billingUserID, adjustmentAttributes[metering.AttributeBillingUserID])
	require.Equal(t, billingEmail, adjustmentAttributes[metering.AttributeBillingUserAccountEmail])
	require.Equal(t, "Billing Division", adjustmentAttributes[metering.AttributeBillingUserDivisionName])
	require.Equal(t, `["alpha-billing-role","zeta-billing-role"]`, adjustmentAttributes[metering.AttributeBillingUserRBACRoles])
}

func TestMeterReadingCHWriterPreservesBillingUserIdentityBoundaries(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, organizationID := newMeteringPostgres(t)

	directUserID := "direct-user-" + uuid.NewString()
	seedMeteringUser(t, conn, organizationID, directUserID, "direct@example.test", "Direct Division", "Direct Department", true)

	emailUserID := "email-user-" + uuid.NewString()
	seedMeteringUser(t, conn, organizationID, emailUserID, "email@example.test", "Email Division", "Email Department", false)

	ambiguousUserID := "ambiguous-user-" + uuid.NewString()
	ambiguousEmail := "ambiguous@example.test"
	seedMeteringAccount(t, conn, organizationID, ambiguousUserID, ambiguousEmail)
	seedMeteringDirectoryUser(t, conn, organizationID, ambiguousUserID, ambiguousEmail, "First Division", "First Department", false)
	seedMeteringDirectoryUser(t, conn, organizationID, ambiguousUserID, ambiguousEmail, "Second Division", "Second Department", false)

	malformedUserID := "malformed-user-" + uuid.NewString()
	seedMeteringAccount(t, conn, organizationID, malformedUserID, "malformed@example.test")
	upsertMeteringDirectoryUser(t, conn, organizationID, malformedUserID, "malformed@example.test", map[string]any{
		"division_name":    42,
		"department_name":  true,
		"job_title":        map[string]string{"invalid": "object"},
		"employee_type":    []string{"invalid", "array"},
		"cost_center_name": nil,
	}, true)

	now := time.Now().UTC()
	newMessage := func(operationID string, attributes map[string]string) *meteringv1.MeterReading {
		t.Helper()
		message, _ := usageMessage(t, metering.UsageInput{
			Meter:       metering.AgentSessionStorage(),
			Scope:       metering.ProjectScope(organizationID, uuid.New()),
			OperationID: operationID,
			Value:       1,
			OccurredAt:  now,
			ProducedAt:  now,
			Source:      "test",
			Attributes:  attributes,
		})
		return message
	}
	messages := []*meteringv1.MeterReading{
		newMessage("direct", map[string]string{
			metering.AttributeBillingUserID: directUserID,
		}),
		newMessage("email", map[string]string{
			metering.AttributeBillingUserID: emailUserID,
		}),
		newMessage("ambiguous", map[string]string{
			metering.AttributeBillingUserID:             ambiguousUserID,
			metering.AttributeBillingUserDirectoryMatch: "spoofed",
		}),
		newMessage("malformed", map[string]string{
			metering.AttributeBillingUserID:             malformedUserID,
			metering.AttributeBillingUserDivisionName:   "spoofed",
			metering.AttributeBillingUserDepartmentName: "spoofed",
		}),
		newMessage("unresolved", map[string]string{
			metering.AttributeBillingUserID:           "missing-" + uuid.NewString(),
			metering.AttributeBillingUserAccountEmail: "spoofed@example.test",
			metering.AttributeBillingUserRBACRoles:    `["spoofed"]`,
		}),
		newMessage("absent", map[string]string{
			metering.AttributeMessageUserEmail:           "observed@example.test",
			metering.AttributeBillingUserAccountEmail:    "spoofed@example.test",
			metering.AttributeBillingUserDirectoryGroups: `["spoofed"]`,
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

	direct := rows["direct"].Attributes
	require.Equal(t, "direct@example.test", direct[metering.AttributeBillingUserAccountEmail])
	require.Equal(t, "Direct Division", direct[metering.AttributeBillingUserDivisionName])
	require.Equal(t, "user_id", direct[metering.AttributeBillingUserDirectoryMatch])

	email := rows["email"].Attributes
	require.Equal(t, "email@example.test", email[metering.AttributeBillingUserAccountEmail])
	require.Equal(t, "Email Division", email[metering.AttributeBillingUserDivisionName])
	require.Equal(t, "email", email[metering.AttributeBillingUserDirectoryMatch])

	ambiguous := rows["ambiguous"].Attributes
	require.Equal(t, ambiguousUserID, ambiguous[metering.AttributeBillingUserID])
	require.Equal(t, ambiguousEmail, ambiguous[metering.AttributeBillingUserAccountEmail])
	require.NotContains(t, ambiguous, metering.AttributeBillingUserDivisionName)
	require.NotContains(t, ambiguous, metering.AttributeBillingUserDirectoryMatch)

	malformed := rows["malformed"].Attributes
	require.Equal(t, "malformed@example.test", malformed[metering.AttributeBillingUserAccountEmail])
	require.Equal(t, "user_id", malformed[metering.AttributeBillingUserDirectoryMatch])
	require.NotContains(t, malformed, metering.AttributeBillingUserDivisionName)
	require.NotContains(t, malformed, metering.AttributeBillingUserDepartmentName)
	require.NotContains(t, malformed, metering.AttributeBillingUserJobTitle)
	require.NotContains(t, malformed, metering.AttributeBillingUserEmployeeType)
	require.NotContains(t, malformed, metering.AttributeBillingUserCostCenterName)

	unresolved := rows["unresolved"].Attributes
	require.Contains(t, unresolved, metering.AttributeBillingUserID)
	require.NotContains(t, unresolved, metering.AttributeBillingUserAccountEmail)
	require.NotContains(t, unresolved, metering.AttributeBillingUserRBACRoles)

	absent := rows["absent"].Attributes
	require.Equal(t, "observed@example.test", absent[metering.AttributeMessageUserEmail])
	require.NotContains(t, absent, metering.AttributeBillingUserAccountEmail)
	require.NotContains(t, absent, metering.AttributeBillingUserDirectoryGroups)
}

func TestMeterReadingCHWriterDoesNotEnrichBillingUserAcrossOrganizations(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, organizationID := newMeteringPostgres(t)
	foreignOrganizationID := "org_" + uuid.NewString()
	seedMeteringOrganization(t, conn, foreignOrganizationID)

	localUserID := "local-user-" + uuid.NewString()
	localEmail := "local@example.test"
	seedMeteringAccount(t, conn, organizationID, localUserID, localEmail)
	seedMeteringRole(t, conn, organizationID, localUserID, "local-role", false)
	seedMeteringDirectoryUser(t, conn, foreignOrganizationID, localUserID, localEmail, "Foreign Division", "Foreign Department", true)
	seedMeteringRole(t, conn, foreignOrganizationID, localUserID, "foreign-role", false)

	foreignUserID := "foreign-user-" + uuid.NewString()
	foreignEmail := "foreign@example.test"
	seedMeteringUser(t, conn, foreignOrganizationID, foreignUserID, foreignEmail, "Foreign User Division", "Foreign User Department", true)

	now := time.Now().UTC()
	newMessage := func(operationID, scopeOrganizationID string, billingUserID string) *meteringv1.MeterReading {
		t.Helper()
		message, _ := usageMessage(t, metering.UsageInput{
			Meter:       metering.AgentSessionStorage(),
			Scope:       metering.ProjectScope(scopeOrganizationID, uuid.New()),
			OperationID: operationID,
			Value:       1,
			OccurredAt:  now,
			ProducedAt:  now,
			Source:      "test",
			Attributes: map[string]string{
				metering.AttributeBillingUserID:             billingUserID,
				metering.AttributeBillingUserAccountEmail:   "spoofed@example.test",
				metering.AttributeBillingUserDivisionName:   "Spoofed Division",
				metering.AttributeBillingUserDirectoryMatch: "spoofed",
			},
		})
		return message
	}
	messages := []*meteringv1.MeterReading{
		newMessage("local-user", organizationID, localUserID),
		newMessage("foreign-user-in-local-org", organizationID, foreignUserID),
		newMessage("foreign-user-in-foreign-org", foreignOrganizationID, foreignUserID),
	}
	capture := &captureReadingInserter{rows: nil, err: nil}
	writer := metering.NewMeterReadingCHWriter(testenv.NewLogger(t), conn, capture)

	require.NoError(t, writer.HandleBatch(ctx, messages, nil))
	require.Len(t, capture.rows, len(messages))
	rows := make(map[string]chrepo.ReadingRow, len(capture.rows))
	for _, row := range capture.rows {
		rows[row.OperationID] = row
	}

	local := rows["local-user"].Attributes
	require.Equal(t, localEmail, local[metering.AttributeBillingUserAccountEmail])
	require.Equal(t, `["local-role"]`, local[metering.AttributeBillingUserRBACRoles])
	require.NotContains(t, local, metering.AttributeBillingUserDivisionName)
	require.NotContains(t, local, metering.AttributeBillingUserDirectoryMatch)

	foreignInLocal := rows["foreign-user-in-local-org"].Attributes
	require.Equal(t, foreignUserID, foreignInLocal[metering.AttributeBillingUserID])
	require.NotContains(t, foreignInLocal, metering.AttributeBillingUserAccountEmail)
	require.NotContains(t, foreignInLocal, metering.AttributeBillingUserDivisionName)
	require.NotContains(t, foreignInLocal, metering.AttributeBillingUserDirectoryMatch)

	foreign := rows["foreign-user-in-foreign-org"].Attributes
	require.Equal(t, foreignEmail, foreign[metering.AttributeBillingUserAccountEmail])
	require.Equal(t, "Foreign User Division", foreign[metering.AttributeBillingUserDivisionName])
	require.Equal(t, "user_id", foreign[metering.AttributeBillingUserDirectoryMatch])
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
			metering.AttributeChatID:        uuid.NewString(),
			metering.AttributeBillingUserID: "billing-user-" + uuid.NewString(),
		},
	})
	capture := &captureReadingInserter{rows: nil, err: nil}
	writer := metering.NewMeterReadingCHWriter(testenv.NewLogger(t), failingMeteringDB{err: errors.New("postgres unavailable")}, capture)

	err := writer.HandleBatch(t.Context(), []*meteringv1.MeterReading{message}, nil)

	require.ErrorContains(t, err, "resolve billing user attributes")
	require.Empty(t, capture.rows)
}
