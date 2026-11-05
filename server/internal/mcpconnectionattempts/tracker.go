package mcpconnectionattempts

import (
	"context"
	"errors"
	"slices"
)

// NoVerdictDetail is the sub-reason of a request that left without saying what happened.
const NoVerdictDetail = "no_verdict"

// Tracker follows one request that can leave many ways, so an exit nobody classified
// shows up as internal_error rather than as nothing. Use it from the request's goroutine:
//
//	t := recorder.Track(scope, step, options...)
//	defer t.Finish(ctx)
//	t.Refuse(ctx, code)  // every refusal branch
//	t.Attach(attempt)    // once the attempt is known
//	t.Consumed()         // from here a verdict ends the attempt
//	t.Succeed(ctx, result) or t.Continue() // on the way out
//
// The first verdict is the only one recorded. With no attempt attached it is a Once row,
// before Consumed a Step, and after it an End. Nil is safe, and nothing here returns an error.
type Tracker struct {
	recorder *Recorder
	scope    Scope
	step     Step
	attempt  Attempt
	opts     []Option
	ending   bool
	decided  bool
}

// Track starts following one request. The options go on whatever row it records.
func (r *Recorder) Track(scope Scope, step Step, opts ...Option) *Tracker {
	if r == nil {
		return nil
	}
	return &Tracker{recorder: r, scope: scope, step: step, attempt: NoAttempt, opts: opts, ending: false, decided: false}
}

// Scope replaces the scope, for when validating a client names the app.
func (t *Tracker) Scope(scope Scope) {
	if t != nil {
		t.scope = scope
	}
}

// At moves the request on to a later step.
func (t *Tracker) At(step Step) {
	if t != nil {
		t.step = step
	}
}

// With adds options once they are known, such as the subject after sign-in.
func (t *Tracker) With(opts ...Option) {
	if t != nil {
		t.opts = append(slices.Clone(t.opts), opts...)
	}
}

// Attach names the attempt this request belongs to. A zero attempt leaves it on Once rows.
func (t *Tracker) Attach(attempt Attempt) {
	if t != nil {
		t.attempt = attempt
	}
}

// Consumed says the sign-in state is spent, so the next verdict ends the attempt.
func (t *Tracker) Consumed() {
	if t != nil {
		t.ending = true
	}
}

// Refuse is the verdict for anything but success. It records one row.
func (t *Tracker) Refuse(ctx context.Context, code Code, opts ...Option) {
	t.decide(ctx, code, opts)
}

// Succeed is the verdict for success. It records one row.
func (t *Tracker) Succeed(ctx context.Context, result Result, opts ...Option) {
	t.decide(ctx, result, opts)
}

// Continue is the only silent exit: the flow goes on, and a later request reports how it went.
func (t *Tracker) Continue() {
	if t != nil {
		t.decided = true
	}
}

// Finish goes in a defer. With no verdict it records internal_error, so a missed branch is
// visible, unless the caller disconnected, which isn't an outage.
func (t *Tracker) Finish(ctx context.Context) {
	if errors.Is(ctx.Err(), context.Canceled) {
		t.Continue()
		return
	}
	t.decide(ctx, CodeInternalError, []Option{Detail(NoVerdictDetail)})
}

func (t *Tracker) decide(ctx context.Context, what Happened, opts []Option) {
	if t == nil || t.decided {
		return
	}
	t.decided = true

	all := append(slices.Clone(t.opts), opts...)
	switch {
	case t.attempt.IsZero():
		t.recorder.Once(ctx, t.scope, t.step, what, all...)
	case t.ending:
		t.recorder.End(ctx, t.attempt, t.step, what, all...)
	default:
		t.recorder.Step(ctx, t.attempt, t.step, what, all...)
	}
}
