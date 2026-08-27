package killswitches

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	canonicalMutationEncodingV1 = "killswitch-mutation-v1"
	maxExternalNoteRunes        = 500
	maxInternalNoteRunes        = 4000

	// noteTrimCutsetV1 is part of the V1 canonical encoding contract. Do not derive it from
	// unicode.IsSpace or strings.TrimSpace: toolchain Unicode tables may change over time.
	noteTrimCutsetV1 = "\t\n\r \u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000"
)

// CanonicalMutationV1 contains only client-supplied fields included in a V1 request hash.
// Activate supplies immutable identity and the full desired mutable payload. Change and
// reactivate supply a target, expected version, and the full desired mutable payload without
// immutable identity. Deactivate supplies only a target and expected version. A nil ExpiresAt
// in a desired payload explicitly means no expiry.
type CanonicalMutationV1 struct {
	Operation            MutationOperation
	PrescriptionID       *PrescriptionID
	Definition           *DefinitionKey
	PrincipalKind        *PrincipalKind
	PrincipalKey         *PrincipalKey
	ResourceKind         *ResourceKind
	ResourceScope        *ResourceScope
	SelectedResourceKeys []ResourceKey
	StartMode            *StartMode
	StartsAt             *time.Time
	ExpiresAt            *time.Time
	ExternalNote         *string
	InternalNote         *string
	ExpectedVersion      *int64
}

type canonicalMutationWireV1 struct {
	EncodingVersion      string            `json:"encoding_version"`
	Operation            MutationOperation `json:"operation"`
	PrescriptionID       *PrescriptionID   `json:"prescription_id"`
	Definition           *DefinitionKey    `json:"definition_key"`
	PrincipalKind        *PrincipalKind    `json:"principal_kind"`
	PrincipalKey         *PrincipalKey     `json:"principal_key"`
	ResourceKind         *ResourceKind     `json:"resource_kind"`
	ResourceScope        *ResourceScope    `json:"resource_scope"`
	SelectedResourceKeys []ResourceKey     `json:"selected_resource_keys"`
	StartMode            *StartMode        `json:"start_mode"`
	StartsAt             *string           `json:"starts_at"`
	ExpiresAt            *string           `json:"expires_at"`
	ExternalNote         *string           `json:"external_note"`
	InternalNote         *string           `json:"internal_note"`
	ExpectedVersion      *int64            `json:"expected_version"`
}

// NormalizeExternalNote trims and validates a present external note.
func NormalizeExternalNote(note string) (string, error) {
	return normalizeNote(note, maxExternalNoteRunes)
}

// NormalizeInternalNote trims and validates a present internal note.
func NormalizeInternalNote(note string) (string, error) {
	return normalizeNote(note, maxInternalNoteRunes)
}

func normalizeNote(note string, limit int) (string, error) {
	if !utf8.ValidString(note) {
		return "", fmt.Errorf("must be valid UTF-8")
	}
	for _, r := range note {
		if isDisallowedNoteControl(r) {
			return "", fmt.Errorf("contains a disallowed control character U+%04X", r)
		}
	}
	normalized := strings.Trim(note, noteTrimCutsetV1)
	length := utf8.RuneCountInString(normalized)
	if length == 0 {
		return "", fmt.Errorf("must not be empty")
	}
	if length > limit {
		return "", fmt.Errorf("must be at most %d characters", limit)
	}
	return normalized, nil
}

func isDisallowedNoteControl(r rune) bool {
	if r == '\t' || r == '\n' || r == '\r' {
		return false
	}
	return r <= '\x1f' || (r >= '\x7f' && r <= '\x9f')
}

func canonicalMutationJSONV1(input CanonicalMutationV1) ([]byte, error) {
	wire, err := canonicalizeMutationV1(input)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("encode canonical mutation V1: %w", err)
	}
	return encoded, nil
}

// CanonicalMutationHashV1 returns the SHA-256 digest of the canonical V1 request fields as
// sha256 followed by a colon and 64 lowercase hexadecimal characters.
func CanonicalMutationHashV1(input CanonicalMutationV1) (string, error) {
	encoded, err := canonicalMutationJSONV1(input)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func canonicalizeMutationV1(input CanonicalMutationV1) (canonicalMutationWireV1, error) {
	if !validMutationOperation(input.Operation) {
		return canonicalMutationWireV1{}, fmt.Errorf("invalid mutation operation %q", input.Operation)
	}

	switch input.Operation {
	case MutationOperationActivate:
		if input.PrescriptionID != nil {
			return canonicalMutationWireV1{}, fmt.Errorf("activate must not target a prescription ID")
		}
		if input.ExpectedVersion != nil {
			return canonicalMutationWireV1{}, fmt.Errorf("activate must not include an expected version")
		}
		if err := validateActivationIdentity(input); err != nil {
			return canonicalMutationWireV1{}, err
		}
		if err := validateDesiredMutablePayload(input); err != nil {
			return canonicalMutationWireV1{}, err
		}
	case MutationOperationChange, MutationOperationReactivate:
		if err := validateExistingTarget(input); err != nil {
			return canonicalMutationWireV1{}, err
		}
		if hasImmutableIdentity(input) {
			return canonicalMutationWireV1{}, fmt.Errorf("%s must not include immutable identity fields", input.Operation)
		}
		if err := validateDesiredMutablePayload(input); err != nil {
			return canonicalMutationWireV1{}, err
		}
	case MutationOperationDeactivate:
		if err := validateExistingTarget(input); err != nil {
			return canonicalMutationWireV1{}, err
		}
		if hasNonTargetFields(input) {
			return canonicalMutationWireV1{}, fmt.Errorf("deactivate must not include fields other than target and expected version")
		}
		prescriptionID, err := canonicalPrescriptionID(input.PrescriptionID)
		if err != nil {
			return canonicalMutationWireV1{}, err
		}
		return canonicalMutationWireV1{
			EncodingVersion:      canonicalMutationEncodingV1,
			Operation:            input.Operation,
			PrescriptionID:       prescriptionID,
			Definition:           nil,
			PrincipalKind:        nil,
			PrincipalKey:         nil,
			ResourceKind:         nil,
			ResourceScope:        nil,
			SelectedResourceKeys: []ResourceKey{},
			StartMode:            nil,
			StartsAt:             nil,
			ExpiresAt:            nil,
			ExternalNote:         nil,
			InternalNote:         nil,
			ExpectedVersion:      input.ExpectedVersion,
		}, nil
	}

	if err := validateOptionalIdentifier("definition key", input.Definition); err != nil {
		return canonicalMutationWireV1{}, err
	}
	if err := validateOptionalIdentifier("principal kind", input.PrincipalKind); err != nil {
		return canonicalMutationWireV1{}, err
	}
	if err := validateOptionalIdentifier("principal key", input.PrincipalKey); err != nil {
		return canonicalMutationWireV1{}, err
	}
	if err := validateOptionalIdentifier("resource kind", input.ResourceKind); err != nil {
		return canonicalMutationWireV1{}, err
	}

	prescriptionID, err := canonicalPrescriptionID(input.PrescriptionID)
	if err != nil {
		return canonicalMutationWireV1{}, err
	}
	selected, err := canonicalSelectedResources(input.ResourceScope, input.SelectedResourceKeys)
	if err != nil {
		return canonicalMutationWireV1{}, err
	}
	startsAt, err := canonicalStart(input.StartMode, input.StartsAt)
	if err != nil {
		return canonicalMutationWireV1{}, err
	}
	if input.StartsAt != nil && input.ExpiresAt != nil && !input.StartsAt.Before(*input.ExpiresAt) {
		return canonicalMutationWireV1{}, fmt.Errorf("expires at must be after starts at")
	}

	var expiresAt *string
	if input.ExpiresAt != nil {
		value := input.ExpiresAt.UTC().Format(time.RFC3339Nano)
		expiresAt = &value
	}
	externalNote, err := normalizeOptionalNote(input.ExternalNote, NormalizeExternalNote)
	if err != nil {
		return canonicalMutationWireV1{}, fmt.Errorf("external note: %w", err)
	}
	internalNote, err := normalizeOptionalNote(input.InternalNote, NormalizeInternalNote)
	if err != nil {
		return canonicalMutationWireV1{}, fmt.Errorf("internal note: %w", err)
	}

	return canonicalMutationWireV1{
		EncodingVersion:      canonicalMutationEncodingV1,
		Operation:            input.Operation,
		PrescriptionID:       prescriptionID,
		Definition:           input.Definition,
		PrincipalKind:        input.PrincipalKind,
		PrincipalKey:         input.PrincipalKey,
		ResourceKind:         input.ResourceKind,
		ResourceScope:        input.ResourceScope,
		SelectedResourceKeys: selected,
		StartMode:            input.StartMode,
		StartsAt:             startsAt,
		ExpiresAt:            expiresAt,
		ExternalNote:         externalNote,
		InternalNote:         internalNote,
		ExpectedVersion:      input.ExpectedVersion,
	}, nil
}

func validateActivationIdentity(input CanonicalMutationV1) error {
	if input.Definition == nil {
		return fmt.Errorf("activate requires a definition key")
	}
	if input.PrincipalKind == nil {
		return fmt.Errorf("activate requires a principal kind")
	}
	if input.PrincipalKey == nil {
		return fmt.Errorf("activate requires a principal key")
	}
	if input.ResourceKind == nil {
		return fmt.Errorf("activate requires a resource kind")
	}
	return nil
}

func validateDesiredMutablePayload(input CanonicalMutationV1) error {
	if input.ResourceScope == nil {
		return fmt.Errorf("%s requires a resource scope", input.Operation)
	}
	if input.StartMode == nil {
		return fmt.Errorf("%s requires a start mode", input.Operation)
	}
	if input.ExternalNote == nil {
		return fmt.Errorf("%s requires an external note", input.Operation)
	}
	if input.InternalNote == nil {
		return fmt.Errorf("%s requires an internal note", input.Operation)
	}
	return nil
}

func validateExistingTarget(input CanonicalMutationV1) error {
	if input.PrescriptionID == nil {
		return fmt.Errorf("%s requires a target prescription ID", input.Operation)
	}
	if input.ExpectedVersion == nil {
		return fmt.Errorf("%s requires an expected version", input.Operation)
	}
	if *input.ExpectedVersion < 1 {
		return fmt.Errorf("expected version must be positive")
	}
	return nil
}

func hasImmutableIdentity(input CanonicalMutationV1) bool {
	return input.Definition != nil || input.PrincipalKind != nil || input.PrincipalKey != nil || input.ResourceKind != nil
}

func hasNonTargetFields(input CanonicalMutationV1) bool {
	return input.Definition != nil ||
		input.PrincipalKind != nil ||
		input.PrincipalKey != nil ||
		input.ResourceKind != nil ||
		input.ResourceScope != nil ||
		len(input.SelectedResourceKeys) != 0 ||
		input.StartMode != nil ||
		input.StartsAt != nil ||
		input.ExpiresAt != nil ||
		input.ExternalNote != nil ||
		input.InternalNote != nil
}

func validateOptionalIdentifier[T ~string](label string, value *T) error {
	if value == nil {
		return nil
	}
	return validateIdentifier(label, string(*value))
}

func canonicalSelectedResources(scope *ResourceScope, keys []ResourceKey) ([]ResourceKey, error) {
	selected := slices.Clone(keys)
	for _, key := range selected {
		if err := validateIdentifier("selected resource key", string(key)); err != nil {
			return nil, err
		}
	}
	slices.Sort(selected)
	selected = slices.Compact(selected)
	if selected == nil {
		selected = []ResourceKey{}
	}

	if scope == nil {
		if len(selected) != 0 {
			return nil, fmt.Errorf("selected resource keys require a resource scope")
		}
		return selected, nil
	}
	switch *scope {
	case ResourceScopeAll:
		if len(selected) != 0 {
			return nil, fmt.Errorf("all resource scope must not include selected resource keys")
		}
	case ResourceScopeSelected:
		if len(selected) == 0 {
			return nil, fmt.Errorf("selected resource scope requires at least one resource key")
		}
	default:
		return nil, fmt.Errorf("invalid resource scope %q", *scope)
	}
	return selected, nil
}

func canonicalStart(mode *StartMode, startsAt *time.Time) (*string, error) {
	if mode == nil {
		if startsAt != nil {
			return nil, fmt.Errorf("start time requires a start mode")
		}
		return nil, nil
	}
	switch *mode {
	case StartModeNow:
		if startsAt != nil {
			return nil, fmt.Errorf("now start mode requires a null start time")
		}
		return nil, nil
	case StartModeAt:
		if startsAt == nil {
			return nil, fmt.Errorf("at start mode requires a start time")
		}
		value := startsAt.UTC().Format(time.RFC3339Nano)
		return &value, nil
	default:
		return nil, fmt.Errorf("invalid start mode %q", *mode)
	}
}

func normalizeOptionalNote(note *string, normalize func(string) (string, error)) (*string, error) {
	if note == nil {
		return nil, nil
	}
	value, err := normalize(*note)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func canonicalPrescriptionID(value *PrescriptionID) (*PrescriptionID, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := uuid.Parse(string(*value))
	if err != nil {
		return nil, fmt.Errorf("prescription ID must be a UUID: %w", err)
	}
	canonical := PrescriptionID(parsed.String())
	return &canonical, nil
}

func validMutationOperation(operation MutationOperation) bool {
	return operation == MutationOperationActivate || operation == MutationOperationChange || operation == MutationOperationDeactivate || operation == MutationOperationReactivate
}
