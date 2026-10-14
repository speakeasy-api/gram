package identity

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

// fakeLookups is an in-memory directory: addresses the org owns, addresses
// several members own, device owners, and linked-account owners.
type fakeLookups struct {
	users     map[string]string
	ambiguous map[string]bool
	devices   map[string]string
	accounts  map[string]string
	err       error
}

func (f *fakeLookups) UserByEmail(_ context.Context, _, email string) (string, LookupOutcome, error) {
	if f.err != nil {
		return "", LookupNone, f.err
	}
	if f.ambiguous[email] {
		return "", LookupAmbiguous, nil
	}
	if id, ok := f.users[email]; ok {
		return id, LookupFound, nil
	}

	return "", LookupNone, nil
}

func (f *fakeLookups) DeviceOwner(_ context.Context, _, provider, deviceID string) (string, error) {
	if f.err != nil {
		return "", f.err
	}

	return f.devices[provider+"/"+deviceID], nil
}

func (f *fakeLookups) AccountOwner(_ context.Context, _, email string) (string, error) {
	if f.err != nil {
		return "", f.err
	}

	return f.accounts[email], nil
}

const (
	testOrg     = "org_1"
	testProject = "proj_1"

	member      = "user_member"
	memberEmail = "dev@example.com"
	owner       = "user_owner"
	ownerEmail  = "owner@example.com"
)

// newTestMatcher builds a matcher over in-memory lookups and returns the
// buffer its warnings land in. testenv imports this package through the
// session views, so its logger cannot be used here.
func newTestMatcher(t *testing.T, lookups Lookups) (*Matcher, *bytes.Buffer) {
	t.Helper()

	var logs bytes.Buffer

	return &Matcher{logger: slog.New(slog.NewJSONHandler(&logs, nil)), lookups: lookups}, &logs
}

func testLookups() *fakeLookups {
	return &fakeLookups{
		users:     map[string]string{memberEmail: member, ownerEmail: owner},
		ambiguous: map[string]bool{"dupe@example.com": true},
		devices:   map[string]string{"anthropic/device-1": member},
		accounts:  map[string]string{"personal@gmail.com": member},
		err:       nil,
	}
}

func scoped(signals Signals) Signals {
	signals.OrganizationID = testOrg
	signals.ProjectID = testProject

	return signals
}

// Every ingestion path holds a different subset of the signals; each case is
// one path's shape and the identity the ladder must settle on for it.
func TestMatcher_Match(t *testing.T) {
	t.Parallel()

	sharedKey := Credential{Principal: urn.PrincipalTypeUser, UserID: owner, Email: ownerEmail, Shared: true, SessionIdentityFallback: true}
	personalKey := Credential{Principal: urn.PrincipalTypeUser, UserID: owner, Email: ownerEmail, Shared: false, SessionIdentityFallback: true}

	tests := []struct {
		name    string
		signals Signals
		want    Attribution
	}{
		{
			name: "agent principal carries no human identity",
			signals: scoped(Signals{
				Credential: Credential{Principal: urn.PrincipalTypeAgent, UserID: member, Email: memberEmail},
				Reported:   Reported{UserEmail: memberEmail},
				Account:    Account{Provider: "anthropic", DeviceID: "device-1", Email: "personal@gmail.com"},
			}),
			want: Attribution{Basis: BasisAgent},
		},
		{
			name: "user session names the actor without a lookup",
			signals: scoped(Signals{
				Credential: Credential{Principal: urn.PrincipalTypeUser, UserID: "user_unlisted", Email: "Unlisted@Example.com"},
			}),
			want: Attribution{UserID: "user_unlisted", Email: "unlisted@example.com", ExternalUserID: "unlisted@example.com", Basis: BasisCredential},
		},
		{
			name: "chat session token keeps its end user id",
			signals: scoped(Signals{
				Credential: Credential{Principal: urn.PrincipalTypeUser, UserID: member, Email: memberEmail, ExternalUserID: "end-user-7"},
			}),
			want: Attribution{UserID: member, Email: memberEmail, ExternalUserID: "end-user-7", Basis: BasisCredential},
		},
		{
			name: "reported email resolves to a member over a shared key owner",
			signals: scoped(Signals{
				Credential: sharedKey,
				Reported:   Reported{UserEmail: " Dev@Example.com "},
			}),
			want: Attribution{UserID: member, Email: memberEmail, ExternalUserID: memberEmail, Basis: BasisReportedEmail},
		},
		{
			name: "reported email naming the shared key owner uses the owner",
			signals: scoped(Signals{
				Credential: sharedKey,
				Reported:   Reported{UserEmail: "Owner@Example.com"},
			}),
			want: Attribution{UserID: owner, Email: ownerEmail, ExternalUserID: ownerEmail, Basis: BasisCredential},
		},
		{
			name: "unresolved reported email on a personal key recovers the owner",
			signals: scoped(Signals{
				Credential: personalKey,
				Reported:   Reported{UserEmail: "nobody@example.com"},
			}),
			want: Attribution{UserID: owner, Email: ownerEmail, ExternalUserID: ownerEmail, Basis: BasisCredential},
		},
		{
			name: "unresolved reported email on a shared key stays unattributed",
			signals: scoped(Signals{
				Credential: sharedKey,
				Reported:   Reported{UserEmail: "nobody@example.com"},
			}),
			want: Attribution{Email: "nobody@example.com", ExternalUserID: "nobody@example.com", Basis: BasisUnattributed},
		},
		{
			name: "shared key with no reported email inherits the session",
			signals: scoped(Signals{
				Credential: sharedKey,
				Session:    Session{OrganizationID: testOrg, ProjectID: testProject, UserID: member, Email: "max@gmail.com"},
			}),
			want: Attribution{UserID: member, Email: "max@gmail.com", ExternalUserID: "max@gmail.com", Basis: BasisSession},
		},
		{
			name: "session seeded by another project is ignored",
			signals: scoped(Signals{
				Credential: sharedKey,
				Session:    Session{OrganizationID: testOrg, ProjectID: "proj_2", UserID: member, Email: memberEmail},
			}),
			want: Attribution{Basis: BasisUnattributed},
		},
		{
			name: "proxy observation never inherits the session",
			signals: scoped(Signals{
				Credential: Credential{Principal: urn.PrincipalTypeUser, UserID: owner, Email: ownerEmail, Shared: true, SessionIdentityFallback: false},
				Session:    Session{OrganizationID: testOrg, ProjectID: testProject, UserID: member, Email: "max@gmail.com"},
			}),
			want: Attribution{Basis: BasisUnattributed},
		},
		{
			name: "changed reported email does not inherit the session",
			signals: scoped(Signals{
				Credential: sharedKey,
				Reported:   Reported{UserEmail: "nobody@example.com"},
				Session:    Session{OrganizationID: testOrg, ProjectID: testProject, UserID: member, Email: "max@gmail.com"},
			}),
			want: Attribution{Email: "nobody@example.com", ExternalUserID: "nobody@example.com", Basis: BasisUnattributed},
		},
		{
			name: "session attributed without an address does not lend its user to a new address",
			signals: scoped(Signals{
				Credential: sharedKey,
				Reported:   Reported{UserEmail: "nobody@example.com"},
				Session:    Session{OrganizationID: testOrg, ProjectID: testProject, UserID: member, Email: ""},
			}),
			want: Attribution{Email: "nobody@example.com", ExternalUserID: "nobody@example.com", Basis: BasisUnattributed},
		},
		{
			name: "personal account resolves through the device bridge",
			signals: scoped(Signals{
				Credential: sharedKey,
				Account:    Account{Provider: "anthropic", DeviceID: "device-1", Email: "Max@Gmail.com"},
			}),
			want: Attribution{UserID: member, Email: "max@gmail.com", ExternalUserID: "max@gmail.com", AccountEmail: "max@gmail.com", Basis: BasisDeviceBridge},
		},
		{
			name: "account email the directory owns resolves as a team account",
			signals: scoped(Signals{
				Credential: sharedKey,
				Account:    Account{Provider: "anthropic", DeviceID: "device-9", Email: "Dev@Example.com"},
			}),
			want: Attribution{UserID: member, Email: memberEmail, ExternalUserID: memberEmail, AccountEmail: memberEmail, Basis: BasisAccountEmail},
		},
		{
			name: "linked account email resolves to its single owner",
			signals: scoped(Signals{
				Credential: sharedKey,
				Reported:   Reported{UserEmail: "personal@gmail.com"},
			}),
			want: Attribution{UserID: member, Email: "personal@gmail.com", ExternalUserID: "personal@gmail.com", Basis: BasisLinkedAccount},
		},
		{
			name: "ambiguous address is nobody's even when session and device would answer",
			signals: scoped(Signals{
				Credential: sharedKey,
				Reported:   Reported{UserEmail: "dupe@example.com"},
				Account:    Account{Provider: "anthropic", DeviceID: "device-1"},
				Session:    Session{OrganizationID: testOrg, ProjectID: testProject, UserID: member, Email: "dupe@example.com"},
			}),
			want: Attribution{Email: "dupe@example.com", ExternalUserID: "dupe@example.com", Basis: BasisUnattributed, Ambiguous: true},
		},
		{
			name: "ambiguous address still yields to a personal key",
			signals: scoped(Signals{
				Credential: personalKey,
				Reported:   Reported{UserEmail: "dupe@example.com"},
			}),
			want: Attribution{UserID: owner, Email: ownerEmail, ExternalUserID: ownerEmail, Basis: BasisCredential, Ambiguous: true},
		},
		{
			name: "chat owner continues a chat when the message names no actor",
			signals: scoped(Signals{
				Credential:   Credential{SessionIdentityFallback: true},
				Conversation: Conversation{UserID: member, ExternalUserID: "actor-9"},
			}),
			want: Attribution{UserID: member, ExternalUserID: "actor-9", Basis: BasisConversation},
		},
		{
			name: "chat owner is refused for a different actor",
			signals: scoped(Signals{
				Reported:     Reported{ExternalUserID: "actor-10"},
				Conversation: Conversation{UserID: member, ExternalUserID: "actor-9"},
			}),
			want: Attribution{ExternalUserID: "actor-10", Basis: BasisUnattributed},
		},
		{
			name: "provider user id is recorded ahead of the address",
			signals: scoped(Signals{
				Reported: Reported{UserEmail: memberEmail, ExternalUserID: " user_01ABC "},
			}),
			want: Attribution{UserID: member, Email: memberEmail, ExternalUserID: "user_01ABC", Basis: BasisReportedEmail},
		},
		{
			name:    "nothing known is unattributed",
			signals: scoped(Signals{}),
			want:    Attribution{Basis: BasisUnattributed},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			matcher, logs := newTestMatcher(t, testLookups())

			require.Equal(t, tt.want, matcher.Match(t.Context(), tt.signals))
			require.Empty(t, logs.String())
		})
	}
}

// A directory outage must not turn into a confident "nobody": the row is
// written unattributed, but flagged so a caller can tell the difference.
func TestMatcher_Match_LookupFailureIsReported(t *testing.T) {
	t.Parallel()

	lookups := testLookups()
	lookups.err = errors.New("directory unavailable")
	matcher, logs := newTestMatcher(t, lookups)

	got := matcher.Match(t.Context(), scoped(Signals{
		Credential: Credential{Shared: true, SessionIdentityFallback: true},
		Reported:   Reported{UserEmail: memberEmail},
		Account:    Account{Provider: "anthropic", DeviceID: "device-1"},
	}))

	require.Equal(t, Attribution{Email: memberEmail, ExternalUserID: memberEmail, Basis: BasisUnattributed, LookupFailed: true}, got)
	require.Contains(t, logs.String(), "identity match: resolve actor email")
	require.Contains(t, logs.String(), "identity match: resolve device owner")
	require.Contains(t, logs.String(), "identity match: resolve linked account owner")
}

// Only an identity the request itself proved may teach the device bridge; an
// inherited identity would teach the bridge its own earlier guess.
func TestAttribution_Authoritative(t *testing.T) {
	t.Parallel()

	tests := []struct {
		basis Basis
		want  bool
	}{
		{basis: BasisCredential, want: true},
		{basis: BasisReportedEmail, want: true},
		{basis: BasisAccountEmail, want: true},
		{basis: BasisSession, want: false},
		{basis: BasisDeviceBridge, want: false},
		{basis: BasisLinkedAccount, want: false},
		{basis: BasisConversation, want: false},
		{basis: BasisAgent, want: false},
		{basis: BasisUnattributed, want: false},
	}

	for _, tt := range tests {
		t.Run(string(tt.basis), func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, Attribution{Basis: tt.basis}.Authoritative())
		})
	}
}
