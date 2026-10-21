package functions

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"hash/crc32"
	"testing"

	"github.com/stretchr/testify/require"
)

// maxCompressedBombBytes is the most a test decompression bomb may occupy
// compressed, keeping it far below the 15 MiB functions upload limit.
const maxCompressedBombBytes = 64 * 1024 // 64 KiB

func zipManifestEntry(t *testing.T, content []byte) (*zip.File, int) {
	t.Helper()

	buf := &bytes.Buffer{}
	w := zip.NewWriter(buf)
	fw, err := w.Create("manifest.json")
	require.NoError(t, err)
	_, err = fw.Write(content)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	r, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)
	require.Len(t, r.File, 1)

	return r.File[0], buf.Len()
}

// zipManifestEntryWithDeclaredSize writes content as a deflated entry whose
// header claims an uncompressed size of declaredSize, as a crafted archive can.
func zipManifestEntryWithDeclaredSize(t *testing.T, content []byte, declaredSize uint64) *zip.File {
	t.Helper()

	compressed := &bytes.Buffer{}
	fw, err := flate.NewWriter(compressed, flate.BestCompression)
	require.NoError(t, err)
	_, err = fw.Write(content)
	require.NoError(t, err)
	require.NoError(t, fw.Close())

	buf := &bytes.Buffer{}
	w := zip.NewWriter(buf)
	raw, err := w.CreateRaw(&zip.FileHeader{
		Name:               "manifest.json",
		Method:             zip.Deflate,
		CRC32:              crc32.ChecksumIEEE(content),
		CompressedSize64:   uint64(compressed.Len()),
		UncompressedSize64: declaredSize,
	})
	require.NoError(t, err)
	_, err = raw.Write(compressed.Bytes())
	require.NoError(t, err)
	require.NoError(t, w.Close())

	r, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)
	require.Len(t, r.File, 1)
	require.Equal(t, declaredSize, r.File[0].UncompressedSize64)

	return r.File[0]
}

func TestReadManifest_Normal(t *testing.T) {
	t.Parallel()

	content := []byte(`{"version":"0.0.0","tools":[{"name":"echo","description":"Echo input"}]}`)
	f, _ := zipManifestEntry(t, content)

	bs, err := readManifest(f)
	require.NoError(t, err)
	require.Equal(t, content, bs)

	var manifest Manifest
	require.NoError(t, manifest.UnmarshalJSON(bs))
	require.Len(t, manifest.V0.Tools, 1)
}

func TestReadManifest_ExactLimit(t *testing.T) {
	t.Parallel()

	content := bytes.Repeat([]byte(" "), MaxManifestBytes)
	f, _ := zipManifestEntry(t, content)

	bs, err := readManifest(f)
	require.NoError(t, err)
	require.Len(t, bs, MaxManifestBytes)
}

func TestReadManifest_InvalidJSONWithinLimitIsReadThenRejectedByParser(t *testing.T) {
	t.Parallel()

	content := []byte(`{"version":"0.0.0","tools":[`)
	f, _ := zipManifestEntry(t, content)

	bs, err := readManifest(f)
	require.NoError(t, err)
	require.Equal(t, content, bs)

	var manifest Manifest
	require.Error(t, manifest.UnmarshalJSON(bs))
}

func TestReadManifest_RejectsDecompressionBomb(t *testing.T) {
	t.Parallel()

	content := bytes.Repeat([]byte(" "), 4*MaxManifestBytes)
	f, compressedSize := zipManifestEntry(t, content)
	require.Less(t, compressedSize, maxCompressedBombBytes, "bomb should be small when compressed")

	bs, err := readManifest(f)
	require.ErrorIs(t, err, ErrManifestTooLarge)
	require.Nil(t, bs)
}

func TestReadManifest_RejectsOneByteOverLimit(t *testing.T) {
	t.Parallel()

	content := bytes.Repeat([]byte(" "), MaxManifestBytes+1)
	f, _ := zipManifestEntry(t, content)

	bs, err := readManifest(f)
	require.ErrorIs(t, err, ErrManifestTooLarge)
	require.Nil(t, bs)
}

func TestReadManifest_RejectsContentLargerThanDeclaredSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		declaredSize uint64
	}{
		{name: "declared small", declaredSize: 64},
		{name: "declared at limit", declaredSize: MaxManifestBytes},
	}

	content := bytes.Repeat([]byte(" "), 4*MaxManifestBytes)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := zipManifestEntryWithDeclaredSize(t, content, tc.declaredSize)

			bs, err := readManifest(f)
			require.Error(t, err)
			require.Nil(t, bs)
		})
	}
}
