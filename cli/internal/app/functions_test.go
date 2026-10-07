package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/cli/internal/deploy"
	"github.com/speakeasy-api/gram/cli/internal/functions"
)

func TestResolveInitOptions_Defaults(t *testing.T) {
	t.Parallel()

	opts, err := resolveInitOptions(nil, initInputs{Dir: "", Template: "", Name: "", Git: nil, Install: nil, UserAgent: "", SDKVersion: ""})
	require.NoError(t, err)
	require.Equal(t, functions.InitOptions{
		Dir:            "gram-mcp-server",
		Template:       "functions",
		Name:           "gram-mcp-server",
		Git:            true,
		Install:        true,
		PackageManager: "npm",
		SDKVersion:     "",
	}, opts)
}

func TestResolveInitOptions_NameFromDir(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "weather-tools")
	opts, err := resolveInitOptions(nil, initInputs{Dir: dir, Template: "", Name: "", Git: nil, Install: nil, UserAgent: "pnpm/10.0.0", SDKVersion: ""})
	require.NoError(t, err)
	require.Equal(t, "weather-tools", opts.Name)
	require.Equal(t, dir, opts.Dir)
	require.Equal(t, "pnpm", opts.PackageManager)

	// A directory name that is not a valid package name falls back.
	opts, err = resolveInitOptions(nil, initInputs{Dir: filepath.Join(t.TempDir(), "Weather Tools"), Template: "", Name: "", Git: nil, Install: nil, UserAgent: "", SDKVersion: ""})
	require.NoError(t, err)
	require.Equal(t, functions.DefaultProjectName, opts.Name)
}

func TestResolveInitOptions_DirFromScopedName(t *testing.T) {
	t.Parallel()

	opts, err := resolveInitOptions(nil, initInputs{Dir: "", Template: "mcp", Name: "@acme/tools", Git: new(false), Install: new(false), UserAgent: "", SDKVersion: ""})
	require.NoError(t, err)
	require.Equal(t, functions.InitOptions{
		Dir:            "tools",
		Template:       "mcp",
		Name:           "@acme/tools",
		Git:            false,
		Install:        false,
		PackageManager: "npm",
		SDKVersion:     "",
	}, opts)
}

func TestResolveInitOptions_Invalid(t *testing.T) {
	t.Parallel()

	_, err := resolveInitOptions(nil, initInputs{Dir: "", Template: "gram", Name: "", Git: nil, Install: nil, UserAgent: "", SDKVersion: ""})
	require.ErrorContains(t, err, `unknown template "gram"`)

	_, err = resolveInitOptions(nil, initInputs{Dir: "", Template: "", Name: "Bad Name", Git: nil, Install: nil, UserAgent: "", SDKVersion: ""})
	require.ErrorContains(t, err, `invalid project name "Bad Name"`)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "x"), nil, 0o600))
	_, err = resolveInitOptions(nil, initInputs{Dir: dir, Template: "", Name: "", Git: nil, Install: nil, UserAgent: "", SDKVersion: ""})
	require.ErrorContains(t, err, "already exists and is not empty")
}

func TestResolveInitOptions_Prompts(t *testing.T) {
	t.Parallel()

	taken := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(taken, "x"), nil, 0o600))
	target := filepath.Join(t.TempDir(), "fresh")

	answers := strings.Join([]string{
		"3",           // template, rejected
		"2",           // template, by number
		"Not Valid",   // name, rejected
		"@acme/tools", // name
		taken,         // dir, rejected because it is not empty
		target,        // dir
		"maybe",       // git, asked again
		"n",           // git
		"",            // install, default yes
	}, "\n") + "\n"

	var out bytes.Buffer
	p := &prompter{in: bufio.NewReader(strings.NewReader(answers)), out: &out}

	opts, err := resolveInitOptions(p, initInputs{Dir: "", Template: "", Name: "", Git: nil, Install: nil, UserAgent: "", SDKVersion: ""})
	require.NoError(t, err)
	require.Equal(t, functions.InitOptions{
		Dir:            target,
		Template:       "mcp",
		Name:           "@acme/tools",
		Git:            false,
		Install:        true,
		PackageManager: "npm",
		SDKVersion:     "",
	}, opts)

	require.Contains(t, out.String(), "1) Gram Functions")
	require.Contains(t, out.String(), "2) Model Context Protocol SDK")
	require.Contains(t, out.String(), "Enter a number from 1 to 2 or a template name")
	require.Contains(t, out.String(), `invalid project name "Not Valid"`)
	require.Contains(t, out.String(), "already exists and is not empty")
	require.Contains(t, out.String(), "Install dependencies with npm? (y/n) [y]: ")
}

func TestResolveInitOptions_PromptsSkipGivenValues(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	p := &prompter{in: bufio.NewReader(strings.NewReader("")), out: &out}

	dir := filepath.Join(t.TempDir(), "tools")
	_, err := resolveInitOptions(p, initInputs{Dir: dir, Template: "functions", Name: "tools", Git: new(true), Install: new(false), UserAgent: "", SDKVersion: ""})
	require.NoError(t, err)
	require.Empty(t, out.String())
}

func TestResolveInitOptions_PromptInputClosed(t *testing.T) {
	t.Parallel()

	p := &prompter{in: bufio.NewReader(strings.NewReader("")), out: &bytes.Buffer{}}
	_, err := resolveInitOptions(p, initInputs{Dir: "", Template: "", Name: "", Git: nil, Install: nil, UserAgent: "", SDKVersion: ""})
	require.ErrorIs(t, err, errPromptClosed)
}

// fakeFunctionsProject stands in for the project's SDK.
type fakeFunctionsProject struct {
	project  functions.Project
	buildErr error
	built    bool
	resolved bool
	gotOpts  functions.ProjectOptions
}

func (f *fakeFunctionsProject) Build(_ context.Context, opts functions.ProjectOptions) (*functions.BuildResult, error) {
	f.built = true
	f.gotOpts = opts
	if f.buildErr != nil {
		return nil, f.buildErr
	}
	return &functions.BuildResult{Project: f.project, Files: nil}, nil
}

func (f *fakeFunctionsProject) ResolveProject(_ context.Context, opts functions.ProjectOptions) (*functions.Project, error) {
	f.resolved = true
	f.gotOpts = opts
	return &f.project, nil
}

func newFakeProject(p functions.Project, buildErr error) *fakeFunctionsProject {
	return &fakeFunctionsProject{
		project:  p,
		buildErr: buildErr,
		built:    false,
		resolved: false,
		gotOpts:  functions.ProjectOptions{Dir: "", ConfigFile: "", Entrypoint: "", OutDir: ""},
	}
}

// recordPush records the options it is called with.
type recordPush struct {
	calls []PushOptions
}

func (r *recordPush) push(_ context.Context, opts PushOptions) (*PushResult, error) {
	r.calls = append(r.calls, opts)
	return &PushResult{DeploymentID: "dep-1", Status: "completed", LogsURL: "", ProjectURL: ""}, nil
}

func testProject(dir string) functions.Project {
	return functions.Project{
		Dir:               dir,
		OutDir:            filepath.Join(dir, "dist"),
		ZipFile:           filepath.Join(dir, "dist", "gram.zip"),
		DeployStagingFile: filepath.Join(dir, "gram.deploy.json"),
		Slug:              "my-tools",
		DeployProject:     "config-project",
		Scale:             new(uint(2)),
		MemoryMiB:         new(uint(256)),
	}
}

func testPushOptions(dir string) functionsPushOptions {
	return functionsPushOptions{
		Project:   functions.ProjectOptions{Dir: dir, ConfigFile: "", Entrypoint: "", OutDir: ""},
		NoBuild:   false,
		Slug:      "",
		Scale:     nil,
		MemoryMiB: nil,
		Push: PushOptions{
			Profile:        nil,
			ConfigFile:     "",
			ProjectSlug:    "",
			OrgSlug:        "acme",
			IdempotencyKey: "",
			Method:         "merge",
			NonBlocking:    false,
			APIKey:         "key",
			APIURL:         "https://example.test",
		},
	}
}

func readDeployConfig(t *testing.T, path string) deploy.Config {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(path))
	require.NoError(t, err)
	var cfg deploy.Config
	require.NoError(t, json.Unmarshal(raw, &cfg))
	return cfg
}

func TestPushFunction_BuildsStagesAndPushes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	project := newFakeProject(testProject(dir), nil)
	pusher := &recordPush{calls: nil}

	opts := testPushOptions(dir)
	opts.Project.OutDir = "dist"
	result, err := pushFunction(t.Context(), project, pusher.push, opts)
	require.NoError(t, err)
	require.Equal(t, "dep-1", result.DeploymentID)

	require.True(t, project.built)
	require.False(t, project.resolved)
	require.Equal(t, "dist", project.gotOpts.OutDir)

	cfg := readDeployConfig(t, filepath.Join(dir, "gram.deploy.json"))
	require.Len(t, cfg.Sources, 1)
	src := cfg.Sources[0]
	require.Equal(t, deploy.SourceTypeFunction, src.Type)
	require.Equal(t, "my-tools", src.Slug)
	require.Equal(t, "my-tools", src.Name)
	require.Equal(t, filepath.Join("dist", "gram.zip"), src.Location)
	require.Equal(t, "nodejs:22", src.Runtime)
	require.Equal(t, new(uint(2)), src.Scale)
	require.Equal(t, new(uint(256)), src.MemoryMiB)

	require.Len(t, pusher.calls, 1)
	got := pusher.calls[0]
	require.Equal(t, filepath.Join(dir, "gram.deploy.json"), got.ConfigFile)
	require.Equal(t, "config-project", got.ProjectSlug)
	require.Equal(t, "acme", got.OrgSlug)
	require.Equal(t, "key", got.APIKey)
	require.Equal(t, "https://example.test", got.APIURL)
	require.Equal(t, "merge", got.Method)
}

func TestPushFunction_FlagsOverrideConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	p := testProject(dir)
	p.DeployStagingFile = filepath.Join(dir, "deploy", "stage.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(p.DeployStagingFile), 0o750))
	project := newFakeProject(p, nil)
	pusher := &recordPush{calls: nil}

	opts := testPushOptions(dir)
	opts.Slug = "flag-slug"
	opts.Scale = new(uint(5))
	opts.MemoryMiB = new(uint(1024))
	opts.Push.ProjectSlug = "flag-project"

	_, err := pushFunction(t.Context(), project, pusher.push, opts)
	require.NoError(t, err)

	cfg := readDeployConfig(t, p.DeployStagingFile)
	require.Len(t, cfg.Sources, 1)
	require.Equal(t, "flag-slug", cfg.Sources[0].Slug)
	require.Equal(t, filepath.Join("..", "dist", "gram.zip"), cfg.Sources[0].Location)
	require.Equal(t, new(uint(5)), cfg.Sources[0].Scale)
	require.Equal(t, new(uint(1024)), cfg.Sources[0].MemoryMiB)
	require.Equal(t, "flag-project", pusher.calls[0].ProjectSlug)
	require.Equal(t, p.DeployStagingFile, pusher.calls[0].ConfigFile)
}

func TestPushFunction_RestagesSameSlug(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	project := newFakeProject(testProject(dir), nil)
	pusher := &recordPush{calls: nil}

	for range 2 {
		_, err := pushFunction(t.Context(), project, pusher.push, testPushOptions(dir))
		require.NoError(t, err)
	}

	require.Len(t, readDeployConfig(t, filepath.Join(dir, "gram.deploy.json")).Sources, 1)
	require.Len(t, pusher.calls, 2)
}

func TestPushFunction_NoBuild(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	project := newFakeProject(testProject(dir), nil)
	pusher := &recordPush{calls: nil}

	opts := testPushOptions(dir)
	opts.NoBuild = true

	_, err := pushFunction(t.Context(), project, pusher.push, opts)
	require.ErrorContains(t, err, "run 'speakeasy functions build' first")
	require.False(t, project.built)
	require.Empty(t, pusher.calls)
	require.NoFileExists(t, filepath.Join(dir, "gram.deploy.json"))

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "dist"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "dist", "gram.zip"), []byte("zip"), 0o600))

	_, err = pushFunction(t.Context(), project, pusher.push, opts)
	require.NoError(t, err)
	require.False(t, project.built)
	require.True(t, project.resolved)
	require.Len(t, pusher.calls, 1)
}

func TestPushFunction_NoSlug(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	p := testProject(dir)
	p.Slug = ""
	project := newFakeProject(p, nil)
	pusher := &recordPush{calls: nil}

	_, err := pushFunction(t.Context(), project, pusher.push, testPushOptions(dir))
	require.ErrorContains(t, err, "no function slug")
	require.Empty(t, pusher.calls)
}

func TestPushFunction_InvalidSlug(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	project := newFakeProject(testProject(dir), nil)
	pusher := &recordPush{calls: nil}

	opts := testPushOptions(dir)
	opts.Slug = "Not A Slug"
	_, err := pushFunction(t.Context(), project, pusher.push, opts)
	require.ErrorContains(t, err, "invalid slug")
	require.Empty(t, pusher.calls)
}

func TestPushFunction_BuildFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	project := newFakeProject(testProject(dir), functions.ErrSDKMissing)
	pusher := &recordPush{calls: nil}

	_, err := pushFunction(t.Context(), project, pusher.push, testPushOptions(dir))
	require.ErrorIs(t, err, functions.ErrSDKMissing)
	require.Empty(t, pusher.calls)
	require.NoFileExists(t, filepath.Join(dir, "gram.deploy.json"))
}

func TestPushFunction_PushError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	project := newFakeProject(testProject(dir), nil)
	failing := func(context.Context, PushOptions) (*PushResult, error) {
		return nil, errors.New("unauthorized")
	}

	_, err := pushFunction(t.Context(), project, failing, testPushOptions(dir))
	require.ErrorContains(t, err, "unauthorized")
}

// runApp runs the CLI with args in a fresh app with an empty profile.
func runApp(t *testing.T, args ...string) (string, error) {
	t.Helper()

	var stdout bytes.Buffer
	app := newApp()
	app.Writer = &stdout
	app.ErrWriter = &bytes.Buffer{}
	app.ExitErrHandler = func(*cli.Context, error) {}

	profilePath := filepath.Join(t.TempDir(), "profile.json")
	full := append([]string{"speakeasy", "--profile-path", profilePath}, args...)
	err := app.RunContext(t.Context(), full)
	return stdout.String(), err
}

func TestFunctionsStage_MatchesStageFunction(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	legacy := filepath.Join(dir, "legacy.json")
	modern := filepath.Join(dir, "modern.json")

	_, err := runApp(t, "stage", "--config", legacy, "function", "--slug", "my-fn", "--location", "dist/gram.zip", "--scale", "3", "--memory-mib", "512", "--runtime", "nodejs:24", "--name", "My Fn")
	require.NoError(t, err)
	_, err = runApp(t, "functions", "stage", "--config", modern, "--slug", "my-fn", "--location", "dist/gram.zip", "--scale", "3", "--memory-mib", "512", "--runtime", "nodejs:24", "--name", "My Fn")
	require.NoError(t, err)

	want, err := os.ReadFile(filepath.Clean(legacy))
	require.NoError(t, err)
	got, err := os.ReadFile(filepath.Clean(modern))
	require.NoError(t, err)
	require.JSONEq(t, string(want), string(got))

	cfg := readDeployConfig(t, modern)
	require.Len(t, cfg.Sources, 1)
	require.Equal(t, "my-fn", cfg.Sources[0].Slug)
	require.Equal(t, "My Fn", cfg.Sources[0].Name)
}

func TestFunctionsStage_RequiresSlugAndLocation(t *testing.T) {
	t.Parallel()

	config := filepath.Join(t.TempDir(), "gram.deploy.json")
	_, err := runApp(t, "functions", "stage", "--config", config, "--location", "x.zip")
	require.ErrorContains(t, err, `"slug"`)
}

func TestFunctionsCommand_ListsSubcommands(t *testing.T) {
	t.Parallel()

	out, err := runApp(t, "functions", "--help")
	require.NoError(t, err)
	for _, name := range []string{"init", "build", "dev", "push", "stage"} {
		require.Contains(t, out, name)
	}
}

func TestFunctionsInit_FlagsAfterDir(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "my-tools")
	out, err := runApp(t, "functions", "init", dir, "--template", "mcp", "--name", "@acme/tools", "--git=false", "--install=false", "-y")
	require.NoError(t, err)
	require.Contains(t, out, "speakeasy functions build")

	require.FileExists(t, filepath.Join(dir, "src", "mcp.ts"))
	raw, err := os.ReadFile(filepath.Clean(filepath.Join(dir, "package.json")))
	require.NoError(t, err)
	require.Contains(t, string(raw), `"name": "@acme/tools"`)
	require.NoDirExists(t, filepath.Join(dir, ".git"))
	require.NoDirExists(t, filepath.Join(dir, "node_modules"))
}

func TestFunctionsInit_ExtraArgs(t *testing.T) {
	t.Parallel()

	_, err := runApp(t, "functions", "init", "a", "b")
	require.ErrorContains(t, err, "expected at most one directory argument, got 2")

	_, err = runApp(t, "functions", "init", "a", "--nope")
	require.ErrorContains(t, err, "flag provided but not defined: -nope")
}

func TestOptionalUint_ZeroIsUnset(t *testing.T) {
	t.Parallel()

	got := map[string]*uint{}
	app := &cli.App{
		Flags: []cli.Flag{&cli.UintFlag{Name: "scale"}, &cli.UintFlag{Name: "memory-mib"}, &cli.UintFlag{Name: "other"}},
		Action: func(c *cli.Context) error {
			for _, name := range []string{"scale", "memory-mib", "other"} {
				got[name] = optionalUint(c, name)
			}
			return nil
		},
	}
	require.NoError(t, app.RunContext(t.Context(), []string{"x", "--scale", "0", "--memory-mib", "512"}))
	require.Nil(t, got["scale"])
	require.Equal(t, new(uint(512)), got["memory-mib"])
	require.Nil(t, got["other"])
}
