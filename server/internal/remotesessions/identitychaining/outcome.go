package identitychaining

import "github.com/speakeasy-api/gram/server/internal/remotesessions"

// Outcome is the typed, credential-free result of one attempt.
type Outcome struct {
	// Stage is where the attempt stopped.
	Stage Stage `json:"stage"`

	// Reason is the normalized cause.
	Reason Reason `json:"reason"`

	// Confidence qualifies Reason.
	Confidence Confidence `json:"confidence"`

	// Retryable reports whether an identical later attempt may succeed
	// without configuration or reauthentication changes.
	Retryable bool `json:"retryable"`

	// Cached reports that a recent identical failure answered the attempt
	// without contacting the provider.
	Cached bool `json:"cached"`

	// Provider is the identity provider kind that produced the outcome; empty
	// when the attempt stopped before any provider was tried.
	Provider remotesessions.EMAProviderKind `json:"provider,omitempty"`

	// Unavailable says why the provider could not serve; set only with
	// ReasonProviderUnavailable.
	Unavailable remotesessions.EMAProviderUnavailableReason `json:"unavailable,omitempty"`

	// Submitted reports that a token request may already have reached the
	// provider, so repeating the attempt could mint a duplicate grant.
	Submitted bool `json:"submitted,omitempty"`

	// Failures lists every provider's failure when more than one provider in
	// the order was tried or skipped and none succeeded.
	Failures []ProviderFailure `json:"failures,omitempty"`

	// providerDescription is the provider's error_description, for logs only.
	providerDescription string
}

// Applicable reports whether identity chaining owns the upstream. When false,
// callers keep the interactive behavior they had without chaining.
func (o Outcome) Applicable() bool {
	return o.Reason != ReasonNotApplicable && o.Reason != ReasonBindingNotReady && o.Reason != ReasonProviderUnavailable
}

// SafeToRepeat reports a transient failure that is safe to repeat: one that
// happened before any token request could have reached the provider. A
// failure after submission is ambiguous, and repeating it could mint a
// duplicate grant against the provider's quota, so it, a shared failure
// answered from the cache, stale configuration and every provider rejection
// are never repeated. Combined provider failures omit individual submission
// details, so they cannot establish that repeating the whole chain is safe.
func (o Outcome) SafeToRepeat() bool {
	return o.Reason == ReasonTransientFailure && o.Retryable && !o.Cached && !o.Submitted && len(o.Failures) == 0
}

// Succeeded reports whether the attempt yielded a usable token.
func (o Outcome) Succeeded() bool { return o.Reason == ReasonSuccess }

// cacheable keeps provider rejections from repeating on every call; local,
// delegation and transient failures stay immediately retryable.
func (o Outcome) cacheable() bool { return o.ProviderRefusal() }

// ProviderRefusal reports a definitive refusal by the provider, the only
// failure a refresh can see as RefreshRefused.
func (o Outcome) ProviderRefusal() bool {
	if o.Retryable {
		return false
	}
	switch o.Stage {
	case StageExchange, StageValidation, StageRedemption:
		return true
	case StageSelection, StageAuthorization, StageDelegation, StagePersistence, StageComplete:
		return false
	}
	return false
}

func newOutcome(stage Stage, reason Reason, confidence Confidence, retryable bool) Outcome {
	return Outcome{Stage: stage, Reason: reason, Confidence: confidence, Retryable: retryable, Cached: false, Provider: "", Unavailable: "", Submitted: false, Failures: nil, providerDescription: ""}
}

var (
	notApplicable   = newOutcome(StageSelection, ReasonNotApplicable, ConfidenceVerified, false)
	bindingNotReady = newOutcome(StageSelection, ReasonBindingNotReady, ConfidenceVerified, false)
	success         = newOutcome(StageComplete, ReasonSuccess, ConfidenceVerified, false)
)
