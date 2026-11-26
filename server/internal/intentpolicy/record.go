package intentpolicy

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"

	intentpolicyv1 "github.com/speakeasy-api/gram/infra/gen/gram/intentpolicy/v1"
)

// TriggerDashboard marks analyses requested from the dashboard analysis page.
const TriggerDashboard = "dashboard"

// assessmentNamespace scopes logical assessment identities.
var assessmentNamespace = uuid.MustParse("6f3d8f9e-2c41-4f7a-9a55-0d1b7c4e8a21")

// RecordContext attributes the assessments of one analysis request.
type RecordContext struct {
	// AnalysisID is shared by every assessment one request produced.
	AnalysisID uuid.UUID

	// OrganizationID owns the project.
	OrganizationID string

	// ProjectID owns the chat.
	ProjectID uuid.UUID

	// UserID is the person who ran the analysis.
	UserID string

	// UserEmail is that person's email, when known.
	UserEmail string

	// Generation is the chat generation the calls were read from.
	Generation int32

	// TriggerKind says what started the analysis.
	TriggerKind string

	// RequestedAt is when the analysis request began.
	RequestedAt time.Time

	// EvaluatedAt is when the assessments were produced.
	EvaluatedAt time.Time
}

// AssessmentID is the logical identity of one call's assessment under one
// guardrail version and evaluator model. The carrying message keeps calls
// distinct when a provider reuses or omits tool-call ids.
func AssessmentID(chatID uuid.UUID, generation int32, messageID uuid.UUID, toolCallID, guardrailVersion, model string) uuid.UUID {
	key := chatID.String() + "\x00" + strconv.FormatInt(int64(generation), 10) + "\x00" + messageID.String() + "\x00" + toolCallID + "\x00" + guardrailVersion + "\x00" + model
	return uuid.NewSHA1(assessmentNamespace, []byte(key))
}

// Record projects one assessment into its published form. Arguments are
// reduced to their size.
func Record(rc RecordContext, g Guardrail, a Assessment) *intentpolicyv1.Assessment {
	action := a.Action

	call := intentpolicyv1.Assessment_ToolCall_builder{
		ChatId:         new(action.ChatID.String()),
		Generation:     new(rc.Generation),
		MessageId:      new(action.MessageID.String()),
		Sequence:       new(action.Seq),
		ToolCallId:     new(action.ToolCallID),
		ToolName:       new(action.Tool.Name),
		McpServer:      new(action.Tool.MCPServer),
		McpFunction:    new(action.Tool.MCPFunction),
		ArgumentsBytes: new(int64(len(action.Arguments))),
		OccurredAt:     new(formatTime(action.OccurredAt)),
	}.Build()

	actor := intentpolicyv1.Assessment_Actor_builder{UserId: new(rc.UserID)}.Build()
	if rc.UserEmail != "" {
		actor.SetUserEmail(rc.UserEmail)
	}

	contracts := make([]*intentpolicyv1.Assessment_ContractOutcome, 0, len(a.Outcomes))
	for _, o := range a.Outcomes {
		c := intentpolicyv1.Assessment_ContractOutcome_builder{
			ContractId: new(o.ContractID),
			Result:     new(string(o.Result)),
			Inputs:     encodeInputs(o.Inputs),
		}.Build()
		if o.Error != "" {
			c.SetError(o.Error)
		}
		contracts = append(contracts, c)
	}

	kinds := make(map[string]Kind, len(g.Questions))
	for _, q := range g.Questions {
		kinds[q.Key] = q.Kind
	}
	labels := make([]*intentpolicyv1.Assessment_Label, 0, len(a.Labels))
	for _, key := range slices.Sorted(maps.Keys(a.Labels)) {
		labels = append(labels, recordLabel(key, kinds[key], a.Labels[key]))
	}

	r := intentpolicyv1.Assessment_builder{
		Id:             new(AssessmentID(action.ChatID, rc.Generation, action.MessageID, action.ToolCallID, g.Version, a.Descriptor.Model).String()),
		AnalysisId:     new(rc.AnalysisID.String()),
		OrganizationId: new(rc.OrganizationID),
		ProjectId:      new(rc.ProjectID.String()),
		Actor:          actor,
		ToolCall:       call,
		Guardrail:      intentpolicyv1.Assessment_Guardrail_builder{Id: new(g.ID), Version: new(g.Version)}.Build(),
		Evaluator: intentpolicyv1.Assessment_Evaluator_builder{
			Kind:          new(a.Descriptor.Evaluator),
			Provider:      new(a.Descriptor.Provider),
			Model:         new(a.Descriptor.Model),
			PromptVersion: new(a.Descriptor.PromptVersion),
		}.Build(),
		Decision:  new(string(a.Decision)),
		DecidedBy: slices.Clone(a.DecidedBy),
		Contracts: contracts,
		Labels:    labels,
		Usage: intentpolicyv1.Assessment_Usage_builder{
			InputTokens:  new(a.Usage.InputTokens),
			OutputTokens: new(a.Usage.OutputTokens),
			CostUsd:      new(a.Usage.CostUSD),
			LatencyMs:    new(a.Usage.LatencyMs),
		}.Build(),
		TriggerKind:         new(rc.TriggerKind),
		AnalysisRequestedAt: new(formatTime(rc.RequestedAt)),
		EvaluatedAt:         new(formatTime(rc.EvaluatedAt)),
	}.Build()
	if a.EvaluatorError != "" {
		r.SetEvaluatorError(a.EvaluatorError)
	}
	return r
}

func recordLabel(key string, kind Kind, v LabelValue) *intentpolicyv1.Assessment_Label {
	l := intentpolicyv1.Assessment_Label_builder{
		Key:          new(key),
		Rank:         new(int32(max(min(v.Rank, math.MaxInt32), math.MinInt32))), // #nosec G115 -- clamped
		Probability:  new(v.Probability),
		Distribution: maps.Clone(v.Distribution),
	}.Build()
	if v.Rationale != "" {
		l.SetRationale(v.Rationale)
	}
	switch value := v.Value.(type) {
	case bool:
		l.SetKind(intentpolicyv1.Assessment_Label_KIND_NOUL)
		l.SetBoolValue(value)
	case string:
		l.SetKind(intentpolicyv1.Assessment_Label_KIND_CHOICE)
		l.SetChoiceValue(value)
	case []string:
		l.SetKind(intentpolicyv1.Assessment_Label_KIND_MULTI_NOUL)
		l.SetMembers(slices.Clone(value))
	default:
		l.SetKind(labelKind(kind))
	}
	return l
}

func labelKind(k Kind) intentpolicyv1.Assessment_Label_Kind {
	switch k {
	case KindNoul:
		return intentpolicyv1.Assessment_Label_KIND_NOUL
	case KindChoice:
		return intentpolicyv1.Assessment_Label_KIND_CHOICE
	case KindMultiNoul:
		return intentpolicyv1.Assessment_Label_KIND_MULTI_NOUL
	default:
		return intentpolicyv1.Assessment_Label_KIND_UNSPECIFIED
	}
}

func encodeInputs(inputs map[string]any) map[string]string {
	if len(inputs) == 0 {
		return nil
	}
	out := make(map[string]string, len(inputs))
	for k, v := range inputs {
		b, err := json.Marshal(v)
		if err != nil {
			out[k] = strconv.Quote(fmt.Sprint(v))
			continue
		}
		out[k] = string(b)
	}
	return out
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
