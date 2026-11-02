package mcpconnectionattempts

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestNormalizeProviderHost(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"issuer url", "https://login.example.com/oauth2/default", "login.example.com"},
		{"uppercase", "HTTPS://Login.Example.COM/", "login.example.com"},
		{"bare host", "login.example.com", "login.example.com"},
		{"trailing dot", "https://login.example.com./", "login.example.com"},
		{"default https port", "https://login.example.com:443/x", "login.example.com"},
		{"default http port", "http://login.example.com:80", "login.example.com"},
		{"other port kept", "https://login.example.com:8443/x", "login.example.com:8443"},
		{"https port on http", "http://login.example.com:443", "login.example.com"},
		{"credentials dropped", "https://user:secret@login.example.com/", "login.example.com"},
		{"query dropped", "https://login.example.com/x?tenant=a#b", "login.example.com"},
		{"tenant subdomain kept", "https://tenant-a.idp.example.com", "tenant-a.idp.example.com"},
		{"ipv6", "https://[2001:db8::1]/x", "[2001:db8::1]"},
		{"ipv6 with port", "https://[2001:db8::1]:8443/x", "[2001:db8::1]:8443"},
		{"surrounding space", "  https://login.example.com  ", "login.example.com"},
		{"empty", "", ""},
		{"no host", "https:///path", ""},
		{"not a url", "https://bad host/", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := NormalizeProviderHost(tc.input)
			require.Equal(t, tc.want, got)
			require.Equal(t, got, NormalizeProviderHost(got), "normalizing twice changes nothing")
		})
	}
}

func TestAppOf(t *testing.T) {
	t.Parallel()

	long := "https://" + strings.Repeat("a", maxProviderHostBytes) + ".example.com/m"
	cases := []struct {
		name     string
		clientID string
		want     App
	}{
		{"host only", "https://Client.Example.com/path/meta.json", App{"client.example.com"}},
		{"query and fragment dropped", "https://client.example.com/meta.json?a=b#c", App{"client.example.com"}},
		{"caller text in the path dropped", "https://client.example.com/anything/a/caller/chooses", App{"client.example.com"}},
		{"credentials dropped", "https://user:secret@client.example.com/m", App{"client.example.com"}},
		{"dynamically registered", "3fa85f64-5717-4562-b3fc-2c963f66afa6", AppDCR},
		{"dashboard", "", AppFirstParty},
		{"host spelled like a kind", "https://dcr/meta.json", AppUnverified},
		{"other host spelled like a kind", "https://workload:8443/meta.json", App{"workload:8443"}},
		{"too long", long, AppUnverified},
		{"unparsable", "https://bad host/", AppUnverified},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, AppOf(tc.clientID))
		})
	}
}

func testScope() Scope {
	return ServerScope("org-test", uuid.New(), uuid.New(), AppDCR)
}

type signInState struct {
	ID      string  `json:"id"`
	Attempt Attempt `json:"attempt,omitzero"`
}

// A value this code cannot read must never fail the sign-in whose state holds it.
func TestAttempt_MalformedStateReadsAsZero(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		`{"id":"s","attempt":{"id":"not-a-uuid"}}`,
		`{"id":"s","attempt":"text"}`,
		`{"id":"s","attempt":{"started_at":"not-a-time"}}`,
		`{"id":"s","attempt":7}`,
	} {
		var state signInState
		require.NoError(t, json.Unmarshal([]byte(raw), &state), raw)
		require.Equal(t, "s", state.ID)
		require.True(t, state.Attempt.IsZero(), raw)
	}
}

func TestAttempt_SurvivesSignInStateUnchanged(t *testing.T) {
	t.Parallel()

	attempt := Begin(OrganizationGatewayScope("org-test", uuid.New(), AppOf("https://client.example.com/m")))
	raw, err := json.Marshal(signInState{ID: "s", Attempt: attempt})
	require.NoError(t, err)

	var decoded signInState
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Equal(t, attempt.ID(), decoded.Attempt.ID())
	require.Equal(t, attempt.scope, decoded.Attempt.scope)
	require.True(t, attempt.StartedAt().Equal(decoded.Attempt.StartedAt()))

	empty, err := json.Marshal(signInState{ID: "s", Attempt: NoAttempt})
	require.NoError(t, err)
	require.JSONEq(t, `{"id":"s"}`, string(empty), "state without an attempt stores nothing")
}

func TestScope_OrganizationGatewayHasNoProject(t *testing.T) {
	t.Parallel()

	row, err := RowAt(Begin(OrganizationGatewayScope("org-test", uuid.New(), AppDCR)), StepInboundToken, ResultCodeIssued, true, time.Now())
	require.NoError(t, err)
	require.Equal(t, uuid.Nil, row.ProjectID)
	require.Equal(t, serverKindMeta, row.ServerKind)
}

// A scheduled refresh reaches a provider and no server, and is still recorded.
func TestScope_ProviderSessionHasNoServer(t *testing.T) {
	t.Parallel()

	row, err := RowAt(Begin(ProviderSessionScope("org-test", uuid.New(), AppDCR)), StepProviderRefresh, ResultRefreshed, true, time.Now())
	require.NoError(t, err)
	require.Empty(t, row.ServerID)
	require.Equal(t, serverKindProviderSession, row.ServerKind)
}

// Renewing a company sign-in reaches an identity provider, with no project or server.
func TestScope_IdentityProviderHasNoProjectOrServer(t *testing.T) {
	t.Parallel()

	row, err := RowAt(Begin(IdentityProviderScope("org-test", AppDCR)), StepSignInRenewal, ResultRenewed, true, time.Now())
	require.NoError(t, err)
	require.Empty(t, row.ServerID)
	require.Equal(t, uuid.Nil, row.ProjectID)
	require.Equal(t, serverKindIdentityProvider, row.ServerKind)
}

type cachedSignInState struct {
	ID      string
	Attempt Attempt `msgpack:",omitempty"`
}

// stateWithoutAttempt is the same state on a replica without the field.
type stateWithoutAttempt struct {
	ID string
}

func (c cachedSignInState) CacheKey() string   { return "connectionAttemptTest:" + c.ID }
func (c cachedSignInState) TTL() time.Duration { return time.Minute }

// Sign-in state lives in Redis, whose encoding is not JSON.
func TestAttempt_SurvivesTheRedisCache(t *testing.T) {
	t.Parallel()

	states := cache.NewTypedObjectCache[cachedSignInState](testenv.NewLogger(t), redisCache(t), cache.SuffixNone)
	attempt := Begin(OrganizationGatewayScope("org-test", uuid.New(), AppOf("https://client.example.com/m")))
	id := uuid.NewString()
	require.NoError(t, states.Store(t.Context(), cachedSignInState{ID: id, Attempt: attempt}))

	got, err := states.Get(t.Context(), "connectionAttemptTest:"+id)
	require.NoError(t, err)
	require.Equal(t, attempt.ID(), got.Attempt.ID())
	require.Equal(t, attempt.scope, got.Attempt.scope)
	require.True(t, attempt.StartedAt().Equal(got.Attempt.StartedAt()))

	emptyID := uuid.NewString()
	require.NoError(t, states.Store(t.Context(), cachedSignInState{ID: emptyID, Attempt: NoAttempt}))
	got, err = states.Get(t.Context(), "connectionAttemptTest:"+emptyID)
	require.NoError(t, err)
	require.True(t, got.Attempt.IsZero())
}

func TestAttempt_UnreadableBytesReadAsZero(t *testing.T) {
	t.Parallel()

	var attempt Attempt
	require.NoError(t, attempt.UnmarshalBinary([]byte("not json")))
	require.True(t, attempt.IsZero())
	require.NoError(t, attempt.UnmarshalBinary(nil))
	require.True(t, attempt.IsZero())
}

// CompareAndSwap compares encoded bytes, so a zero attempt must encode as if the
// field were not there, in both directions of a rollout.
func TestAttempt_ZeroEncodesAsNoFieldForCompareAndSwap(t *testing.T) {
	t.Parallel()

	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	adapter := cache.NewRedisCacheAdapter(client)

	key := "connectionAttemptTest:" + uuid.NewString()
	require.NoError(t, adapter.Set(t.Context(), key, cachedSignInState{ID: "s", Attempt: NoAttempt}, time.Minute))
	swapped, err := adapter.CompareAndSwap(t.Context(), key, stateWithoutAttempt{ID: "s"}, stateWithoutAttempt{ID: "s"}, time.Minute)
	require.NoError(t, err)
	require.True(t, swapped, "written with the field, compared without it")

	swapped, err = adapter.CompareAndSwap(t.Context(), key, cachedSignInState{ID: "s", Attempt: NoAttempt}, cachedSignInState{ID: "s", Attempt: NoAttempt}, time.Minute)
	require.NoError(t, err)
	require.True(t, swapped, "written without the field, compared with it")
}

// An attempt read back from Redis encodes to the same bytes, so swaps on it work.
func TestAttempt_RoundTripEncodesTheSameForCompareAndSwap(t *testing.T) {
	t.Parallel()

	states := cache.NewTypedObjectCache[cachedSignInState](testenv.NewLogger(t), redisCache(t), cache.SuffixNone)
	state := cachedSignInState{ID: uuid.NewString(), Attempt: Begin(testScope())}
	require.NoError(t, states.Store(t.Context(), state))

	read, err := states.Get(t.Context(), "connectionAttemptTest:"+state.ID)
	require.NoError(t, err)
	swapped, err := states.CompareAndSwap(t.Context(), read, read)
	require.NoError(t, err)
	require.True(t, swapped)
}

func redisCache(t *testing.T) cache.Cache {
	t.Helper()

	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	return cache.NewRedisCacheAdapter(client)
}
