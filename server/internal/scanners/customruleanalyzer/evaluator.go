package customruleanalyzer

import (
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/celeval"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
)

// evaluatorCacheSize bounds the number of distinct compiled CEL programs held in
// memory across all projects. Edited expressions acquire independent entries.
const evaluatorCacheSize = 8192

// evaluator shares compiled predicates across messages, while the risk engine
// owns the language and creates an independent span collector for each call.
type evaluator struct {
	eng      *celenv.Engine
	compiler *celeval.Compiler
}

func newEvaluator(size int) (*evaluator, error) {
	eng, err := celenv.New()
	if err != nil {
		return nil, fmt.Errorf("create cel engine: %w", err)
	}
	compiler, err := eng.NewCompiler(size)
	if err != nil {
		return nil, fmt.Errorf("create compile cache: %w", err)
	}
	return &evaluator{eng: eng, compiler: compiler}, nil
}

// execute evaluates expr against msg and returns the matched spans. Compilation
// failures are not cached; callers retain their existing retry/error policy.
func (e *evaluator) execute(expr string, msg celenv.Message) ([]celenv.Span, bool, error) {
	prg, err := e.compiler.Compile(expr)
	if err != nil {
		return nil, false, fmt.Errorf("compile detection expr: %w", err)
	}
	spans, matched, err := e.eng.EvalDetection(prg, msg)
	if err != nil {
		return nil, false, fmt.Errorf("eval detection expr: %w", err)
	}
	return spans, matched, nil
}
