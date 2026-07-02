package customruleanalyzer

import (
	"fmt"

	"github.com/google/cel-go/cel"
	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
)

// evaluatorCacheSize bounds the number of distinct compiled CEL programs held in
// memory — effectively "how many distinct custom-rule predicates we keep
// compiled at once" across all projects.
const evaluatorCacheSize = 8192

// evaluator evaluates CEL detection expressions against messages, memoizing each
// expression's compiled program so identical predicates compile once across
// every message and project. An edited rule mints a new expression and
// recompiles; least-recently-used programs are evicted to bound memory. The
// underlying LRU is goroutine-safe and cel.Program is safe for concurrent
// evaluation, so a single evaluator serves all receive goroutines without extra
// locking.
type evaluator struct {
	eng   *celenv.Engine
	cache *lru.Cache[string, cel.Program]
}

func newEvaluator(size int) (*evaluator, error) {
	eng, err := celenv.New()
	if err != nil {
		return nil, fmt.Errorf("create cel engine: %w", err)
	}

	cache, err := lru.New[string, cel.Program](size)
	if err != nil {
		return nil, fmt.Errorf("create compile cache: %w", err)
	}

	return &evaluator{eng: eng, cache: cache}, nil
}

// execute evaluates expr against msg and returns the matched spans. The compiled
// program for expr is memoized: a repeated expression is a cache hit and is not
// recompiled. A compile failure is not cached, so a transiently bad expression
// is retried on its next occurrence.
func (e *evaluator) execute(expr string, msg celenv.Message) ([]celenv.Span, bool, error) {
	prg, ok := e.cache.Get(expr)
	if !ok {
		compiled, err := e.eng.Compile(expr)
		if err != nil {
			return nil, false, fmt.Errorf("compile detection expr: %w", err)
		}
		e.cache.Add(expr, compiled)
		prg = compiled
	}

	spans, matched, err := e.eng.EvalDetection(prg, msg)
	if err != nil {
		return nil, false, fmt.Errorf("eval detection expr: %w", err)
	}

	return spans, matched, nil
}
