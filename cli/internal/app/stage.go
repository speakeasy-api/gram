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
			_, err := os.Stat(configPath)
			switch {
			case errors.Is(err, os.ErrNotExist):
				// we'll create the file below
			case err != nil:
				return fmt.Errorf("%s: stat: %w", configPath, err)
			default:
				if err := validateExistingStageFile(configPath); err != nil {
					return fmt.Errorf("invalid config file %s: %w", configPath, err)
				}
				return nil
			}

			file, err := os.OpenFile(configPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
			if err != nil {
				if errors.Is(err, os.ErrExist) {
					return nil
				}
				return fmt.Errorf("failed to create config file: %w", err)
			}

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
			slug := cCtx.String("slug")
			location := cCtx.String("location")
			runtime := cCtx.String("runtime")
			name := cCtx.String("name")
			if name == "" {
				name = slug
			}

			if err := appendSourcesToConfig(cCtx.Path("config"), []deploy.Source{
				{
					Type:     deploy.SourceTypeFunction,
					Slug:     slug,
					Name:     name,
					Location: location,
					Runtime:  runtime,
				},
			}); err != nil {
				return fmt.Errorf("failed to append source to config: %w", err)
			}

			return nil
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
			slug := cCtx.String("slug")
			location := cCtx.String("location")
			name := cCtx.String("name")
			if name == "" {
				name = slug
			}

			if err := appendSourcesToConfig(cCtx.Path("config"), []deploy.Source{
				{
					Type:     deploy.SourceTypeOpenAPIV3,
					Slug:     slug,
					Name:     name,
					Location: location,
				},
			}); err != nil {
				return fmt.Errorf("failed to append source to config: %w", err)
			}

			return nil
		},
	}
}

func validateExistingStageFile(configPath string) error {
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

	file, err := os.OpenFile(configPath, os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ")
	err = enc.Encode(config)
	if err != nil {
		return fmt.Errorf("failed to encode config file: %w", err)
	}

	return nil
}
