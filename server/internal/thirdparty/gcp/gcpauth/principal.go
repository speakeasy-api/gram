package gcpauth

// Principal is the effective identity a credential resolves to.
type Principal struct {
	// Email is the resolved principal (a service-account email). It may be empty
	// when the resolution source cannot report one (e.g. local ADC backed by a
	// user login rather than a service-account key); an empty Email on a
	// successful resolve is not itself a failure.
	Email string

	// Source records how Email was resolved.
	Source Source
}
