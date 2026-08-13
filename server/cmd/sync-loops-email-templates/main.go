package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/server/internal/email"
	"github.com/speakeasy-api/gram/server/internal/email/loopsync"
	"github.com/speakeasy-api/gram/server/internal/guardian"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("sync-loops-email-templates", flag.ContinueOnError)
	manifestPath := flags.String("manifest", "server/internal/email/loops/manifest.json", "path to the email manifest")
	idsPath := flags.String("ids", "", "optional JSON file containing the current logical-key to Loops-ID mapping")
	outputPath := flags.String("output", "", "JSON file to write with the resolved mapping")
	baseURL := flags.String("base-url", loopsync.DefaultBaseURL, "Loops Content API base URL")
	validateOnly := flags.Bool("validate-only", false, "validate repository templates without calling Loops")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse command flags: %w", err)
	}

	manifest, err := loopsync.LoadManifest(*manifestPath)
	if err != nil {
		return fmt.Errorf("load transactional email templates: %w", err)
	}
	if err := validateApplicationContract(manifest); err != nil {
		return err
	}
	if *validateOnly {
		fmt.Printf("validated %d transactional email templates\n", len(manifest.Templates))
		return nil
	}

	apiKey := os.Getenv("LOOPS_API_KEY")
	if apiKey == "" || apiKey == "unset" {
		return fmt.Errorf("LOOPS_API_KEY is required unless --validate-only is set")
	}
	if *outputPath == "" {
		return fmt.Errorf("--output is required")
	}

	existingIDs, err := readIDs(*idsPath)
	if err != nil {
		return err
	}
	policy := guardian.NewDefaultPolicy(noop.NewTracerProvider())
	client := loopsync.NewClient(*baseURL, apiKey, policy.PooledClient())
	resolved, err := (&loopsync.Reconciler{API: client, Log: os.Stdout}).Reconcile(ctx, manifest, existingIDs)
	if err != nil {
		return fmt.Errorf("reconcile transactional email templates: %w", err)
	}
	return writeJSONAtomically(*outputPath, resolved)
}

func validateApplicationContract(manifest *loopsync.Manifest) error {
	registered := make(map[string][]string, len(email.RegisteredTemplates))
	for _, tmpl := range email.RegisteredTemplates {
		variables := make([]string, 0, len(tmpl.Variables()))
		for variable := range tmpl.Variables() {
			variables = append(variables, variable)
		}
		slices.Sort(variables)
		registered[string(tmpl.Key())] = variables
	}
	if len(registered) != len(manifest.Templates) {
		return fmt.Errorf("email manifest has %d templates; application registers %d", len(manifest.Templates), len(registered))
	}
	for key, variables := range registered {
		spec, ok := manifest.Templates[key]
		if !ok {
			return fmt.Errorf("email manifest is missing registered template %q", key)
		}
		declared := slices.Clone(spec.Variables)
		slices.Sort(declared)
		if !slices.Equal(variables, declared) {
			return fmt.Errorf("email template %q variables are %v in Go and %v in manifest", key, variables, declared)
		}
	}
	return nil
}

func readIDs(path string) (map[string]string, error) {
	if path == "" {
		return map[string]string{}, nil
	}
	data, err := os.ReadFile(path) // #nosec G304 -- the operator explicitly supplies the current ID mapping path
	if err != nil {
		return nil, fmt.Errorf("read current email template IDs: %w", err)
	}
	var ids map[string]string
	if err := json.Unmarshal(data, &ids); err != nil {
		return nil, fmt.Errorf("decode current email template IDs: %w", err)
	}
	return ids, nil
}

func writeJSONAtomically(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode resolved email template IDs: %w", err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".email-template-ids-*")
	if err != nil {
		return fmt.Errorf("create temporary ID mapping: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary ID mapping: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary ID mapping: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace resolved ID mapping: %w", err)
	}
	return nil
}
