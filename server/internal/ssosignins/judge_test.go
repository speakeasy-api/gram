package ssosignins

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/ssosignins/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func eligibleRow(now time.Time) repo.GetSignInEligibilityRow {
	signIn := uuid.New()
	return repo.GetSignInEligibilityRow{
		UserSessionID:           uuid.New(),
		SubjectUrn:              urn.NewUserSubject("user-1").String(),
		SsoSignInID:             uuid.NullUUID{UUID: signIn, Valid: true},
		SessionLive:             true,
		SignInID:                uuid.NullUUID{UUID: signIn, Valid: true},
		SignInUserID:            pgtype.Text{String: "user-1", Valid: true},
		AuthenticationMethod:    pgtype.Text{String: MethodSSO, Valid: true},
		MfaVerified:             pgtype.Bool{Bool: false, Valid: true},
		IdpHostedDomain:         pgtype.Text{String: "", Valid: false},
		IdpTenantID:             pgtype.Text{String: "", Valid: false},
		AuthenticatedAt:         pgtype.Timestamptz{Time: now.Add(-time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		RevokedAt:               pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		UserEmail:               pgtype.Text{String: "Jane@Example.com", Valid: true},
		VerifiedDomains:         []string{"example.com"},
		OrganizationDisabledAt:  pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		IsMember:                true,
		IsDeprovisioned:         false,
		AllSignInMethodsAllowed: true,
		AllowedSignInMethods:    []string{},
	}
}

func google(r *repo.GetSignInEligibilityRow, hd string) {
	r.AuthenticationMethod.String = MethodGoogleOAuth
	r.IdpHostedDomain = pgtype.Text{String: hd, Valid: hd != ""}
}

func microsoft(r *repo.GetSignInEligibilityRow, tenant, domain string) {
	r.AuthenticationMethod.String = MethodMicrosoftOAuth
	r.IdpTenantID = pgtype.Text{String: tenant, Valid: tenant != ""}
	r.IdpHostedDomain = pgtype.Text{String: domain, Valid: domain != ""}
}

func TestJudge(t *testing.T) {
	t.Parallel()

	now := time.Now()
	for _, tc := range []struct {
		name   string
		mutate func(*repo.GetSignInEligibilityRow)
		want   Reason
	}{
		{name: "eligible", mutate: func(*repo.GetSignInEligibilityRow) {}, want: ReasonEligible},
		{name: "expired session", mutate: func(r *repo.GetSignInEligibilityRow) { r.SessionLive = false }, want: ReasonSessionExpired},
		{name: "not linked", mutate: func(r *repo.GetSignInEligibilityRow) { r.SsoSignInID.Valid = false }, want: ReasonNotLinked},
		{name: "sign-in in another organization", mutate: func(r *repo.GetSignInEligibilityRow) { r.SignInID.Valid = false }, want: ReasonNotLinked},
		{name: "agent subject", mutate: func(r *repo.GetSignInEligibilityRow) { r.SubjectUrn = urn.NewAgentSubject(uuid.New()).String() }, want: ReasonNotUserSubject},
		{name: "another user", mutate: func(r *repo.GetSignInEligibilityRow) { r.SubjectUrn = urn.NewUserSubject("user-2").String() }, want: ReasonNotUserSubject},
		{name: "revoked", mutate: func(r *repo.GetSignInEligibilityRow) { r.RevokedAt.Valid = true }, want: ReasonSignInRevoked},
		{name: "impersonation", mutate: func(r *repo.GetSignInEligibilityRow) { r.AuthenticationMethod.String = "Impersonation" }, want: ReasonMethodNotEligible},
		{name: "github", mutate: func(r *repo.GetSignInEligibilityRow) { r.AuthenticationMethod.String = "GitHubOAuth" }, want: ReasonMethodNotEligible},
		{name: "sso too old", mutate: func(r *repo.GetSignInEligibilityRow) { r.AuthenticatedAt.Time = now.Add(-MaxAge - time.Minute) }, want: ReasonExpired},
		{name: "google workspace account", mutate: func(r *repo.GetSignInEligibilityRow) { google(r, "Example.com") }, want: ReasonEligible},
		{name: "personal google account on the company email", mutate: func(r *repo.GetSignInEligibilityRow) { google(r, "") }, want: ReasonUnmanagedAccount},
		{name: "google workspace of another domain", mutate: func(r *repo.GetSignInEligibilityRow) { google(r, "other.test") }, want: ReasonUnmanagedAccount},
		{name: "google older than a day", mutate: func(r *repo.GetSignInEligibilityRow) {
			google(r, "example.com")
			r.AuthenticatedAt.Time = now.Add(-25 * time.Hour)
		}, want: ReasonExpired},
		{name: "microsoft work account", mutate: func(r *repo.GetSignInEligibilityRow) { microsoft(r, "tenant-1", "example.com") }, want: ReasonEligible},
		{name: "personal microsoft account", mutate: func(r *repo.GetSignInEligibilityRow) { microsoft(r, MicrosoftConsumerTenantID, "example.com") }, want: ReasonUnmanagedAccount},
		{name: "microsoft without a tenant", mutate: func(r *repo.GetSignInEligibilityRow) { microsoft(r, "", "example.com") }, want: ReasonUnmanagedAccount},
		{name: "microsoft account of another domain", mutate: func(r *repo.GetSignInEligibilityRow) { microsoft(r, "tenant-1", "other.test") }, want: ReasonUnmanagedAccount},
		{name: "magic link", mutate: func(r *repo.GetSignInEligibilityRow) { r.AuthenticationMethod.String = MethodMagicAuth }, want: ReasonEligible},
		{name: "magic link older than four hours", mutate: func(r *repo.GetSignInEligibilityRow) {
			r.AuthenticationMethod.String = MethodMagicAuth
			r.AuthenticatedAt.Time = now.Add(-5 * time.Hour)
		}, want: ReasonExpired},
		{name: "password with mfa", mutate: func(r *repo.GetSignInEligibilityRow) {
			r.AuthenticationMethod.String = MethodPassword
			r.MfaVerified.Bool = true
		}, want: ReasonEligible},
		{name: "password without mfa", mutate: func(r *repo.GetSignInEligibilityRow) { r.AuthenticationMethod.String = MethodPassword }, want: ReasonMFARequired},
		{name: "password with unknown mfa", mutate: func(r *repo.GetSignInEligibilityRow) {
			r.AuthenticationMethod.String = MethodPassword
			r.MfaVerified = pgtype.Bool{Bool: true, Valid: false}
		}, want: ReasonMFARequired},
		{name: "tier turned off by the admin", mutate: func(r *repo.GetSignInEligibilityRow) {
			r.AuthenticationMethod.String = MethodMagicAuth
			r.AllSignInMethodsAllowed = false
			r.AllowedSignInMethods = []string{TierSSO}
		}, want: ReasonMethodNotAllowed},
		{name: "every tier turned off", mutate: func(r *repo.GetSignInEligibilityRow) {
			r.AllSignInMethodsAllowed = false
			r.AllowedSignInMethods = []string{}
		}, want: ReasonMethodNotAllowed},
		{name: "tier the admin kept", mutate: func(r *repo.GetSignInEligibilityRow) {
			r.AllSignInMethodsAllowed = false
			r.AllowedSignInMethods = []string{TierSSO}
		}, want: ReasonEligible},
		{name: "organization disabled", mutate: func(r *repo.GetSignInEligibilityRow) { r.OrganizationDisabledAt.Valid = true }, want: ReasonOrganizationDisabled},
		{name: "not a member", mutate: func(r *repo.GetSignInEligibilityRow) { r.IsMember = false }, want: ReasonNotMember},
		{name: "deleted user", mutate: func(r *repo.GetSignInEligibilityRow) { r.UserEmail.Valid = false }, want: ReasonNotMember},
		{name: "deprovisioned", mutate: func(r *repo.GetSignInEligibilityRow) { r.IsDeprovisioned = true }, want: ReasonDeprovisioned},
		{name: "unverified domain", mutate: func(r *repo.GetSignInEligibilityRow) { r.VerifiedDomains = []string{"other.test"} }, want: ReasonEmailDomain},
		{name: "subdomain of a verified domain", mutate: func(r *repo.GetSignInEligibilityRow) { r.UserEmail.String = "jane@eu.example.com" }, want: ReasonEmailDomain},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			row := eligibleRow(now)
			tc.mutate(&row)
			got := judge(row, now, MaxAge)
			require.Equal(t, tc.want, got.Reason)
			if tc.want == ReasonEligible {
				require.True(t, got.Eligible())
				require.Equal(t, "jane@example.com", got.Identity.Email)
				require.Equal(t, "user-1", got.Identity.UserID)
			} else {
				require.Nil(t, got.Identity)
			}
		})
	}
}

func TestJudge_MaxAgeCapsTheTier(t *testing.T) {
	t.Parallel()

	now := time.Now()
	row := eligibleRow(now)
	row.AuthenticatedAt.Time = now.Add(-2 * time.Hour)
	require.True(t, judge(row, now, MaxAge).Eligible(), "MaxAge leaves the SSO tier's bound")
	require.True(t, judge(row, now, 0).Eligible(), "zero uses the tier's bound")
	require.Equal(t, ReasonExpired, judge(row, now, time.Hour).Reason, "a shorter cap wins")

	row.AuthenticationMethod.String = MethodMagicAuth
	row.AuthenticatedAt.Time = now.Add(-5 * time.Hour)
	require.Equal(t, ReasonExpired, judge(row, now, MaxAge).Reason, "a longer cap never extends the tier")
}

func TestJudge_ReportsTheTier(t *testing.T) {
	t.Parallel()

	now := time.Now()
	row := eligibleRow(now)
	google(&row, "example.com")
	require.Equal(t, TierGoogle, judge(row, now, MaxAge).Identity.Tier)
}
