package customruleanalyzer

import (
	"fmt"

	"github.com/google/cel-go/cel"
	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/sync/singleflight"

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
// evaluation; concurrent cache misses for the same expression are collapsed onto
// a single compilation via singleflight, so a single evaluator serves all
// receive goroutines with no duplicate work and no extra locking.
type evaluator struct {
	eng           *celenv.Engine
	cache         *lru.Cache[string, cel.Program]
	compileFlight singleflight.Group
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

	return &evaluator{eng: eng, cache: cache, compileFlight: singleflight.Group{}}, nil
}

// program returns the compiled program for expr, compiling on a cache miss.
// Concurrent misses for the same expression share one compilation via
// singleflight rather than each recompiling, preserving the "compile once
// globally" guarantee under load. A compile failure is not cached, so a
// transiently bad expression is retried on its next occurrence.
func (e *evaluator) program(expr string) (cel.Program, error) {
	if prg, ok := e.cache.Get(expr); ok {
		return prg, nil
	}

	v, err, _ := e.compileFlight.Do(expr, func() (any, error) {
		// A concurrent flight for expr may have already compiled and cached it
		// while we waited, so re-check before compiling again.
		if prg, ok := e.cache.Get(expr); ok {
			return prg, nil
		}

		prg, err := e.eng.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf("compile detection expr: %w", err)
		}
		e.cache.Add(expr, prg)
		return prg, nil
	})
	if err != nil {
		return nil, err
	}

	prg, ok := v.(cel.Program)
	if !ok {
		return nil, fmt.Errorf("compile flight returned unexpected type %T", v)
	}
	return prg, nil
}

// execute evaluates expr against msg and returns the matched spans.
func (e *evaluator) execute(expr string, msg celenv.Message) ([]celenv.Span, bool, error) {
	prg, err := e.program(expr)
	if err != nil {
		return nil, false, err
	}

	spans, matched, err := e.eng.EvalDetection(prg, msg)
	if err != nil {
		return nil, false, fmt.Errorf("eval detection expr: %w", err)
	}

	return spans, matched, nil
}
