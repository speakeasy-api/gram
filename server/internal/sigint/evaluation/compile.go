package evaluation

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	conversationv1 "github.com/speakeasy-api/gram/infra/gen/gram/conversation/v1"
	sigintv1 "github.com/speakeasy-api/gram/infra/gen/gram/sigint/v1"
	"github.com/speakeasy-api/gram/server/internal/classifier"
)

// Keep this namespace stable so repeated evaluations retain their reading ID.
var readingNamespace = uuid.NewSHA1(uuid.NameSpaceURL, []byte("gram:sigint:reading"))

type compiledSensor struct {
	id        string
	mode      string
	hash      string
	questions []classifier.Question
}

func compileSensor(sensor Sensor) (compiledSensor, bool) {
	var compiled compiledSensor
	if sensor.Instructions == nil || len(sensor.Signals) == 0 {
		return compiled, false
	}
	instructions := classifier.Text(*sensor.Instructions)
	key := classifier.QuestionKey(sensor.ID)
	compiled.id, compiled.mode = sensor.ID, sensor.Mode
	switch sensor.Mode {
	case "multi_label":
		for _, signal := range sensor.Signals {
			compiled.questions = append(compiled.questions, classifier.Noul(classifier.QuestionKey(sensor.ID+"/"+string(signal.Key)), instructions, classifier.WithPositive(signal.Description)))
		}
	case "exclusive":
		if len(sensor.Signals) > 255 {
			return compiled, false
		}
		compiled.questions = []classifier.Question{classifier.Choice(key, instructions, sensor.Signals...)}
	case "ordered_score":
		if len(sensor.Signals) < 2 || len(sensor.Signals) > 10 {
			return compiled, false
		}
		for _, signal := range sensor.Signals {
			data, _ := json.Marshal(signal.Description)
			if string(data) == "null" {
				return compiled, false
			}
		}
		compiled.questions = []classifier.Question{classifier.Score(key, instructions, sensor.Signals...)}
	default:
		return compiled, false
	}
	// Hash a versioned semantic definition, independent of Go field names and
	// classifier wire keys. Signal identity/order belongs to that definition.
	signals := make([]map[string]any, 0, len(sensor.Signals))
	for _, signal := range sensor.Signals {
		signals = append(signals, map[string]any{"id": signal.Key, "criteria": signal.Description})
	}
	data, _ := json.Marshal(map[string]any{"version": "sigint-definition-v1", "mode": sensor.Mode, "instructions": instructions, "signals": signals})
	compiled.hash = digest(data)
	return compiled, true
}

func reading(m *conversationv1.Message, sensor compiledSensor, answers map[classifier.QuestionKey]classifier.QuestionOutcome, attempt, at string, result classifier.Result) (*sigintv1.Reading, error) {
	for _, q := range sensor.questions {
		outcome, ok := answers[q.Key]
		if !ok || outcome.Answer == nil || outcome.Failure != nil {
			return nil, fmt.Errorf("incomplete sensor evaluation")
		}
	}
	r := &sigintv1.Reading{}
	identity, _ := json.Marshal([]string{"sigint-reading-v1", m.GetOrganizationId(), m.GetProjectId(), m.GetId(), sensor.id})
	r.SetId(uuid.NewSHA1(readingNamespace, identity).String())
	r.SetEvaluationAttemptId(attempt)
	r.SetOrganizationId(m.GetOrganizationId())
	r.SetProjectId(m.GetProjectId())
	r.SetConversationId(m.GetConversationId())
	r.SetMessageId(m.GetId())
	r.SetMessageRole(sigintv1.Reading_MESSAGE_ROLE_USER)
	if m.GetRole() == conversationv1.Message_ROLE_ASSISTANT {
		r.SetMessageRole(sigintv1.Reading_MESSAGE_ROLE_ASSISTANT)
	}
	r.SetSensorId(sensor.id)
	r.SetMessageCreatedAt(m.GetCreatedAt())
	r.SetEvaluatedAt(at)
	r.SetDefinitionHash(sensor.hash)
	r.SetConfiguredModel(result.Metadata.Model)
	r.SetModels(result.Models)
	r.SetCompilerVersion("sigint-v1/" + result.Metadata.CompilerVersion)
	switch sensor.mode {
	case "multi_label":
		value := &sigintv1.Reading_MultiLabel{}
		var signals []*sigintv1.Reading_Probability
		for _, q := range sensor.questions {
			answer := answers[q.Key].Answer.Noul
			if answer == nil {
				return nil, fmt.Errorf("invalid Noul answer")
			}
			p := &sigintv1.Reading_Probability{}
			p.SetSignalId(string(q.Key)[len(sensor.id)+1:])
			p.SetProbability(answer.Probability)
			signals = append(signals, p)
		}
		value.SetSignals(signals)
		r.SetMultiLabel(value)
	case "exclusive":
		answer := answers[sensor.questions[0].Key].Answer.Choice
		if answer == nil {
			return nil, fmt.Errorf("invalid Choice answer")
		}
		value := &sigintv1.Reading_Choice{}
		value.SetSelectedSignalId(string(answer.Selected))
		value.SetDistribution(distribution(answer.Distribution))
		if answer.Confidence != nil {
			value.SetConfidence(*answer.Confidence)
		}
		r.SetChoice(value)
	case "ordered_score":
		answer := answers[sensor.questions[0].Key].Answer.Score
		if answer == nil {
			return nil, fmt.Errorf("invalid Score answer")
		}
		value := &sigintv1.Reading_Score{}
		value.SetExpectedIndex(answer.ExpectedIndex)
		value.SetDistribution(distribution(answer.Distribution))
		if answer.Confidence != nil {
			value.SetConfidence(*answer.Confidence)
		}
		r.SetScore(value)
	}
	return r, nil
}

func distribution(values []classifier.Probability) []*sigintv1.Reading_Probability {
	result := make([]*sigintv1.Reading_Probability, 0, len(values))
	for _, value := range values {
		p := &sigintv1.Reading_Probability{}
		p.SetSignalId(string(value.Option))
		p.SetProbability(value.Value)
		result = append(result, p)
	}
	return result
}
