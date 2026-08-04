package policies

import (
	"context"

	"github.com/speakeasy-api/agenthooks"
)

// actorKey carries the resolved actor for the gating stages. Only the
// ActorResolution middleware writes it: stages depend on "the actor is in
// ctx", not on how it got there.
type actorKey struct{}

// ActorFromContext returns the actor ActorResolution stashed for the current
// event, or the zero Actor when the middleware has not run.
func ActorFromContext(ctx context.Context) Actor {
	actor, _ := ctx.Value(actorKey{}).(Actor)
	return actor
}

// ActorResolution stashes the request's resolved actor in ctx for the gating
// stages. The resolution itself is shared with the rest of Ingest: the actor
// is resolved exactly once per request (including the session-metadata cache
// fallback) and carried in the policy request, so enforcement and
// persistence can never disagree on the identity — resolving again here
// could diverge when the session cache changes between reads.
func ActorResolution(ctx context.Context, typed any, next agenthooks.Next) (agenthooks.Decision, error) {
	if req := RequestFromContext(ctx); req != nil {
		ctx = context.WithValue(ctx, actorKey{}, req.Actor)
	}
	return next(ctx, typed)
}
