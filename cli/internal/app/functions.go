package app

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/urfave/cli/v2"
	"golang.org/x/term"

	"github.com/speakeasy-api/gram/cli/internal/app/logging"
	"github.com/speakeasy-api/gram/cli/internal/flags"
	"github.com/speakeasy-api/gram/cli/internal/functions"
	"github.com/speakeasy-api/gram/cli/internal/profile"
)

func newFunctionsCommand() *cli.Command {
	return &cli.Command{
		Name:  "functions",
		Usage: "Create, build and deploy Gram Functions projects",
		Description: `
Work with a Gram Functions project: scaffold one with "init", run it locally
with "dev", and build and deploy it with "build" and "push". The build runs
the project's own ` + functions.SDKPackage + ` through Node.js ` + functions.MinNodeVersion + ` or later.
`[1:],
		Subcommands: []*cli.Command{
			newFunctionsInitCommand(),
			newFunctionsBuildCommand(),
			newFunctionsDevCommand(),
			newFunctionsPushCommand(),
			newFunctionsStageCommand(),
		},
	}
}

func newFunctionsRunner(c *cli.Context) functions.Runner {
	r := functions.NewRunner()
	r.Stdout = c.App.Writer
	r.Stderr = c.App.ErrWriter
	return r
}

// projectFlags select the project config and override parts of it.
func projectFlags() []cli.Flag {
	return []cli.Flag{
		&cli.PathFlag{
			Name:  "config",
			Usage: "Path to the project config file (default: the first gram.config.{ts,mts,js,mjs} in the current directory)",
		},
		&cli.PathFlag{
			Name:  "entry",
			Usage: "Path to the function entrypoint, overriding the config (default: src/gram.ts)",
		},
		&cli.PathFlag{
			Name:  "out-dir",
			Usage: "Directory for build output, overriding the config (default: dist)",
		},
	}
}

func projectOptions(c *cli.Context) (functions.ProjectOptions, error) {
	dir, err := os.Getwd()
	if err != nil {
		return functions.ProjectOptions{}, fmt.Errorf("get working directory: %w", err)
	}
	return functions.ProjectOptions{
		Dir:        dir,
		ConfigFile: c.Path("config"),
		Entrypoint: c.Path("entry"),
		OutDir:     c.Path("out-dir"),
	}, nil
}

func newFunctionsInitCommand() *cli.Command {
	return &cli.Command{
		Name:      "init",
		Usage:     "Create a new Gram Functions project",
		ArgsUsage: "[dir]",
		Description: `
Create a Gram Functions project from a built-in template. Missing values are
prompted for when stdin is a terminal; otherwise, or with --yes, defaults are
used: the functions template, git init and dependency install.
`[1:],
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "template",
				Usage: "Project template: " + strings.Join(functions.TemplateNames(), " or "),
			},
			&cli.StringFlag{
				Name:  "name",
				Usage: "Package name for the project (default: the directory name)",
			},
			&cli.BoolFlag{
				Name:  "git",
				Usage: "Initialize a git repository (default: true)",
			},
			&cli.BoolFlag{
				Name:  "install",
				Usage: "Install dependencies with the detected package manager (default: true)",
			},
			&cli.BoolFlag{
				Name:    "yes",
				Aliases: []string{"y"},
				Usage:   "Use defaults instead of prompting",
			},
		},
		Action: func(c *cli.Context) error {
			if err := parseFlagsAfterArg(c); err != nil {
				return err
			}

			r := newFunctionsRunner(c)
			var p *prompter
			if !c.Bool("yes") && stdinIsTerminal() {
				p = &prompter{in: bufio.NewReader(os.Stdin), out: c.App.Writer}
			}

			opts, err := resolveInitOptions(p, initInputs{
				Dir:        c.Args().First(),
				Template:   c.String("template"),
				Name:       c.String("name"),
				Git:        optionalBool(c, "git"),
				Install:    optionalBool(c, "install"),
				UserAgent:  r.Getenv("npm_config_user_agent"),
				SDKVersion: r.Getenv(functions.SDKVersionEnv),
			})
			if err != nil {
				return err
			}

			return r.Init(c.Context, opts)
		},
	}
}

// parseFlagsAfterArg parses flags that follow the directory argument, as in
// "init my-tools --template mcp". urfave/cli stops parsing flags at the first
// argument and leaves the rest in Args.
func parseFlagsAfterArg(c *cli.Context) error {
	rest := c.Args().Tail()
	if len(rest) == 0 {
		return nil
	}

	fs := flag.NewFlagSet(c.Command.Name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	for _, f := range c.Command.Flags {
		if err := f.Apply(fs); err != nil {
			return fmt.Errorf("register flag: %w", err)
		}
	}
	if err := fs.Parse(rest); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("expected at most one directory argument, got %d", 1+fs.NArg())
	}

	var setErr error
	fs.Visit(func(f *flag.Flag) {
		if err := c.Set(f.Name, f.Value.String()); err != nil {
			setErr = errors.Join(setErr, fmt.Errorf("set --%s: %w", f.Name, err))
		}
	})
	return setErr
}

// initInputs are the init values given on the command line. Empty strings
// and nil bools are missing.
type initInputs struct {
	Dir       string
	Template  string
	Name      string
	Git       *bool
	Install   *bool
	UserAgent string
	// SDKVersion overrides the SDK dependency version.
	SDKVersion string
}

// resolveInitOptions fills in missing init values, prompting when p is not
// nil and falling back to defaults otherwise.
func resolveInitOptions(p *prompter, in initInputs) (functions.InitOptions, error) {
	template := in.Template
	if template == "" {
		template = functions.Templates[0].Name
		if p != nil {
			var err error
			if template, err = p.choose("Pick a template", functions.Templates); err != nil {
				return functions.InitOptions{}, err
			}
		}
	}
	if _, err := functions.LookupTemplate(template); err != nil {
		return functions.InitOptions{}, fmt.Errorf("init: %w", err)
	}

	name := in.Name
	if name == "" {
		name = functions.DefaultProjectName
		if in.Dir != "" {
			if abs, err := filepath.Abs(in.Dir); err == nil && functions.ValidateProjectName(filepath.Base(abs)) == nil {
				name = filepath.Base(abs)
			}
		}
		if p != nil {
			var err error
			if name, err = p.text("What do you want to call your project?", name, functions.ValidateProjectName); err != nil {
				return functions.InitOptions{}, err
			}
		}
	}
	if err := functions.ValidateProjectName(name); err != nil {
		return functions.InitOptions{}, fmt.Errorf("init: %w", err)
	}

	dir := in.Dir
	if dir == "" {
		dir = functions.DefaultDir(name)
		if p != nil {
			var err error
			if dir, err = p.text("What directory should we create the project in?", dir, functions.CheckTargetDir); err != nil {
				return functions.InitOptions{}, err
			}
		}
	}
	if err := functions.CheckTargetDir(dir); err != nil {
		return functions.InitOptions{}, fmt.Errorf("init: %w", err)
	}

	pm := functions.DetectPackageManager(dir, in.UserAgent)

	git, err := p.confirmOrDefault(in.Git, "Initialize a git repository?")
	if err != nil {
		return functions.InitOptions{}, err
	}
	install, err := p.confirmOrDefault(in.Install, fmt.Sprintf("Install dependencies with %s?", pm))
	if err != nil {
		return functions.InitOptions{}, err
	}

	return functions.InitOptions{
		Dir:            dir,
		Template:       template,
		Name:           name,
		Git:            git,
		Install:        install,
		PackageManager: pm,
		SDKVersion:     in.SDKVersion,
	}, nil
}

func optionalBool(c *cli.Context, name string) *bool {
	if !c.IsSet(name) {
		return nil
	}
	return new(c.Bool(name))
}

func stdinIsTerminal() bool {
	// A character device check would also match /dev/null.
	return term.IsTerminal(int(os.Stdin.Fd())) // #nosec G115 -- file descriptors fit in an int.
}

// prompter asks questions on a terminal. Every answer has a default that an
// empty line accepts.
type prompter struct {
	in  *bufio.Reader
	out io.Writer
}

var errPromptClosed = errors.New("input closed before the question was answered")

func (p *prompter) ask(question string, def string) (string, error) {
	_, _ = fmt.Fprintf(p.out, "%s [%s]: ", question, def)
	line, err := p.in.ReadString('\n')
	if err != nil && (!errors.Is(err, io.EOF) || line == "") {
		return "", errPromptClosed
	}
	if answer := strings.TrimSpace(line); answer != "" {
		return answer, nil
	}
	return def, nil
}

// text asks until validate accepts the answer.
func (p *prompter) text(question string, def string, validate func(string) error) (string, error) {
	for {
		answer, err := p.ask(question, def)
		if err != nil {
			return "", err
		}
		if err := validate(answer); err != nil {
			_, _ = fmt.Fprintln(p.out, err)
			continue
		}
		return answer, nil
	}
}

// choose asks for one of templates by number or name until it gets one.
func (p *prompter) choose(question string, templates []functions.Template) (string, error) {
	_, _ = fmt.Fprintln(p.out, question)
	for i, t := range templates {
		_, _ = fmt.Fprintf(p.out, "  %d) %s - %s\n", i+1, t.Label, t.Hint)
	}
	for {
		answer, err := p.ask("Template", templates[0].Name)
		if err != nil {
			return "", err
		}
		if n, err := strconv.Atoi(answer); err == nil && n >= 1 && n <= len(templates) {
			return templates[n-1].Name, nil
		}
		for _, t := range templates {
			if t.Name == answer {
				return answer, nil
			}
		}
		_, _ = fmt.Fprintf(p.out, "Enter a number from 1 to %d or a template name\n", len(templates))
	}
}

// confirmOrDefault returns given when set. Otherwise it asks when p is not
// nil and defaults to yes.
func (p *prompter) confirmOrDefault(given *bool, question string) (bool, error) {
	if given != nil {
		return *given, nil
	}
	if p == nil {
		return true, nil
	}
	for {
		answer, err := p.ask(question+" (y/n)", "y")
		if err != nil {
			return false, err
		}
		switch strings.ToLower(answer) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
	}
}

func newFunctionsBuildCommand() *cli.Command {
	return &cli.Command{
		Name:  "build",
		Usage: "Build the project into a deployable zip file",
		Description: `
Build the Gram Functions project in the current directory with the project's
own ` + functions.SDKPackage + `, writing manifest.json, functions.js and gram.zip
to the output directory.
`[1:],
		Flags: projectFlags(),
		Action: func(c *cli.Context) error {
			opts, err := projectOptions(c)
			if err != nil {
				return err
			}
			result, err := newFunctionsRunner(c).Build(c.Context, opts)
			if err != nil {
				return fmt.Errorf("build: %w", err)
			}
			_, _ = fmt.Fprintf(c.App.Writer, "Built %s\n", result.Project.ZipFile)
			return nil
		},
	}
}

func newFunctionsDevCommand() *cli.Command {
	return &cli.Command{
		Name:      "dev",
		Usage:     "Run the project's dev script",
		ArgsUsage: "[-- args...]",
		Description: `
Run the "dev" script from the project's package.json with the project's
package manager. Arguments after -- are passed to the script.
`[1:],
		Action: func(c *cli.Context) error {
			dir, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("get working directory: %w", err)
			}
			return newFunctionsRunner(c).Dev(c.Context, dir, c.Args().Slice())
		},
	}
}

func newFunctionsPushCommand() *cli.Command {
	return &cli.Command{
		Name:  "push",
		Usage: "Build the project and deploy it to Gram",
		Description: `
Build the project, stage its zip file in the deployment file and push a
deployment. The slug defaults to the config's slug, then the package.json
name without its scope.
`[1:],
		Flags: append(projectFlags(),
			flags.APIEndpoint(),
			flags.APIKey(),
			flags.Project(),
			flags.Org(),
			&cli.StringFlag{
				Name:  "slug",
				Usage: "Slug for the function source (default: from the config or package.json)",
			},
			&cli.UintFlag{
				Name:  "scale",
				Usage: "Number of instances to run for the function, overriding the config",
			},
			&cli.UintFlag{
				Name:  "memory-mib",
				Usage: "Memory in MiB for the function, overriding the config",
			},
			&cli.BoolFlag{
				Name:  "no-build",
				Usage: "Deploy the existing build output without building first",
			},
		),
		Action: func(c *cli.Context) error {
			ctx, cancel := signal.NotifyContext(c.Context, os.Interrupt, syscall.SIGTERM)
			defer cancel()

			opts, err := projectOptions(c)
			if err != nil {
				return err
			}

			result, err := pushFunction(ctx, newFunctionsRunner(c), DoPush, functionsPushOptions{
				Project:   opts,
				NoBuild:   c.Bool("no-build"),
				Slug:      c.String("slug"),
				Scale:     optionalUint(c, "scale"),
				MemoryMiB: optionalUint(c, "memory-mib"),
				Push: PushOptions{
					Profile:        profile.FromContext(ctx),
					ConfigFile:     "",
					ProjectSlug:    c.String("project"),
					OrgSlug:        c.String("org"),
					IdempotencyKey: "",
					Method:         "merge",
					NonBlocking:    false,
					APIKey:         c.String("api-key"),
					APIURL:         explicitAPIURL(c),
				},
			})
			if reportErr := reportPushResult(ctx, logging.PullLogger(ctx), result, err); reportErr != nil {
				return reportErr
			}
			if err == nil && result.Status == "completed" {
				_, _ = fmt.Fprintf(c.App.Writer, "Create an MCP server from your tools: %s/mcp/add/from-existing-source?from=cli\n", result.ProjectURL)
			}
			return nil
		},
	}
}

// optionalUint returns the flag's value, or nil when it is unset or 0, which
// stage function also treats as unset.
func optionalUint(c *cli.Context, name string) *uint {
	v := c.Uint(name)
	if v == 0 {
		return nil
	}
	return &v
}

// functionsProject builds or resolves a functions project.
type functionsProject interface {
	Build(ctx context.Context, opts functions.ProjectOptions) (*functions.BuildResult, error)
	ResolveProject(ctx context.Context, opts functions.ProjectOptions) (*functions.Project, error)
}

type functionsPushOptions struct {
	Project   functions.ProjectOptions
	NoBuild   bool
	Slug      string
	Scale     *uint
	MemoryMiB *uint
	// Push configures the deployment. Its ConfigFile is set to the project's
	// staging file, and its ProjectSlug defaults to the config's project.
	Push PushOptions
}

// pushFunction builds the project unless told not to, stages its zip file in
// the project's deployment file and pushes that file.
func pushFunction(
	ctx context.Context,
	project functionsProject,
	push func(context.Context, PushOptions) (*PushResult, error),
	opts functionsPushOptions,
) (*PushResult, error) {
	var resolved *functions.Project
	if opts.NoBuild {
		p, err := project.ResolveProject(ctx, opts.Project)
		if err != nil {
			return nil, fmt.Errorf("resolve project: %w", err)
		}
		if _, err := os.Stat(p.ZipFile); errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%s does not exist: run 'speakeasy functions build' first or drop --no-build", p.ZipFile)
		}
		resolved = p
	} else {
		result, err := project.Build(ctx, opts.Project)
		if err != nil {
			return nil, fmt.Errorf("build: %w", err)
		}
		resolved = &result.Project
	}

	slug := opts.Slug
	if slug == "" {
		slug = resolved.Slug
	}
	if slug == "" {
		return nil, errors.New("no function slug: pass --slug, set slug in gram.config.ts, or set a name in package.json")
	}

	scale := resolved.Scale
	if opts.Scale != nil {
		scale = opts.Scale
	}
	memory := resolved.MemoryMiB
	if opts.MemoryMiB != nil {
		memory = opts.MemoryMiB
	}

	location, err := filepath.Rel(filepath.Dir(resolved.DeployStagingFile), resolved.ZipFile)
	if err != nil {
		location = resolved.ZipFile
	}

	if err := DoStageFunction(StageFunctionOptions{
		ConfigFile: resolved.DeployStagingFile,
		Slug:       slug,
		Name:       "",
		Location:   location,
		Runtime:    "",
		Scale:      scale,
		MemoryMiB:  memory,
	}); err != nil {
		return nil, fmt.Errorf("stage function: %w", err)
	}

	pushOpts := opts.Push
	pushOpts.ConfigFile = resolved.DeployStagingFile
	if pushOpts.ProjectSlug == "" {
		pushOpts.ProjectSlug = resolved.DeployProject
	}
	return push(ctx, pushOpts)
}

func newFunctionsStageCommand() *cli.Command {
	cmd := newStageFunctionCommand()
	cmd.Name = "stage"
	cmd.Usage = "Add a Gram Functions zip file to a deployment file without pushing it"
	cmd.Description = `
Add a Gram Functions zip file to the deployment file, the same way
"speakeasy stage function" does. Run "speakeasy push" to deploy it.
`[1:]
	cmd.Flags = append([]cli.Flag{stageConfigFlag()}, cmd.Flags...)
	cmd.Before = ensureStageConfig
	return cmd
}
