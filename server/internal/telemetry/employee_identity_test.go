package telemetry_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	hooksRepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	usersRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The employee views pass one identifier, but ingest attributes a person's rows
// two different ways: hook events carry a resolved gram user id, while the rows
// that carry tokens and cost (Claude/Codex OTEL and the usage imports) carry
// only the provider account's email. These tests pin the fold — DNO-827, where
// the employee page showed sessions and tool calls with no tokens or cost.

func TestGetUserMetricsSummary_FoldsEmailOnlyUsageIntoEmployee(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	employee := createOrgEmployee(t, ctx, ti)
	// A personal AI account signs in with its own email, so its usage rows only
	// reach the employee through the user_accounts directory.
	personalEmail := "personal-" + uuid.New().String() + "@example.com"
	linkUserAccount(t, ctx, ti, employee.ID, personalEmail, "personal")

	strangerEmail := "stranger-" + uuid.New().String() + "@example.com"

	now := time.Now().UTC()

	// Hook-shaped rows: a resolved gram user id alongside the directory email,
	// carrying tool calls but no token usage.
	insertToolCallLogWithUser(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), "tools:http:petstore:listPets", 200, 0.5, employee.ID, "")

	// OTEL-shaped rows: tokens and cost, attributed by email only.
	insertUsageLogForEmail(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), uuid.New().String(), employee.Email, 100, 50, 2.5)
	insertUsageLogForEmail(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), uuid.New().String(), personalEmail, 400, 200, 7.5)

	// Someone else's usage must stay out of this employee's totals.
	insertUsageLogForEmail(t, ctx, projectID, deploymentID, now.Add(-7*time.Minute), uuid.New().String(), strangerEmail, 9000, 9000, 99)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.GetUserMetricsSummary(ctx, &gen.GetUserMetricsSummaryPayload{
			From:   from,
			To:     to,
			UserID: &employee.ID,
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) || !assert.NotNil(c, res.Metrics) {
			return
		}

		m := res.Metrics

		// Directory email row (100/50) + personal account row (400/200).
		assert.Equal(c, int64(500), m.TotalInputTokens)
		assert.Equal(c, int64(250), m.TotalOutputTokens)
		assert.Equal(c, int64(750), m.TotalTokens)
		assert.Equal(c, int64(2), m.TotalChatRequests)

		// Cost is what the page's tile reads, and it only reached the response
		// once this query started selecting it.
		assert.InDelta(c, 10.0, m.TotalCost, 0.001)

		// The id-carrying hook row still aggregates alongside them.
		assert.Equal(c, int64(1), m.TotalToolCalls)
	}, 10*time.Second, 200*time.Millisecond)
}

// An employee identifier that is an email — the fallback the page uses for
// someone with usage but no directory row — must still pick up the rows that
// carry their resolved gram user id.
func TestGetUserMetricsSummary_EmailIdentifierFoldsIDCarryingRows(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	employee := createOrgEmployee(t, ctx, ti)

	now := time.Now().UTC()
	insertToolCallLogWithUser(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), "tools:http:petstore:listPets", 200, 0.5, employee.ID, "")
	insertUsageLogForEmail(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), uuid.New().String(), employee.Email, 100, 50, 2.5)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.GetUserMetricsSummary(ctx, &gen.GetUserMetricsSummaryPayload{
			From:   from,
			To:     to,
			UserID: &employee.Email,
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) || !assert.NotNil(c, res.Metrics) {
			return
		}

		assert.Equal(c, int64(100), res.Metrics.TotalInputTokens)
		assert.Equal(c, int64(1), res.Metrics.TotalToolCalls)
	}, 10*time.Second, 200*time.Millisecond)
}

type orgEmployee struct {
	ID    string
	Email string
}

// createOrgEmployee adds a second directory user to the test org, so employee
// identity resolution has something to resolve that is not the caller.
func createOrgEmployee(t *testing.T, ctx context.Context, ti *testInstance) orgEmployee {
	t.Helper()

	id := "user-" + uuid.New().String()
	email := "employee-" + uuid.New().String() + "@example.com"

	user, err := usersRepo.New(ti.conn).UpsertUser(ctx, usersRepo.UpsertUserParams{
		ID:          id,
		Email:       email,
		DisplayName: "Test Employee",
		PhotoUrl:    conv.PtrToPGText(nil),
		Admin:       false,
	})
	require.NoError(t, err)

	_, err = orgRepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, orgRepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: ti.orgID,
		UserID:         conv.ToPGText(user.ID),
	})
	require.NoError(t, err)

	return orgEmployee{ID: user.ID, Email: user.Email}
}

func linkUserAccount(t *testing.T, ctx context.Context, ti *testInstance, userID, email, accountType string) {
	t.Helper()

	_, err := hooksRepo.New(ti.conn).UpsertUserAccount(ctx, hooksRepo.UpsertUserAccountParams{
		OrganizationID:      ti.orgID,
		Provider:            "anthropic",
		ExternalAccountUuid: uuid.New().String(),
		UserID:              conv.ToPGText(userID),
		ExternalOrgID:       conv.PtrToPGText(nil),
		ExternalAccountID:   conv.PtrToPGText(nil),
		Email:               conv.ToPGText(email),
		AccountType:         conv.ToPGText(accountType),
	})
	require.NoError(t, err)
}

// insertUsageLogForEmail writes the row shape the OTEL and usage-import paths
// produce: gen_ai usage attributes attributed by user.email, with no user.id.
func insertUsageLogForEmail(t *testing.T, ctx context.Context, projectID, deploymentID string, timestamp time.Time, chatID, userEmail string, inputTokens, outputTokens int, cost float64) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"gen_ai.conversation.id":     chatID,
		"gen_ai.response.id":         uuid.New().String(),
		"gen_ai.usage.input_tokens":  inputTokens,
		"gen_ai.usage.output_tokens": outputTokens,
		"gen_ai.usage.cost":          cost,
		"gen_ai.response.model":      "claude-opus-5",
		"gen_ai.provider.name":       "anthropic",
		"user.email":                 userEmail,
		"gram.resource.urn":          "chat:completion",
	}

	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_deployment_id, gram_urn, service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "chat completion",
		nil, nil, string(attrsJSON), "{}",
		projectID, deploymentID, "chat:completion", "gram-server")
	require.NoError(t, err)
}
