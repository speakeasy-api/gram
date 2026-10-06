package functions

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeNode writes a node that reports version for --version and otherwise
// runs body, which can read the request and write the result through the
// environment variables the CLI sets.
func fakeNode(t *testing.T, version string, body string) string {
	t.Helper()
	bin := fakeBinDir(t)
	writeFakeBin(t, bin, "node", `if [ "$1" = "--version" ]; then echo "`+version+`"; exit 0; fi
`+body)
	return bin
}

func TestBuild_NodeMissing(t *testing.T) {
	t.Parallel()

	_, err := testRunner(t.TempDir(), &bytes.Buffer{}).Build(t.Context(), ProjectOptions{Dir: t.TempDir(), ConfigFile: "", Entrypoint: "", OutDir: ""})
	require.ErrorIs(t, err, ErrNodeNotFound)
	require.ErrorContains(t, err, "install Node.js 22.18.0 or later")
}

func TestBuild_NodeTooOld(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"v20.11.1", "v22.17.9", "not-a-version"} {
		bin := fakeNode(t, version, "exit 99\n")
		_, err := testRunner(bin, &bytes.Buffer{}).Build(t.Context(), ProjectOptions{Dir: t.TempDir(), ConfigFile: "", Entrypoint: "", OutDir: ""})
		require.ErrorContains(t, err, "is too old", version)
	}
}

func TestBuild_SDKMissing(t *testing.T) {
	t.Parallel()

	bin := fakeNode(t, "v22.18.0", `echo '{"error":"missing"}' > "$SPEAKEASY_FUNCTIONS_RESULT"`+"\n")
	_, err := testRunner(bin, &bytes.Buffer{}).Build(t.Context(), ProjectOptions{Dir: t.TempDir(), ConfigFile: "", Entrypoint: "", OutDir: ""})
	require.ErrorIs(t, err, ErrSDKMissing)
}

func TestBuild_SDKOutdated(t *testing.T) {
	t.Parallel()

	bin := fakeNode(t, "v24.0.0", `echo '{"error":"outdated"}' > "$SPEAKEASY_FUNCTIONS_RESULT"`+"\n")
	_, err := testRunner(bin, &bytes.Buffer{}).Build(t.Context(), ProjectOptions{Dir: t.TempDir(), ConfigFile: "", Entrypoint: "", OutDir: ""})
	require.ErrorIs(t, err, ErrSDKOutdated)
	require.ErrorContains(t, err, "upgrade it with 'npm install @gram-ai/functions@^"+MinSDKVersion+"'")
}

func TestBuild_NodeFails(t *testing.T) {
	t.Parallel()

	bin := fakeNode(t, "v22.18.0", "echo boom >&2\nexit 1\n")
	var out bytes.Buffer
	_, err := testRunner(bin, &out).Build(t.Context(), ProjectOptions{Dir: t.TempDir(), ConfigFile: "", Entrypoint: "", OutDir: ""})
	require.ErrorContains(t, err, "@gram-ai/functions build failed")
	require.Contains(t, out.String(), "boom")
}

func TestBuild_Success(t *testing.T) {
	t.Parallel()

	captured := filepath.Join(t.TempDir(), "captured")
	bin := fakeNode(t, "v22.18.0", `
echo "building"
printf '%s' "$*" > "`+captured+`.args"
printf '%s' "$SPEAKEASY_FUNCTIONS_REQUEST" > "`+captured+`.request"
pwd > "`+captured+`.cwd"
echo '{"result":{"project":{"cwd":"/p","outDir":"/p/out","zipFile":"/p/out/gram.zip","deployStagingFile":"/p/gram.deploy.json","slug":"my-tools","scale":2},"files":[{"path":"/p/out/gram.zip","size":123}]}}' > "$SPEAKEASY_FUNCTIONS_RESULT"
`)

	dir := t.TempDir()
	var out bytes.Buffer
	result, err := testRunner(bin, &out).Build(t.Context(), ProjectOptions{
		Dir:        dir,
		ConfigFile: "custom.config.ts",
		Entrypoint: "src/other.ts",
		OutDir:     "out",
	})
	require.NoError(t, err)

	require.Equal(t, &BuildResult{
		Project: Project{
			Dir:               "/p",
			OutDir:            "/p/out",
			ZipFile:           "/p/out/gram.zip",
			DeployStagingFile: "/p/gram.deploy.json",
			Slug:              "my-tools",
			DeployProject:     "",
			Scale:             new(uint(2)),
			MemoryMiB:         nil,
		},
		Files: []BuiltFile{{Path: "/p/out/gram.zip", Size: 123}},
	}, result)
	require.Contains(t, out.String(), "building")

	args, err := os.ReadFile(filepath.Clean(captured + ".args"))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(args), "--input-type=module -e "), string(args))

	var request map[string]any
	raw, err := os.ReadFile(filepath.Clean(captured + ".request"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &request))
	require.Equal(t, map[string]any{
		"package": "@gram-ai/functions",
		"module":  "@gram-ai/functions/build",
		"action":  "build",
		"options": map[string]any{
			"cwd":        dir,
			"configFile": "custom.config.ts",
			"entrypoint": "src/other.ts",
			"outDir":     "out",
		},
	}, request)

	cwd, err := os.ReadFile(filepath.Clean(captured + ".cwd"))
	require.NoError(t, err)
	want, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	require.Equal(t, want, strings.TrimSpace(string(cwd)))
}

func TestResolveProject_Success(t *testing.T) {
	t.Parallel()

	bin := fakeNode(t, "v22.18.0", `
case "$SPEAKEASY_FUNCTIONS_REQUEST" in
  *'"action":"resolveProject"'*) ;;
  *) exit 1 ;;
esac
echo '{"result":{"cwd":"/p","outDir":"/p/dist","zipFile":"/p/dist/gram.zip","deployStagingFile":"/p/gram.deploy.json","deployProject":"proj","memoryMiB":256}}' > "$SPEAKEASY_FUNCTIONS_RESULT"
`)

	project, err := testRunner(bin, &bytes.Buffer{}).ResolveProject(t.Context(), ProjectOptions{Dir: t.TempDir(), ConfigFile: "", Entrypoint: "", OutDir: ""})
	require.NoError(t, err)
	require.Equal(t, "proj", project.DeployProject)
	require.Empty(t, project.Slug)
	require.Nil(t, project.Scale)
	require.Equal(t, new(uint(256)), project.MemoryMiB)
}

// TestBuild_RealNode runs the SDK script with the real node against fake SDK
// packages, to check how it resolves the project's SDK.
func TestBuild_RealNode(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	version, err := exec.CommandContext(t.Context(), "node", "--version").Output()
	require.NoError(t, err)
	if !versionAtLeast(strings.TrimSpace(string(version)), MinNodeVersion) {
		t.Skipf("node %s is older than %s", version, MinNodeVersion)
	}

	runner := Runner{Env: os.Environ(), Stdin: nil, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}

	installSDK := func(t *testing.T, dir string, buildJS string) {
		t.Helper()
		sdkDir := filepath.Join(dir, "node_modules", "@gram-ai", "functions")
		writeFile(t, filepath.Join(sdkDir, "package.json"), `{"name":"@gram-ai/functions","type":"module","exports":{"./build":"./build.js"}}`)
		writeFile(t, filepath.Join(sdkDir, "build.js"), buildJS)
	}

	t.Run("missing", func(t *testing.T) {
		t.Parallel()
		_, err := runner.Build(t.Context(), ProjectOptions{Dir: t.TempDir(), ConfigFile: "", Entrypoint: "", OutDir: ""})
		require.ErrorIs(t, err, ErrSDKMissing)
	})

	t.Run("outdated", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		installSDK(t, dir, "export function defineConfig(c) { return c; }\n")
		_, err := runner.Build(t.Context(), ProjectOptions{Dir: dir, ConfigFile: "", Entrypoint: "", OutDir: ""})
		require.ErrorIs(t, err, ErrSDKOutdated)
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		installSDK(t, dir, `export async function build(opts) {
  return { project: { cwd: opts.cwd, outDir: opts.outDir, zipFile: opts.outDir + "/gram.zip", deployStagingFile: "", slug: "fake" }, files: [] };
}
`)
		result, err := runner.Build(t.Context(), ProjectOptions{Dir: dir, ConfigFile: "", Entrypoint: "", OutDir: "out"})
		require.NoError(t, err)
		require.Equal(t, dir, result.Project.Dir)
		require.Equal(t, "out/gram.zip", result.Project.ZipFile)
		require.Equal(t, "fake", result.Project.Slug)
	})

	t.Run("build entry not exported", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		sdkDir := filepath.Join(dir, "node_modules", "@gram-ai", "functions")
		writeFile(t, filepath.Join(sdkDir, "package.json"), `{"name":"@gram-ai/functions","type":"module","exports":{".":"./index.js"}}`)
		writeFile(t, filepath.Join(sdkDir, "index.js"), "export {};\n")
		_, err := runner.Build(t.Context(), ProjectOptions{Dir: dir, ConfigFile: "", Entrypoint: "", OutDir: ""})
		require.ErrorIs(t, err, ErrSDKOutdated)
	})

	t.Run("dependency export error is not an outdated sdk", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		depDir := filepath.Join(dir, "node_modules", "some-dep")
		writeFile(t, filepath.Join(depDir, "package.json"), `{"name":"some-dep","type":"module","exports":{".":"./index.js"}}`)
		writeFile(t, filepath.Join(depDir, "index.js"), "export {};\n")
		installSDK(t, dir, "import \"some-dep/missing\";\nexport async function build() { return {}; }\n")
		_, err := runner.Build(t.Context(), ProjectOptions{Dir: dir, ConfigFile: "", Entrypoint: "", OutDir: ""})
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrSDKOutdated)
		require.ErrorContains(t, err, "@gram-ai/functions build failed")
	})

	t.Run("sdk throws", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		installSDK(t, dir, "export async function build() { throw new Error(\"entrypoint broke\"); }\n")
		_, err := runner.Build(t.Context(), ProjectOptions{Dir: dir, ConfigFile: "", Entrypoint: "", OutDir: ""})
		require.ErrorContains(t, err, "@gram-ai/functions build failed")
	})
}

func TestVersionAtLeast(t *testing.T) {
	t.Parallel()

	tests := []struct {
		version string
		want    bool
	}{
		{"v22.18.0", true},
		{"22.18.0", true},
		{"v22.18.1", true},
		{"v22.19.0", true},
		{"v23.0.0", true},
		{"v24.16.0", true},
		{"v22.17.9", false},
		{"v21.99.99", false},
		{"v18.0.0", false},
		{"", false},
		{"garbage", false},
	}
	for _, tc := range tests {
		require.Equal(t, tc.want, versionAtLeast(tc.version, MinNodeVersion), tc.version)
	}
}

func TestBuild_IgnoresRelativePathEntries(t *testing.T) {
	t.Parallel()

	bin := fakeNode(t, "v22.18.0", `echo '{"result":{}}' > "$SPEAKEASY_FUNCTIONS_RESULT"`+"\n")
	wd, err := os.Getwd()
	require.NoError(t, err)
	rel, err := filepath.Rel(wd, bin)
	require.NoError(t, err)

	_, err = testRunner(rel, &bytes.Buffer{}).Build(t.Context(), ProjectOptions{Dir: t.TempDir(), ConfigFile: "", Entrypoint: "", OutDir: ""})
	require.ErrorIs(t, err, ErrNodeNotFound)
}

func TestRunnerGetenv(t *testing.T) {
	t.Parallel()

	r := Runner{Env: []string{"A=1", "B=x=y", "A=2", "broken"}, Stdin: nil, Stdout: nil, Stderr: nil}
	require.Equal(t, "2", r.Getenv("A"))
	require.Equal(t, "x=y", r.Getenv("B"))
	require.Empty(t, r.Getenv("C"))
}
