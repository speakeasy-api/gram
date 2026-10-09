package identitychaining

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

	// providerDescription is the provider's error_description, for logs only.
	providerDescription string
}

// Applicable reports whether identity chaining owns the upstream. When false,
// callers keep the interactive behavior they had without chaining.
func (o Outcome) Applicable() bool {
	return o.Reason != ReasonNotApplicable && o.Reason != ReasonBindingNotReady
}

// Succeeded reports whether the attempt yielded a usable token.
func (o Outcome) Succeeded() bool { return o.Reason == ReasonSuccess }

// cacheable keeps provider rejections from repeating on every call; local,
// delegation and transient failures stay immediately retryable.
func (o Outcome) cacheable() bool {
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
	return Outcome{Stage: stage, Reason: reason, Confidence: confidence, Retryable: retryable, Cached: false, providerDescription: ""}
}

var (
	notApplicable   = newOutcome(StageSelection, ReasonNotApplicable, ConfidenceVerified, false)
	bindingNotReady = newOutcome(StageSelection, ReasonBindingNotReady, ConfidenceVerified, false)
	success         = newOutcome(StageComplete, ReasonSuccess, ConfidenceVerified, false)
)
