package skills

type FeedbackOutcome string

const (
	FeedbackOutcomeHelped          FeedbackOutcome = "helped"
	FeedbackOutcomePartiallyHelped FeedbackOutcome = "partially_helped"
	FeedbackOutcomeDidNotHelp      FeedbackOutcome = "did_not_help"
	FeedbackOutcomeMisleading      FeedbackOutcome = "misleading"
	FeedbackOutcomeHarmful         FeedbackOutcome = "harmful"
)

func (o FeedbackOutcome) Valid() bool {
	switch o {
	case FeedbackOutcomeHelped,
		FeedbackOutcomePartiallyHelped,
		FeedbackOutcomeDidNotHelp,
		FeedbackOutcomeMisleading,
		FeedbackOutcomeHarmful:
		return true
	default:
		return false
	}
}

type FeedbackSource string

const (
	FeedbackSourceDev       FeedbackSource = "dev"
	FeedbackSourceAssistant FeedbackSource = "assistant"
)

func (s FeedbackSource) Valid() bool {
	return s == FeedbackSourceDev || s == FeedbackSourceAssistant
}

type EditSuggestionStatus string

const (
	EditSuggestionStatusOpen       EditSuggestionStatus = "open"
	EditSuggestionStatusApproved   EditSuggestionStatus = "approved"
	EditSuggestionStatusDismissed  EditSuggestionStatus = "dismissed"
	EditSuggestionStatusSuperseded EditSuggestionStatus = "superseded"
)
