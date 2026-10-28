//go:build dagger_integration

package main

import (
	"testing"

	"dagger/go-pipeline/internal/dagger"

	"github.com/stretchr/testify/require"
)

func TestPublishFunctionsRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		prefix string
		arch   string
		err    string
	}{
		{name: "empty prefix", prefix: "", arch: "x86_64", err: "image-prefix must not be empty"},
		{name: "blank prefix", prefix: " \t", arch: "x86_64", err: "image-prefix must not be empty"},
		{name: "unsupported architecture", prefix: "registry.example/functions", arch: "invalid", err: `unsupported Functions architecture "invalid"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Invalid inputs must be rejected before accessing the tarball or credentials.
			pipeline := new(GoPipeline)
			_, err := pipeline.PublishFunctions(t.Context(), nil, "{}", tc.prefix, nil, "x", tc.arch)
			require.EqualError(t, err, tc.err)
		})
	}
}

func TestImportFunctionsImageArchitecture(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		arch     string
		platform dagger.Platform
	}{
		{arch: "x86_64", platform: "linux/amd64"},
		{arch: "aarch64", platform: "linux/arm64"},
	} {
		t.Run(tc.arch, func(t *testing.T) {
			t.Parallel()

			tarball := dag.Container(dagger.ContainerOpts{Platform: tc.platform}).
				WithNewFile("/marker", tc.arch).AsTarball()
			image, err := importFunctionsImage(tarball, tc.arch)
			require.NoError(t, err)

			platform, err := image.Platform(t.Context())
			require.NoError(t, err)
			require.Equal(t, tc.platform, platform)

			contents, err := image.File("/marker").Contents(t.Context())
			require.NoError(t, err)
			require.Equal(t, tc.arch, contents)
		})
	}
}
