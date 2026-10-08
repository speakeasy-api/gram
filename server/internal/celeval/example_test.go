package celeval_test

import (
	"context"
	"fmt"

	"github.com/google/cel-go/cel"

	"github.com/speakeasy-api/gram/server/internal/celeval"
)

func ExampleCompiler() {
	env, err := cel.NewEnv(cel.Variable("name", cel.StringType))
	if err != nil {
		panic(err)
	}
	compiler, err := celeval.New(env, celeval.Config{
		Capacity:           128,
		Predicate:          true,
		MaxExpressionBytes: 4096,
		CostLimit:          new(uint64(10000)),
	})
	if err != nil {
		panic(err)
	}
	program, err := compiler.Compile(`name.startsWith("example")`)
	if err != nil {
		panic(err)
	}
	matched, err := celeval.EvalPredicate(context.Background(), program, map[string]any{"name": "example input"})
	if err != nil {
		panic(err)
	}
	fmt.Println(matched)
	// Output: true
}
