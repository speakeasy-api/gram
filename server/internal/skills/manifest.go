package skills

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
	"gopkg.in/yaml.v3"
)

const (
	maxSkillContentBytes = 64 * 1024
	maxSkillYAMLDepth    = 64
	maxSkillYAMLNodes    = 8 * 1024
)

var errCanonicalDocumentTooLarge = errors.New("canonical skill manifest exceeds 65536 bytes")
var errYAMLSourceTooDeep = errors.New("YAML source exceeds maximum nesting depth of 64")

type validationError struct {
	Code    string `json:"code"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

type parsedSkillManifest struct {
	RawContent       string
	Name             string
	DisplayName      string
	Description      *string
	Metadata         map[string]any
	SpecValid        bool
	ValidationErrors []validationError
	RawSHA256        string
	CanonicalSHA256  string
	canonicalContent string
}

func parseSkillManifest(content string) (parsedSkillManifest, error) {
	if content == "" {
		return parsedSkillManifest{}, errors.New("parse skill manifest: content is empty")
	}
	if len(content) > maxSkillContentBytes {
		return parsedSkillManifest{}, fmt.Errorf("parse skill manifest: content exceeds %d bytes", maxSkillContentBytes)
	}
	if !utf8.ValidString(content) {
		return parsedSkillManifest{}, errors.New("parse skill manifest: content is not valid UTF-8")
	}
	if strings.IndexByte(content, 0) >= 0 {
		return parsedSkillManifest{}, errors.New("parse skill manifest: content contains NUL")
	}

	normalized := content
	normalized = strings.TrimPrefix(normalized, "\ufeff")
	normalized = strings.ReplaceAll(normalized, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	normalized = norm.NFC.String(normalized)
	lines := strings.Split(normalized, "\n")
	for i := range lines {
		lines[i] = strings.TrimRightFunc(lines[i], unicode.IsSpace)
	}
	if len(lines) == 0 || lines[0] != "---" {
		return parsedSkillManifest{}, errors.New("parse skill manifest: missing opening frontmatter delimiter")
	}

	closingLine := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			closingLine = i
			break
		}
	}
	if closingLine < 0 {
		return parsedSkillManifest{}, errors.New("parse skill manifest: missing closing frontmatter delimiter")
	}

	frontmatter := strings.Join(lines[1:closingLine], "\n") + "\n"
	if err := preflightYAMLSource(frontmatter); err != nil {
		return parsedSkillManifest{}, fmt.Errorf("preflight skill manifest frontmatter: %w", err)
	}
	var document yaml.Node
	decoder := yaml.NewDecoder(strings.NewReader(frontmatter))
	if err := decoder.Decode(&document); err != nil {
		return parsedSkillManifest{}, fmt.Errorf("parse skill manifest frontmatter: %w", err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err == nil {
		return parsedSkillManifest{}, errors.New("parse skill manifest frontmatter: multiple YAML documents")
	} else if !errors.Is(err, io.EOF) {
		return parsedSkillManifest{}, fmt.Errorf("parse skill manifest frontmatter: %w", err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return parsedSkillManifest{}, errors.New("parse skill manifest frontmatter: top level must be a mapping")
	}
	root := document.Content[0]
	if err := validateYAMLTree(&document); err != nil {
		return parsedSkillManifest{}, fmt.Errorf("parse skill manifest frontmatter: %w", err)
	}

	fields := mappingFields(root)
	nameNode, hasName := fields["name"]
	if !hasName {
		return parsedSkillManifest{}, errors.New("parse skill manifest frontmatter: name is required")
	}
	if nameNode.Kind != yaml.ScalarNode || nameNode.Tag != "!!str" {
		return parsedSkillManifest{}, errors.New("parse skill manifest frontmatter: name must be a string")
	}
	displayName := strings.TrimSpace(nameNode.Value)
	if displayName == "" {
		return parsedSkillManifest{}, errors.New("parse skill manifest frontmatter: name must not be empty")
	}
	name, err := normalizeSkillName(displayName)
	if err != nil {
		return parsedSkillManifest{}, fmt.Errorf("parse skill manifest frontmatter: %w", err)
	}

	validationErrors := validateSkillSpec(fields, displayName)
	description := stringField(fields, "description")
	metadata := metadataField(fields)

	body := strings.Join(lines[closingLine+1:], "\n")
	body = strings.Trim(body, "\n")
	canonicalEnvelopeBytes := len("---\n") + len("---\n")
	if body != "" {
		canonicalEnvelopeBytes += len(body) + 2
	}
	if canonicalEnvelopeBytes > maxSkillContentBytes {
		return parsedSkillManifest{}, fmt.Errorf("canonicalize skill manifest: %w", errCanonicalDocumentTooLarge)
	}

	canonicalRoot, err := canonicalYAMLNode(root)
	if err != nil {
		return parsedSkillManifest{}, fmt.Errorf("canonicalize skill manifest frontmatter: %w", err)
	}
	encoded := boundedBuffer{Buffer: bytes.Buffer{}, limit: maxSkillContentBytes - canonicalEnvelopeBytes, exceeded: false}
	encoder := yaml.NewEncoder(&encoded)
	encoder.SetIndent(2)
	if err := encoder.Encode(canonicalRoot); err != nil {
		if encoded.exceeded {
			return parsedSkillManifest{}, fmt.Errorf("canonicalize skill manifest: %w", errCanonicalDocumentTooLarge)
		}
		return parsedSkillManifest{}, fmt.Errorf("canonicalize skill manifest frontmatter: %w", err)
	}
	if err := encoder.Close(); err != nil {
		if encoded.exceeded {
			return parsedSkillManifest{}, fmt.Errorf("canonicalize skill manifest: %w", errCanonicalDocumentTooLarge)
		}
		return parsedSkillManifest{}, fmt.Errorf("canonicalize skill manifest frontmatter: %w", err)
	}

	canonicalContent := "---\n" + encoded.String() + "---\n"
	if body != "" {
		canonicalContent += "\n" + body + "\n"
	}
	if len(canonicalContent) > maxSkillContentBytes {
		return parsedSkillManifest{}, fmt.Errorf("canonicalize skill manifest: %w", errCanonicalDocumentTooLarge)
	}

	rawDigest := sha256.Sum256([]byte(content))
	fileDigest := sha256.Sum256([]byte(canonicalContent))
	manifestPreimage := make([]byte, 0, len("skill-manifest-v1")+1+len("SKILL.md")+1+sha256.Size+1)
	manifestPreimage = append(manifestPreimage, "skill-manifest-v1"...)
	manifestPreimage = append(manifestPreimage, 0)
	manifestPreimage = append(manifestPreimage, "SKILL.md"...)
	manifestPreimage = append(manifestPreimage, 0)
	manifestPreimage = append(manifestPreimage, fileDigest[:]...)
	manifestPreimage = append(manifestPreimage, 0)
	manifestDigest := sha256.Sum256(manifestPreimage)

	return parsedSkillManifest{
		RawContent:       content,
		Name:             name,
		DisplayName:      displayName,
		Description:      description,
		Metadata:         metadata,
		SpecValid:        len(validationErrors) == 0,
		ValidationErrors: validationErrors,
		RawSHA256:        hex.EncodeToString(rawDigest[:]),
		CanonicalSHA256:  hex.EncodeToString(manifestDigest[:]),
		canonicalContent: canonicalContent,
	}, nil
}

func preflightYAMLSource(source string) error {
	scannerSource := strings.NewReplacer("\u0085", "\n", "\u2028", "\n", "\u2029", "\n").Replace(source)
	var indentationLevels [maxSkillYAMLDepth + 1]int
	indentationCount := 0
	flowDepth := 0
	var quote byte
	blockScalar := false
	blockHeaderIndent := 0
	blockContentIndent := -1
	blockExplicitIndent := 0

	for offset := 0; offset < len(scannerSource); {
		lineEnd := strings.IndexByte(scannerSource[offset:], '\n')
		if lineEnd < 0 {
			lineEnd = len(scannerSource)
		} else {
			lineEnd += offset
		}
		line := scannerSource[offset:lineEnd]
		offset = lineEnd + 1

		indent := 0
		for indent < len(line) && line[indent] == ' ' {
			indent++
		}
		blank := indent == len(line)
		if blockScalar {
			if blank {
				continue
			}
			if blockContentIndent < 0 {
				if blockExplicitIndent > 0 {
					blockContentIndent = blockHeaderIndent + blockExplicitIndent
				} else if indent > blockHeaderIndent {
					blockContentIndent = indent
				} else {
					blockScalar = false
				}
			}
			if blockScalar && indent >= blockContentIndent {
				continue
			}
			blockScalar = false
		}

		lineStartsInQuote := quote != 0
		tokenStart := !lineStartsInQuote
		plainScalar := false
		compactSequenceDepth := 0
		logicalNodeIndent := indent
		blockMappingKeyIndent := -1
		for i := 0; i < len(line); i++ {
			current := line[i]
			switch quote {
			case '\'':
				if current != '\'' {
					continue
				}
				if i+1 < len(line) && line[i+1] == '\'' {
					i++
					continue
				}
				quote = 0
				plainScalar = true
				continue
			case '"':
				if current == '\\' {
					i++
					continue
				}
				if current == '"' {
					quote = 0
					plainScalar = true
				}
				continue
			}

			if plainScalar {
				if current == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t') {
					break
				}
				if current == ':' && yamlSeparationAfter(line, i, flowDepth > 0) {
					if flowDepth == 0 {
						if err := recordYAMLBlockIndent(&indentationLevels, &indentationCount, indent); err != nil {
							return err
						}
						blockMappingKeyIndent = logicalNodeIndent
					}
					plainScalar = false
					tokenStart = true
					logicalNodeIndent = -1
					continue
				}
				if flowDepth == 0 || current != ',' && current != ']' && current != '}' {
					continue
				}
				plainScalar = false
			}

			if current == ' ' || current == '\t' {
				continue
			}
			if current == '#' {
				break
			}
			if tokenStart && (current == '!' || current == '&') {
				if logicalNodeIndent < 0 {
					logicalNodeIndent = i
				}
				i = yamlNodePropertyEnd(line, i) - 1
				continue
			}
			if tokenStart && logicalNodeIndent < 0 {
				logicalNodeIndent = i
			}

			switch current {
			case '\'', '"':
				if tokenStart {
					quote = current
					tokenStart = false
				} else {
					plainScalar = true
				}
			case '-':
				if tokenStart && flowDepth == 0 && yamlSeparationAfter(line, i, false) {
					if err := recordYAMLBlockIndent(&indentationLevels, &indentationCount, indent); err != nil {
						return err
					}
					compactSequenceDepth++
					logicalNodeIndent = -1
					blockDepth := max(indentationCount-1, 0)
					if compactSequenceDepth+blockDepth > maxSkillYAMLDepth {
						return errYAMLSourceTooDeep
					}
				} else {
					plainScalar = true
					tokenStart = false
				}
			case '{', '[':
				if tokenStart {
					flowDepth++
					blockDepth := max(indentationCount-1, 0)
					if flowDepth+blockDepth+compactSequenceDepth > maxSkillYAMLDepth {
						return errYAMLSourceTooDeep
					}
				} else {
					plainScalar = true
				}
			case '}', ']':
				if flowDepth > 0 {
					flowDepth--
				}
				plainScalar = true
				tokenStart = false
			case ',':
				if flowDepth > 0 {
					tokenStart = true
				} else {
					plainScalar = true
					tokenStart = false
				}
			case ':':
				if yamlSeparationAfter(line, i, flowDepth > 0) {
					if flowDepth == 0 {
						if err := recordYAMLBlockIndent(&indentationLevels, &indentationCount, indent); err != nil {
							return err
						}
						blockMappingKeyIndent = logicalNodeIndent
					}
					tokenStart = true
					logicalNodeIndent = -1
				} else {
					plainScalar = true
					tokenStart = false
				}
			case '?':
				if tokenStart && yamlSeparationAfter(line, i, flowDepth > 0) {
					if flowDepth == 0 {
						if err := recordYAMLBlockIndent(&indentationLevels, &indentationCount, indent); err != nil {
							return err
						}
					}
					tokenStart = true
				} else {
					plainScalar = true
					tokenStart = false
				}
			case '|', '>':
				if tokenStart && flowDepth == 0 {
					if explicitIndent, ok := yamlBlockScalarHeader(line, i); ok {
						blockScalar = true
						blockHeaderIndent = indent
						if blockMappingKeyIndent >= 0 {
							blockHeaderIndent = blockMappingKeyIndent
						}
						blockContentIndent = -1
						blockExplicitIndent = explicitIndent
						i = len(line)
					} else {
						plainScalar = true
						tokenStart = false
					}
				} else {
					plainScalar = true
					tokenStart = false
				}
			default:
				plainScalar = true
				tokenStart = false
			}
		}
	}

	return nil
}

func recordYAMLBlockIndent(levels *[maxSkillYAMLDepth + 1]int, count *int, indent int) error {
	if *count == 0 {
		levels[0] = indent
		*count = 1
		return nil
	}

	// #nosec G602 -- count is initialized above and capped before every push.
	currentIndent := levels[*count-1]
	switch {
	case indent > currentIndent:
		if *count >= maxSkillYAMLDepth {
			return errYAMLSourceTooDeep
		}
		levels[*count] = indent
		*count++
	case indent < currentIndent:
		for *count > 1 && indent < currentIndent {
			*count--
			// #nosec G602 -- count remains between one and maxSkillYAMLDepth.
			currentIndent = levels[*count-1]
		}
	}
	return nil
}

func yamlNodePropertyEnd(line string, start int) int {
	if line[start] == '!' && start+1 < len(line) && line[start+1] == '<' {
		for i := start + 2; i < len(line); i++ {
			if line[i] == '>' {
				return i + 1
			}
		}
		return len(line)
	}

	for i := start + 1; i < len(line); i++ {
		current := line[i]
		if current == ' ' || current == '\t' || current == ',' || current == '[' || current == ']' || current == '{' || current == '}' {
			return i
		}
	}
	return len(line)
}

func yamlSeparationAfter(line string, indicator int, flow bool) bool {
	if indicator+1 == len(line) {
		return true
	}
	next := line[indicator+1]
	if next == ' ' || next == '\t' {
		return true
	}
	return flow && (next == ',' || next == '[' || next == ']' || next == '{' || next == '}' || next == '!' || next == '&')
}

func yamlBlockScalarHeader(line string, indicator int) (int, bool) {
	explicitIndent := 0
	chompingSeen := false
	for i := indicator + 1; i < len(line); i++ {
		switch current := line[i]; {
		case current == '+' || current == '-':
			if chompingSeen {
				return 0, false
			}
			chompingSeen = true
		case current >= '1' && current <= '9':
			if explicitIndent != 0 {
				return 0, false
			}
			explicitIndent = int(current - '0')
		case current == ' ' || current == '\t':
			for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
				i++
			}
			return explicitIndent, i == len(line) || line[i] == '#'
		case current == '#':
			return explicitIndent, true
		default:
			return 0, false
		}
	}
	return explicitIndent, true
}

func validateYAMLTree(node *yaml.Node) error {
	type treeEntry struct {
		node  *yaml.Node
		depth int
	}

	stack := []treeEntry{{node: node, depth: 1}}
	nodeCount := 0
	for len(stack) > 0 {
		entry := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		nodeCount++
		if nodeCount > maxSkillYAMLNodes {
			return fmt.Errorf("YAML exceeds maximum node count of %d", maxSkillYAMLNodes)
		}
		if entry.depth > maxSkillYAMLDepth {
			return fmt.Errorf("YAML exceeds maximum nesting depth of %d", maxSkillYAMLDepth)
		}
		current := entry.node
		if current.Anchor != "" || current.Kind == yaml.AliasNode {
			return errors.New("aliases and anchors are not allowed")
		}
		if err := validateYAMLNodeTag(current); err != nil {
			return err
		}
		if current.Kind == yaml.MappingNode {
			seen := make(map[string]struct{}, len(current.Content)/2)
			for i := 0; i < len(current.Content); i += 2 {
				key := current.Content[i]
				if key.Kind == yaml.ScalarNode && key.Tag == "!!merge" {
					return errors.New("YAML merge keys are not allowed")
				}
				if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
					return errors.New("mapping keys must be strings")
				}
				normalizedKey := norm.NFC.String(key.Value)
				if _, ok := seen[normalizedKey]; ok {
					return fmt.Errorf("duplicate mapping key %q", normalizedKey)
				}
				seen[normalizedKey] = struct{}{}
			}
		}
		if current.Kind == yaml.ScalarNode {
			if _, err := canonicalScalarValue(current); err != nil {
				return err
			}
		}
		for i := len(current.Content) - 1; i >= 0; i-- {
			stack = append(stack, treeEntry{node: current.Content[i], depth: entry.depth + 1})
		}
	}
	return nil
}

func validateYAMLNodeTag(node *yaml.Node) error {
	switch node.Kind {
	case yaml.DocumentNode:
		if node.Tag == "" {
			return nil
		}
	case yaml.MappingNode:
		if node.Tag == "!!map" {
			return nil
		}
	case yaml.SequenceNode:
		if node.Tag == "!!seq" {
			return nil
		}
	case yaml.ScalarNode:
		switch node.Tag {
		case "!!str", "!!bool", "!!int", "!!float", "!!null", "!!timestamp", "!!binary":
			return nil
		}
	case yaml.AliasNode:
		return errors.New("aliases are not allowed")
	}

	switch node.Tag {
	case "", "!!map", "!!seq", "!!str", "!!bool", "!!int", "!!float", "!!null", "!!timestamp", "!!binary", "!!merge":
		return fmt.Errorf("YAML tag %q is incompatible with node kind %d", node.Tag, node.Kind)
	default:
		return fmt.Errorf("custom YAML tag %q is not allowed", node.Tag)
	}
}

func normalizeSkillName(displayName string) (string, error) {
	var normalized strings.Builder
	separator := false
	for _, r := range norm.NFC.String(displayName) {
		switch {
		case r >= 'A' && r <= 'Z':
			normalized.WriteRune(r + ('a' - 'A'))
			separator = false
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			normalized.WriteRune(r)
			separator = false
		case r == '-' || r == '_' || r == '.':
			if !separator {
				normalized.WriteByte('-')
				separator = true
			}
		default:
			return "", fmt.Errorf("name %q cannot be normalized", displayName)
		}
	}
	name := normalized.String()
	if len(name) == 0 || len(name) > 64 || name[0] == '-' || name[len(name)-1] == '-' {
		return "", fmt.Errorf("name %q cannot be normalized to 1-64 characters with alphanumeric boundaries", displayName)
	}
	return name, nil
}

func validateSkillSpec(fields map[string]*yaml.Node, displayName string) []validationError {
	validationErrors := make([]validationError, 0)
	if !validSpecName(displayName) {
		validationErrors = append(validationErrors, validationError{
			Code:    "invalid_format",
			Field:   "name",
			Message: "name must contain only lowercase letters, digits, and single hyphens, with alphanumeric boundaries, and be at most 64 characters",
		})
	}

	description, ok := fields["description"]
	switch {
	case !ok:
		validationErrors = append(validationErrors, validationError{Code: "required", Field: "description", Message: "description is required"})
	case description.Kind != yaml.ScalarNode || description.Tag != "!!str":
		validationErrors = append(validationErrors, validationError{Code: "invalid_type", Field: "description", Message: "description must be a string"})
	default:
		if strings.TrimSpace(description.Value) == "" {
			validationErrors = append(validationErrors, validationError{Code: "required", Field: "description", Message: "description must not be empty"})
		} else if utf8.RuneCountInString(description.Value) > 1024 {
			validationErrors = append(validationErrors, validationError{Code: "too_long", Field: "description", Message: "description must be at most 1024 Unicode code points"})
		}
	}

	if compatibility, ok := fields["compatibility"]; ok {
		switch {
		case compatibility.Kind != yaml.ScalarNode || compatibility.Tag != "!!str":
			validationErrors = append(validationErrors, validationError{Code: "invalid_type", Field: "compatibility", Message: "compatibility must be a string"})
		default:
			if strings.TrimSpace(compatibility.Value) == "" {
				validationErrors = append(validationErrors, validationError{Code: "required", Field: "compatibility", Message: "compatibility must not be empty"})
			} else if utf8.RuneCountInString(compatibility.Value) > 500 {
				validationErrors = append(validationErrors, validationError{Code: "too_long", Field: "compatibility", Message: "compatibility must be at most 500 Unicode code points"})
			}
		}
	}

	for _, field := range []string{"allowed-tools", "license"} {
		if value, ok := fields[field]; ok && (value.Kind != yaml.ScalarNode || value.Tag != "!!str") {
			validationErrors = append(validationErrors, validationError{Code: "invalid_type", Field: field, Message: field + " must be a string"})
		}
	}

	if metadata, ok := fields["metadata"]; ok {
		if metadata.Kind != yaml.MappingNode {
			validationErrors = append(validationErrors, validationError{Code: "invalid_type", Field: "metadata", Message: "metadata must be a mapping"})
		} else {
			for i := 0; i < len(metadata.Content); i += 2 {
				key, value := metadata.Content[i], metadata.Content[i+1]
				if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
					validationErrors = append(validationErrors, validationError{
						Code:    "invalid_type",
						Field:   "metadata." + key.Value,
						Message: "metadata values must be strings",
					})
				}
			}
		}
	}

	slices.SortFunc(validationErrors, func(a, b validationError) int {
		if a.Field != b.Field {
			return cmp.Compare(a.Field, b.Field)
		}
		if a.Code != b.Code {
			return cmp.Compare(a.Code, b.Code)
		}
		return cmp.Compare(a.Message, b.Message)
	})
	return validationErrors
}

func validSpecName(name string) bool {
	if len(name) == 0 || len(name) > 64 || name[0] == '-' || name[len(name)-1] == '-' {
		return false
	}
	previousHyphen := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			previousHyphen = false
		case r == '-' && !previousHyphen:
			previousHyphen = true
		default:
			return false
		}
	}
	return true
}

func mappingFields(node *yaml.Node) map[string]*yaml.Node {
	fields := make(map[string]*yaml.Node, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		fields[norm.NFC.String(node.Content[i].Value)] = node.Content[i+1]
	}
	return fields
}

func stringField(fields map[string]*yaml.Node, field string) *string {
	node, ok := fields[field]
	if !ok || node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return nil
	}
	value := strings.TrimSpace(node.Value)
	return &value
}

func metadataField(fields map[string]*yaml.Node) map[string]any {
	metadata := make(map[string]any)
	node, ok := fields["metadata"]
	if !ok || node.Kind != yaml.MappingNode {
		return metadata
	}
	for i := 0; i < len(node.Content); i += 2 {
		metadata[norm.NFC.String(node.Content[i].Value)] = yamlJSONValue(node.Content[i+1])
	}
	return metadata
}

func yamlJSONValue(node *yaml.Node) any {
	switch node.Kind {
	case yaml.MappingNode:
		value := make(map[string]any, len(node.Content)/2)
		for i := 0; i < len(node.Content); i += 2 {
			value[norm.NFC.String(node.Content[i].Value)] = yamlJSONValue(node.Content[i+1])
		}
		return value
	case yaml.SequenceNode:
		value := make([]any, len(node.Content))
		for i, child := range node.Content {
			value[i] = yamlJSONValue(child)
		}
		return value
	case yaml.ScalarNode:
		switch node.Tag {
		case "!!null":
			return nil
		case "!!bool":
			value, err := strconv.ParseBool(strings.ToLower(node.Value))
			if err == nil {
				return value
			}
		case "!!int":
			if value, ok := yamlIntegerNumber(node.Value); ok {
				return value
			}
		case "!!float":
			if value, err := canonicalYAMLFloat(node.Value); err == nil {
				return json.Number(value)
			}
		case "!!timestamp", "!!binary":
			if value, err := canonicalScalarValue(node); err == nil {
				return value
			}
		}
		return norm.NFC.String(node.Value)
	default:
		return nil
	}
}

func yamlIntegerNumber(value string) (json.Number, bool) {
	clean := strings.ReplaceAll(value, "_", "")
	integer := new(big.Int)
	if _, ok := integer.SetString(clean, 0); !ok {
		if _, ok := integer.SetString(clean, 10); !ok {
			return "", false
		}
	}
	return json.Number(integer.String()), true
}

func canonicalYAMLFloat(value string) (string, error) {
	clean := strings.ReplaceAll(strings.ToLower(value), "_", "")
	if strings.Contains(clean, "inf") || strings.Contains(clean, "nan") {
		return "", fmt.Errorf("non-finite YAML float %q is not allowed", value)
	}
	if strings.Count(clean, "e") > 1 {
		return "", fmt.Errorf("invalid YAML float %q", value)
	}
	mantissa, exponentText, hasExponent := strings.Cut(clean, "e")
	exponent := new(big.Int)
	if hasExponent {
		exponentText = strings.TrimPrefix(exponentText, "+")
		if exponentText == "" {
			return "", fmt.Errorf("invalid YAML float %q", value)
		}
		if _, ok := exponent.SetString(exponentText, 10); !ok {
			return "", fmt.Errorf("invalid YAML float %q", value)
		}
	}
	negative := strings.HasPrefix(mantissa, "-")
	mantissa = strings.TrimPrefix(strings.TrimPrefix(mantissa, "+"), "-")
	if strings.Count(mantissa, ".") > 1 {
		return "", fmt.Errorf("invalid YAML float %q", value)
	}
	integerPart, fractionalPart, _ := strings.Cut(mantissa, ".")
	if integerPart == "" && fractionalPart == "" {
		return "", fmt.Errorf("invalid YAML float %q", value)
	}
	for _, digit := range integerPart + fractionalPart {
		if digit < '0' || digit > '9' {
			return "", fmt.Errorf("invalid YAML float %q", value)
		}
	}

	digits := strings.TrimLeft(integerPart+fractionalPart, "0")
	if digits == "" {
		return "0.0e0", nil
	}
	power := new(big.Int).Sub(exponent, big.NewInt(int64(len(fractionalPart))))
	trimmedDigits := strings.TrimRight(digits, "0")
	power.Add(power, big.NewInt(int64(len(digits)-len(trimmedDigits))))
	digits = trimmedDigits
	scientificExponent := new(big.Int).Add(power, big.NewInt(int64(len(digits)-1)))
	fraction := digits[1:]
	if fraction == "" {
		fraction = "0"
	}
	canonical := digits[:1] + "." + fraction + "e" + scientificExponent.String()
	if negative {
		canonical = "-" + canonical
	}
	return canonical, nil
}

func canonicalScalarValue(node *yaml.Node) (string, error) {
	switch node.Tag {
	case "!!str":
		return norm.NFC.String(node.Value), nil
	case "!!bool":
		value, err := strconv.ParseBool(strings.ToLower(node.Value))
		if err != nil {
			return "", fmt.Errorf("invalid YAML boolean %q: %w", node.Value, err)
		}
		return strconv.FormatBool(value), nil
	case "!!int":
		value, ok := yamlIntegerNumber(node.Value)
		if !ok {
			return "", fmt.Errorf("invalid YAML integer %q", node.Value)
		}
		return value.String(), nil
	case "!!float":
		return canonicalYAMLFloat(node.Value)
	case "!!null":
		switch node.Value {
		case "", "~", "null", "Null", "NULL":
		default:
			return "", fmt.Errorf("invalid YAML null value %q", node.Value)
		}
		return "null", nil
	case "!!timestamp":
		var value time.Time
		if err := node.Decode(&value); err != nil {
			return "", fmt.Errorf("invalid YAML timestamp %q: %w", node.Value, err)
		}
		value = value.UTC()
		if value.Year() < 0 || value.Year() > 9999 {
			return "", fmt.Errorf("YAML timestamp %q normalizes outside supported year range 0000..9999", node.Value)
		}
		canonical := value.Format(time.RFC3339Nano)
		if _, err := time.Parse(time.RFC3339Nano, canonical); err != nil {
			return "", fmt.Errorf("canonicalize YAML timestamp %q: %w", node.Value, err)
		}
		return canonical, nil
	case "!!binary":
		compact := strings.Join(strings.Fields(node.Value), "")
		value, err := base64.StdEncoding.DecodeString(compact)
		if err != nil {
			return "", fmt.Errorf("invalid YAML binary value: %w", err)
		}
		return base64.StdEncoding.EncodeToString(value), nil
	default:
		return node.Value, nil
	}
}

func canonicalYAMLNode(node *yaml.Node) (*yaml.Node, error) {
	canonical := &yaml.Node{
		Kind:        node.Kind,
		Style:       0,
		Tag:         node.Tag,
		Value:       node.Value,
		Anchor:      "",
		Alias:       nil,
		Content:     nil,
		HeadComment: "",
		LineComment: "",
		FootComment: "",
		Line:        0,
		Column:      0,
	}
	if node.Kind == yaml.ScalarNode {
		value, err := canonicalScalarValue(node)
		if err != nil {
			return nil, err
		}
		canonical.Value = value
		if node.Tag == "!!str" && value == "<<" {
			canonical.Style = yaml.DoubleQuotedStyle
		}
	}
	canonical.Content = make([]*yaml.Node, len(node.Content))
	for i, child := range node.Content {
		canonicalChild, err := canonicalYAMLNode(child)
		if err != nil {
			return nil, err
		}
		canonical.Content[i] = canonicalChild
	}
	if canonical.Kind == yaml.MappingNode {
		pairs := make([][2]*yaml.Node, 0, len(canonical.Content)/2)
		for i := 0; i < len(canonical.Content); i += 2 {
			pairs = append(pairs, [2]*yaml.Node{canonical.Content[i], canonical.Content[i+1]})
		}
		slices.SortFunc(pairs, func(a, b [2]*yaml.Node) int {
			return strings.Compare(a[0].Value, b[0].Value)
		})
		canonical.Content = canonical.Content[:0]
		for _, pair := range pairs {
			canonical.Content = append(canonical.Content, pair[0], pair[1])
		}
	}
	return canonical, nil
}

type boundedBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		b.exceeded = true
		return 0, errCanonicalDocumentTooLarge
	}
	n, err := b.Buffer.Write(p)
	if err != nil {
		return n, fmt.Errorf("buffer canonical skill manifest: %w", err)
	}
	return n, nil
}
