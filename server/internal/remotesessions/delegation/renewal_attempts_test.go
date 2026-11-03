package delegation

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestRenewal_HeldAssertionRecordsNothing(t *testing.T) {
	t.Parallel()

	s, _, p, b, allow := newDelegationUnitFixture(t)
	rec := mcpconnectionattemptstest.New(t)
	s.connections = rec.Recorder
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "id", "refresh", time.Hour), true))

	_, err := s.Resolve(t.Context(), b, allow)
	require.NoError(t, err)
	rec.RequireNothing()
}

func TestRenewal_SuccessRecordsRenewed(t *testing.T) {
	t.Parallel()

	s, _, p, b, allow := newDelegationUnitFixture(t)
	rec := mcpconnectionattemptstest.New(t)
	s.connections = rec.Recorder
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second), true))
	s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
		return delegationRenewal(p, s.now(), "new-id", "new-refresh"), nil
	}

	_, err := s.Resolve(t.Context(), b, allow)
	require.NoError(t, err)

	row := rec.RequireEnded(mcpconnectionattempts.StepSignInRenewal, mcpconnectionattempts.ResultRenewed)
	require.Equal(t, b.OrganizationID, row.OrganizationID)
	require.Equal(t, "identity_provider", row.ServerKind)
	require.Equal(t, b.IssuerID.String(), row.RemoteSessionIssuerID)
	require.Equal(t, urn.NewUserSubject(b.HumanID).String(), row.SubjectURN)
}

func TestRenewal_NoIDTokenAsksForSignIn(t *testing.T) {
	t.Parallel()

	s, _, p, b, allow := newDelegationUnitFixture(t)
	rec := mcpconnectionattemptstest.New(t)
	s.connections = rec.Recorder
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second), true))
	s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
		return delegationRenewal(p, s.now(), "", "new-refresh"), nil
	}

	_, err := s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrReauthentication)
	rec.RequireEnded(mcpconnectionattempts.StepSignInRenewal, mcpconnectionattempts.CodeReauthenticationRequired)
}

func TestRenewal_FailuresKeepWhoFixes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		kind RefreshFailure
		want mcpconnectionattempts.Code
		who  mcpconnectionattempts.WhoFixes
		rows int
	}{
		{name: "invalid grant", kind: RefreshInvalidGrant, want: mcpconnectionattempts.CodeInvalidGrant, who: mcpconnectionattempts.WhoPerson, rows: 1},
		{name: "configuration", kind: RefreshConfiguration, want: mcpconnectionattempts.CodeRenewalMisconfigured, who: mcpconnectionattempts.WhoAdmin, rows: 2},
		{name: "invalid identity", kind: RefreshInvalidIdentity, want: mcpconnectionattempts.CodeInvalidIdentity, who: mcpconnectionattempts.WhoAdmin, rows: 2},
		{name: "retryable", kind: RefreshRetryable, want: mcpconnectionattempts.CodeRenewalRetryable, who: mcpconnectionattempts.WhoProvider, rows: 1},
		{name: "ambiguous", kind: RefreshAmbiguous, want: mcpconnectionattempts.CodeRenewalAmbiguous, who: mcpconnectionattempts.WhoPerson, rows: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s, _, p, b, allow := newDelegationUnitFixture(t)
			rec := mcpconnectionattemptstest.New(t)
			s.connections = rec.Recorder
			require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second), true))
			s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
				return nil, &RefreshError{Kind: tc.kind}
			}

			_, err := s.Resolve(t.Context(), b, allow)
			require.Error(t, err)
			row := rec.RequireEnded(mcpconnectionattempts.StepSignInRenewal, tc.want)
			require.Equal(t, string(tc.who), row.WhoFixes)

			// A later call adds a row only for a broken setup, which the limiter holds to a trickle.
			_, err = s.Resolve(t.Context(), b, allow)
			require.Error(t, err)
			require.Len(t, rec.Rows(), tc.rows)
		})
	}
}

func TestRenewal_OfflineRefusalIsRecorded(t *testing.T) {
	t.Parallel()

	s, _, p, b, _ := newDelegationUnitFixture(t)
	rec := mcpconnectionattemptstest.New(t)
	s.connections = rec.Recorder

	require.NoError(t, s.RecordOfflineRefusal(t.Context(), p, b.HumanID))
	row := rec.RequireEnded(mcpconnectionattempts.StepSignInRenewal, mcpconnectionattempts.CodeRenewalRefused)
	require.Equal(t, string(mcpconnectionattempts.WhoPerson), row.WhoFixes)
}

// A setup broken before any POST still shows up.
func TestRenewal_ChangedConfigurationIsRecorded(t *testing.T) {
	t.Parallel()

	s, _, p, b, allow := newDelegationUnitFixture(t)
	rec := mcpconnectionattemptstest.New(t)
	s.connections = rec.Recorder
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "id", "refresh", time.Hour), true))
	s.loadBinding = func(context.Context, string, uuid.UUID, uuid.UUID) (Provider, error) { return nil, ErrConfiguration }

	_, err := s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrConfiguration)
	row := rec.RequireEnded(mcpconnectionattempts.StepSignInRenewal, mcpconnectionattempts.CodeRenewalMisconfigured)
	require.Equal(t, string(mcpconnectionattempts.WhoAdmin), row.WhoFixes)
}

// An unproven caller does not reach the row: authorization comes first.
func TestRenewal_UnauthorizedCallerRecordsNothing(t *testing.T) {
	t.Parallel()

	s, _, _, b, _ := newDelegationUnitFixture(t)
	rec := mcpconnectionattemptstest.New(t)
	s.connections = rec.Recorder
	denied := delegationTestAuthority(func(context.Context, Binding) error { return ErrConfiguration })

	_, err := s.Resolve(t.Context(), b, denied)
	require.Error(t, err)
	rec.RequireNothing()
}
