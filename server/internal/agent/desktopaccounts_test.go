package agent_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	hooksrepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

type storedAccount struct {
	UserID      string
	OrgUUID     string
	AccountType string
	LastSeenAt  time.Time
}

// storedAccountFor reads the user_accounts row for a Claude login, reporting
// false when none exists.
func storedAccountFor(t *testing.T, ti *testInstance, accountUUID string) (storedAccount, bool) {
	t.Helper()
	row, err := hooksrepo.New(ti.conn).GetUserAccount(t.Context(), hooksrepo.GetUserAccountParams{
		OrganizationID:      ti.orgID,
		Provider:            "anthropic",
		ExternalAccountUuid: accountUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return storedAccount{}, false
	}
	require.NoError(t, err)
	return storedAccount{
		UserID:      row.UserID.String,
		OrgUUID:     row.ExternalOrgID.String,
		AccountType: row.AccountType.String,
		LastSeenAt:  row.LastSeenAt.Time,
	}, true
}

func mustStoredAccount(t *testing.T, ti *testInstance, accountUUID string) storedAccount {
	t.Helper()
	a, ok := storedAccountFor(t, ti, accountUUID)
	require.True(t, ok, "no user_accounts row for %s", accountUUID)
	return a
}

// seedEmployeeInOrg records another employee signed in to a provider org, the
// way an earlier scan or hook session from their machine would have.
func seedEmployeeInOrg(t *testing.T, ctx context.Context, ti *testInstance, orgUUID string) {
	t.Helper()
	id := "user_" + uuid.NewString()
	_, err := usersrepo.New(ti.conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          id,
		Email:       id + "@example.com",
		DisplayName: "Teammate",
		PhotoUrl:    pgtype.Text{},
		Admin:       false,
	})
	require.NoError(t, err)
	require.NoError(t, agentrepo.New(ti.conn).UpsertDesktopUserAccount(ctx, agentrepo.UpsertDesktopUserAccountParams{
		OrganizationID:      ti.orgID,
		Provider:            "anthropic",
		ExternalAccountUuid: uuid.NewString(),
		UserID:              pgtype.Text{String: id, Valid: true},
		ExternalOrgID:       pgtype.Text{String: orgUUID, Valid: true},
		AccountType:         pgtype.Text{},
		LastSeenAt:          pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}))
}

func reportAccounts(t *testing.T, ctx context.Context, ti *testInstance, accounts ...*gen.AIScanAccount) {
	t.Helper()
	now := time.Now().UTC()
	require.NoError(t, ti.service.ReportAIScan(ctx, &gen.ReportAIScanPayload{
		ScanStartedAt:     now.Add(-time.Minute).Format(time.RFC3339),
		ScanCompletedAt:   now.Format(time.RFC3339),
		TargetListVersion: 0,
		Matches:           []*gen.AIScanMatch{},
		Accounts:          accounts,
		Email:             nil,
		SerialNumber:      nil,
		Hostname:          nil,
	}))
}

func desktopAccount(accountUUID, orgUUID string, lastSeen time.Time) *gen.AIScanAccount {
	return &gen.AIScanAccount{
		Provider:    "anthropic",
		Surface:     "cowork",
		AccountUUID: accountUUID,
		OrgUUID:     orgUUID,
		LastSeenAt:  lastSeen.Format(time.RFC3339),
	}
}

// A login in the company org is team; a login in an org no one else uses is
// personal, because the same report shows the employee in the company org.
func TestReportAIScan_ClassifiesDesktopAccounts(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = withPerUserKeyAuth(t, ctx, "owner@example.com")
	authCtx, _ := contextvalues.GetAuthContext(ctx)

	companyOrg, personalOrg := uuid.NewString(), uuid.NewString()
	seedEmployeeInOrg(t, ctx, ti, companyOrg)

	workLogin, personalLogin := uuid.NewString(), uuid.NewString()
	seen := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	reportAccounts(t, ctx, ti,
		desktopAccount(workLogin, companyOrg, seen),
		desktopAccount(personalLogin, personalOrg, seen),
	)

	work := mustStoredAccount(t, ti, workLogin)
	require.Equal(t, authCtx.UserID, work.UserID)
	require.Equal(t, companyOrg, work.OrgUUID)
	require.Equal(t, "team", work.AccountType)
	personal := mustStoredAccount(t, ti, personalLogin)
	require.Equal(t, authCtx.UserID, personal.UserID)
	require.Equal(t, "personal", personal.AccountType)
	require.True(t, personal.LastSeenAt.Equal(seen))
}

// With no other employee seen in any org yet, the scan cannot tell which org
// is the company's, so accounts are stored unclassified.
func TestReportAIScan_LeavesDesktopAccountsUnclassifiedWithoutASharedOrg(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = withPerUserKeyAuth(t, ctx, "owner@example.com")

	login := uuid.NewString()
	reportAccounts(t, ctx, ti, desktopAccount(login, uuid.NewString(), time.Now().UTC()))

	require.Empty(t, mustStoredAccount(t, ti, login).AccountType)
}

// A login in several orgs keeps the one it was used in most recently, and a
// last-seen time from a skewed device clock never lands in the future.
func TestReportAIScan_KeepsMostRecentOrgPerDesktopLogin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = withPerUserKeyAuth(t, ctx, "owner@example.com")

	login, olderOrg, newerOrg := uuid.NewString(), uuid.NewString(), uuid.NewString()
	now := time.Now().UTC()
	reportAccounts(t, ctx, ti,
		desktopAccount(login, newerOrg, now.Add(48*time.Hour)),
		desktopAccount(login, olderOrg, now.Add(-48*time.Hour)),
	)

	stored := mustStoredAccount(t, ti, login)
	require.Equal(t, newerOrg, stored.OrgUUID)
	require.False(t, stored.LastSeenAt.After(time.Now().UTC()))
}

// The org install key attributes accounts to the connected member behind the
// vouched email; an email with no member records nothing but still accepts
// the scan.
func TestReportAIScan_DesktopAccountsUseVouchedMember(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	require.NotNil(t, authCtx.Email)

	login, strangerLogin := uuid.NewString(), uuid.NewString()
	now := time.Now().UTC()
	payload := func(email, account string) *gen.ReportAIScanPayload {
		return &gen.ReportAIScanPayload{
			ScanStartedAt:     now.Add(-time.Minute).Format(time.RFC3339),
			ScanCompletedAt:   now.Format(time.RFC3339),
			TargetListVersion: 0,
			Matches:           []*gen.AIScanMatch{},
			Accounts:          []*gen.AIScanAccount{desktopAccount(account, uuid.NewString(), now)},
			Email:             &email,
			SerialNumber:      nil,
			Hostname:          nil,
		}
	}

	require.NoError(t, ti.service.ReportAIScan(ctx, payload(*authCtx.Email, login)))
	require.NoError(t, ti.service.ReportAIScan(ctx, payload("stranger@example.com", strangerLogin)))

	require.Equal(t, authCtx.UserID, mustStoredAccount(t, ti, login).UserID)
	_, stored := storedAccountFor(t, ti, strangerLogin)
	require.False(t, stored)
}

// A pair from a surface this server does not know (a newer agent) is skipped
// without dropping the rest of the report.
func TestReportAIScan_SkipsUnknownDesktopSurfaces(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = withPerUserKeyAuth(t, ctx, "owner@example.com")

	known, unknown := uuid.NewString(), uuid.NewString()
	future := desktopAccount(unknown, uuid.NewString(), time.Now().UTC())
	future.Surface = "claude-chat-desktop"
	reportAccounts(t, ctx, ti, desktopAccount(known, uuid.NewString(), time.Now().UTC()), future)

	mustStoredAccount(t, ti, known)
	_, stored := storedAccountFor(t, ti, unknown)
	require.False(t, stored)
}
