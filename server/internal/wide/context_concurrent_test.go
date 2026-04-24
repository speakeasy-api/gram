package wide_test

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/wide"
)

func TestConcurrentPushAllAttrsDelivered(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		const (
			goroutines = 16
			pushesEach = 64
		)

		ctx := wide.Start(t.Context())

		for g := range goroutines {
			go func() {
				for i := range pushesEach {
					wide.Push(ctx, slog.String("v", fmt.Sprintf("g%d-i%d", g, i)))
				}
			}()
		}
		synctest.Wait()

		got := wide.Emit(ctx)
		require.Len(t, got, goroutines*pushesEach)

		seen := make(map[string]bool, goroutines*pushesEach)
		for _, a := range got {
			v := a.Value.String()
			require.False(t, seen[v], "duplicate value: %s", v)
			seen[v] = true
		}
		require.Len(t, seen, goroutines*pushesEach)
	})
}

func TestConcurrentPushWithinBatchOrderPreserved(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		const (
			goroutines = 16
			batches    = 10
		)

		ctx := wide.Start(t.Context())

		for g := range goroutines {
			go func() {
				for b := range batches {
					prefix := fmt.Sprintf("g%d-b%d", g, b)
					wide.Push(ctx,
						slog.String("v", prefix+"-A"),
						slog.String("v", prefix+"-B"),
						slog.String("v", prefix+"-C"),
					)
				}
			}()
		}
		synctest.Wait()

		got := wide.Emit(ctx)
		require.Len(t, got, goroutines*batches*3)

		// Walk the output in triples. Cross-batch ordering is unspecified, but
		// each 3-tuple must be a contiguous (A, B, C) of the same batch prefix —
		// this is the regression surface for the intra-node reversal bug.
		for k := 0; k < len(got); k += 3 {
			a := got[k].Value.String()
			b := got[k+1].Value.String()
			c := got[k+2].Value.String()

			require.True(t, strings.HasSuffix(a, "-A"), "pos %d: got %q, want suffix -A", k, a)
			require.True(t, strings.HasSuffix(b, "-B"), "pos %d: got %q, want suffix -B", k+1, b)
			require.True(t, strings.HasSuffix(c, "-C"), "pos %d: got %q, want suffix -C", k+2, c)

			aPfx := strings.TrimSuffix(a, "-A")
			bPfx := strings.TrimSuffix(b, "-B")
			cPfx := strings.TrimSuffix(c, "-C")
			require.Equal(t, aPfx, bPfx, "batch prefix drift at pos %d", k)
			require.Equal(t, bPfx, cPfx, "batch prefix drift at pos %d", k)
		}
	})
}

func TestConcurrentPushAndEmit(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		const (
			writers   = 4
			perWriter = 32
		)

		ctx := wide.Start(t.Context())

		for g := range writers {
			go func() {
				for i := range perWriter {
					wide.Push(ctx, slog.String("v", fmt.Sprintf("w%d-i%d", g, i)))
					_ = wide.Emit(ctx) // interleave reads with writes
				}
			}()
		}
		synctest.Wait()

		got := wide.Emit(ctx)
		require.Len(t, got, writers*perWriter)

		seen := make(map[string]bool, writers*perWriter)
		for _, a := range got {
			v := a.Value.String()
			require.False(t, seen[v], "duplicate: %s", v)
			seen[v] = true
		}
	})
}

func TestConcurrentEmitNoTornReads(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		const (
			pushes    = 128
			emitters  = 4
			emitsEach = 16
		)

		ctx := wide.Start(t.Context())

		go func() {
			for i := range pushes {
				wide.Push(ctx, slog.String("v", fmt.Sprintf("p%d", i)))
			}
		}()

		for range emitters {
			go func() {
				for range emitsEach {
					snap := wide.Emit(ctx)
					for _, a := range snap {
						if a.Key == "" {
							t.Errorf("torn read: empty attr key in snapshot of len %d", len(snap))
							return
						}
					}
				}
			}()
		}
		synctest.Wait()

		got := wide.Emit(ctx)
		require.Len(t, got, pushes)
	})
}
