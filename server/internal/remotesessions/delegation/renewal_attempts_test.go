package delegation

import (
	"context"
	"errors"
	"fmt"
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
	require.Equal(t, b.ClientID.String(), row.RemoteSessionClientID)
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
		{name: "invalid grant", kind: RefreshInvalidGrant, want: mcpconnectionattempts.CodeReauthenticationRequired, who: mcpconnectionattempts.WhoPerson, rows: 1},
		{name: "configuration", kind: RefreshConfiguration, want: mcpconnectionattempts.CodeRenewalMisconfigured, who: mcpconnectionattempts.WhoAdmin, rows: 2},
		{name: "client rejected", kind: RefreshClientRejected, want: mcpconnectionattempts.CodeRenewalMisconfigured, who: mcpconnectionattempts.WhoAdmin, rows: 2},
		{name: "setup", kind: RefreshSetup, want: mcpconnectionattempts.CodeRenewalMisconfigured, who: mcpconnectionattempts.WhoAdmin, rows: 2},
		{name: "setup retryable", kind: RefreshSetupRetryable, want: mcpconnectionattempts.CodeRenewalRetryable, who: mcpconnectionattempts.WhoProvider, rows: 1},
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
			if tc.kind == RefreshInvalidIdentity {
				require.Equal(t, "reauthentication_required", rec.Rows()[1].Reason)
			}
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

// A setup changed since the sign-in is the person's to fix: signing in again binds the new setup.
func TestRenewal_SignInFromAnOlderSetupAsksForSignIn(t *testing.T) {
	t.Parallel()

	s, _, p, b, allow := newDelegationUnitFixture(t)
	rec := mcpconnectionattemptstest.New(t)
	s.connections = rec.Recorder
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "id", "refresh", time.Hour), true))
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

	s, _, p, b, allow := newDelegationUnitFixture(t)
	rec := mcpconnectionattemptstest.New(t)
	s.connections = rec.Recorder
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second), true))
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
		{name: "unreadable retained nonce", secret: "nonce", wantErr: ErrConfiguration, want: mcpconnectionattempts.CodeReauthenticationRequired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, store, p, b, allow := newDelegationUnitFixture(t)
			rec := mcpconnectionattemptstest.New(t)
			s.connections = rec.Recorder
			require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second), true))
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

func TestRenewal_PrePOSTStoreFaults(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"reread failure", "mark failure", "superseded claim", "mark conflict"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, store, p, b, allow := newDelegationUnitFixture(t)
			rec := mcpconnectionattemptstest.New(t)
			s.connections = rec.Recorder
			require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second), true))
			switch name {
			case "reread failure":
				s.store = &delegationRereadHookStore{delegationStore: store, beforeReread: func() {}, rereadError: errors.New("database unavailable")}
			case "mark failure":
				store.attemptError = errors.New("database unavailable")
			case "superseded claim":
				s.store = &delegationRereadHookStore{delegationStore: store, beforeReread: func() {
					c := store.rows[b]
					c.claim = uuid.New()
					c.generation++
					store.rows[b] = c
				}}
			case "mark conflict":
				store.attemptConflict = true
			}
			s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
				t.Error("pre-POST failure must not refresh")
				return nil, ErrTemporary
			}
			_, err := s.Resolve(t.Context(), b, allow)
			require.ErrorIs(t, err, ErrTemporary)
			if name == "superseded claim" || name == "mark conflict" {
				rec.RequireNothing()
			} else {
				row := rec.RequireEnded(mcpconnectionattempts.StepSignInRenewal, mcpconnectionattempts.CodeInternalError)
				require.Len(t, rec.Rows(), 1)
				require.Equal(t, b.IssuerID.String(), row.RemoteSessionIssuerID)
				require.Equal(t, b.ClientID.String(), row.RemoteSessionClientID)
			}
			if name != "superseded claim" {
				require.Equal(t, uuid.Nil, store.rows[b].claim)
			}
			require.Equal(t, "old-refresh", delegationPlain(t, s, store.rows[b].refresh))
		})
	}
}

func TestRenewal_PostPOSTInternalFaults(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"panic", "encryption", "finish", "assertion decrypt", "superseded"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, store, p, b, allow := newDelegationUnitFixture(t)
			rec := mcpconnectionattemptstest.New(t)
			s.connections = rec.Recorder
			require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second), true))
			originalEncryption := s.enc
			s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
				switch name {
				case "panic":
					panic("fixture refresh panic")
				case "encryption":
					s.enc = &encryption.Client{}
				case "finish":
					store.finishError = errors.New("fixture store unavailable")
				case "assertion decrypt":
					s.store = renewalFinishHookStore{delegationStore: store, after: func() { s.enc = &encryption.Client{} }}
				case "superseded":
					c := store.rows[b]
					c.generation++
					store.rows[b] = c
				}
				return delegationRenewal(p, s.now(), "new-id", "new-refresh"), nil
			}
			if name == "panic" {
				require.PanicsWithValue(t, "fixture refresh panic", func() { _, _ = s.Resolve(t.Context(), b, allow) })
			} else {
				_, err := s.Resolve(t.Context(), b, allow)
				require.Error(t, err)
			}
			if name == "superseded" {
				rec.RequireNothing()
			} else {
				row := rec.RequireEnded(mcpconnectionattempts.StepSignInRenewal, mcpconnectionattempts.CodeInternalError)
				require.Equal(t, string(mcpconnectionattempts.WhoSpeakeasy), row.WhoFixes)
				require.Len(t, rec.Rows(), 1)
			}
			s.enc = originalEncryption
			if name != "assertion decrypt" {
				require.NotEqual(t, uuid.Nil, store.rows[b].claim, "never release a potentially spent refresh")
				require.Equal(t, "old-refresh", delegationPlain(t, s, store.rows[b].refresh))
			}
		})
	}
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
	s, store, p, b, allow := newDelegationUnitFixture(t)
	rec := mcpconnectionattemptstest.New(t)
	s.connections = rec.Recorder
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second), true))
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

func TestRenewal_FailurePersistenceOutcome(t *testing.T) {
	t.Parallel()
	for _, superseded := range []bool{false, true} {
		t.Run(fmt.Sprint(superseded), func(t *testing.T) {
			t.Parallel()
			s, store, p, b, allow := newDelegationUnitFixture(t)
			rec := mcpconnectionattemptstest.New(t)
			s.connections = rec.Recorder
			require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second), true))
			s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
				if superseded {
					c := store.rows[b]
					c.generation++
					store.rows[b] = c
				} else {
					store.finishError = errors.New("database unavailable")
				}
				return nil, &RefreshError{Kind: RefreshInvalidGrant}
			}
			_, err := s.Resolve(t.Context(), b, allow)
			require.ErrorIs(t, err, ErrTemporary)
			if superseded {
				rec.RequireNothing()
			} else {
				rec.RequireEnded(mcpconnectionattempts.StepSignInRenewal, mcpconnectionattempts.CodeInternalError)
				require.Len(t, rec.Rows(), 1)
				require.NotEqual(t, uuid.Nil, store.rows[b].claim, "retain the claim after a potentially spent refresh POST")
			}
		})
	}
}
