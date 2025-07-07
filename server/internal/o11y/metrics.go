package o11y

type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure"
)

func OutcomeFromError(err error) Outcome {
	if err == nil {
		return OutcomeSuccess
	}
	return OutcomeFailure
}
