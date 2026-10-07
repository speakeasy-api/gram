package catalog

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"sync"
)

// customFlowsFile holds the custom flows, beside platforms/ so the platform
// loader never reads it as an entry.
const customFlowsFile = "custom.yaml"

// FormField is a value a custom flow's form collects with an input.
type FormField string

const (
	// FieldName is a trusted platform's name.
	FieldName FormField = "name"

	// FieldDescription is a trusted platform's description.
	FieldDescription FormField = "description"

	// FieldIssuer is a trusted platform's issuer URL.
	FieldIssuer FormField = "issuer"

	// FieldJWKSURI is where a trusted platform publishes its signing keys.
	FieldJWKSURI FormField = "jwks_uri"

	// FieldSubject is the subject, or wildcard rule, access is allowed for.
	FieldSubject FormField = "subject"

	// FieldLabel is an allowed workload's optional label, submitted as its
	// name.
	FieldLabel FormField = "label"
)

// InputFormat names the dashboard validator an input's value must pass. The
// server holds the same rules on the write path; the validator only lets the
// form say what is wrong before submitting.
type InputFormat string

const (
	// FormatIssuerURL is an https URL on a fully qualified domain, with no
	// query or fragment.
	FormatIssuerURL InputFormat = "issuer_url"

	// FormatJWKSURI is an https URL on a fully qualified domain.
	FormatJWKSURI InputFormat = "jwks_uri"

	// FormatPlatformName is a trusted platform's name, within the length the
	// server allows.
	FormatPlatformName InputFormat = "platform_name"

	// FormatPlatformDescription is a trusted platform's description, within the
	// length the server allows.
	FormatPlatformDescription InputFormat = "platform_description"

	// FormatSubjectRule is a subject or a wildcard rule ending in "*", which the
	// issuer must permit.
	FormatSubjectRule InputFormat = "subject_rule"

	// FormatNone accepts any value.
	FormatNone InputFormat = "none"
)

// inputFormats is the format each field's input must declare, so a flow
// cannot pair a value with a validator meant for another.
var inputFormats = map[FormField]InputFormat{
	FieldName:        FormatPlatformName,
	FieldDescription: FormatPlatformDescription,
	FieldIssuer:      FormatIssuerURL,
	FieldJWKSURI:     FormatJWKSURI,
	FieldSubject:     FormatSubjectRule,
	FieldLabel:       FormatNone,
}

// customFlowBlocks are the blocks a custom flow may use.
var customFlowBlocks = []BlockType{
	BlockText, BlockLink, BlockInput, BlockAgentPicker, BlockTags, BlockWildcardCaution,
}

// Form is one custom flow: a single-step form that writes or edits one row.
type Form struct {
	// Title heads the form.
	Title string `yaml:"title"`

	// Description is shown under the title.
	Description string `yaml:"description"`

	// SubmitLabel is the submit button's label.
	SubmitLabel string `yaml:"submit_label"`

	// PendingLabel is the submit button's label while the form submits.
	PendingLabel string `yaml:"pending_label"`

	// Steps holds the form's one step. Its phase is left empty.
	Steps []Step `yaml:"steps"`
}

// CustomFlows are the forms for trusting a platform the catalog does not list
// and allowing its workloads, each validated against the management API form
// it feeds.
type CustomFlows struct {
	// RegisterPlatform trusts a new platform.
	RegisterPlatform Form `yaml:"register_platform"`

	// EditPlatform edits a trusted platform.
	EditPlatform Form `yaml:"edit_platform"`

	// AllowAccess admits a subject under a trusted platform.
	AllowAccess Form `yaml:"allow_access"`

	// EditAccess edits an admitted subject.
	EditAccess Form `yaml:"edit_access"`
}

// fieldRule is how a flow may place one field's input.
type fieldRule string

const (
	// fieldRefused inputs must not be placed: the flow's form has no use for
	// the value.
	fieldRefused fieldRule = "refused"

	// fieldRequired inputs must be placed, and must be editable.
	fieldRequired fieldRule = "required"

	// fieldOptional inputs may be placed, and must be editable.
	fieldOptional fieldRule = "optional"

	// fieldFixed inputs must be placed read-only: the value is shown and never
	// submitted.
	fieldFixed fieldRule = "fixed"
)

// flowContract is what one custom flow may place.
type flowContract struct {
	// fields is how the flow may place each field's input. An editable input's
	// value is submitted, so its field must be one the flow's form accepts.
	fields map[FormField]fieldRule

	// agentPicker requires an agent picker; without it one is refused.
	agentPicker bool

	// wildcardCaution allows a wildcard caution.
	wildcardCaution bool
}

// flowContracts mirror the management API forms the flows feed:
// register_platform submits RegisterWorkloadIssuerForm, edit_platform
// UpdateWorkloadIssuerForm, allow_access AdmitWorkloadSubjectForm and
// edit_access UpdateWorkloadSubjectForm. A value a form does not accept, such
// as the issuer of a platform being edited, can only be shown fixed. Tags, text
// and links are allowed in every flow.
var flowContracts = []struct {
	// key names the flow in the file and in errors.
	key string

	// form picks the flow out of the file.
	form func(CustomFlows) Form

	// contract is what the flow may place.
	contract flowContract
}{
	{
		key:  "register_platform",
		form: func(f CustomFlows) Form { return f.RegisterPlatform },
		contract: flowContract{
			fields: map[FormField]fieldRule{
				FieldName:        fieldRequired,
				FieldDescription: fieldOptional,
				FieldIssuer:      fieldRequired,
				FieldJWKSURI:     fieldRequired,
				FieldSubject:     fieldRefused,
				FieldLabel:       fieldRefused,
			},
			agentPicker:     false,
			wildcardCaution: false,
		},
	},
	{
		key:  "edit_platform",
		form: func(f CustomFlows) Form { return f.EditPlatform },
		contract: flowContract{
			fields: map[FormField]fieldRule{
				FieldName:        fieldRequired,
				FieldDescription: fieldOptional,
				FieldIssuer:      fieldFixed,
				FieldJWKSURI:     fieldRequired,
				FieldSubject:     fieldRefused,
				FieldLabel:       fieldRefused,
			},
			agentPicker:     false,
			wildcardCaution: false,
		},
	},
	{
		key:  "allow_access",
		form: func(f CustomFlows) Form { return f.AllowAccess },
		contract: flowContract{
			fields: map[FormField]fieldRule{
				FieldName:        fieldRefused,
				FieldDescription: fieldRefused,
				FieldIssuer:      fieldRefused,
				FieldJWKSURI:     fieldRefused,
				FieldSubject:     fieldRequired,
				FieldLabel:       fieldOptional,
			},
			agentPicker:     true,
			wildcardCaution: true,
		},
	},
	{
		key:  "edit_access",
		form: func(f CustomFlows) Form { return f.EditAccess },
		contract: flowContract{
			fields: map[FormField]fieldRule{
				FieldName:        fieldRefused,
				FieldDescription: fieldRefused,
				FieldIssuer:      fieldRefused,
				FieldJWKSURI:     fieldRefused,
				FieldSubject:     fieldFixed,
				FieldLabel:       fieldOptional,
			},
			agentPicker:     true,
			wildcardCaution: true,
		},
	},
}

var loadEmbeddedCustomFlows = sync.OnceValues(func() (CustomFlows, error) {
	raw, err := fs.ReadFile(embeddedFiles, customFlowsFile)
	if err != nil {
		return CustomFlows{}, fmt.Errorf("read %s: %w", customFlowsFile, err)
	}
	flows, err := ParseCustomFlows(raw)
	if err != nil {
		return CustomFlows{}, fmt.Errorf("%s: %w", customFlowsFile, err)
	}
	return flows, nil
})

func (embeddedSource) CustomFlows(_ context.Context) (CustomFlows, error) {
	return loadEmbeddedCustomFlows()
}

// ParseCustomFlows decodes and validates the custom flows file. Unknown fields
// are refused, so a misspelled key fails instead of silently dropping its
// value.
func ParseCustomFlows(raw []byte) (CustomFlows, error) {
	flows, err := decodeOne[CustomFlows](raw, "the custom flows file")
	if err != nil {
		return CustomFlows{}, err
	}

	for _, flow := range flowContracts {
		if err := validateForm(flow.form(flows), flow.contract); err != nil {
			return CustomFlows{}, fmt.Errorf("%s: %w", flow.key, err)
		}
	}
	return flows, nil
}

func validateForm(form Form, contract flowContract) error {
	if strings.TrimSpace(form.Title) == "" {
		return errors.New("title is required")
	}
	if strings.TrimSpace(form.Description) == "" {
		return errors.New("description is required")
	}
	if strings.TrimSpace(form.SubmitLabel) == "" {
		return errors.New("submit_label is required")
	}
	if strings.TrimSpace(form.PendingLabel) == "" {
		return errors.New("pending_label is required")
	}
	if len(form.Steps) != 1 {
		return fmt.Errorf("a custom flow has exactly one step, found %d", len(form.Steps))
	}

	step := form.Steps[0]
	if !keyPattern.MatchString(step.ID) {
		return fmt.Errorf("step id %q must be lowercase words joined by hyphens", step.ID)
	}
	if strings.TrimSpace(step.Title) == "" {
		return fmt.Errorf("step %q: title is required", step.ID)
	}
	if step.Phase != "" {
		return fmt.Errorf("step %q: a custom flow's step has no phase", step.ID)
	}

	placed := make(map[FormField]bool, len(contract.fields))
	counts := make(map[BlockType]int, len(customFlowBlocks))
	for i, block := range step.Blocks {
		if err := validateCustomBlock(block, contract, placed); err != nil {
			return fmt.Errorf("step %q block %d: %w", step.ID, i+1, err)
		}
		counts[block.Type]++
	}

	for _, field := range slices.Sorted(maps.Keys(contract.fields)) {
		if rule := contract.fields[field]; (rule == fieldRequired || rule == fieldFixed) && !placed[field] {
			return fmt.Errorf("step %q: input %s is required", step.ID, field)
		}
	}
	for _, blockType := range []BlockType{BlockAgentPicker, BlockTags, BlockWildcardCaution} {
		if counts[blockType] > 1 {
			return fmt.Errorf("step %q: %s is placed more than once", step.ID, blockType)
		}
	}
	if contract.agentPicker && counts[BlockAgentPicker] == 0 {
		return fmt.Errorf("step %q: agent_picker is required", step.ID)
	}
	return nil
}

// validateCustomBlock checks one block against the shared rules and the flow's
// contract, recording each input's field in placed.
func validateCustomBlock(b Block, contract flowContract, placed map[FormField]bool) error {
	if err := validateBlock(b, customFlowBlocks, nil); err != nil {
		return err
	}

	// Refused rather than ignored, so a setting the form never reads cannot look
	// like it took effect.
	if b.Src != "" || b.Alt != "" || b.Caption != "" || b.Variable != "" || b.Value != "" {
		return fmt.Errorf("%s: src, alt, caption, variable and value belong to catalog platforms", b.Type)
	}
	if b.Type != BlockInput && (b.Field != "" || b.Format != "" || b.Multiline || b.ReadOnly) {
		return fmt.Errorf("%s: field, format, multiline and read_only belong to inputs", b.Type)
	}

	// A form control's help is Markdown, and an image in it could load from any
	// host.
	if strings.Contains(b.Help, "![") {
		return fmt.Errorf("%s help must not hold an image", b.Type)
	}

	switch b.Type {
	case BlockInput:
		rule := contract.fields[b.Field]
		if rule == fieldRefused {
			return fmt.Errorf("input %s is not one this flow submits", b.Field)
		}
		if placed[b.Field] {
			return fmt.Errorf("input %s is placed more than once", b.Field)
		}
		placed[b.Field] = true
		if rule == fieldFixed && !b.ReadOnly {
			return fmt.Errorf("input %s must be read_only", b.Field)
		}
		if rule != fieldFixed && b.ReadOnly {
			return fmt.Errorf("input %s must not be read_only, because the form submits it", b.Field)
		}
	case BlockAgentPicker:
		if !contract.agentPicker {
			return errors.New("agent_picker is not allowed in this flow")
		}
		return requireLabel(b)
	case BlockTags:
		return requireLabel(b)
	case BlockWildcardCaution:
		if !contract.wildcardCaution {
			return errors.New("wildcard_caution is not allowed in this flow")
		}
	case BlockText, BlockLink, BlockImage, BlockField, BlockSubjectRule, BlockComputedStatus, BlockComputed, BlockChecklistItem:
	}
	return nil
}

func requireLabel(b Block) error {
	if strings.TrimSpace(b.Label) == "" {
		return fmt.Errorf("%s needs a label", b.Type)
	}
	return nil
}
