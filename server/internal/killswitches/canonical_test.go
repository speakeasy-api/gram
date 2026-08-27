package killswitches

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

const testPrescriptionID PrescriptionID = "018f1e78-7c4a-7b2c-9d3e-123456789abc"

func TestNormalizeNotes(t *testing.T) {
	t.Parallel()

	invalidUTF8 := string([]byte{0xff})
	tests := []struct {
		name      string
		normalize func(string) (string, error)
		input     string
		want      string
		wantError bool
	}{
		{name: "external trims unicode whitespace", normalize: NormalizeExternalNote, input: "\u2003  paused\t \u2003", want: "paused"},
		{name: "internal preserves whitespace and line endings", normalize: NormalizeInternalNote, input: "  first  line\r\nsecond\tline  ", want: "first  line\r\nsecond\tline"},
		{name: "multibyte rune limit", normalize: NormalizeExternalNote, input: strings.Repeat("界", maxExternalNoteRunes), want: strings.Repeat("界", maxExternalNoteRunes)},
		{name: "external over rune limit", normalize: NormalizeExternalNote, input: strings.Repeat("界", maxExternalNoteRunes+1), wantError: true},
		{name: "internal boundary", normalize: NormalizeInternalNote, input: strings.Repeat("a", maxInternalNoteRunes), want: strings.Repeat("a", maxInternalNoteRunes)},
		{name: "internal over limit", normalize: NormalizeInternalNote, input: strings.Repeat("a", maxInternalNoteRunes+1), wantError: true},
		{name: "empty", normalize: NormalizeExternalNote, input: " \t\r\n", wantError: true},
		{name: "invalid UTF-8", normalize: NormalizeExternalNote, input: invalidUTF8, wantError: true},
		{name: "tab LF CR preserved", normalize: NormalizeExternalNote, input: "a\tb\nc\rd", want: "a\tb\nc\rd"},
		{name: "zero width space is not edge trimmed", normalize: NormalizeExternalNote, input: "\u200bnote\u200b", want: "\u200bnote\u200b"},
		{name: "byte order mark is not edge trimmed", normalize: NormalizeExternalNote, input: "\ufeffnote\ufeff", want: "\ufeffnote\ufeff"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.normalize(tt.input)
			if tt.wantError {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalize: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeNotesRejectsC0AndC1ControlsBeforeTrimming(t *testing.T) {
	t.Parallel()

	for _, control := range []rune{'\x00', '\x08', '\x0b', '\x0c', '\x1f', '\x7f', '\u0080', '\u0085', '\u009f'} {
		for _, input := range []string{string(control) + "note", "note" + string(control), "a" + string(control) + "b"} {
			t.Run(fmt.Sprintf("U+%04X/%q", control, input), func(t *testing.T) {
				t.Parallel()
				if _, err := NormalizeExternalNote(input); err == nil {
					t.Fatal("expected disallowed control error")
				}
			})
		}
	}
}

func TestNormalizeNotesV1TrimSetIsFrozen(t *testing.T) {
	t.Parallel()

	const expectedTrimCutsetV1 = "\t\n\r \u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000"
	if noteTrimCutsetV1 != expectedTrimCutsetV1 {
		t.Fatalf("V1 trim cutset changed: %q", noteTrimCutsetV1)
	}

	seen := map[rune]struct{}{}
	for _, trimRune := range noteTrimCutsetV1 {
		if _, duplicate := seen[trimRune]; duplicate {
			t.Fatalf("duplicate trim rune U+%04X", trimRune)
		}
		seen[trimRune] = struct{}{}
		got, err := NormalizeExternalNote(string(trimRune) + "note" + string(trimRune))
		if err != nil {
			t.Fatalf("trim U+%04X: %v", trimRune, err)
		}
		if got != "note" {
			t.Fatalf("trim U+%04X got %q", trimRune, got)
		}
	}
}

func TestCanonicalMutationGoldenVectorsV1(t *testing.T) {
	t.Parallel()

	prescriptionID := PrescriptionID("018F1E78-7C4A-7B2C-9D3E-123456789ABC")
	version7, version8, version9 := int64(7), int64(8), int64(9)
	scopeSelected := ResourceScopeSelected
	startAt := StartModeAt
	startsAt := mustParseTime(t, "2026-03-01T05:15:30.123456789-05:00")
	expiresAt := mustParseTime(t, "2026-03-02T12:16:31+01:00")
	updatedExternal, updatedInternal := "  Updated. ", ` change
request `
	reactivationExternal, reactivationInternal := " Paused — contact support. ", " operator request "
	unicodeNotes := baseActivationV1()
	unicodeExternal, unicodeInternal := "\u3000暂停 — contactez-nous.\u00a0", "\u2003操作\tone\n二\rtres\u202f"
	unicodeNotes.ExternalNote, unicodeNotes.InternalNote = &unicodeExternal, &unicodeInternal

	change := baseExistingMutationV1(MutationOperationChange)
	change.PrescriptionID, change.ExpectedVersion = &prescriptionID, &version7
	change.ResourceScope = &scopeSelected
	change.SelectedResourceKeys = []ResourceKey{"tool:b", "tool:a", "tool:b"}
	change.StartMode, change.StartsAt = &startAt, &startsAt
	change.ExternalNote, change.InternalNote = &updatedExternal, &updatedInternal

	reactivate := baseExistingMutationV1(MutationOperationReactivate)
	reactivate.PrescriptionID, reactivate.ExpectedVersion = &prescriptionID, &version9
	reactivate.ExpiresAt = &expiresAt
	reactivate.ExternalNote, reactivate.InternalNote = &reactivationExternal, &reactivationInternal

	tests := []struct {
		name     string
		input    CanonicalMutationV1
		wantJSON string
		wantHash string
	}{
		{
			name: "activate", input: baseActivationV1(),
			wantJSON: `{"encoding_version":"killswitch-mutation-v1","operation":"activate","prescription_id":null,"definition_key":"block-tools","principal_kind":"user","principal_key":"user:alpha","resource_kind":"tool","resource_scope":"all","selected_resource_keys":[],"start_mode":"now","starts_at":null,"expires_at":null,"external_note":"Access paused.","internal_note":"operator request","expected_version":null}`,
			wantHash: "sha256:9de637b150d7167da07ad87867cba5ae64ec13d5ad97703ef0c808c39b05409d",
		},
		{
			name: "activate frozen unicode notes", input: unicodeNotes,
			wantJSON: `{"encoding_version":"killswitch-mutation-v1","operation":"activate","prescription_id":null,"definition_key":"block-tools","principal_kind":"user","principal_key":"user:alpha","resource_kind":"tool","resource_scope":"all","selected_resource_keys":[],"start_mode":"now","starts_at":null,"expires_at":null,"external_note":"暂停 — contactez-nous.","internal_note":"操作\tone\n二\rtres","expected_version":null}`,
			wantHash: "sha256:fb4bf0dd3c466d203eff38f3e4994dd4c83bbf3a4ed0077c034b0ca5e9e5a552",
		},
		{
			name: "change", input: change,
			wantJSON: `{"encoding_version":"killswitch-mutation-v1","operation":"change","prescription_id":"018f1e78-7c4a-7b2c-9d3e-123456789abc","definition_key":null,"principal_kind":null,"principal_key":null,"resource_kind":null,"resource_scope":"selected","selected_resource_keys":["tool:a","tool:b"],"start_mode":"at","starts_at":"2026-03-01T10:15:30.123456789Z","expires_at":null,"external_note":"Updated.","internal_note":"change\nrequest","expected_version":7}`,
			wantHash: "sha256:94311b264e32116085420ef8f86816499a2d1f36c79f2ea73ea72215de14aa0d",
		},
		{
			name: "deactivate", input: CanonicalMutationV1{Operation: MutationOperationDeactivate, PrescriptionID: &prescriptionID, ExpectedVersion: &version8},
			wantJSON: `{"encoding_version":"killswitch-mutation-v1","operation":"deactivate","prescription_id":"018f1e78-7c4a-7b2c-9d3e-123456789abc","definition_key":null,"principal_kind":null,"principal_key":null,"resource_kind":null,"resource_scope":null,"selected_resource_keys":[],"start_mode":null,"starts_at":null,"expires_at":null,"external_note":null,"internal_note":null,"expected_version":8}`,
			wantHash: "sha256:784adf9cee2d4140fba610a8bec3d886cdf133c59e7b5feb15891ca96e8a3222",
		},
		{
			name: "reactivate", input: reactivate,
			wantJSON: `{"encoding_version":"killswitch-mutation-v1","operation":"reactivate","prescription_id":"018f1e78-7c4a-7b2c-9d3e-123456789abc","definition_key":null,"principal_kind":null,"principal_key":null,"resource_kind":null,"resource_scope":"all","selected_resource_keys":[],"start_mode":"now","starts_at":null,"expires_at":"2026-03-02T11:16:31Z","external_note":"Paused — contact support.","internal_note":"operator request","expected_version":9}`,
			wantHash: "sha256:7ddc06a135715966424f1da565546157301a42fe5e46c264a5452361dcfa4732",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			encoded, err := canonicalMutationJSONV1(tt.input)
			if err != nil {
				t.Fatalf("canonical JSON V1: %v", err)
			}
			if string(encoded) != tt.wantJSON {
				t.Fatalf("canonical JSON mismatch\n got: %s\nwant: %s", encoded, tt.wantJSON)
			}
			hash, err := CanonicalMutationHashV1(tt.input)
			if err != nil {
				t.Fatalf("canonical hash V1: %v", err)
			}
			if hash != tt.wantHash {
				t.Fatalf("hash got %q, want %q", hash, tt.wantHash)
			}
		})
	}
}

func TestCanonicalMutationExistingOperationsRequireRequestPayloadWithoutStoredIdentityV1(t *testing.T) {
	t.Parallel()

	for _, operation := range []MutationOperation{MutationOperationChange, MutationOperationReactivate} {
		t.Run(string(operation), func(t *testing.T) {
			t.Parallel()
			input := baseExistingMutationV1(operation)
			targetOnly := CanonicalMutationV1{Operation: operation, PrescriptionID: input.PrescriptionID, ExpectedVersion: input.ExpectedVersion}
			if _, err := canonicalMutationJSONV1(targetOnly); err == nil {
				t.Fatal("target and expected version accepted without full desired payload")
			}
		})
	}
}

func TestCanonicalMutationEquivalenceAndPreservationV1(t *testing.T) {
	t.Parallel()

	scope := ResourceScopeSelected
	mode := StartModeAt
	noteA, noteB := "\u2003Access paused. ", "Access paused."
	internalCRLF, internalLF := "one\r\ntwo", "one\ntwo"
	composed, decomposed := "café", "cafe\u0301"
	timeA := mustParseTime(t, "2026-03-01T10:15:30Z")
	timeB := mustParseTime(t, "2026-03-01T05:15:30-05:00")

	first := baseExistingMutationV1(MutationOperationChange)
	first.ResourceScope = &scope
	first.SelectedResourceKeys = []ResourceKey{"tool:b", "tool:a", "tool:b"}
	first.StartMode, first.StartsAt, first.ExternalNote = &mode, &timeA, &noteA
	second := first
	second.SelectedResourceKeys = []ResourceKey{"tool:a", "tool:b"}
	second.StartsAt, second.ExternalNote = &timeB, &noteB
	assertSameHashV1(t, first, second)

	first.InternalNote, second.InternalNote = &internalCRLF, &internalLF
	assertDifferentHashV1(t, first, second, "line endings must be preserved")

	first.InternalNote, second.InternalNote = new("operator request"), new("operator request")
	first.ExternalNote, second.ExternalNote = &composed, &decomposed
	assertDifferentHashV1(t, first, second, "Unicode normalization must not be applied")

	originalKeys := []ResourceKey{"tool:b", "tool:a", "tool:b"}
	first.SelectedResourceKeys = originalKeys
	if _, err := canonicalMutationJSONV1(first); err != nil {
		t.Fatalf("canonical JSON V1: %v", err)
	}
	if strings.Join([]string{string(originalKeys[0]), string(originalKeys[1]), string(originalKeys[2])}, ",") != "tool:b,tool:a,tool:b" {
		t.Fatal("canonicalization mutated caller-owned resource keys")
	}
}

func TestCanonicalMutationValidationV1(t *testing.T) {
	t.Parallel()

	now := mustParseTime(t, "2026-03-01T10:00:00Z")
	later := mustParseTime(t, "2026-03-01T11:00:00Z")
	scopeSelected := ResourceScopeSelected
	startAt := StartModeAt

	tests := []struct {
		name   string
		base   func() CanonicalMutationV1
		mutate func(*CanonicalMutationV1)
	}{
		{name: "unknown operation", base: baseActivationV1, mutate: func(m *CanonicalMutationV1) { m.Operation = "unknown" }},
		{name: "activate target", base: baseActivationV1, mutate: func(m *CanonicalMutationV1) { m.PrescriptionID = new(testPrescriptionID) }},
		{name: "activate expected version", base: baseActivationV1, mutate: func(m *CanonicalMutationV1) { m.ExpectedVersion = new(int64(1)) }},
		{name: "activate missing definition", base: baseActivationV1, mutate: func(m *CanonicalMutationV1) { m.Definition = nil }},
		{name: "activate missing principal kind", base: baseActivationV1, mutate: func(m *CanonicalMutationV1) { m.PrincipalKind = nil }},
		{name: "activate missing principal key", base: baseActivationV1, mutate: func(m *CanonicalMutationV1) { m.PrincipalKey = nil }},
		{name: "activate missing resource kind", base: baseActivationV1, mutate: func(m *CanonicalMutationV1) { m.ResourceKind = nil }},
		{name: "activate missing scope", base: baseActivationV1, mutate: func(m *CanonicalMutationV1) { m.ResourceScope = nil }},
		{name: "activate missing start mode", base: baseActivationV1, mutate: func(m *CanonicalMutationV1) { m.StartMode = nil }},
		{name: "activate missing external note", base: baseActivationV1, mutate: func(m *CanonicalMutationV1) { m.ExternalNote = nil }},
		{name: "activate missing internal note", base: baseActivationV1, mutate: func(m *CanonicalMutationV1) { m.InternalNote = nil }},
		{name: "activate blank definition", base: baseActivationV1, mutate: func(m *CanonicalMutationV1) { m.Definition = new(DefinitionKey(" ")) }},
		{name: "change missing target", base: baseChangeV1, mutate: func(m *CanonicalMutationV1) { m.PrescriptionID = nil }},
		{name: "change invalid target UUID", base: baseChangeV1, mutate: func(m *CanonicalMutationV1) { m.PrescriptionID = new(PrescriptionID("not-a-uuid")) }},
		{name: "change missing expected version", base: baseChangeV1, mutate: func(m *CanonicalMutationV1) { m.ExpectedVersion = nil }},
		{name: "change nonpositive expected version", base: baseChangeV1, mutate: func(m *CanonicalMutationV1) { m.ExpectedVersion = new(int64(0)) }},
		{name: "change definition", base: baseChangeV1, mutate: func(m *CanonicalMutationV1) { m.Definition = new(DefinitionKey("block-tools")) }},
		{name: "change principal kind", base: baseChangeV1, mutate: func(m *CanonicalMutationV1) { m.PrincipalKind = new(PrincipalKind("user")) }},
		{name: "change principal key", base: baseChangeV1, mutate: func(m *CanonicalMutationV1) { m.PrincipalKey = new(PrincipalKey("user:alpha")) }},
		{name: "change resource kind", base: baseChangeV1, mutate: func(m *CanonicalMutationV1) { m.ResourceKind = new(ResourceKind("tool")) }},
		{name: "change missing scope", base: baseChangeV1, mutate: func(m *CanonicalMutationV1) { m.ResourceScope = nil }},
		{name: "change invalid scope", base: baseChangeV1, mutate: func(m *CanonicalMutationV1) { m.ResourceScope = new(ResourceScope("invalid")) }},
		{name: "all scope with selected keys", base: baseChangeV1, mutate: func(m *CanonicalMutationV1) { m.SelectedResourceKeys = []ResourceKey{"tool:a"} }},
		{name: "selected scope without keys", base: baseChangeV1, mutate: func(m *CanonicalMutationV1) { m.ResourceScope = &scopeSelected }},
		{name: "blank selected resource key", base: baseChangeV1, mutate: func(m *CanonicalMutationV1) {
			m.ResourceScope = &scopeSelected
			m.SelectedResourceKeys = []ResourceKey{" "}
		}},
		{name: "change missing start mode", base: baseChangeV1, mutate: func(m *CanonicalMutationV1) { m.StartMode = nil }},
		{name: "now with timestamp", base: baseChangeV1, mutate: func(m *CanonicalMutationV1) { m.StartsAt = &now }},
		{name: "at without timestamp", base: baseChangeV1, mutate: func(m *CanonicalMutationV1) { m.StartMode = &startAt }},
		{name: "invalid start mode", base: baseChangeV1, mutate: func(m *CanonicalMutationV1) { m.StartMode = new(StartMode("later")) }},
		{name: "expiry equal to start", base: baseChangeV1, mutate: func(m *CanonicalMutationV1) { m.StartMode = &startAt; m.StartsAt = &now; m.ExpiresAt = &now }},
		{name: "expiry before start", base: baseChangeV1, mutate: func(m *CanonicalMutationV1) { m.StartMode = &startAt; m.StartsAt = &later; m.ExpiresAt = &now }},
		{name: "missing external note", base: baseChangeV1, mutate: func(m *CanonicalMutationV1) { m.ExternalNote = nil }},
		{name: "blank external note", base: baseChangeV1, mutate: func(m *CanonicalMutationV1) { m.ExternalNote = new(" ") }},
		{name: "missing internal note", base: baseChangeV1, mutate: func(m *CanonicalMutationV1) { m.InternalNote = nil }},
		{name: "blank internal note", base: baseChangeV1, mutate: func(m *CanonicalMutationV1) { m.InternalNote = new(" ") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := tt.base()
			tt.mutate(&input)
			if _, err := canonicalMutationJSONV1(input); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestDeactivateRejectsEveryOtherFieldV1(t *testing.T) {
	t.Parallel()

	now := time.Now()
	tests := []struct {
		name   string
		mutate func(*CanonicalMutationV1)
	}{
		{name: "definition", mutate: func(m *CanonicalMutationV1) { m.Definition = new(DefinitionKey("block-tools")) }},
		{name: "principal kind", mutate: func(m *CanonicalMutationV1) { m.PrincipalKind = new(PrincipalKind("user")) }},
		{name: "principal key", mutate: func(m *CanonicalMutationV1) { m.PrincipalKey = new(PrincipalKey("user:alpha")) }},
		{name: "resource kind", mutate: func(m *CanonicalMutationV1) { m.ResourceKind = new(ResourceKind("tool")) }},
		{name: "resource scope", mutate: func(m *CanonicalMutationV1) { m.ResourceScope = new(ResourceScopeAll) }},
		{name: "selected resources", mutate: func(m *CanonicalMutationV1) { m.SelectedResourceKeys = []ResourceKey{"tool:a"} }},
		{name: "start mode", mutate: func(m *CanonicalMutationV1) { m.StartMode = new(StartModeNow) }},
		{name: "starts at", mutate: func(m *CanonicalMutationV1) { m.StartsAt = &now }},
		{name: "expires at", mutate: func(m *CanonicalMutationV1) { m.ExpiresAt = &now }},
		{name: "external note", mutate: func(m *CanonicalMutationV1) { m.ExternalNote = new("note") }},
		{name: "internal note", mutate: func(m *CanonicalMutationV1) { m.InternalNote = new("note") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := baseDeactivationV1()
			tt.mutate(&input)
			if _, err := canonicalMutationJSONV1(input); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func baseActivationV1() CanonicalMutationV1 {
	definition := DefinitionKey("block-tools")
	principalKind := PrincipalKind("user")
	principalKey := PrincipalKey("user:alpha")
	resourceKind := ResourceKind("tool")
	scope := ResourceScopeAll
	startMode := StartModeNow
	external := "Access paused."
	internal := "operator request"
	return CanonicalMutationV1{
		Operation: MutationOperationActivate, Definition: &definition, PrincipalKind: &principalKind, PrincipalKey: &principalKey,
		ResourceKind: &resourceKind, ResourceScope: &scope, StartMode: &startMode, ExternalNote: &external, InternalNote: &internal,
	}
}

func baseExistingMutationV1(operation MutationOperation) CanonicalMutationV1 {
	input := baseDeactivationV1()
	input.Operation = operation
	input.ResourceScope = new(ResourceScopeAll)
	input.StartMode = new(StartModeNow)
	input.ExternalNote = new("Access paused.")
	input.InternalNote = new("operator request")
	return input
}

func baseChangeV1() CanonicalMutationV1 {
	return baseExistingMutationV1(MutationOperationChange)
}

func baseDeactivationV1() CanonicalMutationV1 {
	return CanonicalMutationV1{
		Operation:       MutationOperationDeactivate,
		PrescriptionID:  new(testPrescriptionID),
		ExpectedVersion: new(int64(1)),
	}
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatalf("parse time: %v", err)
	}
	return parsed
}

//go:fix inline
func ptr[T any](value T) *T { return new(value) }

func assertSameHashV1(t *testing.T, first, second CanonicalMutationV1) {
	t.Helper()
	firstHash, err := CanonicalMutationHashV1(first)
	if err != nil {
		t.Fatalf("first hash: %v", err)
	}
	secondHash, err := CanonicalMutationHashV1(second)
	if err != nil {
		t.Fatalf("second hash: %v", err)
	}
	if firstHash != secondHash {
		t.Fatalf("hashes differ: %s != %s", firstHash, secondHash)
	}
}

func assertDifferentHashV1(t *testing.T, first, second CanonicalMutationV1, message string) {
	t.Helper()
	firstHash, err := CanonicalMutationHashV1(first)
	if err != nil {
		t.Fatalf("first hash: %v", err)
	}
	secondHash, err := CanonicalMutationHashV1(second)
	if err != nil {
		t.Fatalf("second hash: %v", err)
	}
	if firstHash == secondHash {
		t.Fatal(message)
	}
}
