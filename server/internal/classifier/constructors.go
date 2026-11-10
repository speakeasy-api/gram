package classifier

import "slices"

// NewRequest creates a batch with shared state and no questions. Construction is
// permissive; Classify validates the complete batch before provider execution.
func NewRequest(state Entry) *Request {
	return &Request{Input: state, Questions: nil}
}

// Ask appends a question in submission order and returns the same request for
// chaining. It mutates the request and must not race with classification.
func (r *Request) Ask(question Question) *Request {
	r.Questions = append(r.Questions, question)
	return r
}

// NoulOption configures the criteria of a Noul question.
type NoulOption func(*NoulQuestion)

// WithPositive sets positive-outcome criteria. Repeated options use the last value.
func WithPositive(criteria Entry) NoulOption {
	return func(question *NoulQuestion) { question.Positive = criteria }
}

// WithNegative sets negative-outcome criteria. Repeated options use the last value.
func WithNegative(criteria Entry) NoulOption {
	return func(question *NoulQuestion) { question.Negative = criteria }
}

// Noul constructs an independent positive-outcome probability question.
// Unspecified criteria are null. Options are applied in order.
func Noul(key QuestionKey, instructions Entry, options ...NoulOption) Question {
	var question NoulQuestion
	question.Instructions = instructions
	for _, option := range options {
		option(&question)
	}
	return Question{Key: key, Noul: &question, Choice: nil, Score: nil}
}

// Choice constructs a mutually exclusive choice question. It copies the
// options slice; validity and provider-specific limits are checked at execution time.
func Choice(key QuestionKey, instructions Entry, options ...Option) Question {
	return Question{
		Key:    key,
		Noul:   nil,
		Choice: &ChoiceQuestion{Instructions: instructions, Options: slices.Clone(options)},
		Score:  nil,
	}
}

// Score constructs an ordered rubric, copying the levels slice. Level order
// determines the zero-based scores, from low to high. Validity and provider-specific
// limits are checked at execution time.
func Score(key QuestionKey, instructions Entry, levels ...Option) Question {
	return Question{
		Key:    key,
		Noul:   nil,
		Choice: nil,
		Score:  &ScoreQuestion{Instructions: instructions, Levels: slices.Clone(levels)},
	}
}

// NewOption constructs a Choice option or Score level with an opaque correlation key.
func NewOption(key OptionKey, description Entry) Option {
	return Option{Key: key, Description: description}
}
