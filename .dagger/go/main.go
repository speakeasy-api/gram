// Containerized build, lint, test, and image pipelines for the Go projects.
package main

import (
	"context"
	"fmt"
	"strings"

	"dagger/go-pipeline/internal/dagger"
)

type GoPipeline struct {
	// Repository inputs, without local configuration or build outputs.
	// +private
	Source *dagger.Directory

	// Workspace metadata supplies the real commit for binary version stamps.
	// +private
	Workspace *dagger.Workspace

	// Docker lifecycle wrapper, shared by tests and packaging.
	// +private
	DockerExec *dagger.File
}

func New(ws *dagger.Workspace) *GoPipeline {
	return &GoPipeline{
		Source: ws.Directory("/", dagger.WorkspaceDirectoryOpts{
			Gitignore: true,
			Exclude: []string{
				".git", ".dagger", "local", "**/node_modules", "**/bin", "**/dist", "**/target",
				"mise.local.toml", "mise.*.local.toml", "**/.env", "**/.env.*", "gha-creds-*.json",
				"**/junit-report.xml", "**/test-timings.json", "**/test-exit-code", "**/test-output.txt",
				"functions/packages", "functions/oci",
			},
		}),
		Workspace:  ws,
		DockerExec: ws.File("/.dagger/docker-exec.sh"),
	}
}

// Build binaries using the same mise tasks as local development.
func (m *GoPipeline) Build(
	ctx context.Context,
	component string,
	// Assistant image content hash stamped into the server.
	// +default="dev"
	runtimeImageHash string,
) (*dagger.Directory, error) {
	switch component {
	case "server", "cli", "plog", "hooks":
	default:
		return nil, fmt.Errorf("no binary build for %q", component)
	}

	args := []string{"mise", "run", "--skip-deps", "build:" + component, "--readonly"}
	if component == "server" || component == "cli" {
		commit, err := m.Workspace.Git().Head().CommitSHA(ctx)
		if err != nil {
			return nil, fmt.Errorf("resolve build commit: %w", err)
		}
		args = append(args, "--commit", commit)
	}

	ctr := m.base().WithEnvVariable("CGO_ENABLED", "0").
		WithEnvVariable("GOOS", "linux").
		WithEnvVariable("GOARCH", "amd64").
		WithEnvVariable("GRAM_ASSISTANT_RUNTIME_IMAGE_HASH", runtimeImageHash).
		WithExec(args)
	if component == "server" {
		ctr = ctr.WithExec([]string{"mise", "run", "--skip-deps", "build:tunnel-gateway", "--readonly"})
	}
	return ctr.Directory("/src/" + component + "/bin"), nil
}

// Lint a Go project; server lint includes the repository's custom glint plugin.
func (m *GoPipeline) Lint(ctx context.Context, component string) error {
	switch component {
	case "server", "cli", "plog", "functions":
	default:
		return fmt.Errorf("no lint task for %q", component)
	}
	ctr := m.base("golangci-lint").
		WithMountedCache("/cache/golangci-lint", dag.CacheVolume("go-pipeline-golangci-lint")).
		WithEnvVariable("GOLANGCI_LINT_CACHE", "/cache/golangci-lint")
	if component == "server" {
		// The task fingerprints the plugin and toolchain before reusing gcl.
		ctr = ctr.WithMountedCache("/src/server/bin", dag.CacheVolume("go-pipeline-gcl"))
	}
	_, err := ctr.WithExec([]string{"mise", "run", "lint:" + component}).Sync(ctx)
	return err
}

// Run tests and return reports plus test-exit-code, including on test failure.
// Callers must check test-exit-code after exporting the directory.
func (m *GoPipeline) Test(
	component string,
	// Server test shard, index/total.
	// +optional
	shard string,
	// +optional
	race bool,
	// Run the separately tagged demo seed safety test.
	// +optional
	demoSeed bool,
	// +default=["./..."]
	packages []string,
) (*dagger.Directory, error) {
	switch component {
	case "server", "cli", "plog", "functions", "hooks", "glint", "ci":
	default:
		return nil, fmt.Errorf("unknown test component %q", component)
	}
	if (shard != "" || demoSeed) && component != "server" {
		return nil, fmt.Errorf("sharding and demo-seed apply only to server tests")
	}
	if demoSeed && shard != "" {
		return nil, fmt.Errorf("demo-seed must run separately from the server shards")
	}

	ctr := m.base()
	args := []string{"mise", "run", "test:" + component}
	if component == "server" || component == "cli" || component == "functions" {
		if race {
			args = append(args, "-race")
		}
		if demoSeed {
			args = append(args, "-count=1", "-tags=demoseed_safety", "./internal/demoseed/...")
		} else {
			if shard != "" {
				args = append(args, "--shard="+shard)
			}
			args = append(args, "-p=2", "-tags=inv.debug")
			args = append(args, packages...)
		}
	} else if race || len(packages) != 1 || packages[0] != "./..." {
		return nil, fmt.Errorf("%s uses its mise test task's fixed arguments", component)
	}

	if component == "server" {
		ctr = m.withDocker(m.base("github:temporalio/cli", "aube"))
		if !demoSeed {
			// The discovery wire test calls the real generated TypeScript SDK.
			// Install its dependencies only on the shard that owns that package.
			ctr = ctr.WithEnvVariable("TEST_SHARD", shard).
				WithExec(append([]string{"bash", "-ec", `
if [ -n "$TEST_SHARD" ]; then
  selected=$(cd server && mise exec -- go run -buildvcs=false github.com/speakeasy-api/gram/ci/cmd/shard -i "$TEST_SHARD" -tags=inv.debug "$@")
else
  selected=$(cd server && mise exec -- go list -tags=inv.debug "$@")
fi
if grep -qx github.com/speakeasy-api/gram/server/internal/mcpregistry <<< "$selected"; then
  mise exec -- aube install --frozen-lockfile --workspace-root --filter . --filter 'dashboard...'
fi`, "wire-deps"}, packages...))
		}
		ctr = ctr.WithEnvVariable("GOTESTSUM_JSONFILE_TIMING_EVENTS", "test-timings.json")
	}

	reportDir := "/src/" + component
	if component == "ci" {
		reportDir = "/src"
	}
	// Capture only the test command's status. Setup failures still fail the
	// pipeline and cannot be mistaken for a quarantined flaky test.
	command := []string{"bash", "-c", `
# Dagger's exec telemetry config is not the application's OTEL config.
# In particular, Temporal's test server rejects its http/protobuf protocol.
for name in ${!OTEL_@}; do unset "$name"; done
mkdir -p /reports
set -o pipefail
"$@" 2>&1 | tee /reports/test-output.txt
status=$?
set -e
printf '%s\n' "$status" > /reports/test-exit-code
for report in junit-report.xml test-timings.json; do
  if [ -f "$REPORT_DIR/$report" ]; then cp "$REPORT_DIR/$report" /reports/; fi
done`, "test"}
	command = append(command, args...)
	if component == "server" {
		command = append([]string{"/usr/local/bin/docker-exec"}, command...)
	}
	return ctr.WithEnvVariable("REPORT_DIR", reportDir).
		WithExec(command, dagger.ContainerWithExecOpts{InsecureRootCapabilities: component == "server"}).
		Directory("/reports"), nil
}

// Tool installation is cached independently of source changes. Mise tool versions
// come exclusively from the repository's mise configuration and lockfile.
func (m *GoPipeline) base(extraTools ...string) *dagger.Container {
	tools := append([]string{"mise", "install", "--locked", "go", "github:gotestyourself/gotestsum", "nodejs"}, extraTools...)
	return dag.Container().From("ghcr.io/jdx/mise:2026.10.8-debian").
		WithExec([]string{"sh", "-ec", "apt-get update && apt-get install -y --no-install-recommends build-essential iptables python3 zip unzip xz-utils && rm -rf /var/lib/apt/lists/*"}).
		WithEnvVariable("CI", "true").
		WithEnvVariable("MISE_YES", "1").
		WithEnvVariable("MISE_TRUSTED_CONFIG_PATHS", "/src").
		WithEnvVariable("MISE_TASK_RUN_AUTO_INSTALL", "false").
		WithEnvVariable("MISE_EXEC_AUTO_INSTALL", "false").
		WithWorkdir("/src").
		WithFile("mise.toml", m.Source.File("mise.toml")).
		WithFile("mise.lock", m.Source.File("mise.lock")).
		WithExec(tools).
		WithMountedCache("/go/pkg/mod", dag.CacheVolume("go-pipeline-modules")).
		WithMountedCache("/cache/go-build", dag.CacheVolume("go-pipeline-build")).
		WithMountedCache("/root/.local/share/aube/store", dag.CacheVolume("go-pipeline-aube")).
		WithEnvVariable("GOMODCACHE", "/go/pkg/mod").
		WithEnvVariable("GOCACHE", "/cache/go-build").
		// Tests execute freshly written helper scripts; tmpfs avoids overlayfs
		// text-file-busy races and keeps temporary test data out of snapshots.
		WithMountedTemp("/tmp").
		WithDirectory("/src", m.Source).
		WithEnvVariable("MISE_ENV", "dagger").
		WithNewFile("/src/mise.dagger.toml", "[env]\nGOFLAGS = \"-trimpath -buildvcs=false\"\n").
		// Lint fingerprints need an index, not the host's .git directory (which
		// may be a worktree pointer). Real build SHAs are passed explicitly.
		WithExec([]string{"sh", "-ec", "git init -q && git add ."})
}

func (m *GoPipeline) withDocker(ctr *dagger.Container) *dagger.Container {
	return ctr.
		WithDirectory("/usr/local/bin", dag.Container().From("docker:28.5.2-dind").Directory("/usr/local/bin")).
		WithFile("/usr/local/bin/docker-exec", m.DockerExec, dagger.ContainerWithFileOpts{Permissions: 0o755}).
		WithMountedTemp("/var/lib/docker").
		WithEnvVariable("DOCKER_HOST", "unix:///var/run/docker.sock").
		WithEnvVariable("TESTCONTAINERS_RYUK_DISABLED", "true")
}

// Go projects discoverable through dagger check and its project filter.
func (m *GoPipeline) Projects() *Projects {
	return &Projects{Names: []string{"server", "cli", "plog", "functions"}, Pipeline: m}
}

// Projects is keyed by the repository directory.
// +collection
type Projects struct {
	// Project directories.
	// +keys
	Names []string

	// Shared pipeline inputs.
	// +private
	Pipeline *GoPipeline
}

// The selected Go project.
// +get
func (p *Projects) Get(name string) *Project {
	return &Project{Name: name, Pipeline: p.Pipeline}
}

type Project struct {
	// Repository directory.
	Name string

	// Shared pipeline inputs.
	// +private
	Pipeline *GoPipeline
}

// Compile the project without production signing credentials.
// +check
func (p *Project) Build(ctx context.Context) error {
	_, err := p.Pipeline.base().WithWorkdir("/src/" + p.Name).
		WithExec([]string{"mise", "exec", "--", "go", "build", "-mod=readonly", "./..."}).Sync(ctx)
	return err
}

// Run the project's configured linter.
// +check
func (p *Project) Lint(ctx context.Context) error {
	return p.Pipeline.Lint(ctx, p.Name)
}

// Run the project's test suite.
// +check
func (p *Project) Test(ctx context.Context) error {
	reports, err := p.Pipeline.Test(p.Name, "", false, false, []string{"./..."})
	if err != nil {
		return err
	}
	status, err := reports.File("test-exit-code").Contents(ctx)
	if err != nil {
		return fmt.Errorf("read test status: %w", err)
	}
	if strings.TrimSpace(status) != "0" {
		return fmt.Errorf("%s tests exited with status %s", p.Name, strings.TrimSpace(status))
	}
	return nil
}
