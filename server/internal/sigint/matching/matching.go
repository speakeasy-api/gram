// Package matching defines the versioned sensor eligibility contract.
package matching

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/ext"
	"github.com/speakeasy-api/gram/server/internal/celeval"
)

// Version identifies the declarations and semantics used by sensor predicates.
const Version = "sigint-message-v1"

// DefaultExpression targets user messages when an author omits a predicate.
const DefaultExpression = `message.role == "user"`

// MaxExpressionBytes bounds author-controlled parsing work to 4 KiB.
const MaxExpressionBytes = 4096

// programCapacity bounds retained compiled definitions across projects.
const programCapacity = 1024

// evaluationCostLimit allows metadata predicates while bounding comprehensions.
const evaluationCostLimit uint64 = 10000

// Message contains current persisted metadata, never classifier content.
type Message struct {
	// Role is the lowercase persisted message role.
	Role string `cel:"role"`
}

// compiler shares successful programs across sensors and concurrent deliveries.
// Its environment is immutable; activations and results are never cached.
var compiler = sync.OnceValues(func() (*celeval.Compiler, error) {
	env, err := cel.NewEnv(ext.NativeTypes(reflect.TypeFor[Message](), ext.ParseStructTags(true)), cel.Variable("message", cel.ObjectType("matching.Message")))
	if err != nil {
		return nil, fmt.Errorf("create sensor matching environment: %w", err)
	}

	// Bound retained programs to 1024 and each evaluation to 10,000 CEL operations.
	c, err := celeval.New(env, celeval.Config{Capacity: programCapacity, Predicate: true, MaxExpressionBytes: MaxExpressionBytes, CostLimit: new(evaluationCostLimit)})
	if err != nil {
		return nil, fmt.Errorf("create sensor matching compiler: %w", err)
	}

	return c, nil
})

// Validate checks the same typed boolean contract used during evaluation.
func Validate(expression string) error {
	c, err := compiler()
	if err != nil {
		return err
	}

	if _, err := c.Compile(expression); err != nil {
		return fmt.Errorf("validate sensor predicate: %w", err)
	}

	return nil
}

// Match evaluates metadata with a cached program and a fresh activation.
// A nil message leaves the variable unbound: predicates requiring message data
// fail evaluation rather than treating an absent role as an empty string.
// Predicates independent of message data (such as true) can still succeed.
func Match(ctx context.Context, expression string, message *Message) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("match sensor: %w", err)
	}

	c, err := compiler()
	if err != nil {
		return false, err
	}

	program, err := c.Compile(expression)
	if err != nil {
		return false, fmt.Errorf("compile sensor predicate: %w", err)
	}

	activation := make(map[string]any)
	if message != nil {
		activation["message"] = message
	}

	matched, err := celeval.EvalPredicate(ctx, program, activation)
	if err != nil {
		return false, fmt.Errorf("evaluate sensor predicate: %w", err)
	}

	return matched, nil
}
