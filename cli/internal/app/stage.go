package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/cli/internal/constants"
	"github.com/speakeasy-api/gram/cli/internal/deploy"
	"github.com/speakeasy-api/gram/cli/internal/o11y"
)

var (
	sourceSlugFlag = &cli.StringFlag{
		Name:  "slug",
		Usage: "Slug to identify the source",
		Action: func(ctx *cli.Context, s string) error {
			if !constants.SlugPatternRE.MatchString(s) {
				return fmt.Errorf("invalid slug: %s: %s", s, constants.SlugMessage)
			}
			return nil
		},
		Required: true,
	}

	sourceNameFlag = &cli.StringFlag{
		Name:  "name",
		Usage: "Human-readable name for the source",
	}
)

var supportedRuntimes = map[string]struct{}{"nodejs:22": {}}

// StageFunctionOptions contains the options for staging a function.
type StageFunctionOptions struct {
	// ConfigFile is the path to the deployment config file (default: gram.deploy.json)
	ConfigFile string
	// Slug is the unique identifier for the function (required)
	Slug string
	// Name is the human-readable name for the function (defaults to Slug if empty)
	Name string
	// Location is the path or URL to the function zip file (required)
	Location string
	// Runtime is the runtime environment (default: nodejs:22)
	Runtime string
}

// StageOpenAPIOptions contains the options for staging an OpenAPI document.
type StageOpenAPIOptions struct {
	// ConfigFile is the path to the deployment config file (default: gram.deploy.json)
	ConfigFile string
	// Slug is the unique identifier for the OpenAPI source (required)
	Slug string
	// Name is the human-readable name for the source (defaults to Slug if empty)
	Name string
	// Location is the path or URL to the OpenAPI document (required)
	Location string
}

// DoStageFunction stages a Gram Functions zip file for deployment.
// It creates the config file if it doesn't exist and appends the function source.
func DoStageFunction(opts StageFunctionOptions) error {
	if opts.ConfigFile == "" {
		opts.ConfigFile = "gram.deploy.json"
	}
	if opts.Runtime == "" {
		opts.Runtime = "nodejs:22"
	}
	if opts.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	if !constants.SlugPatternRE.MatchString(opts.Slug) {
		return fmt.Errorf("invalid slug: %s: %s", opts.Slug, constants.SlugMessage)
	}
	if opts.Location == "" {
		return fmt.Errorf("location is required")
	}
	if _, ok := supportedRuntimes[opts.Runtime]; !ok {
		return fmt.Errorf("unsupported runtime: %s", opts.Runtime)
	}

	name := opts.Name
	if name == "" {
		name = opts.Slug
	}

	if err := ensureConfigFileExists(opts.ConfigFile); err != nil {
		return err
	}

	if err := appendSourcesToConfig(opts.ConfigFile, []deploy.Source{
		{
			Type:     deploy.SourceTypeFunction,
			Slug:     opts.Slug,
			Name:     name,
			Location: opts.Location,
			Runtime:  opts.Runtime,
		},
	}); err != nil {
		return fmt.Errorf("failed to append source to config: %w", err)
	}

	return nil
}

// DoStageOpenAPI stages an OpenAPI document for deployment.
// It creates the config file if it doesn't exist and appends the OpenAPI source.
func DoStageOpenAPI(opts StageOpenAPIOptions) error {
	if opts.ConfigFile == "" {
		opts.ConfigFile = "gram.deploy.json"
	}
	if opts.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	if !constants.SlugPatternRE.MatchString(opts.Slug) {
		return fmt.Errorf("invalid slug: %s: %s", opts.Slug, constants.SlugMessage)
	}
	if opts.Location == "" {
		return fmt.Errorf("location is required")
	}

	name := opts.Name
	if name == "" {
		name = opts.Slug
	}

	if err := ensureConfigFileExists(opts.ConfigFile); err != nil {
		return err
	}

	if err := appendSourcesToConfig(opts.ConfigFile, []deploy.Source{
		{
			Type:     deploy.SourceTypeOpenAPIV3,
			Slug:     opts.Slug,
			Name:     name,
			Location: opts.Location,
		},
	}); err != nil {
		return fmt.Errorf("failed to append source to config: %w", err)
	}

	return nil
}

// ensureConfigFileExists creates the config file with initial structure if it doesn't exist.
func ensureConfigFileExists(configPath string) error {
	_, err := os.Stat(configPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// we'll create the file below
	case err != nil:
		return fmt.Errorf("%s: stat: %w", configPath, err)
	default:
		return validateExistingStageFile(configPath)
	}

	// #nosec G304 - the config path is user-specified and is treated as
	// JSON to be decoded into a struct with defined shape.
	file, err := os.OpenFile(configPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return file.Close() })

	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ")
	err = enc.Encode(deploy.Config{
		SchemaVersion: "1.0.0",
		Type:          deploy.ConfigTypeDeployment,
		Sources:       []deploy.Source{},
	})
	if err != nil {
		return fmt.Errorf("failed to write initial config: %w", err)
	}

	return nil
}

func newStageCommand() *cli.Command {
	return &cli.Command{
		Name:  "stage",
		Usage: "Stage an artifact for deployment to Gram",
		Description: `
The stage command will gradually build a deployment config that can later be
passed to "gram push". It is used to add Gram Functions zip files and OpenAPI
YAML/JSON documents.
`[1:],
		Flags: []cli.Flag{
			&cli.PathFlag{
				Name:  "config",
				Usage: "Path to the deployment config file",
				Value: "gram.deploy.json",
			},
		},
		Subcommands: []*cli.Command{
			newStageFunctionCommand(),
			newStageOpenAPICommand(),
		},
		Before: func(cCtx *cli.Context) error {
			configPath := cCtx.Path("config")
			if err := ensureConfigFileExists(configPath); err != nil {
				return fmt.Errorf("invalid config file %s: %w", configPath, err)
			}
			return nil
		},
	}
}

func newStageFunctionCommand() *cli.Command {
	return &cli.Command{
		Name:  "function",
		Usage: "Stage a Gram Functions zip file for deployment",
		Flags: []cli.Flag{
			sourceNameFlag,
			sourceSlugFlag,
			&cli.StringFlag{
				Name:     "location",
				Usage:    "Location to a zip file containing Gram Functions code. This can be a local file or a URL.",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "runtime",
				Usage: "Runtime environment for the function",
				Action: func(ctx *cli.Context, s string) error {
					if _, ok := supportedRuntimes[s]; !ok {
						return fmt.Errorf("unsupported runtime: %s", s)
					}
					return nil
				},
				Value: "nodejs:22",
			},
		},
		Action: func(cCtx *cli.Context) error {
			// Note: config file is already ensured to exist by the parent command's Before hook
			return DoStageFunction(StageFunctionOptions{
				ConfigFile: cCtx.Path("config"),
				Slug:       cCtx.String("slug"),
				Name:       cCtx.String("name"),
				Location:   cCtx.String("location"),
				Runtime:    cCtx.String("runtime"),
			})
		},
	}
}

func newStageOpenAPICommand() *cli.Command {
	return &cli.Command{
		Name:  "openapi",
		Usage: "Stage an OpenAPI document for deployment",
		Flags: []cli.Flag{
			sourceNameFlag,
			sourceSlugFlag,
			&cli.StringFlag{
				Name:     "location",
				Usage:    "Location to an OpenAPI JSON or YAML document. This can be a local file or a URL.",
				Required: true,
			},
		},
		Action: func(cCtx *cli.Context) error {
			// Note: config file is already ensured to exist by the parent command's Before hook
			return DoStageOpenAPI(StageOpenAPIOptions{
				ConfigFile: cCtx.Path("config"),
				Slug:       cCtx.String("slug"),
				Name:       cCtx.String("name"),
				Location:   cCtx.String("location"),
			})
		},
	}
}

func validateExistingStageFile(configPath string) error {
	// #nosec G304 - this function is only validating the file contents and not
	// using it in an unsafe way.
	current, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var config deploy.Config
	dec := json.NewDecoder(bytes.NewBuffer(current))
	err = dec.Decode(&config)
	if err != nil {
		return fmt.Errorf("failed to decode config file: %w", err)
	}

	if config.Type != deploy.ConfigTypeDeployment {
		return fmt.Errorf("invalid config type: %s", config.Type)
	}

	return nil
}

func appendSourcesToConfig(configPath string, sources []deploy.Source) error {
	// #nosec G304 - this function is only editing the file contents and not
	// passing its contents onwards to the rest of the program in an unsafe way.
	current, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var config deploy.Config
	dec := json.NewDecoder(bytes.NewBuffer(current))
	err = dec.Decode(&config)
	if err != nil {
		return fmt.Errorf("failed to decode config file: %w", err)
	}

	config.Sources = append(config.Sources, sources...)

	seen := make(map[string]int)
	for i, source := range config.Sources {
		seen[source.Slug] = i
	}

	deduped := make([]deploy.Source, 0, len(seen))
	for _, idx := range seen {
		deduped = append(deduped, config.Sources[idx])
	}
	config.Sources = deduped

	// #nosec G304 - this function is only updating a file contents and not
	// passing its contents onwards to the rest of the program in an unsafe way.
	file, err := os.OpenFile(configPath, os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open config file: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return file.Close() })

	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ")
	err = enc.Encode(config)
	if err != nil {
		return fmt.Errorf("failed to encode config file: %w", err)
	}

	return nil
}
