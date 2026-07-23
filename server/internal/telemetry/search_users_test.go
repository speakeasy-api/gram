package telemetry_test

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	hooksRepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	userrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchUsers_LogsDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	ctx = switchOrganizationInCtx(t, ctx, ti.disabledLogsOrgID)

	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter: &gen.SearchUsersFilter{
			From: from,
			To:   to,
		},
		UserType: "internal",
		Limit:    50,
		Sort:     "desc",
	})

	require.Error(t, err)
	require.Nil(t, result)
	require.Contains(t, err.Error(), "logs are not enabled")
}

func TestSearchUsers_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter: &gen.SearchUsersFilter{
			From: from,
			To:   to,
		},
		UserType: "internal",
		Limit:    50,
		Sort:     "desc",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.Users)
	require.Nil(t, result.NextCursor)
}

// TestSearchUsers_NilFilter guards against the nil pointer dereference that
// occurred when a direct caller (e.g. a platform tool bypassing Goa transport
// validation) invoked SearchUsers with a nil Filter. Both the employee and role
// grouping paths must treat a nil filter as an empty filter (full time range).
func TestSearchUsers_NilFilter(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	for _, groupBy := range []string{"employee", "role"} {
		t.Run(groupBy, func(t *testing.T) {
			t.Parallel()

			result, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
				Filter:   nil,
				UserType: "internal",
				GroupBy:  groupBy,
				Limit:    50,
				Sort:     "desc",
			})

			require.NoError(t, err)
			require.NotNil(t, result)
		})
	}
}

func TestSearchUsers_GroupByInternalUser(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	user1 := "user-a-" + uuid.New().String()
	user2 := "user-b-" + uuid.New().String()
	chatID1 := uuid.New().String()
	chatID2 := uuid.New().String()

	// User 1: 2 completions in 1 chat + 1 successful tool call
	insertChatCompletionLogWithUser(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), chatID1, 100, 50, 150, 1.5, "stop", "gpt-4", "openai", user1, "")
	insertChatCompletionLogWithUser(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), chatID1, 200, 100, 300, 2.0, "tool_calls", "gpt-4", "openai", user1, "")
	insertToolCallLogWithUser(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), "tools:http:petstore:listPets", 200, 0.5, user1, "")

	// User 2: 1 completion in 1 chat + 1 failed tool call
	insertChatCompletionLogWithUser(t, ctx, projectID, deploymentID, now.Add(-6*time.Minute), chatID2, 150, 75, 225, 1.8, "stop", "claude-3", "anthropic", user2, "")
	insertToolCallLogWithUser(t, ctx, projectID, deploymentID, now.Add(-5*time.Minute), "tools:http:petstore:getPet", 500, 1.0, user2, "")

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
			Filter: &gen.SearchUsersFilter{
				From: from,
				To:   to,
			},
			UserType: "internal",
			Limit:    100,
			Sort:     "desc",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		if !assert.Len(c, res.Users, 2) {
			return
		}

		// Index by user ID
		byUser := make(map[string]*gen.UserSummary)
		for _, u := range res.Users {
			byUser[u.UserID] = u
		}

		// User 1
		u1 := byUser[user1]
		if !assert.NotNil(c, u1) {
			return
		}
		assert.Equal(c, int64(1), u1.TotalChats)
		assert.Equal(c, int64(2), u1.TotalChatRequests)
		assert.Equal(c, int64(300), u1.TotalInputTokens)  // 100 + 200
		assert.Equal(c, int64(150), u1.TotalOutputTokens) // 50 + 100
		assert.Equal(c, int64(450), u1.TotalTokens)       // 150 + 300
		assert.Equal(c, int64(1), u1.TotalToolCalls)
		assert.Equal(c, int64(1), u1.ToolCallSuccess)
		assert.Equal(c, int64(0), u1.ToolCallFailure)
		if assert.Len(c, u1.Tools, 1) {
			assert.Equal(c, "tools:http:petstore:listPets", u1.Tools[0].Urn)
			assert.Equal(c, int64(1), u1.Tools[0].Count)
			assert.Equal(c, int64(1), u1.Tools[0].SuccessCount)
			assert.Equal(c, int64(0), u1.Tools[0].FailureCount)
		}

		// User 2
		u2 := byUser[user2]
		if !assert.NotNil(c, u2) {
			return
		}
		assert.Equal(c, int64(1), u2.TotalChats)
		assert.Equal(c, int64(1), u2.TotalChatRequests)
		assert.Equal(c, int64(150), u2.TotalInputTokens)
		assert.Equal(c, int64(75), u2.TotalOutputTokens)
		assert.Equal(c, int64(225), u2.TotalTokens)
		assert.Equal(c, int64(1), u2.TotalToolCalls)
		assert.Equal(c, int64(0), u2.ToolCallSuccess)
		assert.Equal(c, int64(1), u2.ToolCallFailure)
		if assert.Len(c, u2.Tools, 1) {
			assert.Equal(c, "tools:http:petstore:getPet", u2.Tools[0].Urn)
			assert.Equal(c, int64(1), u2.Tools[0].Count)
			assert.Equal(c, int64(0), u2.Tools[0].SuccessCount)
			assert.Equal(c, int64(1), u2.Tools[0].FailureCount)
		}
	}, 10*time.Second, 200*time.Millisecond)
}

func TestSearchUsers_GroupByExternalUser(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	extUser := "ext-user-" + uuid.New().String()
	chatID := uuid.New().String()

	insertChatCompletionLogWithUser(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), chatID, 100, 50, 150, 1.5, "stop", "gpt-4", "openai", "", extUser)
	insertToolCallLogWithUser(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), "tools:http:api:call", 200, 0.5, "", extUser)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
			Filter: &gen.SearchUsersFilter{
				From: from,
				To:   to,
			},
			UserType: "external",
			Limit:    100,
			Sort:     "desc",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		if !assert.Len(c, res.Users, 1) {
			return
		}

		u := res.Users[0]
		assert.Equal(c, extUser, u.UserID)
		assert.Equal(c, int64(1), u.TotalChats)
		assert.Equal(c, int64(1), u.TotalChatRequests)
		assert.Equal(c, int64(100), u.TotalInputTokens)
		assert.Equal(c, int64(50), u.TotalOutputTokens)
		assert.Equal(c, int64(150), u.TotalTokens)
		assert.Equal(c, int64(1), u.TotalToolCalls)
		assert.Equal(c, int64(1), u.ToolCallSuccess)
		assert.Equal(c, int64(0), u.ToolCallFailure)
	}, 10*time.Second, 200*time.Millisecond)
}

func TestSearchUsers_FallsBackToUserEmail(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	email := "unmatched-user-" + uuid.New().String() + "@example.com"
	insertPollingLogWithEmail(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), email, 100, 50, 1.25)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
			Filter: &gen.SearchUsersFilter{
				From: from,
				To:   to,
			},
			UserType: "internal",
			Limit:    100,
			Sort:     "desc",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		if !assert.Len(c, res.Users, 1) {
			return
		}
		assert.Equal(c, email, res.Users[0].UserID)
		assert.Equal(c, int64(100), res.Users[0].TotalInputTokens)
		assert.Equal(c, int64(50), res.Users[0].TotalOutputTokens)
		assert.InDelta(c, 1.25, res.Users[0].TotalCost, 0.000001)
	}, 10*time.Second, 200*time.Millisecond)

	filtered, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter: &gen.SearchUsersFilter{
			From:    from,
			To:      to,
			UserIds: []string{email},
		},
		UserType: "internal",
		Limit:    100,
		Sort:     "desc",
	})
	require.NoError(t, err)
	require.Len(t, filtered.Users, 1)
	require.Equal(t, email, filtered.Users[0].UserID)
}

func TestSearchUsers_GroupsInternalUsersByEmailWhenOpaqueUserIDPresent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	userID := "01924a0eb409b0ecf44e06d0ec03cbc4"
	email := "smartnews-user@example.com"
	insertHookLogWithUserAndEmail(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), userID, email)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
			Filter: &gen.SearchUsersFilter{
				From: from,
				To:   to,
			},
			UserType: "internal",
			Limit:    100,
			Sort:     "desc",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) || !assert.Len(c, res.Users, 1) {
			return
		}
		assert.Equal(c, email, res.Users[0].UserID)
		assert.Equal(c, email, res.Users[0].UserEmail)
	}, 10*time.Second, 200*time.Millisecond)
}

func TestSearchUsers_MergesInternalUsersByEmail(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	userID := "01924a0eb409b0ecf44e06d0ec03cbc4"
	email := "merged-internal-" + uuid.New().String() + "@example.com"
	insertPollingLogWithEmail(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), email, 100, 50, 1.25)
	insertPollingLogWithUserAndEmail(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), userID, email, 200, 100, 2.5)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
			Filter: &gen.SearchUsersFilter{
				From: from,
				To:   to,
			},
			UserType: "internal",
			Limit:    100,
			Sort:     "desc",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) || !assert.Len(c, res.Users, 1) {
			return
		}

		user := res.Users[0]
		assert.Equal(c, email, user.UserID)
		assert.Equal(c, email, user.UserEmail)
		assert.Equal(c, int64(300), user.TotalInputTokens)
		assert.Equal(c, int64(150), user.TotalOutputTokens)
		assert.Equal(c, int64(450), user.TotalTokens)
	}, 10*time.Second, 200*time.Millisecond)

	filtered, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter: &gen.SearchUsersFilter{
			From:    from,
			To:      to,
			UserIds: []string{email},
		},
		UserType: "internal",
		Limit:    100,
		Sort:     "desc",
	})
	require.NoError(t, err)
	require.Len(t, filtered.Users, 1)
	require.Equal(t, email, filtered.Users[0].UserID)
	require.Equal(t, int64(450), filtered.Users[0].TotalTokens)
}

// TestSearchUsers_MergesEmaillessRowsByKnownUserID guards the enrollment page
// token counts: a person's rows that carry a user_id but no email (e.g. tool
// calls attributed by id only) must fold into their email-keyed summary instead
// of surfacing as a separate zero-token summary keyed by the raw user_id.
func TestSearchUsers_MergesEmaillessRowsByKnownUserID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	userID := "gram-user-" + uuid.New().String()
	email := "merged-emailless-" + uuid.New().String() + "@example.com"

	// Token usage reported with both id and email; a later tool call carries the
	// id only.
	insertPollingLogWithUserAndEmail(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), userID, email, 200, 100, 2.5)
	toolCallAt := now.Add(-5 * time.Minute)
	insertToolCallLogWithUser(t, ctx, projectID, deploymentID, toolCallAt, "tools:http:petstore:listPets", 200, 0.5, userID, "")

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
			Filter: &gen.SearchUsersFilter{
				From: from,
				To:   to,
			},
			UserType: "internal",
			Limit:    100,
			Sort:     "desc",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) || !assert.Len(c, res.Users, 1) {
			return
		}

		user := res.Users[0]
		assert.Equal(c, email, user.UserID)
		assert.Equal(c, email, user.UserEmail)
		assert.Equal(c, int64(200), user.TotalInputTokens)
		assert.Equal(c, int64(100), user.TotalOutputTokens)
		assert.Equal(c, int64(1), user.TotalToolCalls)
		assert.Equal(c, strconv.FormatInt(toolCallAt.UnixNano(), 10), user.LastSeenUnixNano)
	}, 10*time.Second, 200*time.Millisecond)
}

// TestSearchUsers_AttachesAccountsToEmailKeyedSummary guards the accounts
// breakdown on the employees list: the user_accounts directory is keyed by raw
// gram user id while summaries are keyed email-first, so a summary must pick up
// accounts through the user_id values folded into it.
func TestSearchUsers_AttachesAccountsToEmailKeyedSummary(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	// user_accounts.user_id has a FK to users, so reuse the seeded session user.
	userID := authCtx.UserID
	email := "account-holder-" + uuid.New().String() + "@example.com"

	hooksQueries := hooksRepo.New(ti.conn)
	_, err := hooksQueries.UpsertUserAccount(ctx, hooksRepo.UpsertUserAccountParams{
		OrganizationID:      authCtx.ActiveOrganizationID,
		Provider:            "anthropic",
		ExternalAccountUuid: uuid.New().String(),
		UserID:              conv.ToPGTextEmpty(userID),
		ExternalOrgID:       conv.ToPGTextEmpty("ext-org-" + uuid.New().String()),
		ExternalAccountID:   conv.ToPGTextEmpty(""),
		Email:               conv.ToPGTextEmpty(email),
		AccountType:         conv.ToPGTextEmpty("team"),
	})
	require.NoError(t, err)

	insertPollingLogWithUserAndEmail(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), userID, email, 100, 50, 1.0)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
			Filter: &gen.SearchUsersFilter{
				From: from,
				To:   to,
			},
			UserType: "internal",
			Limit:    100,
			Sort:     "desc",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) || !assert.Len(c, res.Users, 1) {
			return
		}

		user := res.Users[0]
		assert.Equal(c, email, user.UserID)
		if !assert.Len(c, user.Accounts, 1) {
			return
		}
		account := user.Accounts[0]
		assert.Equal(c, "anthropic", account.Provider)
		if assert.NotNil(c, account.AccountType) {
			assert.Equal(c, "team", *account.AccountType)
		}
		if assert.NotNil(c, account.Email) {
			assert.Equal(c, email, *account.Email)
		}
	}, 10*time.Second, 200*time.Millisecond)
}

// TestSearchUsers_ForeignRawUserIDsDoNotStealAccounts reproduces DNO-509: a
// stray telemetry row pairing one employee's email with another employee's
// user_id folds the second employee's id into the first summary's raw_user_ids.
// Accounts must still attach by directory ownership — the first summary must
// not pick up the second employee's account, and the second employee must keep
// it.
func TestSearchUsers_ForeignRawUserIDsDoNotStealAccounts(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	userA, emailA := seedConnectedOrgUser(t, ctx, ti, "alice")
	userB, emailB := seedConnectedOrgUser(t, ctx, ti, "bob")

	hooksQueries := hooksRepo.New(ti.conn)
	accountEmailByOwner := map[string]string{userA: emailA, userB: emailB}
	for owner, accountEmail := range accountEmailByOwner {
		_, err := hooksQueries.UpsertUserAccount(ctx, hooksRepo.UpsertUserAccountParams{
			OrganizationID:      authCtx.ActiveOrganizationID,
			Provider:            "anthropic",
			ExternalAccountUuid: uuid.New().String(),
			UserID:              conv.ToPGTextEmpty(owner),
			ExternalOrgID:       conv.ToPGTextEmpty("ext-org-" + uuid.New().String()),
			ExternalAccountID:   conv.ToPGTextEmpty(""),
			Email:               conv.ToPGTextEmpty(accountEmail),
			AccountType:         conv.ToPGTextEmpty("team"),
		})
		require.NoError(t, err)
	}

	insertPollingLogWithUserAndEmail(t, ctx, projectID, deploymentID, now.Add(-30*time.Minute), userB, emailB, 100, 50, 1.0)
	insertPollingLogWithUserAndEmail(t, ctx, projectID, deploymentID, now.Add(-20*time.Minute), userA, emailA, 100, 50, 1.0)
	// The poisoned row: A's email with B's user_id, most recent so A's summary
	// sorts first and would claim B's id under raw-id attachment.
	insertPollingLogWithUserAndEmail(t, ctx, projectID, deploymentID, now.Add(-5*time.Minute), userB, emailA, 10, 5, 0.1)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
			Filter: &gen.SearchUsersFilter{
				From: from,
				To:   to,
			},
			UserType: "internal",
			Limit:    100,
			Sort:     "desc",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) || !assert.Len(c, res.Users, 2) {
			return
		}

		byKey := make(map[string]*gen.UserSummary, len(res.Users))
		for _, u := range res.Users {
			byKey[u.UserID] = u
		}
		summaryA := byKey[emailA]
		summaryB := byKey[emailB]
		if !assert.NotNil(c, summaryA) || !assert.NotNil(c, summaryB) {
			return
		}
		if assert.Len(c, summaryA.Accounts, 1) && assert.NotNil(c, summaryA.Accounts[0].Email) {
			assert.Equal(c, emailA, *summaryA.Accounts[0].Email)
		}
		if assert.Len(c, summaryB.Accounts, 1) && assert.NotNil(c, summaryB.Accounts[0].Email) {
			assert.Equal(c, emailB, *summaryB.Accounts[0].Email)
		}
	}, 10*time.Second, 200*time.Millisecond)
}

// TestSearchUsers_PersonalAccountAttachesToOwnerSummary covers the
// device-bridge shape: a personal session's rows carry the employee's user_id
// under the personal email, producing a personal-email summary whose
// raw_user_ids hold the employee's id. Both of the employee's accounts must
// attach to the employee's own summary, not to the personal-email usage row —
// even when the personal-email summary sorts first.
func TestSearchUsers_PersonalAccountAttachesToOwnerSummary(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	userID, workEmail := seedConnectedOrgUser(t, ctx, ti, "carol")
	personalEmail := "carol-personal-" + uuid.New().String() + "@gmail.com"

	hooksQueries := hooksRepo.New(ti.conn)
	accountTypeByEmail := map[string]string{workEmail: "team", personalEmail: "personal"}
	for accountEmail, accountType := range accountTypeByEmail {
		_, err := hooksQueries.UpsertUserAccount(ctx, hooksRepo.UpsertUserAccountParams{
			OrganizationID:      authCtx.ActiveOrganizationID,
			Provider:            "anthropic",
			ExternalAccountUuid: uuid.New().String(),
			UserID:              conv.ToPGTextEmpty(userID),
			ExternalOrgID:       conv.ToPGTextEmpty("ext-org-" + uuid.New().String()),
			ExternalAccountID:   conv.ToPGTextEmpty(""),
			Email:               conv.ToPGTextEmpty(accountEmail),
			AccountType:         conv.ToPGTextEmpty(accountType),
		})
		require.NoError(t, err)
	}

	insertPollingLogWithUserAndEmail(t, ctx, projectID, deploymentID, now.Add(-20*time.Minute), userID, workEmail, 100, 50, 1.0)
	insertPollingLogWithUserAndEmail(t, ctx, projectID, deploymentID, now.Add(-5*time.Minute), userID, personalEmail, 10, 5, 0.1)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
			Filter: &gen.SearchUsersFilter{
				From: from,
				To:   to,
			},
			UserType: "internal",
			Limit:    100,
			Sort:     "desc",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) || !assert.Len(c, res.Users, 2) {
			return
		}

		byKey := make(map[string]*gen.UserSummary, len(res.Users))
		for _, u := range res.Users {
			byKey[u.UserID] = u
		}
		workSummary := byKey[workEmail]
		personalSummary := byKey[personalEmail]
		if !assert.NotNil(c, workSummary) || !assert.NotNil(c, personalSummary) {
			return
		}
		if assert.Len(c, workSummary.Accounts, 2) {
			gotEmails := make([]string, 0, 2)
			for _, account := range workSummary.Accounts {
				if assert.NotNil(c, account.Email) {
					gotEmails = append(gotEmails, *account.Email)
				}
			}
			assert.ElementsMatch(c, []string{workEmail, personalEmail}, gotEmails)
		}
		assert.Empty(c, personalSummary.Accounts)
	}, 10*time.Second, 200*time.Millisecond)
}

// seedConnectedOrgUser creates a user connected to the test org and returns
// its id and directory email, satisfying the user_accounts FK and the email
// resolution that account attachment relies on.
func seedConnectedOrgUser(t *testing.T, ctx context.Context, ti *testInstance, name string) (string, string) {
	t.Helper()

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	userID := uuid.New().String()
	email := name + "-" + uuid.New().String() + "@example.com"

	_, err := userrepo.New(ti.conn).UpsertUser(ctx, userrepo.UpsertUserParams{
		ID:          userID,
		Email:       email,
		DisplayName: name,
		PhotoUrl:    conv.PtrToPGText(nil),
		Admin:       false,
	})
	require.NoError(t, err)

	_, err = orgrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)

	return userID, email
}

func TestSearchUsers_Pagination(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()

	// Create 5 distinct users with staggered timestamps so last_seen differs
	for i := range 5 {
		userID := "paginated-user-" + uuid.New().String()
		ts := now.Add(-time.Duration(50-i*10) * time.Minute)
		insertChatCompletionLogWithUser(t, ctx, projectID, deploymentID, ts, uuid.New().String(), 100, 50, 150, 1.0, "stop", "gpt-4", "openai", userID, "")
	}

	from := now.Add(-2 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	// Page 1: limit 2
	var page1 *gen.SearchUsersResult
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
			Filter: &gen.SearchUsersFilter{
				From: from,
				To:   to,
			},
			UserType: "internal",
			Limit:    2,
			Sort:     "desc",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		assert.Len(c, res.Users, 2)
		assert.NotNil(c, res.NextCursor)
		page1 = res
	}, 10*time.Second, 200*time.Millisecond)

	// Page 2
	page2, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter: &gen.SearchUsersFilter{
			From: from,
			To:   to,
		},
		UserType: "internal",
		Cursor:   page1.NextCursor,
		Limit:    2,
		Sort:     "desc",
	})
	require.NoError(t, err)
	require.Len(t, page2.Users, 2)
	require.NotNil(t, page2.NextCursor)

	// Page 3: remaining
	page3, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter: &gen.SearchUsersFilter{
			From: from,
			To:   to,
		},
		UserType: "internal",
		Cursor:   page2.NextCursor,
		Limit:    2,
		Sort:     "desc",
	})
	require.NoError(t, err)
	require.Len(t, page3.Users, 1)
	require.Nil(t, page3.NextCursor)

	// Verify no duplicate user IDs across pages
	seen := make(map[string]bool)
	allUsers := append(append(page1.Users, page2.Users...), page3.Users...)
	for _, u := range allUsers {
		require.False(t, seen[u.UserID], "duplicate user ID across pages: %s", u.UserID)
		seen[u.UserID] = true
	}
	require.Len(t, seen, 5)
}

func TestSearchUsers_PaginationAscOrder(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()

	for i := range 5 {
		userID := "asc-user-" + uuid.New().String()
		ts := now.Add(-time.Duration(50-i*10) * time.Minute)
		insertChatCompletionLogWithUser(t, ctx, projectID, deploymentID, ts, uuid.New().String(), 100, 50, 150, 1.0, "stop", "gpt-4", "openai", userID, "")
	}

	from := now.Add(-2 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	// Page 1
	var page1 *gen.SearchUsersResult
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
			Filter: &gen.SearchUsersFilter{
				From: from,
				To:   to,
			},
			UserType: "internal",
			Limit:    2,
			Sort:     "asc",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		assert.Len(c, res.Users, 2)
		assert.NotNil(c, res.NextCursor)
		page1 = res
	}, 10*time.Second, 200*time.Millisecond)

	// Page 2
	page2, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter: &gen.SearchUsersFilter{
			From: from,
			To:   to,
		},
		UserType: "internal",
		Cursor:   page1.NextCursor,
		Limit:    2,
		Sort:     "asc",
	})
	require.NoError(t, err)
	require.Len(t, page2.Users, 2)
	require.NotNil(t, page2.NextCursor)

	// Page 3
	page3, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter: &gen.SearchUsersFilter{
			From: from,
			To:   to,
		},
		UserType: "internal",
		Cursor:   page2.NextCursor,
		Limit:    2,
		Sort:     "asc",
	})
	require.NoError(t, err)
	require.Len(t, page3.Users, 1)
	require.Nil(t, page3.NextCursor)

	// Verify no duplicates
	seen := make(map[string]bool)
	allUsers := append(append(page1.Users, page2.Users...), page3.Users...)
	for _, u := range allUsers {
		require.False(t, seen[u.UserID], "duplicate user ID across pages: %s", u.UserID)
		seen[u.UserID] = true
	}
	require.Len(t, seen, 5)
}

func TestSearchUsers_FilterByDeploymentID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deployment1 := uuid.New().String()
	deployment2 := uuid.New().String()

	now := time.Now().UTC()
	user1 := "deploy-user-1-" + uuid.New().String()
	user2 := "deploy-user-2-" + uuid.New().String()

	// User 1 in deployment 1
	insertChatCompletionLogWithUser(t, ctx, projectID, deployment1, now.Add(-10*time.Minute), uuid.New().String(), 100, 50, 150, 1.0, "stop", "gpt-4", "openai", user1, "")
	// User 2 in deployment 2
	insertChatCompletionLogWithUser(t, ctx, projectID, deployment2, now.Add(-9*time.Minute), uuid.New().String(), 100, 50, 150, 1.0, "stop", "gpt-4", "openai", user2, "")

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
			Filter: &gen.SearchUsersFilter{
				From:         from,
				To:           to,
				DeploymentID: &deployment1,
			},
			UserType: "internal",
			Limit:    100,
			Sort:     "desc",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		if !assert.Len(c, res.Users, 1) {
			return
		}
		assert.Equal(c, user1, res.Users[0].UserID)
	}, 10*time.Second, 200*time.Millisecond)
}

func TestSearchUsers_FilterByInternalUserIDs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	includedUser := "internal-included-" + uuid.New().String()
	excludedUser := "internal-excluded-" + uuid.New().String()
	userIDAsExternalID := "external-matches-internal-filter-" + uuid.New().String()

	insertChatCompletionLogWithUser(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), uuid.New().String(), 100, 50, 150, 1.0, "stop", "gpt-4", "openai", includedUser, "")
	insertChatCompletionLogWithUser(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), uuid.New().String(), 200, 100, 300, 1.0, "stop", "gpt-4", "openai", excludedUser, "")
	insertChatCompletionLogWithUser(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), uuid.New().String(), 300, 150, 450, 1.0, "stop", "gpt-4", "openai", userIDAsExternalID, includedUser)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
			Filter: &gen.SearchUsersFilter{
				From:    from,
				To:      to,
				UserIds: []string{includedUser},
			},
			UserType: "internal",
			Limit:    100,
			Sort:     "desc",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		if !assert.Len(c, res.Users, 1) {
			return
		}
		assert.Equal(c, includedUser, res.Users[0].UserID)
		assert.Equal(c, int64(150), res.Users[0].TotalTokens)
	}, 10*time.Second, 200*time.Millisecond)
}

func TestSearchUsers_FilterByExternalUserIDs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	includedExternalUser := "external-included-" + uuid.New().String()
	excludedExternalUser := "external-excluded-" + uuid.New().String()

	insertChatCompletionLogWithUser(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), uuid.New().String(), 100, 50, 150, 1.0, "stop", "gpt-4", "openai", "internal-a-"+uuid.New().String(), includedExternalUser)
	insertChatCompletionLogWithUser(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), uuid.New().String(), 200, 100, 300, 1.0, "stop", "gpt-4", "openai", "internal-b-"+uuid.New().String(), excludedExternalUser)
	insertChatCompletionLogWithUser(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), uuid.New().String(), 300, 150, 450, 1.0, "stop", "gpt-4", "openai", includedExternalUser, "external-c-"+uuid.New().String())

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
			Filter: &gen.SearchUsersFilter{
				From:    from,
				To:      to,
				UserIds: []string{includedExternalUser},
			},
			UserType: "external",
			Limit:    100,
			Sort:     "desc",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		if !assert.Len(c, res.Users, 1) {
			return
		}
		assert.Equal(c, includedExternalUser, res.Users[0].UserID)
		assert.Equal(c, int64(150), res.Users[0].TotalTokens)
	}, 10*time.Second, 200*time.Millisecond)
}

func TestSearchUsers_HookSourceBreakdown(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	userID := "hook-user-" + uuid.New().String()
	excludedUserID := "hook-excluded-" + uuid.New().String()

	insertHookLogWithUser(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), userID, "", "cursor", true)
	insertHookLogWithUser(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), userID, "", "cursor", true)
	insertHookLogWithUser(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), userID, "", "claude-code", false)
	insertHookLogWithUser(t, ctx, projectID, deploymentID, now.Add(-7*time.Minute), excludedUserID, "", "cursor", true)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
			Filter: &gen.SearchUsersFilter{
				From:    from,
				To:      to,
				UserIds: []string{userID},
			},
			UserType: "internal",
			Limit:    100,
			Sort:     "desc",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		if !assert.Len(c, res.Users, 1) {
			return
		}
		assert.Equal(c, userID, res.Users[0].UserID)
		if !assert.Len(c, res.Users[0].HookSources, 2) {
			return
		}

		byHookSource := make(map[string]*gen.HookSourceUsage)
		for _, source := range res.Users[0].HookSources {
			byHookSource[source.Source] = source
		}

		cursor := byHookSource["cursor"]
		if assert.NotNil(c, cursor) {
			assert.Equal(c, int64(2), cursor.EventCount)
		}

		claudeCode := byHookSource["claude-code"]
		if assert.NotNil(c, claudeCode) {
			assert.Equal(c, int64(1), claudeCode.EventCount)
		}
	}, 10*time.Second, 200*time.Millisecond)
}

func TestSearchUsers_ToolsBreakdown(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	userID := "tools-user-" + uuid.New().String()

	// 3 calls to listPets (2 success, 1 failure) + 2 calls to getPet (both success)
	insertToolCallLogWithUser(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), "tools:http:petstore:listPets", 200, 0.5, userID, "")
	insertToolCallLogWithUser(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), "tools:http:petstore:listPets", 200, 0.4, userID, "")
	insertToolCallLogWithUser(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), "tools:http:petstore:listPets", 500, 1.0, userID, "")
	insertToolCallLogWithUser(t, ctx, projectID, deploymentID, now.Add(-7*time.Minute), "tools:http:petstore:getPet", 200, 0.3, userID, "")
	insertToolCallLogWithUser(t, ctx, projectID, deploymentID, now.Add(-6*time.Minute), "tools:http:petstore:getPet", 200, 0.2, userID, "")

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
			Filter: &gen.SearchUsersFilter{
				From: from,
				To:   to,
			},
			UserType: "internal",
			Limit:    100,
			Sort:     "desc",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		if !assert.Len(c, res.Users, 1) {
			return
		}

		u := res.Users[0]
		assert.Equal(c, userID, u.UserID)
		assert.Equal(c, int64(5), u.TotalToolCalls)
		assert.Equal(c, int64(4), u.ToolCallSuccess) // 2+2
		assert.Equal(c, int64(1), u.ToolCallFailure) // 1

		// Per-tool breakdown
		if !assert.Len(c, u.Tools, 2) {
			return
		}
		toolStats := make(map[string]*gen.ToolUsage)
		for _, tool := range u.Tools {
			toolStats[tool.Urn] = tool
		}

		listPets := toolStats["tools:http:petstore:listPets"]
		if assert.NotNil(c, listPets) {
			assert.Equal(c, int64(3), listPets.Count)
			assert.Equal(c, int64(2), listPets.SuccessCount)
			assert.Equal(c, int64(1), listPets.FailureCount)
		}

		getPet := toolStats["tools:http:petstore:getPet"]
		if assert.NotNil(c, getPet) {
			assert.Equal(c, int64(2), getPet.Count)
			assert.Equal(c, int64(2), getPet.SuccessCount)
			assert.Equal(c, int64(0), getPet.FailureCount)
		}
	}, 10*time.Second, 200*time.Millisecond)
}

func TestSearchUsers_ScopedByProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	otherProjectID := uuid.New().String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	sharedUserID := "shared-user-" + uuid.New().String()

	// Insert logs for the same user in both projects
	insertChatCompletionLogWithUser(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), uuid.New().String(), 100, 50, 150, 1.0, "stop", "gpt-4", "openai", sharedUserID, "")
	insertChatCompletionLogWithUser(t, ctx, otherProjectID, deploymentID, now.Add(-9*time.Minute), uuid.New().String(), 500, 250, 750, 2.0, "stop", "gpt-4", "openai", sharedUserID, "")

	// Insert a different user only in the other project
	otherUser := "other-project-user-" + uuid.New().String()
	insertChatCompletionLogWithUser(t, ctx, otherProjectID, deploymentID, now.Add(-8*time.Minute), uuid.New().String(), 200, 100, 300, 1.0, "stop", "gpt-4", "openai", otherUser, "")

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
			Filter: &gen.SearchUsersFilter{
				From: from,
				To:   to,
			},
			UserType: "internal",
			Limit:    100,
			Sort:     "desc",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		if !assert.Len(c, res.Users, 1, "should only return users from the queried project") {
			return
		}
		assert.Equal(c, sharedUserID, res.Users[0].UserID)

		// Metrics should only reflect the current project's data
		assert.Equal(c, int64(100), res.Users[0].TotalInputTokens, "should not include tokens from other project")
		assert.Equal(c, int64(150), res.Users[0].TotalTokens, "should not include tokens from other project")
	}, 10*time.Second, 200*time.Millisecond)
}

// TestSearchUsers_BasicMetricsOmitsBreakdowns pins the "basic" metrics detail
// level used by the employee enrollment list (DNO-618): identity, activity
// window, token sums, and raw_user_ids are computed, while the heavier
// chat/cost/tool/hook aggregates are skipped and left zero/empty.
func TestSearchUsers_BasicMetricsOmitsBreakdowns(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	userID := "basic-user-" + uuid.New().String()
	chatID := uuid.New().String()

	// A chat completion (tokens/cost/chat) plus a tool call, so the full path would
	// populate every breakdown — basic must still leave them empty.
	insertChatCompletionLogWithUser(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), chatID, 100, 50, 150, 1.5, "stop", "gpt-4", "openai", userID, "")
	insertChatCompletionLogWithUser(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), chatID, 200, 100, 300, 2.0, "tool_calls", "gpt-4", "openai", userID, "")
	insertToolCallLogWithUser(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), "tools:http:petstore:listPets", 200, 0.5, userID, "")

	// Telemetry writes use ClickHouse async inserts; drain the queue synchronously
	// so the rows are deterministically visible (no polling — see the telemetry
	// README).
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	res, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter: &gen.SearchUsersFilter{
			From: from,
			To:   to,
		},
		UserType: "internal",
		Limit:    100,
		Sort:     "desc",
		Metrics:  "basic",
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Len(t, res.Users, 1)

	u := res.Users[0]
	// Lean fields the enrollment list renders are computed.
	assert.Equal(t, userID, u.UserID)
	assert.Equal(t, int64(300), u.TotalInputTokens)  // 100 + 200
	assert.Equal(t, int64(150), u.TotalOutputTokens) // 50 + 100
	// Last activity is the most recent inserted row (the -8m tool call).
	assert.Equal(t, strconv.FormatInt(now.Add(-8*time.Minute).UnixNano(), 10), u.LastSeenUnixNano)

	// Heavy aggregates are skipped under basic and left zero/empty.
	assert.Equal(t, int64(0), u.TotalChats)
	assert.Equal(t, int64(0), u.TotalChatRequests)
	assert.Equal(t, int64(0), u.TotalTokens)
	assert.Equal(t, int64(0), u.TotalToolCalls)
	assert.Zero(t, u.TotalCost)
	assert.Empty(t, u.Tools)
}

func insertHookLogWithUser(t *testing.T, ctx context.Context, projectID, deploymentID string, timestamp time.Time, userID, externalUserID, hookSource string, success bool) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"gram.event.source": "hook",
		"gram.hook.source":  hookSource,
		"gram.tool.name":    "Bash",
	}
	if success {
		attributes["gen_ai.tool.call.result"] = "ok"
	} else {
		attributes["gram.hook.error"] = "failed"
	}
	if userID != "" {
		attributes["user.id"] = userID
	}
	if externalUserID != "" {
		attributes["gram.external_user.id"] = externalUserID
	}

	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_deployment_id, gram_urn, service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "hook event",
		nil, nil, string(attrsJSON), "{}",
		projectID, deploymentID, "hooks:Bash", "gram-hooks")
	require.NoError(t, err)
}

func insertHookLogWithUserAndEmail(t *testing.T, ctx context.Context, projectID, deploymentID string, timestamp time.Time, userID, email string) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"gram.event.source":       "hook",
		"gram.hook.source":        "claude-code",
		"gram.tool.name":          "Bash",
		"gen_ai.tool.call.result": "ok",
		"user.id":                 userID,
		"user.email":              email,
	}
	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_deployment_id, gram_urn, service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "hook event",
		nil, nil, string(attrsJSON), "{}",
		projectID, deploymentID, "hooks:Bash", "gram-hooks")
	require.NoError(t, err)
}

func insertPollingLogWithEmail(t *testing.T, ctx context.Context, projectID, deploymentID string, timestamp time.Time, email string, inputTokens, outputTokens int, cost float64) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"gram.event.source":          string(tm.EventSourceAPI),
		"gram.hook.source":           "cursor",
		"user.email":                 email,
		"gen_ai.usage.input_tokens":  inputTokens,
		"gen_ai.usage.output_tokens": outputTokens,
		"gen_ai.usage.cost":          cost,
		"gen_ai.response.model":      "cursor-model",
		"gen_ai.conversation.id":     uuid.New().String(),
		"gen_ai.response.id":         uuid.New().String(),
		"gen_ai.usage.total_tokens":  inputTokens + outputTokens,
		"gen_ai.provider.name":       "cursor",
		"cursor.event_hash":          uuid.New().String(),
		"gram.resource.urn":          "cursor:usage:metrics",
	}

	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_deployment_id, gram_urn, service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "cursor usage metrics",
		nil, nil, string(attrsJSON), "{}",
		projectID, deploymentID, "cursor:usage:metrics", "gram-cursor")
	require.NoError(t, err)
}

func insertPollingLogWithUserAndEmail(t *testing.T, ctx context.Context, projectID, deploymentID string, timestamp time.Time, userID, email string, inputTokens, outputTokens int, cost float64) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"gram.event.source":          string(tm.EventSourceAPI),
		"gram.hook.source":           "cursor",
		"user.id":                    userID,
		"user.email":                 email,
		"gen_ai.usage.input_tokens":  inputTokens,
		"gen_ai.usage.output_tokens": outputTokens,
		"gen_ai.usage.cost":          cost,
		"gen_ai.response.model":      "cursor-model",
		"gen_ai.conversation.id":     uuid.New().String(),
		"gen_ai.response.id":         uuid.New().String(),
		"gen_ai.usage.total_tokens":  inputTokens + outputTokens,
		"gen_ai.provider.name":       "cursor",
		"cursor.event_hash":          uuid.New().String(),
		"gram.resource.urn":          "cursor:usage:metrics",
	}

	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_deployment_id, gram_urn, service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "cursor usage metrics",
		nil, nil, string(attrsJSON), "{}",
		projectID, deploymentID, "cursor:usage:metrics", "gram-cursor")
	require.NoError(t, err)
}
