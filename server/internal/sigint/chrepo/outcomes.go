package chrepo

import (
	"fmt"
	"slices"
	"time"

	"google.golang.org/protobuf/proto"

	sigintv1 "github.com/speakeasy-api/gram/infra/gen/gram/sigint/v1"
)

// ParseOutcome validates a sensor decision without an answer. It shares the
// reading's identity and replacement key, so the latest attempt wins whether
// it is a reading or an outcome. Result columns stay empty. Labels outside the
// storage budget are dropped rather than the outcome, because coverage needs
// the decision more than its labels.
func ParseOutcome(m *sigintv1.Outcome, insertedAt time.Time) (ReadingRow, error) {
	if m == nil {
		return invalid("missing outcome")
	}
	envelope, err := readingEnvelope(m)
	if err != nil {
		return invalid("malformed outcome context")
	}
	row, err := parseAttempt(envelope, insertedAt, false)
	if err != nil {
		return row, err
	}
	reason := m.GetFailureReason()
	switch m.GetStatus() {
	case sigintv1.Outcome_STATUS_NOT_APPLICABLE:
		row.Status = StatusNotApplicable
	case sigintv1.Outcome_STATUS_DRAFT:
		row.Status = StatusDraft
	case sigintv1.Outcome_STATUS_FAILED:
		row.Status = StatusFailed
		if !validFailureReason(reason) {
			return invalid("invalid failure reason")
		}
		row.FailureReason = reason
	default:
		return invalid("unknown outcome status")
	}
	if row.Status != StatusFailed && reason != "" {
		return invalid("failure reason without failure")
	}
	return row, nil
}

// validFailureReason accepts bounded snake_case codes, never free-form text.
func validFailureReason(reason string) bool {
	if reason == "" || len(reason) > maxFailureReasonBytes || reason[0] < 'a' || reason[0] > 'z' {
		return false
	}
	for _, c := range reason {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}

// readingEnvelope maps an outcome onto the reading fields storage validates.
// The nested context messages mirror Reading's, field number for field number,
// so their wire encoding converts without loss.
func readingEnvelope(m *sigintv1.Outcome) (*sigintv1.Reading, error) {
	r := &sigintv1.Reading{}
	r.SetId(m.GetId())
	r.SetEvaluationAttemptId(m.GetEvaluationAttemptId())
	r.SetOrganizationId(m.GetOrganizationId())
	r.SetProjectId(m.GetProjectId())
	r.SetTriggerKind(m.GetTriggerKind())
	r.SetEvaluationRequestedAt(m.GetEvaluationRequestedAt())
	if m.HasRunId() {
		r.SetRunId(m.GetRunId())
	}
	r.SetSensorId(m.GetSensorId())
	r.SetSensorSlug(m.GetSensorSlug())
	r.SetEvaluatedAt(m.GetEvaluatedAt())
	r.SetDefinitionHash(m.GetDefinitionHash())
	r.SetConfiguredModel(m.GetConfiguredModel())
	r.SetModels(slices.Clone(m.GetModels()))
	r.SetCompilerVersion(m.GetCompilerVersion())
	r.SetProvider(m.GetProvider())
	r.SetBillingUserId(m.GetBillingUserId())
	r.SetSource(m.GetSource())
	r.SetAssistantId(m.GetAssistantId())
	if AttributesWithinLimits(m.GetAttributes()) {
		r.SetAttributes(m.GetAttributes())
	}
	if m.HasReplayed() {
		r.SetReplayed(m.GetReplayed())
	}
	if m.HasEvent() {
		event := &sigintv1.Reading_Event{}
		if err := mirror(m.GetEvent(), event); err != nil {
			return nil, err
		}
		r.SetEvent(event)
	}
	if m.HasActor() {
		actor := &sigintv1.Reading_Actor{}
		if err := mirror(m.GetActor(), actor); err != nil {
			return nil, err
		}
		r.SetActor(actor)
	}
	if m.HasAccount() {
		account := &sigintv1.Reading_Account{}
		if err := mirror(m.GetAccount(), account); err != nil {
			return nil, err
		}
		r.SetAccount(account)
	}
	if m.HasInput() {
		input := &sigintv1.Reading_Input{}
		if err := mirror(m.GetInput(), input); err != nil {
			return nil, err
		}
		r.SetInput(input)
	}
	return r, nil
}

func mirror(from, to proto.Message) error {
	data, err := proto.Marshal(from)
	if err != nil {
		return fmt.Errorf("encode outcome context: %w", err)
	}
	if err := proto.Unmarshal(data, to); err != nil {
		return fmt.Errorf("decode outcome context: %w", err)
	}
	return nil
}
