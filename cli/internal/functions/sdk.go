package functions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Masterminds/semver/v3"
)

const (
	// SDKPackage is the npm package that function projects build with.
	SDKPackage = "@speakeasy-api/functions"

	// LegacySDKPackage is the deprecated name of SDKPackage. It re-exports
	// SDKPackage, and projects that depend on it keep building.
	LegacySDKPackage = "@gram-ai/functions"

	// sdkBuildEntry is the subpath of the SDK packages the CLI imports to build
	// a project.
	sdkBuildEntry = "/build"

	// MinSDKVersion is the first release of SDKPackage. New projects depend
	// on ^MinSDKVersion.
	MinSDKVersion = "0.20.0"

	// MinLegacySDKVersion is the first release of LegacySDKPackage that
	// exports the programmatic build entry.
	MinLegacySDKVersion = "0.19.0"

	// MinNodeVersion is the oldest Node.js that runs the SDK. It strips
	// TypeScript types without flags, which loading gram.config.ts needs.
	MinNodeVersion = "22.18.0"

	requestEnv = "SPEAKEASY_FUNCTIONS_REQUEST"
	resultEnv  = "SPEAKEASY_FUNCTIONS_RESULT"
)

// sdkPackages are the SDK packages the CLI builds with, in the order it tries
// them.
var sdkPackages = []string{SDKPackage, LegacySDKPackage}

// sdkScript runs inside the project with Node.js. It imports the first SDK
// package the project has from its node_modules, so the build always uses the
// project's SDK version, calls one exported function and writes the outcome to
// a file for the CLI. Logs from the SDK go to the inherited stdout and stderr.
const sdkScript = `
import { writeFileSync } from "node:fs";

const request = JSON.parse(process.env.` + requestEnv + `);
const done = (outcome) =>
  writeFileSync(process.env.` + resultEnv + `, JSON.stringify(outcome));

let sdk;
let outdated;
for (const pkg of request.packages) {
  let mod;
  try {
    mod = await import(pkg + request.entry);
  } catch (err) {
    if (err?.code === "ERR_MODULE_NOT_FOUND" && String(err.message).includes("'" + pkg + "'")) {
      continue;
    }
    // Only this script's own import counts: Node names "[eval1]" as the
    // importer, while a failing import inside the SDK names an SDK file.
    if (err?.code === "ERR_PACKAGE_PATH_NOT_EXPORTED" && String(err.message).includes("[eval")) {
      outdated ??= pkg;
      continue;
    }
    throw err;
  }
  if (typeof mod[request.action] !== "function") {
    outdated ??= pkg;
    continue;
  }
  sdk = mod;
  break;
}

if (sdk) {
  done({ result: (await sdk[request.action](request.options)) ?? null });
} else if (outdated) {
  done({ error: "outdated", package: outdated });
} else {
  done({ error: "missing" });
}
`

var (
	// ErrNodeNotFound means node is not on PATH.
	ErrNodeNotFound = errors.New("node was not found on PATH: install Node.js " + MinNodeVersion + " or later from https://nodejs.org")

	// ErrSDKMissing means the project has neither SDK package installed.
	ErrSDKMissing = errors.New(SDKPackage + " is not installed in this project: run your package manager's install command, or add it with 'npm install " + SDKPackage + "@^" + MinSDKVersion + "'")

	// ErrSDKOutdated means the installed SDK predates the build entry. The
	// error Build returns wraps it and names the outdated package.
	ErrSDKOutdated = errors.New("functions SDK is too old for the speakeasy CLI")
)

// ProjectOptions selects a project and overrides parts of its config.
type ProjectOptions struct {
	// Dir is the project directory.
	Dir string `json:"cwd"`
	// ConfigFile is the project config file. Empty picks the first
	// speakeasy.config.* file, then the first deprecated gram.config.* file.
	ConfigFile string `json:"configFile,omitempty"`
	// Entrypoint overrides the config's entrypoint.
	Entrypoint string `json:"entrypoint,omitempty"`
	// OutDir overrides the config's output directory.
	OutDir string `json:"outDir,omitempty"`
}

// Project is a project's resolved build and deploy settings. Paths are
// absolute.
type Project struct {
	Dir               string `json:"cwd"`
	OutDir            string `json:"outDir"`
	ZipFile           string `json:"zipFile"`
	DeployStagingFile string `json:"deployStagingFile"`
	// Slug is empty when neither the config nor package.json names one.
	Slug          string `json:"slug"`
	DeployProject string `json:"deployProject"`
	Scale         *uint  `json:"scale"`
	MemoryMiB     *uint  `json:"memoryMiB"`
}

// BuiltFile is a file the build wrote.
type BuiltFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// BuildResult describes a finished build.
type BuildResult struct {
	Project Project     `json:"project"`
	Files   []BuiltFile `json:"files"`
}

// Build builds the project with its own SDK and returns the resolved project.
func (r Runner) Build(ctx context.Context, opts ProjectOptions) (*BuildResult, error) {
	var result BuildResult
	if err := r.callSDK(ctx, "build", opts, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ResolveProject resolves the project's settings with its own SDK without
// building it.
func (r Runner) ResolveProject(ctx context.Context, opts ProjectOptions) (*Project, error) {
	var project Project
	if err := r.callSDK(ctx, "resolveProject", opts, &project); err != nil {
		return nil, err
	}
	return &project, nil
}

// callSDK calls action from the project's SDK build module and decodes its
// result into out.
func (r Runner) callSDK(ctx context.Context, action string, opts ProjectOptions, out any) error {
	if err := r.checkNode(ctx); err != nil {
		return err
	}

	request, err := json.Marshal(map[string]any{
		"packages": sdkPackages,
		"entry":    sdkBuildEntry,
		"action":   action,
		"options":  opts,
	})
	if err != nil {
		return fmt.Errorf("encode sdk request: %w", err)
	}

	resultDir, err := os.MkdirTemp("", "speakeasy-functions-")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(resultDir) }()
	resultFile := filepath.Join(resultDir, "result.json")

	cmd, err := r.command(ctx, opts.Dir, "node", "--input-type=module", "-e", sdkScript)
	if err != nil {
		return err
	}
	cmd.Env = append(cmd.Env, requestEnv+"="+string(request), resultEnv+"="+resultFile)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("functions SDK %s failed: %w", action, err)
	}

	raw, err := os.ReadFile(filepath.Clean(resultFile))
	if err != nil {
		return fmt.Errorf("read functions SDK %s result: %w", action, err)
	}

	var outcome struct {
		Error   string          `json:"error"`
		Package string          `json:"package"`
		Result  json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &outcome); err != nil {
		return fmt.Errorf("decode functions SDK %s result: %w", action, err)
	}

	switch outcome.Error {
	case "":
	case "missing":
		return ErrSDKMissing
	case "outdated":
		pkg, version := SDKPackage, MinSDKVersion
		if outcome.Package == LegacySDKPackage {
			pkg, version = LegacySDKPackage, MinLegacySDKVersion
		}
		return fmt.Errorf("%w: upgrade %s with 'npm install %s@^%s'", ErrSDKOutdated, pkg, pkg, version)
	default:
		return fmt.Errorf("functions SDK %s failed: %s", action, outcome.Error)
	}

	if err := json.Unmarshal(outcome.Result, out); err != nil {
		return fmt.Errorf("decode functions SDK %s result: %w", action, err)
	}
	return nil
}

// checkNode fails when node is missing or older than MinNodeVersion.
func (r Runner) checkNode(ctx context.Context) error {
	cmd, err := r.command(ctx, "", "node", "--version")
	if errors.Is(err, errCommandNotFound) {
		return ErrNodeNotFound
	}
	if err != nil {
		return err
	}

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = nil
	cmd.Stdin = nil
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("node --version: %w", err)
	}

	version := bytes.TrimSpace(stdout.Bytes())
	if !versionAtLeast(string(version), MinNodeVersion) {
		return fmt.Errorf("node %s is too old: install Node.js %s or later from https://nodejs.org", version, MinNodeVersion)
	}
	return nil
}

// versionAtLeast reports whether version, with an optional leading v, is at
// least minimum. An unparsable version fails.
func versionAtLeast(version string, minimum string) bool {
	got, err := semver.NewVersion(version)
	if err != nil {
		return false
	}
	return !got.LessThan(semver.MustParse(minimum))
}
