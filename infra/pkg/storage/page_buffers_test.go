package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
	"github.com/stretchr/testify/require"
)

func TestProcess_DiskPageBuffersAreReleased(t *testing.T) {
	t.Parallel()
	for _, outcome := range []string{"success", "cancellation", "encoder-panic", "write-error", "write-panic", "commit-error"} {
		t.Run(outcome, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			parent := t.TempDir()
			// A neighboring directory belongs to another in-flight object/runner.
			neighbor := filepath.Join(parent, "gram-parquet-neighbor")
			require.NoError(t, os.Mkdir(neighbor, 0o700))
			sentinel := filepath.Join(neighbor, "column-existing")
			require.NoError(t, os.WriteFile(sentinel, []byte("keep"), 0o600))
			var out bytes.Buffer
			r, _ := testRunner(t, storeFunc(func(ctx context.Context, o Object, encode func(io.Writer) error) error {
				var destination io.Writer = &out
				if outcome == "write-error" {
					destination = failingWriter{}
				} else if outcome == "write-panic" {
					destination = panicWriter{}
				}
				if err := encode(destination); err != nil {
					return err
				}
				if outcome == "commit-error" {
					return errors.New("commit response lost")
				}
				return nil
			}), false, Settings{})
			r.config.TempDir = parent
			decode := r.def.Decode
			calls, pageBytes := 0, 0
			var inspectErr error
			r.def.Decode = func(data []byte, meta Metadata) (parquet.Row, error) {
				calls++
				if calls == 2 {
					// The first large row crosses the page threshold before the row
					// group closes. Prove real page files exist before aborting.
					var files []string
					files, inspectErr = filepath.Glob(filepath.Join(parent, "gram-parquet-*", "column-*"))
					for _, file := range files {
						if file == sentinel {
							continue
						}
						var content []byte
						content, inspectErr = os.ReadFile(file)
						if inspectErr != nil {
							return nil, inspectErr
						}
						pageBytes += len(content)
					}
					switch outcome {
					case "cancellation":
						cancel()
					case "encoder-panic":
						panic("encoder failed with open page buffers")
					}
				}
				return decode(data, meta)
			}
			var messages []*delivery
			var states []*settlement
			for _, data := range []string{strings.Repeat("x", 2*parquetBufferBytes), "next", "last"} {
				message, state := testDelivery(data, "region=one", time.Now())
				messages = append(messages, message)
				states = append(states, state)
			}
			r.process(ctx, messages)
			require.NoError(t, inspectErr)
			require.Positive(t, pageBytes, "encoded pages must be spooled to disk before completing the row group")
			entries, err := os.ReadDir(parent)
			require.NoError(t, err)
			require.Len(t, entries, 1, "the object scratch directory must be removed on every exit path")
			content, err := os.ReadFile(sentinel)
			require.NoError(t, err)
			require.Equal(t, "keep", string(content), "cleanup must not touch another writer's directory")
			for _, state := range states {
				if outcome == "success" {
					require.Equal(t, int32(1), state.acks.Load())
					require.Zero(t, state.nacks.Load())
				} else {
					require.Zero(t, state.acks.Load())
					require.Equal(t, int32(1), state.nacks.Load())
				}
			}
			if outcome == "success" {
				file, err := parquet.OpenFile(bytes.NewReader(out.Bytes()), int64(out.Len()))
				require.NoError(t, err)
				require.Equal(t, int64(3), file.NumRows())
			}
		})
	}
}

type panicWriter struct{}

func (panicWriter) Write([]byte) (int, error) { panic("upload panicked") }

func TestProcess_UnavailablePageDirectoryNacks(t *testing.T) {
	t.Parallel()
	r, _ := testRunner(t, storeFunc(func(ctx context.Context, o Object, encode func(io.Writer) error) error {
		return encode(io.Discard)
	}), false, Settings{})
	r.config.TempDir = filepath.Join(t.TempDir(), "missing")
	message, state := testDelivery("payload", "region=one", time.Now())
	r.process(t.Context(), []*delivery{message})
	require.Zero(t, state.acks.Load())
	require.Equal(t, int32(1), state.nacks.Load())
}
