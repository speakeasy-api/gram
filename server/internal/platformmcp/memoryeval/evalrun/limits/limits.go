// Package limits holds the per-run spend caps for Organizational Memory eval
// runs. It is a leaf package so the admin Goa design can bound the start
// payload with the same values the runner enforces and options returns.
package limits

// MaxRuns bounds repeats per answer model and arm so one run cannot spend
// without limit.
const MaxRuns = 3

// MaxAnswerModels bounds the models one run compares.
const MaxAnswerModels = 4
