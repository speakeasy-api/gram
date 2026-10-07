package evaluation

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	sigintv1 "github.com/speakeasy-api/gram/infra/gen/gram/sigint/v1"
	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/speakeasy-api/gram/server/internal/sigint/matching"
)

// Keep this namespace stable so repeated evaluations retain their reading ID.
var readingNamespace = uuid.NewSHA1(uuid.NameSpaceURL, []byte("gram:sigint:reading"))

type compiledSensor struct {
	id          string
	slug        string
	signalSlugs map[classifier.OptionKey]string
	mode        string
	hash        string
	questions   []classifier.Question
}

func compileSensor(sensor Sensor) (compiledSensor, bool) {
	var compiled compiledSensor
	if sensor.Instructions == nil || len(sensor.Signals) == 0 || sensor.Slug == "" {
		return compiled, false
	}
	compiled.signalSlugs = make(map[classifier.OptionKey]string, len(sensor.Signals))
	for _, signal := range sensor.Signals {
		if sensor.SignalSlugs[signal.Key] == "" {
			return compiled, false
		}
		compiled.signalSlugs[signal.Key] = sensor.SignalSlugs[signal.Key]
	}
	compiled.slug = sensor.Slug
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
	data, _ := json.Marshal(map[string]any{"version": "sigint-definition-v2", "mode": sensor.Mode, "instructions": instructions, "signals": signals, "match_expression": sensor.MatchExpression, "matching_contract": matching.Version})
	compiled.hash = digest(data)
	return compiled, true
}

func reading(event Event, sensor compiledSensor, answers map[classifier.QuestionKey]classifier.QuestionOutcome, attempt, at string, result classifier.Result) (*sigintv1.Reading, error) {
	if sensor.slug == "" {
		return nil, fmt.Errorf("missing sensor slug")
	}
	for _, q := range sensor.questions {
		outcome, ok := answers[q.Key]
		if !ok || outcome.Answer == nil || outcome.Failure != nil {
			return nil, fmt.Errorf("incomplete sensor evaluation")
		}
	}
	r := sigintv1.Reading_builder{Event: proto.CloneOf(event.Subject)}.Build()
	identity, _ := json.Marshal([]string{"sigint-reading-v1", event.OrganizationID, event.ProjectID, event.Subject.GetKind(), event.Subject.GetId(), sensor.id})
	r.SetId(uuid.NewSHA1(readingNamespace, identity).String())
	r.SetEvaluationAttemptId(attempt)
	r.SetOrganizationId(event.OrganizationID)
	r.SetProjectId(event.ProjectID)
	r.SetSensorId(sensor.id)
	r.SetSensorSlug(sensor.slug)
	r.SetEvaluatedAt(at)
	r.SetDefinitionHash(sensor.hash)
	r.SetConfiguredModel(result.Metadata.Model)
	r.SetModels(result.Models)
	r.SetCompilerVersion("sigint-v1/" + result.Metadata.CompilerVersion)
	if event.Actor != nil {
		r.SetActor(proto.CloneOf(event.Actor))
	}
	if event.BillingUserID != nil {
		r.SetBillingUserId(*event.BillingUserID)
	}
	if event.Source != nil {
		r.SetSource(*event.Source)
	}
	if event.AssistantID != nil {
		r.SetAssistantId(*event.AssistantID)
	}
	if event.Replayed != nil {
		r.SetReplayed(*event.Replayed)
	}
	if event.Account != nil {
		r.SetAccount(proto.CloneOf(event.Account))
	}
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
			slug := sensor.signalSlugs[classifier.OptionKey(p.GetSignalId())]
			if slug == "" {
				return nil, fmt.Errorf("missing signal slug")
			}
			p.SetSignalSlug(slug)
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
		selectedSlug := sensor.signalSlugs[answer.Selected]
		if selectedSlug == "" {
			return nil, fmt.Errorf("missing selected signal slug")
		}
		value.SetSelectedSignalSlug(selectedSlug)

		probabilities, err := distribution(answer.Distribution, sensor.signalSlugs)
		if err != nil {
			return nil, err
		}

		value.SetDistribution(probabilities)
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

		probabilities, err := distribution(answer.Distribution, sensor.signalSlugs)
		if err != nil {
			return nil, err
		}

		value.SetDistribution(probabilities)
		if answer.Confidence != nil {
			value.SetConfidence(*answer.Confidence)
		}
		r.SetScore(value)
	}
	return r, nil
}

func distribution(values []classifier.Probability, slugs map[classifier.OptionKey]string) ([]*sigintv1.Reading_Probability, error) {
	result := make([]*sigintv1.Reading_Probability, 0, len(values))
	for _, value := range values {
		p := &sigintv1.Reading_Probability{}
		p.SetSignalId(string(value.Option))
		if slugs[value.Option] == "" {
			return nil, fmt.Errorf("missing distribution signal slug")
		}
		p.SetSignalSlug(slugs[value.Option])
		p.SetProbability(value.Value)
		result = append(result, p)
	}
	return result, nil
}
