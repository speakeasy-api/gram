package classifier

// QuestionKey is a caller-assigned correlation key. Implementations treat it as
// opaque and must not use it as semantic classification instructions.
type QuestionKey string

// OptionKey identifies an option within one question. Implementations map it to
// provider identifiers without treating the key as an option description.
type OptionKey string

// Question defines one indivisible classification. Exactly one of Noul,
// Choice, or Score must be non-nil. Implementations validate this invariant
// and key uniqueness before submitting any provider requests.
type Question struct {
	// Key correlates the outcome to this question.
	Key QuestionKey

	// Noul asks for an independent positive-outcome probability.
	Noul *NoulQuestion

	// Choice selects among mutually exclusive options.
	Choice *ChoiceQuestion

	// Score evaluates an ordered rubric.
	Score *ScoreQuestion
}

// NoulQuestion asks whether a condition applies to the input.
type NoulQuestion struct {
	// Instructions states the question and any evaluation guidance.
	Instructions Entry

	// Positive optionally describes the positive outcome.
	Positive Entry

	// Negative optionally describes the negative outcome.
	Negative Entry
}

// Option defines the semantic meaning of one Choice option or Score level.
type Option struct {
	// Key is non-empty and unique within the containing question.
	Key OptionKey

	// Description defines the option for inference, including any display label
	// the caller wants the model to consider.
	Description Entry
}

// ChoiceQuestion chooses one option from a non-empty set. Provider-specific
// minimum and maximum option counts are checked by the implementation.
type ChoiceQuestion struct {
	// Instructions explains the decision to make.
	Instructions Entry

	// Options contains the complete set of mutually exclusive alternatives.
	Options []Option
}

// ScoreQuestion evaluates levels ordered from low to high. It requires at
// least two levels; provider-specific maxima are checked by the implementation.
type ScoreQuestion struct {
	// Instructions explains what the rubric measures.
	Instructions Entry

	// Levels defines the rubric; each zero-based index is that level's score.
	Levels []Option
}
