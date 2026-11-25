package activities_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/directoryreview"
	reviewrepo "github.com/speakeasy-api/gram/server/internal/directoryreview/repo"
	"github.com/speakeasy-api/gram/server/internal/email"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// mapUnheldAttribute maps an attribute value nobody holds and reconciles, so
// the organization gets one pending source_missing item.
func mapUnheldAttribute(t *testing.T, conn *pgxpool.Pool, organizationID, value string) {
	t.Helper()
	_, err := accessrepo.New(conn).InsertDirectoryAttributeRoleMapping(t.Context(), accessrepo.InsertDirectoryAttributeRoleMappingParams{
		OrganizationID: organizationID, AttributeKey: conv.ToPGText("department"), AttributeValue: conv.ToPGText(value),
		RoleUrn: "role:organization:00000000-0000-0000-0000-000000000001",
	})
	require.NoError(t, err)
	_, err = directoryreview.ReconcileOrganization(t.Context(), conn, organizationID)
	require.NoError(t, err)
}

func TestDirectoryReviewEmails_AnnounceThenRemind(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "directory_review_emails")
	require.NoError(t, err)
	organizationID := "org-directory-review-emails"
	adminEmail := seedCustomDomainAdminRecipient(t, conn, organizationID)
	mapUnheldAttribute(t, conn, organizationID, "Sales")
	start := time.Now()

	captured := &captureLoopsClient{sent: nil, failNext: 0}
	sender := activities.NewDirectoryReviewEmails(testenv.NewLogger(t), conn, email.NewService(testenv.NewLogger(t), captured, email.NewTemplateIDs(map[string]string{
		"directory_review_pending": "directory-review-pending-test-id",
	}), true), customDomainTestOrgHosts(t))
	send := func(at time.Time) {
		t.Helper()
		require.NoError(t, sender.Send(t.Context(), activities.SendDirectoryReviewEmailsArgs{Now: at}))
	}

	send(start.Add(5 * time.Minute))
	require.Empty(t, captured.Sent(), "new items wait out the quiet period")

	announcedAt := start.Add(activities.DirectoryReviewEmailQuietPeriod + time.Minute)
	send(announcedAt)
	sent := captured.Sent()
	require.Len(t, sent, 1)
	require.Equal(t, adminEmail, sent[0].Email)
	require.Equal(t, "directory-review-pending-test-id", sent[0].TransactionalID)
	require.NotEmpty(t, sent[0].IdempotencyKey)
	require.Equal(t, map[string]string{
		"organization_name":         "Test Org",
		"summary":                   "1 directory change needs a decision",
		"new_group_count":           "0",
		"new_attribute_value_count": "0",
		"source_missing_count":      "1",
		"review_url":                "https://app.example.com/" + organizationID + "/identity",
		"is_reminder":               "false",
	}, sent[0].DataVariables, "counts only: no attribute values or directory people")

	send(announcedAt.Add(time.Hour))
	require.Len(t, captured.Sent(), 1, "an announced item is not announced again")

	// Weekly reminders while the item stays pending, up to the cap.
	at := announcedAt
	for i := range activities.DirectoryReviewMaxReminders {
		at = at.Add(activities.DirectoryReviewReminderInterval)
		send(at)
		sent = captured.Sent()
		require.Len(t, sent, 2+i)
		require.Equal(t, "true", sent[len(sent)-1].DataVariables["is_reminder"])
		require.NotEqual(t, sent[len(sent)-2].IdempotencyKey, sent[len(sent)-1].IdempotencyKey)
	}
	send(at.Add(activities.DirectoryReviewReminderInterval))
	require.Len(t, captured.Sent(), 1+activities.DirectoryReviewMaxReminders, "reminders stop at the cap")

	// A new item is announced with every pending item counted, and restarts
	// the reminders.
	mapUnheldAttribute(t, conn, organizationID, "Marketing")
	send(time.Now().Add(activities.DirectoryReviewEmailQuietPeriod + time.Minute))
	sent = captured.Sent()
	require.Len(t, sent, 2+activities.DirectoryReviewMaxReminders)
	latest := sent[len(sent)-1].DataVariables
	require.Equal(t, "false", latest["is_reminder"])
	require.Equal(t, "2 directory changes need a decision", latest["summary"])
}

func TestDirectoryReviewEmails_SkipsOrganizationsThatOptedOut(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "directory_review_emails_opt_out")
	require.NoError(t, err)
	organizationID := "org-directory-review-emails-off"
	seedCustomDomainAdminRecipient(t, conn, organizationID)
	mapUnheldAttribute(t, conn, organizationID, "Sales")
	_, err = reviewrepo.New(conn).SetDirectoryReviewEmailsDisabled(t.Context(), reviewrepo.SetDirectoryReviewEmailsDisabledParams{
		OrganizationID: organizationID, Disabled: true,
	})
	require.NoError(t, err)

	captured := &captureLoopsClient{sent: nil, failNext: 0}
	sender := activities.NewDirectoryReviewEmails(testenv.NewLogger(t), conn, email.NewService(testenv.NewLogger(t), captured, email.NewTemplateIDs(map[string]string{
		"directory_review_pending": "directory-review-pending-test-id",
	}), true), customDomainTestOrgHosts(t))
	require.NoError(t, sender.Send(t.Context(), activities.SendDirectoryReviewEmailsArgs{Now: time.Now().Add(time.Hour)}))
	require.Empty(t, captured.Sent())
}
