package identitychaining

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// scriptedTry answers each provider kind with its queued outcomes, blocking
// until the context ends for a hang, and counts calls per kind.
type scriptedTry struct {
	mu       sync.Mutex
	outcomes map[remotesessions.EMAProviderKind][]Outcome
	hang     map[remotesessions.EMAProviderKind]bool
	calls    map[remotesessions.EMAProviderKind]int
}

func (s *scriptedTry) try(ctx context.Context, _ *slog.Logger, p remotesessions.EMAProvider) (Token, Outcome) {
	s.mu.Lock()
	s.calls[p.Kind]++
	hang := s.hang[p.Kind]
	queue := s.outcomes[p.Kind]
	outcome := success
	if len(queue) > 0 {
		outcome, s.outcomes[p.Kind] = queue[0], queue[1:]
	}
	s.mu.Unlock()
	if hang {
		<-ctx.Done()
		return Token{}, newOutcome(StageExchange, ReasonTransientFailure, ConfidenceVerified, true)
	}
	if outcome.Succeeded() {
		return Token{value: "token-" + string(p.Kind), expiresAt: time.Now().Add(time.Hour)}, outcome
	}
	return Token{}, outcome
}

func newScriptedTry() *scriptedTry {
	return &scriptedTry{mu: sync.Mutex{}, outcomes: make(map[remotesessions.EMAProviderKind][]Outcome), hang: make(map[remotesessions.EMAProviderKind]bool), calls: make(map[remotesessions.EMAProviderKind]int)}
}

func meteredChainer(t *testing.T) (*Chainer, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	c := new(Chainer)
	c.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	return c, reader
}

// attemptPoints returns each recorded attempt's attributes.
func attemptPoints(t *testing.T, reader *sdkmetric.ManualReader) []attribute.Set {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	var out []attribute.Set
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != meterAttempts {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, point := range sum.DataPoints {
				out = append(out, point.Attributes)
			}
		}
	}
	return out
}

func attemptPoint(t *testing.T, points []attribute.Set, provider remotesessions.EMAProviderKind) attribute.Set {
	t.Helper()
	for _, set := range points {
		if v, ok := set.Value(attr.IdentityChainingProviderKey); ok && v.AsString() == string(provider) {
			return set
		}
	}
	t.Fatalf("no attempt recorded for %s", provider)
	return attribute.Set{}
}

func twoProviderOrder() remotesessions.EMAProviderOrder {
	return remotesessions.EMAProviderOrder{VendorIssuer: "https://vendor.example.test", IsDefault: false, Providers: []remotesessions.EMAProvider{
		{Kind: remotesessions.EMAProviderSpeakeasy, RemoteSessionIssuerID: uuid.Nil, InternalSessionIssuerID: uuid.New(), Unavailable: ""},
		{Kind: remotesessions.EMAProviderExternal, RemoteSessionIssuerID: uuid.New(), InternalSessionIssuerID: uuid.Nil, Unavailable: ""},
	}}
}

func TestTryProviders_RecordsRetriesAndFallback(t *testing.T) {
	t.Parallel()
	c, reader := meteredChainer(t)
	script := newScriptedTry()
	transient := newOutcome(StageExchange, ReasonTransientFailure, ConfidenceVerified, true)
	script.outcomes[remotesessions.EMAProviderSpeakeasy] = []Outcome{transient, transient}

	ctx, cancel := context.WithTimeout(t.Context(), acquireTimeout)
	defer cancel()
	token, outcome := c.tryProviders(ctx, testenv.NewLogger(t), "acquire", twoProviderOrder(), script.try)
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
	require.Equal(t, "token-external", token.Value())
	require.Equal(t, remotesessions.EMAProviderExternal, outcome.Provider)
	require.Equal(t, maxProviderAttempts, script.calls[remotesessions.EMAProviderSpeakeasy], "a transient failure is retried within the attempt bound")
	require.Equal(t, 1, script.calls[remotesessions.EMAProviderExternal])

	points := attemptPoints(t, reader)
	first := attemptPoint(t, points, remotesessions.EMAProviderSpeakeasy)
	retries, _ := first.Value(attr.IdentityChainingRetriesKey)
	require.EqualValues(t, maxProviderAttempts-1, retries.AsInt64())
	fallback, _ := first.Value(attr.IdentityChainingFallbackKey)
	require.False(t, fallback.AsBool())
	served := attemptPoint(t, points, remotesessions.EMAProviderExternal)
	fallback, _ = served.Value(attr.IdentityChainingFallbackKey)
	require.True(t, fallback.AsBool(), "the serving attempt is marked as a fallback")
	outcomeValue, _ := served.Value(attr.OutcomeKey)
	require.Equal(t, string(ReasonSuccess), outcomeValue.AsString())
}

func TestTryProviders_NonTransientFailureMovesOnWithoutRetry(t *testing.T) {
	t.Parallel()
	c, _ := meteredChainer(t)
	script := newScriptedTry()
	script.outcomes[remotesessions.EMAProviderSpeakeasy] = []Outcome{newOutcome(StageExchange, ReasonIneligibleIdentity, ConfidenceVerified, false)}

	ctx, cancel := context.WithTimeout(t.Context(), acquireTimeout)
	defer cancel()
	_, outcome := c.tryProviders(ctx, testenv.NewLogger(t), "acquire", twoProviderOrder(), script.try)
	require.True(t, outcome.Succeeded(), "an external provider vouches from its own sign-in")
	require.Equal(t, 1, script.calls[remotesessions.EMAProviderSpeakeasy])
}

func TestTryProviders_SlowProviderLeavesBudgetForTheNext(t *testing.T) {
	t.Parallel()
	c, _ := meteredChainer(t)
	script := newScriptedTry()
	script.hang[remotesessions.EMAProviderSpeakeasy] = true

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	started := time.Now()
	_, outcome := c.tryProviders(ctx, testenv.NewLogger(t), "acquire", twoProviderOrder(), script.try)
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
	require.Equal(t, remotesessions.EMAProviderExternal, outcome.Provider)
	require.Equal(t, 1, script.calls[remotesessions.EMAProviderSpeakeasy], "a retry is not started without budget for it")
	require.Less(t, time.Since(started), 1500*time.Millisecond, "the hanging provider gets only its share of the deadline")
}

func TestTryProviders_DeadlineBoundsTheWholeOrder(t *testing.T) {
	t.Parallel()
	c, _ := meteredChainer(t)
	script := newScriptedTry()
	script.hang[remotesessions.EMAProviderSpeakeasy] = true
	script.hang[remotesessions.EMAProviderExternal] = true

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, outcome := c.tryProviders(ctx, testenv.NewLogger(t), "acquire", twoProviderOrder(), script.try)
	require.Less(t, time.Since(started), time.Second, "the request deadline bounds every provider and retry")
	require.Equal(t, ReasonTransientFailure, outcome.Reason)
	require.Len(t, outcome.Failures, 2, "each provider's failure is reported")
}

func TestExhausted_PrefersAnAttemptedProvider(t *testing.T) {
	t.Parallel()
	skipped := unavailableBecause(remotesessions.EMAProviderNotTrustedByEndpoint)
	skipped.Provider = remotesessions.EMAProviderExternal
	denied := newOutcome(StageExchange, ReasonAccessDenied, ConfidenceInferred, false)
	denied.Provider = remotesessions.EMAProviderSpeakeasy

	got := exhausted([]Outcome{skipped, denied})
	require.Equal(t, ReasonAccessDenied, got.Reason)
	require.Equal(t, []ProviderFailure{
		{Provider: remotesessions.EMAProviderExternal, Reason: ReasonProviderUnavailable, Unavailable: remotesessions.EMAProviderNotTrustedByEndpoint},
		{Provider: remotesessions.EMAProviderSpeakeasy, Reason: ReasonAccessDenied, Unavailable: ""},
	}, got.Failures)

	only := exhausted([]Outcome{skipped})
	require.Equal(t, ReasonProviderUnavailable, only.Reason, "with nothing tried the interactive path applies")
	require.Nil(t, only.Failures, "one provider needs no list")
	require.False(t, only.Applicable())
}

func TestExhausted_PrefersReauthentication(t *testing.T) {
	t.Parallel()
	denied := newOutcome(StageExchange, ReasonAccessDenied, ConfidenceInferred, false)
	denied.Provider = remotesessions.EMAProviderExternal
	reauth := newOutcome(StageAuthorization, ReasonReauthenticationRequired, ConfidenceVerified, false)
	reauth.Provider = remotesessions.EMAProviderSpeakeasy

	got := exhausted([]Outcome{denied, reauth})
	require.Equal(t, ReasonReauthenticationRequired, got.Reason, "a reconnect challenge beats an earlier denial")
	require.Equal(t, remotesessions.EMAProviderSpeakeasy, got.Provider)
	require.Equal(t, []ProviderFailure{
		{Provider: remotesessions.EMAProviderExternal, Reason: ReasonAccessDenied, Unavailable: ""},
		{Provider: remotesessions.EMAProviderSpeakeasy, Reason: ReasonReauthenticationRequired, Unavailable: ""},
	}, got.Failures, "every provider's reason is kept")

	invalid := newOutcome(StageRedemption, ReasonInvalidGrant, ConfidenceUnknown, false)
	invalid.Provider = remotesessions.EMAProviderSpeakeasy
	require.Equal(t, ReasonAccessDenied, exhausted([]Outcome{denied, invalid}).Reason, "without a reauthentication the first attempted provider explains it")
}

func TestTryProviders_SubmittedTransientFailureIsNotRepeated(t *testing.T) {
	t.Parallel()
	c, _ := meteredChainer(t)
	script := newScriptedTry()
	ambiguous := newOutcome(StageRedemption, ReasonTransientFailure, ConfidenceUnknown, true)
	ambiguous.Submitted = true
	script.outcomes[remotesessions.EMAProviderSpeakeasy] = []Outcome{ambiguous}

	ctx, cancel := context.WithTimeout(t.Context(), acquireTimeout)
	defer cancel()
	_, outcome := c.tryProviders(ctx, testenv.NewLogger(t), "acquire", twoProviderOrder(), script.try)
	require.True(t, outcome.Succeeded(), "the next provider serves")
	require.Equal(t, 1, script.calls[remotesessions.EMAProviderSpeakeasy], "a request that may have reached the provider is never repeated")
}
