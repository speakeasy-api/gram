package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/google/uuid"

	sigintv1 "github.com/speakeasy-api/gram/infra/gen/gram/sigint/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/classifier"
)

// sensorRef identifies a sensor whose evaluation ended without a reading.
type sensorRef struct {
	// id is the stable sensor UUID.
	id string

	// slug is the sensor slug observed during evaluation.
	slug string

	// hash is the definition fingerprint, empty for an incomplete draft.
	hash string
}

func (s compiledSensor) ref() sensorRef {
	return sensorRef{id: s.id, slug: s.slug, hash: s.hash}
}

// readingID is the logical identity shared by a sensor's readings and outcomes
// for one event, so storage replaces either with the latest attempt.
func readingID(event Event, sensorID string) string {
	identity, _ := json.Marshal([]string{"sigint-reading-v1", event.OrganizationID, event.ProjectID, event.Subject.GetKind(), event.Subject.GetId(), sensorID})
	return uuid.NewSHA1(readingNamespace, identity).String()
}

// outcome records a terminal decision without an answer, with the same identity
// and attribution a reading for the sensor and event would carry.
func outcome(event Event, sensor sensorRef, attempt, at string, status sigintv1.Outcome_Status, reason string) *sigintv1.Outcome {
	o := &sigintv1.Outcome{}
	o.SetId(readingID(event, sensor.id))
	o.SetEvaluationAttemptId(attempt)
	o.SetOrganizationId(event.OrganizationID)
	o.SetProjectId(event.ProjectID)
	o.SetEvent(outcomeEvent(event.Subject))
	o.SetSensorId(sensor.id)
	o.SetSensorSlug(sensor.slug)
	o.SetEvaluatedAt(at)
	o.SetDefinitionHash(sensor.hash)
	o.SetStatus(status)
	if status == sigintv1.Outcome_STATUS_FAILED {
		o.SetFailureReason(reason)
	}
	o.SetAttributes(maps.Clone(event.Attributes))
	if event.Actor != nil {
		actor := &sigintv1.Outcome_Actor{}
		if event.Actor.HasUserId() {
			actor.SetUserId(event.Actor.GetUserId())
		}
		if event.Actor.HasExternalUserId() {
			actor.SetExternalUserId(event.Actor.GetExternalUserId())
		}
		if event.Actor.HasUserEmail() {
			actor.SetUserEmail(event.Actor.GetUserEmail())
		}
		o.SetActor(actor)
	}
	if event.Account != nil {
		account := &sigintv1.Outcome_Account{}
		if event.Account.HasUserAccountId() {
			account.SetUserAccountId(event.Account.GetUserAccountId())
		}
		if event.Account.HasAccountType() {
			account.SetAccountType(event.Account.GetAccountType())
		}
		if event.Account.HasBillingMode() {
			account.SetBillingMode(event.Account.GetBillingMode())
		}
		o.SetAccount(account)
	}
	if event.BillingUserID != nil {
		o.SetBillingUserId(*event.BillingUserID)
	}
	if event.Source != nil {
		o.SetSource(*event.Source)
	}
	if event.AssistantID != nil {
		o.SetAssistantId(*event.AssistantID)
	}
	if event.Replayed != nil {
		o.SetReplayed(*event.Replayed)
	}
	return o
}

func outcomeEvent(subject *sigintv1.Reading_Event) *sigintv1.Outcome_Event {
	e := &sigintv1.Outcome_Event{}
	e.SetKind(subject.GetKind())
	e.SetId(subject.GetId())
	e.SetOccurredAt(subject.GetOccurredAt())
	if message := subject.GetConversationMessage(); message != nil {
		context := &sigintv1.Outcome_ConversationMessage{}
		if message.HasConversationId() {
			context.SetConversationId(message.GetConversationId())
		}
		if message.HasRole() {
			// The role enums mirror each other value for value.
			context.SetRole(sigintv1.Outcome_ConversationMessage_Role(message.GetRole()))
		}
		e.SetConversationMessage(context)
	}
	if call := subject.GetToolCall(); call != nil {
		context := &sigintv1.Outcome_ToolCall{}
		if call.HasToolName() {
			context.SetToolName(call.GetToolName())
		}
		if call.HasToolCallId() {
			context.SetToolCallId(call.GetToolCallId())
		}
		if call.HasMcpServerId() {
			context.SetMcpServerId(call.GetMcpServerId())
		}
		e.SetToolCall(context)
	}
	return e
}

// withInput records the classifier input supplied before the failure.
func withInput(o *sigintv1.Outcome, sensor compiledSensor, in ResolvedInput) *sigintv1.Outcome {
	supplied := readingInput(sensor, in)
	input := &sigintv1.Outcome_Input{}
	input.SetStrategy(supplied.GetStrategy())
	input.SetRequestedMessageCount(supplied.GetRequestedMessageCount())
	input.SetMessageCount(supplied.GetMessageCount())
	input.SetTruncated(supplied.GetTruncated())
	o.SetInput(input)
	return o
}

// withClassification records the classifier metadata of a failure after inference.
func withClassification(o *sigintv1.Outcome, result classifier.Result) *sigintv1.Outcome {
	o.SetConfiguredModel(result.Metadata.Model)
	o.SetProvider(result.Metadata.Provider)
	o.SetModels(slices.Clone(result.Models))
	o.SetCompilerVersion(compilerVersion(result))
	return o
}

// awaitPublications waits for every publication so the source event is acked
// only after its decisions reached Pub/Sub.
func awaitPublications(ctx context.Context, pending []gcp.PublishResult, what string) error {
	var err error
	for _, publication := range pending {
		if _, publishErr := publication.Get(ctx); publishErr != nil {
			err = errors.Join(err, fmt.Errorf("publish %s: %w", what, publishErr))
		}
	}
	return err
}
