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
		UserSessionID:          uuid.New(),
		SubjectUrn:             urn.NewUserSubject("user-1").String(),
		SsoSignInID:            uuid.NullUUID{UUID: signIn, Valid: true},
		SessionLive:            true,
		SignInID:               uuid.NullUUID{UUID: signIn, Valid: true},
		SignInUserID:           pgtype.Text{String: "user-1", Valid: true},
		AuthenticationMethod:   pgtype.Text{String: MethodSSO, Valid: true},
		AuthenticatedAt:        pgtype.Timestamptz{Time: now.Add(-time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		RevokedAt:              pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		UserEmail:              pgtype.Text{String: "Jane@Example.com", Valid: true},
		VerifiedDomains:        []string{"example.com"},
		OrganizationDisabledAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		IsMember:               true,
		IsDeprovisioned:        false,
	}
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
		{name: "magic auth", mutate: func(r *repo.GetSignInEligibilityRow) { r.AuthenticationMethod.String = "MagicAuth" }, want: ReasonNotSSO},
		{name: "impersonation", mutate: func(r *repo.GetSignInEligibilityRow) { r.AuthenticationMethod.String = "Impersonation" }, want: ReasonNotSSO},
		{name: "too old", mutate: func(r *repo.GetSignInEligibilityRow) { r.AuthenticatedAt.Time = now.Add(-MaxAge - time.Minute) }, want: ReasonExpired},
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
