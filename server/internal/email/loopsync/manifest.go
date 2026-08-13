// Package loopsync reconciles repository-owned LMX transactional emails with
// Loops' Content API.
package loopsync

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var dataVariablePattern = regexp.MustCompile(`\{data\.([A-Za-z0-9_-]+)\}`)

type Manifest struct {
	Version   int                     `json:"version"`
	Defaults  MessageDefaults         `json:"defaults"`
	Templates map[string]TemplateSpec `json:"templates"`
	Dir       string                  `json:"-"`
}

type MessageDefaults struct {
	FromName     string `json:"from_name"`
	FromEmail    string `json:"from_email"`
	ReplyToEmail string `json:"reply_to_email"`
}

type TemplateSpec struct {
	ManagedName     string   `json:"managed_name"`
	Subject         string   `json:"subject"`
	PreviewText     string   `json:"preview_text"`
	Source          string   `json:"source"`
	Variables       []string `json:"variables"`
	UnusedVariables []string `json:"unused_variables,omitempty"`
	LMX             string   `json:"-"`
	SourceVariables []string `json:"-"`
}

func LoadManifest(path string) (*Manifest, error) {
	f, err := os.Open(path) // #nosec G304 -- the operator explicitly supplies the manifest path
	if err != nil {
		return nil, fmt.Errorf("open email manifest: %w", err)
	}
	defer func() { _ = f.Close() }()

	var manifest Manifest
	decoder := json.NewDecoder(io.LimitReader(f, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode email manifest: %w", err)
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("decode email manifest: trailing data after JSON object")
		}
		return nil, fmt.Errorf("decode email manifest trailing data: %w", err)
	}
	manifest.Dir = filepath.Dir(path)
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func (m *Manifest) Validate() error {
	if m.Version != 1 {
		return fmt.Errorf("email manifest: unsupported version %d", m.Version)
	}
	if len(m.Templates) == 0 {
		return fmt.Errorf("email manifest: no templates")
	}
	if m.Defaults.FromName == "" || m.Defaults.FromEmail == "" || m.Defaults.ReplyToEmail == "" {
		return fmt.Errorf("email manifest: sender defaults are incomplete")
	}
	if err := validateLMXDirectory(m.Dir); err != nil {
		return err
	}

	managedNames := make(map[string]string, len(m.Templates))
	for key, spec := range m.Templates {
		if key == "" || spec.ManagedName == "" || spec.Subject == "" || spec.Source == "" {
			return fmt.Errorf("email manifest: template %q has incomplete metadata", key)
		}
		expectedManagedName := "gram.transactional.v2." + key
		if spec.ManagedName != expectedManagedName {
			return fmt.Errorf("email manifest: template %q has managed name %q, want %q", key, spec.ManagedName, expectedManagedName)
		}
		if existing, ok := managedNames[spec.ManagedName]; ok {
			return fmt.Errorf("email manifest: templates %q and %q share managed name %q", existing, key, spec.ManagedName)
		}
		managedNames[spec.ManagedName] = key

		cleanSource := filepath.Clean(spec.Source)
		if filepath.IsAbs(cleanSource) || cleanSource == ".." || strings.HasPrefix(cleanSource, ".."+string(filepath.Separator)) {
			return fmt.Errorf("email manifest: template %q source escapes the manifest directory", key)
		}
		sourcePath := filepath.Join(m.Dir, cleanSource)
		lmx, err := os.ReadFile(sourcePath) // #nosec G304 -- source is constrained to the manifest directory above
		if err != nil {
			return fmt.Errorf("read LMX for %q: %w", key, err)
		}
		sourceVariables := extractDataVariables(spec.Subject + "\n" + spec.PreviewText + "\n" + string(lmx))
		declared, duplicate := makeUniqueSet(spec.Variables)
		if duplicate != "" {
			return fmt.Errorf("email manifest: template %q declares variable %q more than once", key, duplicate)
		}
		unused, duplicate := makeUniqueSet(spec.UnusedVariables)
		if duplicate != "" {
			return fmt.Errorf("email manifest: template %q marks variable %q unused more than once", key, duplicate)
		}
		for _, variable := range sourceVariables {
			if _, ok := declared[variable]; !ok {
				return fmt.Errorf("email manifest: template %q uses undeclared variable %q", key, variable)
			}
		}
		used, _ := makeUniqueSet(sourceVariables)
		for variable := range declared {
			if _, ok := used[variable]; ok {
				continue
			}
			if _, ok := unused[variable]; !ok {
				return fmt.Errorf("email manifest: template %q declares unused variable %q", key, variable)
			}
		}
		for variable := range unused {
			if _, ok := declared[variable]; !ok {
				return fmt.Errorf("email manifest: template %q marks undeclared variable %q unused", key, variable)
			}
		}

		spec.LMX = strings.TrimSpace(string(lmx))
		spec.SourceVariables = sourceVariables
		m.Templates[key] = spec
	}
	return nil
}

func validateLMXDirectory(dir string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("open LMX directory: %w", err)
	}
	walkErr := fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk LMX directory: %w", walkErr)
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".lmx" {
			return nil
		}
		lmx, err := root.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read LMX file %q: %w", path, err)
		}
		if err := validateXML(lmx); err != nil {
			return fmt.Errorf("validate LMX file %q: %w", path, err)
		}
		return nil
	})
	closeErr := root.Close()
	if walkErr != nil {
		return fmt.Errorf("validate LMX directory: %w", walkErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close LMX directory: %w", closeErr)
	}
	return nil
}

func validateXML(lmx []byte) error {
	decoder := xml.NewDecoder(strings.NewReader("<Root>" + string(lmx) + "</Root>"))
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read LMX token: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "Columns":
			if err := validateIntegerAttribute(start, "gap", 12, 150); err != nil {
				return err
			}
		case "Paragraph":
			if err := validateIntegerAttribute(start, "fontSize", 12, 64); err != nil {
				return err
			}
		}
	}
}

func validateIntegerAttribute(element xml.StartElement, name string, minimum, maximum int) error {
	for _, attribute := range element.Attr {
		if attribute.Name.Local != name {
			continue
		}
		value, err := strconv.Atoi(attribute.Value)
		if err != nil || value < minimum || value > maximum {
			return fmt.Errorf(
				"%s attribute %q must be an integer between %d and %d (got %q)",
				element.Name.Local,
				name,
				minimum,
				maximum,
				attribute.Value,
			)
		}
	}
	return nil
}

func extractDataVariables(content string) []string {
	seen := map[string]struct{}{}
	for _, match := range dataVariablePattern.FindAllStringSubmatch(content, -1) {
		seen[match[1]] = struct{}{}
	}
	variables := make([]string, 0, len(seen))
	for variable := range seen {
		variables = append(variables, variable)
	}
	slices.Sort(variables)
	return variables
}

func makeUniqueSet(values []string) (map[string]struct{}, string) {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, exists := result[value]; exists {
			return nil, value
		}
		result[value] = struct{}{}
	}
	return result, ""
}
