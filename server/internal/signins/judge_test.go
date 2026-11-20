package signins

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/signins/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const workTenant = "00000000-0000-4000-8000-0000000000aa"

func eligibleRow(now time.Time) repo.GetSignInEligibilityRow {
	signIn := uuid.New()
	return repo.GetSignInEligibilityRow{
		UserSessionID:             uuid.New(),
		SubjectUrn:                urn.NewUserSubject("user-1").String(),
		LinkedSignInID:            uuid.NullUUID{UUID: signIn, Valid: true},
		SessionLive:               true,
		SignInID:                  uuid.NullUUID{UUID: signIn, Valid: true},
		SignInUserID:              pgtype.Text{String: "user-1", Valid: true},
		AuthenticationMethod:      pgtype.Text{String: MethodSSO, Valid: true},
		IdpHostedDomain:           pgtype.Text{String: "", Valid: false},
		IdpTenantID:               pgtype.Text{String: "", Valid: false},
		VerifiedEmail:             pgtype.Text{String: "jane@example.com", Valid: true},
		IdpAuthenticatedAt:        pgtype.Timestamptz{Time: now.Add(-time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		RevokedAt:                 pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		Fenced:                    false,
		UserEmail:                 pgtype.Text{String: "Jane@Example.com", Valid: true},
		VerifiedDomains:           []string{"example.com"},
		OrganizationDisabledAt:    pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		IsMember:                  true,
		IsDeprovisioned:           false,
		AllSignInMethodsAllowed:   true,
		AllowedSignInMethods:      []string{},
		AllowedMicrosoftTenantIds: []string{workTenant},
	}
}

// google records a Google sign-in whose hosted-domain lookup answered hd.
func google(r *repo.GetSignInEligibilityRow, hd string) {
	r.AuthenticationMethod.String = MethodGoogleOAuth
	r.IdpHostedDomain = pgtype.Text{String: hd, Valid: true}
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
		{name: "not linked", mutate: func(r *repo.GetSignInEligibilityRow) { r.LinkedSignInID.Valid = false }, want: ReasonNotLinked},
		{name: "sign-in in another organization", mutate: func(r *repo.GetSignInEligibilityRow) { r.SignInID.Valid = false }, want: ReasonNotLinked},
		{name: "agent subject", mutate: func(r *repo.GetSignInEligibilityRow) { r.SubjectUrn = urn.NewAgentSubject(uuid.New()).String() }, want: ReasonNotUserSubject},
		{name: "another user", mutate: func(r *repo.GetSignInEligibilityRow) { r.SubjectUrn = urn.NewUserSubject("user-2").String() }, want: ReasonNotUserSubject},
		{name: "revoked", mutate: func(r *repo.GetSignInEligibilityRow) { r.RevokedAt.Valid = true }, want: ReasonSignInRevoked},
		{name: "fenced by a later revocation cutoff", mutate: func(r *repo.GetSignInEligibilityRow) { r.Fenced = true }, want: ReasonSignInRevoked},
		{name: "unknown authentication time", mutate: func(r *repo.GetSignInEligibilityRow) { r.IdpAuthenticatedAt.Valid = false }, want: ReasonAuthTimeUnknown},
		{name: "email not verified at sign-in", mutate: func(r *repo.GetSignInEligibilityRow) { r.VerifiedEmail.Valid = false }, want: ReasonEmailNotVerified},
		{name: "profile email changed since sign-in", mutate: func(r *repo.GetSignInEligibilityRow) { r.UserEmail.String = "jane.doe@example.com" }, want: ReasonEmailChanged},
		{name: "impersonation", mutate: func(r *repo.GetSignInEligibilityRow) { r.AuthenticationMethod.String = "Impersonation" }, want: ReasonMethodNotEligible},
		{name: "github", mutate: func(r *repo.GetSignInEligibilityRow) { r.AuthenticationMethod.String = "GitHubOAuth" }, want: ReasonMethodNotEligible},
		{name: "sso too old", mutate: func(r *repo.GetSignInEligibilityRow) { r.IdpAuthenticatedAt.Time = now.Add(-MaxAge - time.Minute) }, want: ReasonExpired},
		{name: "google workspace account", mutate: func(r *repo.GetSignInEligibilityRow) { google(r, "Example.com") }, want: ReasonEligible},
		{name: "personal google account on the company email", mutate: func(r *repo.GetSignInEligibilityRow) { google(r, "") }, want: ReasonUnmanagedAccount},
		{name: "google workspace of an unverified domain", mutate: func(r *repo.GetSignInEligibilityRow) { google(r, "other.test") }, want: ReasonAccountDomainNotVerified},
		{name: "google evidence missing", mutate: func(r *repo.GetSignInEligibilityRow) {
			r.AuthenticationMethod.String = MethodGoogleOAuth
			r.IdpHostedDomain = pgtype.Text{String: "", Valid: false}
		}, want: ReasonAccountEvidenceUnavailable},
		{name: "google not allowed reads as the method, whatever the account", mutate: func(r *repo.GetSignInEligibilityRow) {
			google(r, "")
			r.AllSignInMethodsAllowed = false
			r.AllowedSignInMethods = []string{TierSSO}
		}, want: ReasonMethodNotAllowed},
		{name: "google older than a day", mutate: func(r *repo.GetSignInEligibilityRow) {
			google(r, "example.com")
			r.IdpAuthenticatedAt.Time = now.Add(-25 * time.Hour)
		}, want: ReasonExpired},
		{name: "microsoft work account", mutate: func(r *repo.GetSignInEligibilityRow) { microsoft(r, workTenant, "example.com") }, want: ReasonEligible},
		{name: "microsoft tenant in another case", mutate: func(r *repo.GetSignInEligibilityRow) { microsoft(r, strings.ToUpper(workTenant), "example.com") }, want: ReasonEligible},
		{name: "microsoft tenant the organization did not pin", mutate: func(r *repo.GetSignInEligibilityRow) {
			microsoft(r, "00000000-0000-4000-8000-0000000000bb", "example.com")
		}, want: ReasonTenantNotAllowed},
		{name: "microsoft with no pinned tenants", mutate: func(r *repo.GetSignInEligibilityRow) {
			microsoft(r, workTenant, "example.com")
			r.AllowedMicrosoftTenantIds = nil
		}, want: ReasonTenantNotAllowed},
		{name: "microsoft pin in a non-canonical form", mutate: func(r *repo.GetSignInEligibilityRow) {
			microsoft(r, workTenant, "example.com")
			r.AllowedMicrosoftTenantIds = []string{"urn:uuid:" + workTenant, "{" + workTenant + "}", strings.ReplaceAll(workTenant, "-", "")}
		}, want: ReasonTenantNotAllowed},
		{name: "microsoft pin that is not a tenant id", mutate: func(r *repo.GetSignInEligibilityRow) {
			microsoft(r, "tenant-1", "example.com")
			r.AllowedMicrosoftTenantIds = []string{"tenant-1"}
		}, want: ReasonTenantNotAllowed},
		{name: "personal microsoft account", mutate: func(r *repo.GetSignInEligibilityRow) { microsoft(r, MicrosoftConsumerTenantID, "example.com") }, want: ReasonUnmanagedAccount},
		{name: "microsoft without a tenant", mutate: func(r *repo.GetSignInEligibilityRow) { microsoft(r, "", "example.com") }, want: ReasonAccountEvidenceUnavailable},
		{name: "microsoft account of an unverified domain", mutate: func(r *repo.GetSignInEligibilityRow) { microsoft(r, workTenant, "other.test") }, want: ReasonAccountDomainNotVerified},
		{name: "microsoft evidence missing", mutate: func(r *repo.GetSignInEligibilityRow) { microsoft(r, "", "") }, want: ReasonAccountEvidenceUnavailable},
		{name: "microsoft tenant without a domain", mutate: func(r *repo.GetSignInEligibilityRow) { microsoft(r, workTenant, "") }, want: ReasonAccountEvidenceUnavailable},
		{name: "microsoft empty domain is never proof of a personal account", mutate: func(r *repo.GetSignInEligibilityRow) {
			microsoft(r, workTenant, "")
			r.IdpHostedDomain.Valid = true
		}, want: ReasonAccountEvidenceUnavailable},
		{name: "magic link", mutate: func(r *repo.GetSignInEligibilityRow) { r.AuthenticationMethod.String = MethodMagicAuth }, want: ReasonEligible},
		{name: "magic link older than four hours", mutate: func(r *repo.GetSignInEligibilityRow) {
			r.AuthenticationMethod.String = MethodMagicAuth
			r.IdpAuthenticatedAt.Time = now.Add(-5 * time.Hour)
		}, want: ReasonExpired},
		{name: "password", mutate: func(r *repo.GetSignInEligibilityRow) { r.AuthenticationMethod.String = MethodPassword }, want: ReasonEligible},
		{name: "password older than four hours", mutate: func(r *repo.GetSignInEligibilityRow) {
			r.AuthenticationMethod.String = MethodPassword
			r.IdpAuthenticatedAt.Time = now.Add(-5 * time.Hour)
		}, want: ReasonExpired},
		{name: "password turned off by the admin", mutate: func(r *repo.GetSignInEligibilityRow) {
			r.AuthenticationMethod.String = MethodPassword
			r.AllSignInMethodsAllowed = false
			r.AllowedSignInMethods = []string{TierSSO, TierGoogle, TierMicrosoft, TierMagicLink}
		}, want: ReasonMethodNotAllowed},
		{name: "tier turned off by the admin", mutate: func(r *repo.GetSignInEligibilityRow) {
			r.AuthenticationMethod.String = MethodMagicAuth
			r.AllSignInMethodsAllowed = false
			r.AllowedSignInMethods = []string{TierSSO}
		}, want: ReasonMethodNotAllowed},
		{name: "stored tier that names no tier", mutate: func(r *repo.GetSignInEligibilityRow) {
			r.AllSignInMethodsAllowed = false
			r.AllowedSignInMethods = []string{"SSO"}
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
		{name: "subdomain of a verified domain", mutate: func(r *repo.GetSignInEligibilityRow) {
			r.UserEmail.String = "jane@eu.example.com"
			r.VerifiedEmail.String = "jane@eu.example.com"
		}, want: ReasonEmailDomain},
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
	row.IdpAuthenticatedAt.Time = now.Add(-2 * time.Hour)
	require.True(t, judge(row, now, MaxAge).Eligible(), "MaxAge leaves the SSO tier's bound")
	require.True(t, judge(row, now, 0).Eligible(), "zero uses the tier's bound")
	require.Equal(t, ReasonExpired, judge(row, now, time.Hour).Reason, "a shorter cap wins")

	row.AuthenticationMethod.String = MethodMagicAuth
	row.IdpAuthenticatedAt.Time = now.Add(-5 * time.Hour)
	require.Equal(t, ReasonExpired, judge(row, now, MaxAge).Reason, "a longer cap never extends the tier")
}

func TestJudge_ReportsTheTier(t *testing.T) {
	t.Parallel()

	now := time.Now()
	row := eligibleRow(now)
	google(&row, "example.com")
	require.Equal(t, TierGoogle, judge(row, now, MaxAge).Identity.Tier)
}

// beforeReorderEligible is the account and method check as it was before the
// method's tier was checked first, kept to prove eligibility didn't change.
func beforeReorderEligible(row repo.GetSignInEligibilityRow) bool {
	var tier string
	switch row.AuthenticationMethod.String {
	case MethodSSO:
		tier = TierSSO
	case MethodGoogleOAuth:
		if !onVerifiedDomain(row.IdpHostedDomain.String, row.VerifiedDomains) {
			return false
		}
		tier = TierGoogle
	case MethodMicrosoftOAuth:
		tenant := row.IdpTenantID.String
		if tenant == "" || strings.EqualFold(tenant, MicrosoftConsumerTenantID) || !onVerifiedDomain(row.IdpHostedDomain.String, row.VerifiedDomains) {
			return false
		}
		if !tenantAllowed(tenant, row.AllowedMicrosoftTenantIds) {
			return false
		}
		tier = TierMicrosoft
	case MethodMagicAuth:
		tier = TierMagicLink
	case MethodPassword:
		tier = TierPassword
	default:
		return false
	}
	return tierAllowed(tier, row)
}

// Checking the method's tier first changes only the reason, never who is eligible.
func TestJudge_ReorderKeepsTheEligibleSet(t *testing.T) {
	t.Parallel()

	now := time.Now()
	domains := []pgtype.Text{{String: "", Valid: false}, {String: "", Valid: true}, {String: "example.com", Valid: true}, {String: "other.test", Valid: true}}
	tenants := []string{"", MicrosoftConsumerTenantID, workTenant, "00000000-0000-4000-8000-0000000000bb"}
	allowed := [][]string{nil, {}, {TierSSO}, {TierGoogle}, {TierMicrosoft}, {TierGoogle, TierMicrosoft, TierMagicLink, TierPassword}}
	methods := []string{MethodSSO, MethodGoogleOAuth, MethodMicrosoftOAuth, MethodMagicAuth, MethodPassword, "GitHubOAuth"}
	checked := 0
	for _, method := range methods {
		for _, domain := range domains {
			for _, tenant := range tenants {
				for _, tiers := range allowed {
					row := eligibleRow(now)
					row.AuthenticationMethod.String = method
					row.IdpHostedDomain = domain
					row.IdpTenantID = pgtype.Text{String: tenant, Valid: tenant != ""}
					row.AllSignInMethodsAllowed = tiers == nil
					row.AllowedSignInMethods = tiers
					got := judge(row, now, MaxAge)
					require.Equal(t, beforeReorderEligible(row), got.Eligible(), "%s domain=%+v tenant=%q tiers=%v reason=%s", method, domain, tenant, tiers, got.Reason)
					checked++
				}
			}
		}
	}
	require.Equal(t, len(methods)*len(domains)*len(tenants)*len(allowed), checked)
}
