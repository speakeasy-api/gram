package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"dagger/go-pipeline/internal/dagger"
)

// Build the server Dockerfile with the exported binaries and smoke-test it.
func (m *GoPipeline) ServerImage(ctx context.Context, binaries *dagger.Directory) (*dagger.Container, error) {
	image := m.Source.Directory("server").WithDirectory("bin", binaries).
		DockerBuild(dagger.DirectoryDockerBuildOpts{Platform: "linux/amd64"})
	if _, err := image.WithExec([]string{"version"}, dagger.ContainerWithExecOpts{UseEntrypoint: true}).Sync(ctx); err != nil {
		return nil, fmt.Errorf("server image smoke test: %w", err)
	}
	return image, nil
}

// Publish the server under the exact tags and labels supplied by docker/metadata-action.
func (m *GoPipeline) PublishServer(
	ctx context.Context,
	binaries *dagger.Directory,
	metadata string,
	password *dagger.Secret,
	// +default="oauth2accesstoken"
	username string,
) (string, error) {
	image, err := m.ServerImage(ctx, binaries)
	if err != nil {
		return "", err
	}
	return publish(ctx, image, metadata, "", username, password)
}

// Build signed melange packages using an isolated, disposable Docker daemon.
func (m *GoPipeline) FunctionsPackages(
	signingKey *dagger.Secret,
	commit string,
	buildDate string,
	// APK target architecture. Cross-architecture builds need host binfmt support.
	// +default="x86_64"
	arch string,
) (*dagger.Directory, error) {
	if arch != "x86_64" && arch != "aarch64" {
		return nil, fmt.Errorf("unsupported Functions architecture %q", arch)
	}
	return m.withDocker(m.base("melange")).
		WithMountedSecret("/run/secrets/melange.rsa", signingKey).
		WithExec([]string{
			"/usr/local/bin/docker-exec", "mise", "run", "--skip-deps", "build:functions-bin",
			"--melange-private-key", "/run/secrets/melange.rsa",
			"--commit", commit, "--build-date", buildDate, "--arch", arch,
		}, dagger.ContainerWithExecOpts{InsecureRootCapabilities: true}).
		Directory("/src/functions/packages"), nil
}

// Build an apko runtime image and run the existing filesystem safety checks.
// Returns the image tarball and SBOMs for publication or local inspection.
func (m *GoPipeline) FunctionsImage(
	packages *dagger.Directory,
	publicKey *dagger.File,
	apkoConfig string,
	image string,
	buildDate string,
	// APK target architecture; must match the signed packages.
	// +default="x86_64"
	arch string,
) (*dagger.Directory, error) {
	var imageArch string
	switch arch {
	case "x86_64":
		imageArch = "amd64"
	case "aarch64":
		imageArch = "arm64"
	default:
		return nil, fmt.Errorf("unsupported Functions architecture %q", arch)
	}
	switch apkoConfig {
	case "./images/nodejs22-alpine3.22.yaml", "./images/nodejs24-alpine3.23.yaml":
	default:
		return nil, fmt.Errorf("unsupported apko configuration %q", apkoConfig)
	}
	ctr := m.withDocker(m.base("apko", "aube")).
		WithDirectory("/src/functions/packages", packages).
		WithFile("/run/keys/melange.rsa.pub", publicKey).
		WithExec([]string{"mise", "exec", "--", "aube", "install", "--frozen-lockfile", "--workspace-root", "--filter", "."}).
		WithExec([]string{
			"mise", "run", "build:functions-image", "--melange-public-key", "/run/keys/melange.rsa.pub",
			"--apko-config", apkoConfig, "--image", image, "--tarball-name", "runner.tar", "--out", "oci",
			"--build-date", buildDate, "--arch", arch,
		}).
		WithExec([]string{
			"/usr/local/bin/docker-exec", "mise", "run", "test:apko-functions",
			"--image", image, "--tarball", "oci/runner.tar", "--arch", imageArch,
		}, dagger.ContainerWithExecOpts{InsecureRootCapabilities: true})
	return ctr.Directory("/src/functions/oci"), nil
}

// Publish an apko tarball to one registry image, preserving metadata tags.
func (m *GoPipeline) PublishFunctions(
	ctx context.Context,
	tarball *dagger.File,
	metadata string,
	imagePrefix string,
	password *dagger.Secret,
	// +default="x"
	username string,
) (string, error) {
	image := dag.Container(dagger.ContainerOpts{Platform: "linux/amd64"}).Import(tarball)
	return publish(ctx, image, metadata, imagePrefix, username, password)
}

// Build the tunnel agent for both production architectures without publishing.
func (m *GoPipeline) TunnelImages(ctx context.Context) error {
	for _, platform := range []dagger.Platform{"linux/amd64", "linux/arm64"} {
		_, err := m.Source.DockerBuild(dagger.DirectoryDockerBuildOpts{
			Dockerfile: "tunnel/Dockerfile",
			Platform:   platform,
			BuildArgs:  []dagger.BuildArg{{Name: "VERSION", Value: "0.0.0-pr"}},
		}).Sync(ctx)
		if err != nil {
			return fmt.Errorf("build tunnel image for %s: %w", platform, err)
		}
	}
	return nil
}

// imageMetadata is the JSON output contract of docker/metadata-action.
type imageMetadata struct {
	// Fully qualified image tags.
	Tags []string `json:"tags"`

	// OCI labels shared by every tag.
	Labels map[string]string `json:"labels"`
}

func publish(ctx context.Context, image *dagger.Container, metadata, prefix, username string, password *dagger.Secret) (string, error) {
	var meta imageMetadata
	if err := json.Unmarshal([]byte(metadata), &meta); err != nil {
		return "", fmt.Errorf("parse image metadata: %w", err)
	}
	labels := make([]string, 0, len(meta.Labels))
	for key := range meta.Labels {
		labels = append(labels, key)
	}
	slices.Sort(labels)
	for _, key := range labels {
		image = image.WithLabel(key, meta.Labels[key])
	}

	var published []string
	for _, tag := range meta.Tags {
		if prefix != "" && !strings.HasPrefix(tag, prefix+":") {
			continue
		}
		registry, _, ok := strings.Cut(tag, "/")
		if !ok {
			return "", fmt.Errorf("image tag must be registry-qualified: %q", tag)
		}
		ref, err := image.WithRegistryAuth(registry, username, password).Publish(ctx, tag)
		if err != nil {
			return "", fmt.Errorf("publish %s: %w", tag, err)
		}
		published = append(published, ref)
	}
	if len(published) == 0 {
		return "", fmt.Errorf("no image tags matched %q", prefix)
	}
	return strings.Join(published, "\n"), nil
}
