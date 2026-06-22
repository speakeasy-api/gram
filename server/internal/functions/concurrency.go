package functions

import "math"

// runtimeConcurrency describes how to size a runtime's execution capacity N
// from a machine's memory allocation.
type runtimeConcurrency struct {
	// maxSlots caps N at the CPU-bound ceiling. Guest CPUs are fixed at 4 shared
	// vCPUs across all memory tiers, so beyond this point more memory does not
	// buy usable concurrency.
	maxSlots int

	// memPerSlotMiB is the approximate peak memory budget for one concurrent
	// execution (subprocess RSS plus headroom). Machine memory divided by this
	// yields the memory-bound slot count.
	memPerSlotMiB int
}

const (
	// hardLimitMultiple sets the Fly proxy hard limit at a multiple of the execution
	// capacity N. The hard limit is intentionally above N: the runner semaphore
	// (sized to N) is the real memory/CPU guard, and requests beyond N are cheap
	// parked goroutines, not subprocesses. Routing a bounded burst beyond N into the
	// runner lets its limiter briefly hold the overflow and return a clean
	// 429 + Retry-After, instead of the Fly proxy shedding it (queue then 503) before
	// the runner can respond. If hard equaled N, the proxy would cap every machine at
	// N and the runner's 429 path could effectively never fire.
	hardLimitMultiple = 2

	// minExecutionSlots floors N so even the smallest machine accepts a little
	// concurrency.
	minExecutionSlots = 4
)

// runtimeConcurrencyTable holds the interim, benchmark-tunable sizing inputs per
// runtime. The values are deliberately conservative: the previous memory/48
// formula over-provisioned the request cap relative to real execution capacity.
// Tune with the runner concurrency benchmark in functions/internal/runner;
// revisit once warm runtime pooling lands and the pool size becomes the direct
// source of N.
var runtimeConcurrencyTable = map[Runtime]runtimeConcurrency{
	RuntimeNodeJS22:  {memPerSlotMiB: 128, maxSlots: 24},
	RuntimeNodeJS24:  {memPerSlotMiB: 128, maxSlots: 24},
	RuntimePython312: {memPerSlotMiB: 128, maxSlots: 24},
}

// fallbackConcurrency sizes unknown runtimes conservatively: a larger per-slot
// budget and a low ceiling.
var fallbackConcurrency = runtimeConcurrency{memPerSlotMiB: 192, maxSlots: 8}

// executionSlots returns N, the number of tool/resource calls a single runner
// machine can execute concurrently for the given runtime and memory allocation.
// Both the Fly proxy concurrency limits and the in-runner concurrency cap derive
// from this one value.
//
// N scales with memory (a larger machine fits more concurrent subprocesses) but
// is capped by a per-runtime CPU ceiling, since vCPUs are fixed across memory
// tiers. This gives higher tiers a bounded, sub-linear concurrency affordance
// rather than the previous unbounded memory/48 slope that inflated the request
// cap without reflecting real execution capacity.
func executionSlots(runtime Runtime, memoryMiB int) int {
	cfg, ok := runtimeConcurrencyTable[runtime]
	if !ok {
		cfg = fallbackConcurrency
	}

	slots := memoryMiB / cfg.memPerSlotMiB
	return max(min(slots, cfg.maxSlots), minExecutionSlots)
}

// concurrencyLimits derives the Fly proxy soft and hard concurrency limits from
// the runner's execution capacity N. The hard limit is hardLimitMultiple*N so the
// proxy admits a bounded queue of requests beyond the execution slots. The soft
// limit sits at ~0.65*N (derived from N, not the hard limit, and strictly below
// N for N >= 2) so the proxy begins spreading load and triggers autostart of
// additional machines while the machine still has execution headroom: Fly
// autostart keys on the soft concurrency count, not on response status, so soft
// must be reached well before the runner starts shedding with 429.
func concurrencyLimits(slots int) (softLimit, hardLimit int) {
	n := max(slots, 1)
	hardLimit = n * hardLimitMultiple
	softLimit = max(int(math.Round(float64(n)*0.65)), 1)
	return softLimit, hardLimit
}
