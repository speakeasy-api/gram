package delegation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// newRenewalFixture retains a sign-in whose assertion lasts ttl and records to a test recorder.
func newRenewalFixture(t *testing.T, ttl time.Duration) (*Service, *delegationMemoryStore, *fixtureProvider, Binding, Authorizer, *mcpconnectionattemptstest.Recording) {
	t.Helper()
	s, store, p, b, allow := newDelegationUnitFixture(t)
	rec := mcpconnectionattemptstest.New(t)
	s.connections = rec.Recorder
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", ttl), true))
	return s, store, p, b, allow, rec
}

func TestRenewal_HeldAssertionRecordsNothing(t *testing.T) {
	t.Parallel()

	s, _, _, b, allow, rec := newRenewalFixture(t, time.Hour)

	_, err := s.Resolve(t.Context(), b, allow)
	require.NoError(t, err)
	rec.RequireNothing()
}

func TestRenewal_SuccessRecordsRenewed(t *testing.T) {
	t.Parallel()

	s, _, p, b, allow, rec := newRenewalFixture(t, 30*time.Second)
	s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
		return delegationRenewal(p, s.now(), "new-id", "new-refresh"), nil
	}

	_, err := s.Resolve(t.Context(), b, allow)
	require.NoError(t, err)

	row := rec.RequireEnded(mcpconnectionattempts.StepSignInRenewal, mcpconnectionattempts.ResultRenewed)
	require.Equal(t, b.OrganizationID, row.OrganizationID)
	require.Equal(t, "identity_provider", row.ServerKind)
	require.Equal(t, b.IssuerID.String(), row.RemoteSessionIssuerID)
	require.Equal(t, b.ClientID.String(), row.RemoteSessionClientID)
	require.Equal(t, urn.NewUserSubject(b.HumanID).String(), row.SubjectURN)
}

func TestRenewal_NoIDTokenAsksForSignIn(t *testing.T) {
	t.Parallel()

	s, _, p, b, allow, rec := newRenewalFixture(t, 30*time.Second)
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
	}{
		{name: "invalid grant", kind: RefreshInvalidGrant, want: mcpconnectionattempts.CodeReauthenticationRequired, who: mcpconnectionattempts.WhoPerson},
		{name: "configuration", kind: RefreshConfiguration, want: mcpconnectionattempts.CodeRenewalMisconfigured, who: mcpconnectionattempts.WhoAdmin},
		{name: "client rejected", kind: RefreshClientRejected, want: mcpconnectionattempts.CodeRenewalMisconfigured, who: mcpconnectionattempts.WhoAdmin},
		{name: "setup", kind: RefreshSetup, want: mcpconnectionattempts.CodeRenewalMisconfigured, who: mcpconnectionattempts.WhoAdmin},
		{name: "setup retryable", kind: RefreshSetupRetryable, want: mcpconnectionattempts.CodeRenewalRetryable, who: mcpconnectionattempts.WhoProvider},
		{name: "invalid identity", kind: RefreshInvalidIdentity, want: mcpconnectionattempts.CodeInvalidIdentity, who: mcpconnectionattempts.WhoAdmin},
		{name: "retryable", kind: RefreshRetryable, want: mcpconnectionattempts.CodeRenewalRetryable, who: mcpconnectionattempts.WhoProvider},
		{name: "ambiguous", kind: RefreshAmbiguous, want: mcpconnectionattempts.CodeRenewalAmbiguous, who: mcpconnectionattempts.WhoPerson},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s, _, _, b, allow, rec := newRenewalFixture(t, 30*time.Second)
			s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
				return nil, &RefreshError{Kind: tc.kind}
			}

			_, err := s.Resolve(t.Context(), b, allow)
			require.Error(t, err)
			row := rec.RequireEnded(mcpconnectionattempts.StepSignInRenewal, tc.want)
			require.Equal(t, string(tc.who), row.WhoFixes)

			// The failure was recorded when it happened, so a later call adds nothing.
			_, err = s.Resolve(t.Context(), b, allow)
			require.Error(t, err)
			require.Len(t, rec.Rows(), 1)
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

	s, _, _, b, allow, rec := newRenewalFixture(t, time.Hour)
	s.loadBinding = func(context.Context, string, uuid.UUID, uuid.UUID) (Provider, error) { return nil, ErrConfiguration }

	_, err := s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrConfiguration)
	row := rec.RequireEnded(mcpconnectionattempts.StepSignInRenewal, mcpconnectionattempts.CodeRenewalMisconfigured)
	require.Equal(t, string(mcpconnectionattempts.WhoAdmin), row.WhoFixes)
}

// A setup changed since the sign-in is the person's to fix: signing in again binds the new setup.
func TestRenewal_SignInFromAnOlderSetupAsksForSignIn(t *testing.T) {
	t.Parallel()

	s, _, p, b, allow, rec := newRenewalFixture(t, time.Hour)
	changed := *p
	changed.client.ClientID += "-changed"
	require.NotEqual(t, p.DelegationConfigurationHash(), changed.DelegationConfigurationHash())
	s.loadBinding = func(context.Context, string, uuid.UUID, uuid.UUID) (Provider, error) { return &changed, nil }

	_, err := s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrConfiguration)
	row := rec.RequireEnded(mcpconnectionattempts.StepSignInRenewal, mcpconnectionattempts.CodeReauthenticationRequired)
	require.Equal(t, string(mcpconnectionattempts.WhoPerson), row.WhoFixes)

	require.NoError(t, s.RetainVerifiedLogin(t.Context(), &changed, b.HumanID, delegationLogin(&changed, s.now(), "new-id", "new-refresh", time.Hour), true))
	_, err = s.Resolve(t.Context(), b, allow)
	require.NoError(t, err, "the catalog says signing in again fixes this")
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

func TestRenewal_RegistrationChangedDuringRefreshRecordsSignInRequired(t *testing.T) {
	t.Parallel()

	s, _, p, b, allow, rec := newRenewalFixture(t, 30*time.Second)
	s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
		s.loadBinding = func(context.Context, string, uuid.UUID, uuid.UUID) (Provider, error) {
			changed := *p
			changed.client.ClientID = "changed-client"
			return &changed, nil
		}
		return delegationRenewal(p, s.now(), "new-id", "new-refresh"), nil
	}

	_, err := s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrStaleRegistration)
	row := rec.RequireEnded(mcpconnectionattempts.StepSignInRenewal, mcpconnectionattempts.CodeReauthenticationRequired)
	require.Equal(t, b.IssuerID.String(), row.RemoteSessionIssuerID)
	require.Equal(t, b.ClientID.String(), row.RemoteSessionClientID)
}

func TestRenewal_PostClaimSetupRecordsExactlyOnce(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		providerErr error
		nilProvider bool
		changed     bool
		secret      string
		wantErr     error
		want        mcpconnectionattempts.Code
	}{
		{name: "provider configuration", providerErr: ErrConfiguration, wantErr: ErrConfiguration, want: mcpconnectionattempts.CodeRenewalMisconfigured},
		{name: "provider reauthentication", providerErr: ErrReauthentication, wantErr: ErrConfiguration, want: mcpconnectionattempts.CodeRenewalMisconfigured},
		{name: "provider missing", providerErr: pgx.ErrNoRows, wantErr: ErrConfiguration, want: mcpconnectionattempts.CodeRenewalMisconfigured},
		{name: "provider unavailable", providerErr: ErrProviderUnavailable, wantErr: ErrTemporary, want: mcpconnectionattempts.CodeRenewalRetryable},
		{name: "local provider load failure", providerErr: errors.New("database unavailable"), wantErr: ErrTemporary, want: mcpconnectionattempts.CodeInternalError},
		{name: "nil provider", nilProvider: true, wantErr: ErrConfiguration, want: mcpconnectionattempts.CodeRenewalMisconfigured},
		{name: "registration changed", changed: true, wantErr: ErrStaleRegistration, want: mcpconnectionattempts.CodeReauthenticationRequired},
		{name: "empty retained refresh", secret: "refresh", wantErr: ErrConfiguration, want: mcpconnectionattempts.CodeReauthenticationRequired},
		{name: "missing retained subject", secret: "subject", wantErr: ErrConfiguration, want: mcpconnectionattempts.CodeReauthenticationRequired},
		{name: "unreadable retained nonce", secret: "nonce", wantErr: ErrConfiguration, want: mcpconnectionattempts.CodeInternalError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, store, p, b, allow, rec := newRenewalFixture(t, 30*time.Second)
			c, err := store.load(t.Context(), b)
			require.NoError(t, err)
			switch tc.secret {
			case "refresh":
				c.refresh, err = s.enc.Encrypt([]byte(""))
				require.NoError(t, err)
			case "subject":
				c.subject = ""
			case "nonce":
				c.nonce = "invalid-ciphertext"
			}
			store.rows[b] = c
			s.loadProvider = func(context.Context, string, uuid.UUID, uuid.UUID) (Provider, error) {
				if tc.providerErr != nil || tc.nilProvider {
					return nil, tc.providerErr
				}
				if tc.changed {
					changed := *p
					changed.client.ClientID = "changed-client"
					return &changed, nil
				}
				return p, nil
			}
			s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
				t.Error("setup failure must not start a refresh")
				return nil, ErrTemporary
			}
			_, err = s.Resolve(t.Context(), b, allow)
			require.ErrorIs(t, err, tc.wantErr)
			row := rec.RequireEnded(mcpconnectionattempts.StepSignInRenewal, tc.want)
			require.Len(t, rec.Rows(), 1, "exactly one outcome per setup failure")
			require.Equal(t, b.IssuerID.String(), row.RemoteSessionIssuerID)
			require.Equal(t, b.ClientID.String(), row.RemoteSessionClientID)
			after, err := store.load(t.Context(), b)
			require.NoError(t, err)
			require.Equal(t, uuid.Nil, after.claim, "release the unspent claim")
			require.Equal(t, 1, store.claims)
		})
	}
}

// A provider outage after the claim backs off, so the retry neither repeats discovery nor adds a row.
func TestRenewal_ProviderOutageBacksOff(t *testing.T) {
	t.Parallel()
	s, store, p, b, allow, rec := newRenewalFixture(t, 30*time.Second)
	loads := 0
	s.loadProvider = func(context.Context, string, uuid.UUID, uuid.UUID) (Provider, error) {
		loads++
		return nil, ErrProviderUnavailable
	}
	for range 2 {
		_, err := s.Resolve(t.Context(), b, allow)
		require.ErrorIs(t, err, ErrTemporary)
	}
	require.Equal(t, 1, loads)
	row := rec.RequireEnded(mcpconnectionattempts.StepSignInRenewal, mcpconnectionattempts.CodeRenewalRetryable)
	require.Equal(t, string(mcpconnectionattempts.WhoProvider), row.WhoFixes)
	require.Len(t, rec.Rows(), 1)
	after := store.rows[b]
	require.Equal(t, uuid.Nil, after.claim)
	require.Equal(t, "temporary_failure", after.status)
	require.Equal(t, s.now().Add(time.Minute), after.retryAfter)
	require.Equal(t, "old-refresh", delegationPlain(t, s, after.refresh))

	s.loadProvider = func(context.Context, string, uuid.UUID, uuid.UUID) (Provider, error) { return p, nil }
	s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
		return delegationRenewal(p, s.now(), "new-id", "new-refresh"), nil
	}
	later := s.now().Add(time.Minute + time.Second)
	s.now = func() time.Time { return later }
	_, err := s.Resolve(t.Context(), b, allow)
	require.NoError(t, err)
}

// A backoff on a retained failure would turn the temporary answer into a misconfigured one.
func TestRenewal_ProviderOutageKeepsRetainedFailureAnswer(t *testing.T) {
	t.Parallel()
	s, store, _, b, allow, rec := newRenewalFixture(t, 30*time.Second)
	c := store.rows[b]
	c.status = "configuration_failure"
	c.retryAfter = s.now().Add(-time.Second)
	store.rows[b] = c
	s.loadProvider = func(context.Context, string, uuid.UUID, uuid.UUID) (Provider, error) {
		return nil, ErrProviderUnavailable
	}
	_, err := s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrTemporary)
	rec.RequireEnded(mcpconnectionattempts.StepSignInRenewal, mcpconnectionattempts.CodeRenewalRetryable)
	after := store.rows[b]
	require.Equal(t, uuid.Nil, after.claim)
	require.Equal(t, c.status, after.status)
	require.Equal(t, c.retryAfter, after.retryAfter)
}

// resolveBeforePOST resolves a renewal that must stop before the refresh POST.
func resolveBeforePOST(t *testing.T, s *Service, store *delegationMemoryStore, b Binding, allow Authorizer) {
	t.Helper()
	s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
		t.Error("pre-POST failure must not refresh")
		return nil, ErrTemporary
	}
	_, err := s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrTemporary)
	require.Equal(t, "old-refresh", delegationPlain(t, s, store.rows[b].refresh))
}

func requireOneInternalError(t *testing.T, rec *mcpconnectionattemptstest.Recording, b Binding) {
	t.Helper()
	row := rec.RequireEnded(mcpconnectionattempts.StepSignInRenewal, mcpconnectionattempts.CodeInternalError)
	require.Equal(t, string(mcpconnectionattempts.WhoSpeakeasy), row.WhoFixes)
	require.Equal(t, b.IssuerID.String(), row.RemoteSessionIssuerID)
	require.Equal(t, b.ClientID.String(), row.RemoteSessionClientID)
	require.Len(t, rec.Rows(), 1)
}

func TestRenewal_RereadFailureRecordsInternalError(t *testing.T) {
	t.Parallel()
	s, store, _, b, allow, rec := newRenewalFixture(t, 30*time.Second)
	s.store = &delegationRereadHookStore{delegationStore: store, beforeReread: func() {}, rereadError: errors.New("database unavailable")}
	resolveBeforePOST(t, s, store, b, allow)
	requireOneInternalError(t, rec, b)
	require.Equal(t, uuid.Nil, store.rows[b].claim)
}

func TestRenewal_MarkFailureRecordsInternalError(t *testing.T) {
	t.Parallel()
	s, store, _, b, allow, rec := newRenewalFixture(t, 30*time.Second)
	store.attemptError = errors.New("database unavailable")
	resolveBeforePOST(t, s, store, b, allow)
	requireOneInternalError(t, rec, b)
	require.Equal(t, uuid.Nil, store.rows[b].claim)
}

func TestRenewal_SupersededClaimRecordsNothing(t *testing.T) {
	t.Parallel()
	s, store, _, b, allow, rec := newRenewalFixture(t, 30*time.Second)
	s.store = &delegationRereadHookStore{delegationStore: store, beforeReread: func() {
		c := store.rows[b]
		c.claim = uuid.New()
		c.generation++
		store.rows[b] = c
	}}
	resolveBeforePOST(t, s, store, b, allow)
	rec.RequireNothing()
}

func TestRenewal_MarkConflictRecordsNothing(t *testing.T) {
	t.Parallel()
	s, store, _, b, allow, rec := newRenewalFixture(t, 30*time.Second)
	store.attemptConflict = true
	resolveBeforePOST(t, s, store, b, allow)
	rec.RequireNothing()
	require.Equal(t, uuid.Nil, store.rows[b].claim)
}

// renewThroughFault runs a successful refresh POST with fault applied during it.
func renewThroughFault(t *testing.T, s *Service, p Provider, b Binding, allow Authorizer, fault func()) {
	t.Helper()
	s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
		fault()
		return delegationRenewal(p, s.now(), "new-id", "new-refresh"), nil
	}
	_, err := s.Resolve(t.Context(), b, allow)
	require.Error(t, err)
}

func requireSpentClaimHeld(t *testing.T, s *Service, store *delegationMemoryStore, b Binding) {
	t.Helper()
	require.NotEqual(t, uuid.Nil, store.rows[b].claim, "never release a potentially spent refresh")
	require.Equal(t, "old-refresh", delegationPlain(t, s, store.rows[b].refresh))
}

func TestRenewal_PanicAfterPOSTRecordsInternalError(t *testing.T) {
	t.Parallel()
	s, store, _, b, allow, rec := newRenewalFixture(t, 30*time.Second)
	s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
		panic("fixture refresh panic")
	}
	require.PanicsWithValue(t, "fixture refresh panic", func() { _, _ = s.Resolve(t.Context(), b, allow) })
	requireOneInternalError(t, rec, b)
	requireSpentClaimHeld(t, s, store, b)
}

func TestRenewal_EncryptionFaultAfterPOSTRecordsInternalError(t *testing.T) {
	t.Parallel()
	s, store, p, b, allow, rec := newRenewalFixture(t, 30*time.Second)
	original := s.enc
	renewThroughFault(t, s, p, b, allow, func() { s.enc = &encryption.Client{} })
	s.enc = original
	requireOneInternalError(t, rec, b)
	requireSpentClaimHeld(t, s, store, b)
}

func TestRenewal_FinishFailureAfterPOSTRecordsInternalError(t *testing.T) {
	t.Parallel()
	s, store, p, b, allow, rec := newRenewalFixture(t, 30*time.Second)
	renewThroughFault(t, s, p, b, allow, func() { store.finishError = errors.New("fixture store unavailable") })
	requireOneInternalError(t, rec, b)
	requireSpentClaimHeld(t, s, store, b)
}

func TestRenewal_AssertionDecryptFaultRecordsInternalError(t *testing.T) {
	t.Parallel()
	s, store, p, b, allow, rec := newRenewalFixture(t, 30*time.Second)
	renewThroughFault(t, s, p, b, allow, func() {
		s.store = renewalFinishHookStore{delegationStore: store, after: func() { s.enc = &encryption.Client{} }}
	})
	requireOneInternalError(t, rec, b)
}

func TestRenewal_SupersededAfterPOSTRecordsNothing(t *testing.T) {
	t.Parallel()
	s, store, p, b, allow, rec := newRenewalFixture(t, 30*time.Second)
	renewThroughFault(t, s, p, b, allow, func() {
		c := store.rows[b]
		c.generation++
		store.rows[b] = c
	})
	rec.RequireNothing()
	requireSpentClaimHeld(t, s, store, b)
}

type renewalFinishHookStore struct {
	delegationStore
	after func()
}

func (s renewalFinishHookStore) finish(ctx context.Context, b Binding, generation int64, claim uuid.UUID, next delegationCredential) (bool, error) {
	ok, err := s.delegationStore.finish(ctx, b, generation, claim, next)
	s.after()
	return ok, err
}

func TestRenewal_QuarantineNamesSignInRecovery(t *testing.T) {
	t.Parallel()
	s, store, p, b, allow, rec := newRenewalFixture(t, 30*time.Second)
	s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
		s.loadBinding = func(context.Context, string, uuid.UUID, uuid.UUID) (Provider, error) {
			return nil, errors.New("database unavailable")
		}
		return delegationRenewal(p, s.now(), "new-id", "rotation"), nil
	}
	_, err := s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrTemporary)
	rec.RequireEnded(mcpconnectionattempts.StepSignInRenewal, mcpconnectionattempts.CodeRenewalAmbiguous)
	require.NotEqual(t, uuid.Nil, store.rows[b].claim)
	s.loadBinding = func(context.Context, string, uuid.UUID, uuid.UUID) (Provider, error) { return p, nil }
	_, err = s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrTemporary)
	require.Len(t, rec.Rows(), 1)
	require.Equal(t, 1, store.claims)
}

func TestRenewal_FailedFailurePersistRetainsClaim(t *testing.T) {
	t.Parallel()
	s, store, _, b, allow, rec := newRenewalFixture(t, 30*time.Second)
	s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
		store.finishError = errors.New("database unavailable")
		return nil, &RefreshError{Kind: RefreshInvalidGrant}
	}
	_, err := s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrTemporary)
	requireOneInternalError(t, rec, b)
	require.NotEqual(t, uuid.Nil, store.rows[b].claim, "retain the claim after a potentially spent refresh POST")
}

func TestRenewal_SupersededFailureRecordsNothing(t *testing.T) {
	t.Parallel()
	s, store, _, b, allow, rec := newRenewalFixture(t, 30*time.Second)
	s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
		c := store.rows[b]
		c.generation++
		store.rows[b] = c
		return nil, &RefreshError{Kind: RefreshInvalidGrant}
	}
	_, err := s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrTemporary)
	rec.RequireNothing()
}

func TestRenewal_SignerSetupOwnership(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind    RefreshFailure
		wantErr error
	}{
		{RefreshSetup, ErrRetainedConfiguration},
		{RefreshSetupRetryable, ErrTemporary},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			t.Parallel()
			s, _, _, b, allow, rec := newRenewalFixture(t, 30*time.Second)
			s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
				return nil, &RefreshError{Kind: tc.kind, SigningFailure: true}
			}
			_, err := s.Resolve(t.Context(), b, allow)
			require.ErrorIs(t, err, tc.wantErr)
			rec.RequireEnded(mcpconnectionattempts.StepSignInRenewal, mcpconnectionattempts.CodeInternalError)

			// A later call must not re-blame an administrator for a Speakeasy fault.
			_, err = s.Resolve(t.Context(), b, allow)
			require.ErrorIs(t, err, tc.wantErr)
			require.Len(t, rec.Rows(), 1)
		})
	}
}
