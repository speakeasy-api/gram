package functions

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// templatesFS holds the project templates. They live in this package, rather
// than next to the npm scaffolder, because go:embed only reaches files below
// the embedding package. The npm scaffolder copies them from here.
//
//go:embed templates
var templatesFS embed.FS

// Template is a project template that init can scaffold.
type Template struct {
	Name  string
	Label string
	Hint  string
}

// Templates lists the embedded templates, default first.
var Templates = []Template{
	{
		Name:  "functions",
		Label: "Gram Functions",
		Hint:  "Simplest path to start building your own tools - comes with batteries included",
	},
	{
		Name:  "mcp",
		Label: "Model Context Protocol SDK",
		Hint:  "For advanced use cases where you need more control over MCP responses",
	},
}

const (
	// DefaultProjectName is the package name init suggests.
	DefaultProjectName = "gram-mcp-server"

	// SDKVersionEnv overrides the SDK dependency written into new projects,
	// for example "file:/path/to/gram/ts-framework/functions" to develop
	// against a local SDK checkout.
	SDKVersionEnv = "GRAM_FUNCTIONS_SDK_VERSION"

	mcpSDKPackage = "@modelcontextprotocol/sdk"
	mcpSDKVersion = "^1.20.1"
)

// packageNameRE matches the package names init accepts: npm names, scoped or
// not, limited to alphanumerics, dashes and underscores.
var packageNameRE = regexp.MustCompile(`^(@?[a-z0-9-_]+/)?[a-z0-9-_]+$`)

// skippedTemplateEntries are never copied into a project. NEXT_STEPS.txt is
// printed instead.
var skippedTemplateEntries = []string{"node_modules", "dist", ".git", "CHANGELOG.md", "NEXT_STEPS.txt"}

// ValidateProjectName returns an error when name is not a valid package name.
func ValidateProjectName(name string) error {
	if packageNameRE.MatchString(name) {
		return nil
	}
	return fmt.Errorf("invalid project name %q: use lowercase letters, digits, dashes and underscores, optionally scoped, for example my-mcp-server or @my-org/mcp-server", name)
}

// LookupTemplate returns the template called name.
func LookupTemplate(name string) (Template, error) {
	for _, t := range Templates {
		if t.Name == name {
			return t, nil
		}
	}
	names := make([]string, 0, len(Templates))
	for _, t := range Templates {
		names = append(names, t.Name)
	}
	return Template{}, fmt.Errorf("unknown template %q: choose one of %s", name, strings.Join(names, ", "))
}

// DefaultDir is the directory init suggests for a project name: the name
// without its scope.
func DefaultDir(name string) string {
	return path.Base(name)
}

// CheckTargetDir fails unless dir is missing or an empty directory.
func CheckTargetDir(dir string) error {
	entries, err := os.ReadDir(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("check %s: %w", dir, err)
	case len(entries) > 0:
		return fmt.Errorf("directory %s already exists and is not empty: choose a different directory", dir)
	default:
		return nil
	}
}

// InitOptions configures Init.
type InitOptions struct {
	Dir      string
	Template string
	Name     string
	Git      bool
	Install  bool
	// PackageManager installs dependencies and appears in the next steps.
	PackageManager string
	// SDKVersion is the SDK dependency version. Empty means ^MinSDKVersion.
	SDKVersion string
}

// Init scaffolds a project from an embedded template.
func (r Runner) Init(ctx context.Context, opts InitOptions) error {
	if _, err := LookupTemplate(opts.Template); err != nil {
		return err
	}
	if err := ValidateProjectName(opts.Name); err != nil {
		return err
	}
	if err := CheckTargetDir(opts.Dir); err != nil {
		return err
	}

	sdkVersion := opts.SDKVersion
	if sdkVersion == "" {
		sdkVersion = "^" + MinSDKVersion
	}

	r.logf("Scaffolding %s project in %s", opts.Template, opts.Dir)
	if err := copyTemplate(opts.Template, opts.Dir); err != nil {
		return err
	}

	if err := rewritePackageJSON(filepath.Join(opts.Dir, "package.json"), packageJSONRewrite{
		Name: opts.Name,
		Dependencies: map[string]string{
			SDKPackage:    sdkVersion,
			mcpSDKPackage: mcpSDKVersion,
		},
	}); err != nil {
		return err
	}

	if err := os.Rename(filepath.Join(opts.Dir, "gitignore"), filepath.Join(opts.Dir, ".gitignore")); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("create .gitignore: %w", err)
	}

	if _, err := os.Stat(filepath.Join(opts.Dir, "CONTRIBUTING.md")); err == nil {
		for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
			if err := symlinkOrCopy(opts.Dir, "CONTRIBUTING.md", name); err != nil {
				return err
			}
		}
	}

	if opts.Git {
		r.logf("Initializing git repository")
		if err := r.run(ctx, opts.Dir, "git", "init", "--quiet"); err != nil {
			r.logf("Skipped git init: %s", err)
		}
	}

	if opts.Install {
		r.logf("Installing dependencies with %s", opts.PackageManager)
		if err := r.run(ctx, opts.Dir, opts.PackageManager, "install"); err != nil {
			return fmt.Errorf("install dependencies: %w", err)
		}
	}

	return r.printNextSteps(opts)
}

// copyTemplate writes the template's files into dir.
func copyTemplate(name string, dir string) error {
	root := path.Join("templates", name)
	err := fs.WalkDir(templatesFS, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if slices.Contains(skippedTemplateEntries, d.Name()) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		rel := strings.TrimPrefix(strings.TrimPrefix(p, root), "/")
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		data, err := templatesFS.ReadFile(p)
		if err != nil {
			return fmt.Errorf("read template file: %w", err)
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		return fmt.Errorf("copy %s template: %w", name, err)
	}
	return nil
}

// symlinkOrCopy links dir/name to target, which is relative to dir. It falls
// back to a copy where symlinks are unavailable, such as Windows without
// Developer Mode or FAT filesystems.
func symlinkOrCopy(dir string, target string, name string) error {
	if err := os.Symlink(target, filepath.Join(dir, name)); err == nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(dir, target)) // #nosec G304 -- target is a file this command just wrote.
	if err != nil {
		return fmt.Errorf("copy %s to %s: %w", target, name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		return fmt.Errorf("copy %s to %s: %w", target, name, err)
	}
	return nil
}

func (r Runner) printNextSteps(opts InitOptions) error {
	steps, err := templatesFS.ReadFile(path.Join("templates", opts.Template, "NEXT_STEPS.txt"))
	if err != nil {
		steps = []byte("All done! Jump in with `cd $DIR` and run `speakeasy functions build`.\n")
	}
	text := strings.NewReplacer("$PACKAGE_MANAGER", opts.PackageManager, "$DIR", opts.Dir).Replace(string(steps))
	if _, err := fmt.Fprintln(r.out(), "\n"+strings.TrimRight(text, "\n")); err != nil {
		return fmt.Errorf("print next steps: %w", err)
	}
	return nil
}

func (r Runner) out() io.Writer {
	if r.Stdout == nil {
		return io.Discard
	}
	return r.Stdout
}

func (r Runner) logf(format string, args ...any) {
	_, _ = fmt.Fprintf(r.out(), format+"\n", args...)
}
