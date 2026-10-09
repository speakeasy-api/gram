package gram

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/infra/pkg/storage"
	"github.com/speakeasy-api/gram/server/internal/lake"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestNewLakeStorageLocalDefaultsToFilesystem(t *testing.T) {
	t.Parallel()
	directory := filepath.Join(t.TempDir(), "objects")
	flags := flag.NewFlagSet("lake", flag.ContinueOnError)
	flags.String("environment", "local", "")
	flags.String("storage-buckets", "", "")
	flags.String("lake-directory", directory, "")
	c := cli.NewContext(cli.NewApp(), flags, nil)

	store, buckets, shutdown, err := newLakeStorage(t.Context(), testenv.NewLogger(t), c)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, shutdown(t.Context())) })
	require.IsType(t, &lake.FilesystemStore{}, store)
	require.Equal(t, map[string]string{"lake": "lake"}, buckets)

	err = store.Write(t.Context(), storage.Object{Bucket: buckets["lake"], Name: "example.parquet"}, func(w io.Writer) error {
		_, err := io.WriteString(w, "local object")
		require.NoError(t, err)
		return nil
	})
	require.NoError(t, err)
	contents, err := os.ReadFile(filepath.Join(directory, "lake", "example.parquet"))
	require.NoError(t, err)
	require.Equal(t, "local object", string(contents))
}

func TestNewLakeStorageNonlocalRequiresBucketMapping(t *testing.T) {
	t.Parallel()
	for _, environment := range []string{"test", "prod", ""} {
		t.Run(environment, func(t *testing.T) {
			t.Parallel()
			flags := flag.NewFlagSet("lake", flag.ContinueOnError)
			flags.String("environment", environment, "")
			flags.String("storage-buckets", "", "")
			c := cli.NewContext(cli.NewApp(), flags, nil)

			store, _, _, err := newLakeStorage(t.Context(), testenv.NewLogger(t), c)
			require.ErrorContains(t, err, "storage bucket mapping for lake is required")
			require.Nil(t, store)
		})
	}
}
