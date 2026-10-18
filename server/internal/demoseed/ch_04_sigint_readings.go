package demoseed

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/sigint/chrepo"
	"github.com/speakeasy-api/gram/server/internal/sigint/evaluation"
	"github.com/speakeasy-api/gram/server/internal/sigint/matching"
	"github.com/speakeasy-api/gram/server/internal/sigint/readings"
)

const (
	// sigintReadingDays is how many trailing days of readings each enabled
	// sensor gets: more than a year, so annual trend examples have a full cycle.
	sigintReadingDays = 400

	// sigintReadingMembers is the member count the reading formulas below are
	// written for: three signals or levels per enabled sensor.
	sigintReadingMembers = 3

	// sigintManualRuns supplies independent evaluations of the same selection.
	sigintManualRuns = 2

	// sigintManualMessages keeps each example run bounded to two historical targets.
	sigintManualMessages = 2
)

// sigintReadingResultTypes maps a sensor mode to its stored result variant.
var sigintReadingResultTypes = map[string]string{
	"multi_label":   "multi_label",
	"exclusive":     "choice",
	"ordered_score": "score",
}

// chSigintReadings regenerates synthetic retrospective sensor readings for the
// enabled sensors seed_01_org.go configures, using their ordered members and
// message context. Annual and weekly variation supports trend examples without
// paid classifier calls or claiming these are real evaluations. The org-scoped
// purge in chPurge removes the previous generation.
func (s *seeder) chSigintReadings(ctx context.Context, ch driver.Conn) error {
	rows, err := s.sigintReadingRows()
	if err != nil {
		return err
	}
	if err := chrepo.New(ch).InsertReadings(ctx, rows); err != nil {
		return fmt.Errorf("insert demo sensor readings: %w", err)
	}
	return s.chRunChecks(ctx, ch, []chCheck{{
		cond:     `(SELECT count() FROM sigint_readings_v2 FINAL WHERE organization_id = @org) != ` + strconv.Itoa(len(rows)),
		settings: chFinalSettings,
		msg:      "demo seed postflight: expected streaming history and manual-run readings for each enabled sensor",
	}})
}

// sigintReadingRows builds the readings. They bypass the Pub/Sub writer's
// parser, so a unit test holds them to the same rules.
func (s *seeder) sigintReadingRows() ([]chrepo.ReadingRow, error) {
	var rows []chrepo.ReadingRow
	for _, sensor := range demoSigintSensors {
		if !sensor.enabled {
			continue
		}
		var members []demoSigintMembership
		for _, m := range demoSigintMemberships {
			if m.sensor == sensor.key {
				members = append(members, m)
			}
		}
		slices.SortFunc(members, func(a, b demoSigintMembership) int { return cmp.Compare(a.order, b.order) })
		// The formulas are fixed to three members; a reshaped sensor must update
		// them rather than silently seed stale readings.
		if len(members) != sigintReadingMembers {
			return nil, fmt.Errorf("demo sigint sensor %s has %d members, readings expect %d", sensor.key, len(members), sigintReadingMembers)
		}
		if _, ok := sigintReadingResultTypes[sensor.mode]; !ok {
			return nil, fmt.Errorf("demo sigint sensor %s has unsupported mode %s", sensor.key, sensor.mode)
		}
		if err := evaluation.ValidateInputConfiguration(sensor.inputStrategy, int(sensor.windowSize)); err != nil {
			return nil, fmt.Errorf("demo sigint sensor %s: %w", sensor.key, err)
		}
		_, definitionHash, err := s.sigintDemoDefinition(sensor)
		if err != nil {
			return nil, err
		}
		for n := range signalMessageCount {
			row := s.sigintReading(sensor, members, definitionHash, n)
			switch {
			case n%43 == 0:
				row.Status, row.FailureReason = chrepo.StatusFailed, "demo evaluation failure"
			case n%37 == 0:
				row.Status = chrepo.StatusNotApplicable
			case n%31 == 0:
				row.Status = chrepo.StatusDraft
			}
			if row.Status != chrepo.StatusAnswered {
				row.ResultType, row.Probabilities, row.SelectedSignalID, row.SelectedSignalSlug, row.ExpectedIndex, row.Confidence = "", nil, nil, "", nil, nil
			}
			rows = append(rows, row)
		}
		for run := range sigintManualRuns {
			request := readings.Context{
				TriggerKind: readings.TriggerManual,
				RunID:       s.det("sigint-manual-run-" + strconv.Itoa(run)),
				RequestedAt: s.now.UTC().Add(-time.Duration(sigintManualRuns-run) * time.Hour),
			}
			for target := range sigintManualMessages {
				row := s.sigintReading(sensor, members, definitionHash, signalRecentMessages+target)
				row.TriggerKind, row.RunID, row.EvaluationRequestedAt = string(request.TriggerKind), request.RunID, request.RequestedAt
				row.ID = request.ReadingID(row.OrganizationID, row.ProjectID.String(), row.EventKind, row.EventID, row.SensorID.String())
				row.EvaluationAttemptID = s.det("sigint-manual-attempt-" + strconv.Itoa(run) + "-" + strconv.Itoa(target))
				row.EvaluatedAt = request.RequestedAt.Add(time.Second)
				rows = append(rows, row)
			}
		}
	}
	return rows, nil
}

func (s *seeder) sigintReading(sensor demoSigintSensor, members []demoSigintMembership, definitionHash string, n int) chrepo.ReadingRow {
	day := float64(n)
	probability := 0.5 + 0.3*math.Sin(day*2*math.Pi/365) + 0.1*math.Sin(day*2*math.Pi/7)
	remainderShare := 0.5 + 0.3*math.Sin(day*2*math.Pi/7+1)
	distribution := [sigintReadingMembers]float64{probability, (1 - probability) * remainderShare, (1 - probability) * (1 - remainderShare)}
	signal := func(i int, p float64) chrepo.ProbabilityRow {
		return chrepo.ProbabilityRow{SignalID: s.det("sigint-signal-" + members[i].signal), SignalSlug: members[i].signal, Probability: p}
	}

	// Single-message sensors supply only the target. Short conversations supply
	// fewer messages than a window requests. chSigintReadings rejects any other
	// strategy before rows are built.
	requested, supplied := uint8(1), uint8(1)
	if sensor.inputStrategy == evaluation.InputStrategySlidingWindow {
		window := int(sensor.windowSize)
		requested, _ = conv.ClampedIntToUint8(window)
		supplied, _ = conv.ClampedIntToUint8(min(window, 2+n%(window+1)))
	}

	var (
		probabilities []chrepo.ProbabilityRow
		selectedID    *uuid.UUID
		selectedSlug  string
		expectedIndex *float64
		confidence    *float64
	)
	switch sensor.mode {
	case "multi_label":
		probabilities = []chrepo.ProbabilityRow{
			signal(0, probability),
			signal(1, 0.2+0.6*(1-probability)),
			signal(2, 0.3+0.2*math.Sin(day*2*math.Pi/7)),
		}
	case "exclusive":
		selected := 0
		for i, value := range distribution {
			probabilities = append(probabilities, signal(i, value))
			if value > distribution[selected] {
				selected = i
			}
		}
		selectedID = new(s.det("sigint-signal-" + members[selected].signal))
		selectedSlug = members[selected].signal
	case "ordered_score":
		probabilities = []chrepo.ProbabilityRow{
			signal(0, (1-probability)*0.65),
			signal(1, (1-probability)*0.35),
			signal(2, probability),
		}
		expectedIndex = new((1-probability)*0.35 + 2*probability)
	}
	// Some exclusive and ordered readings carry no classifier confidence.
	if sensor.mode != "multi_label" && n%11 != 0 {
		confidence = new(0.6 + 0.3*math.Cos(day*2*math.Pi/17))
	}

	departments := [2]string{"engineering", "marketing"}
	regions := [2]string{"us-east", "eu-west"}
	attributes := map[string]string{"region": regions[n%2], "environment": "production"}
	// Different missingness patterns exercise absent labels independently. The
	// model dimensions describe the source, not this synthetic classifier.
	if n%5 != 0 {
		attributes["department"] = departments[n%2]
	}
	if n%7 != 0 {
		attributes["model_name"] = "anthropic/claude-opus-5"
		attributes["model_family"] = "anthropic"
		if n%2 != 0 {
			attributes["model_name"] = "gpt-6.1-sol"
			attributes["model_family"] = "openai"
		}
	}
	if n%11 != 0 {
		providers := [3]string{"bedrock", "anthropic", "openrouter"}
		if n%2 != 0 {
			providers = [3]string{"openai", "azure", "openrouter"}
		}
		attributes["inference_provider"] = providers[(n/2)%len(providers)]
	}
	conversationID, messageID, sourceTime := s.signalSource(n)
	request := readings.Context{TriggerKind: readings.TriggerStream, RunID: uuid.Nil, RequestedAt: sourceTime.Add(time.Second)}
	eventID := messageID.String()
	sensorID := s.det("sigint-sensor-" + sensor.key)
	return chrepo.ReadingRow{
		ID:                            request.ReadingID(s.orgID(), s.projectID().String(), "conversation.message", eventID, sensorID.String()),
		EvaluationAttemptID:           s.det("sigint-attempt-" + strconv.Itoa(n)),
		OrganizationID:                s.orgID(),
		ProjectID:                     s.projectID(),
		TriggerKind:                   string(request.TriggerKind),
		RunID:                         request.RunID,
		EvaluationRequestedAt:         request.RequestedAt,
		SensorID:                      sensorID,
		SensorSlug:                    sensor.slug,
		EventKind:                     "conversation.message",
		EventID:                       eventID,
		EventOccurredAt:               sourceTime,
		EvaluatedAt:                   request.RequestedAt.Add(time.Second),
		InsertedAt:                    s.now.UTC(),
		Status:                        chrepo.StatusAnswered,
		FailureReason:                 "",
		DefinitionHash:                definitionHash,
		ConfiguredModel:               "demo-classifier",
		Models:                        nil,
		CompilerVersion:               "demo-compiler-v1",
		Provider:                      "demo",
		InputStrategy:                 sensor.inputStrategy,
		InputRequestedMessageCount:    requested,
		InputMessageCount:             supplied,
		InputTruncated:                false,
		EventContextType:              "conversation_message",
		ConversationID:                &conversationID,
		ConversationRole:              []string{"assistant", "user"}[n%2],
		ConversationGeneration:        0,
		ConversationSequence:          s.signalSequences[n],
		ConversationCaptureKind:       "original",
		ConversationOriginalMessageID: nil,
		ToolName:                      "",
		ToolCallID:                    "",
		MCPServerID:                   nil,
		ActorUserID:                   "",
		ActorExternalUserID:           "",
		ActorUserEmail:                "",
		BillingUserID:                 "",
		Source:                        "claude-code",
		UserAccountID:                 nil,
		AccountType:                   "",
		BillingMode:                   "",
		AssistantID:                   nil,
		Replayed:                      new(true),
		Attributes:                    attributes,
		ResultType:                    sigintReadingResultTypes[sensor.mode],
		Probabilities:                 probabilities,
		SelectedSignalID:              selectedID,
		SelectedSignalSlug:            selectedSlug,
		ExpectedIndex:                 expectedIndex,
		Confidence:                    confidence,
	}
}

// sigintDemoDefinition explicitly labels simulated readings and keeps their
// inspectable definition stable across the PostgreSQL and ClickHouse seeds.
func (s *seeder) sigintDemoDefinition(sensor demoSigintSensor) ([]byte, string, error) {
	config := evaluation.Sensor{
		ID: s.det("sigint-sensor-" + sensor.key).String(), Slug: sensor.slug, Mode: sensor.mode,
		Instructions: new(sensor.instructions), InputStrategy: sensor.inputStrategy, WindowSize: int(sensor.windowSize),
		MatchExpression: matching.DefaultExpression, Signals: nil, SignalSlugs: make(map[classifier.OptionKey]string),
	}
	var members []demoSigintMembership
	for _, member := range demoSigintMemberships {
		if member.sensor == sensor.key {
			members = append(members, member)
		}
	}
	slices.SortFunc(members, func(a, b demoSigintMembership) int { return cmp.Compare(a.order, b.order) })
	for _, member := range members {
		for _, signal := range demoSigintSignals {
			if signal.key == member.signal {
				key := classifier.OptionKey(s.det("sigint-signal-" + signal.key).String())
				config.Signals = append(config.Signals, classifier.NewOption(key, classifier.Text(signal.criteria)))
				config.SignalSlugs[key] = signal.key
				break
			}
		}
	}
	snapshot, ready := evaluation.Snapshot(config)
	if !ready || len(config.Signals) != len(members) {
		return nil, "", fmt.Errorf("incomplete demo sigint definition %s", sensor.key)
	}
	data, _ := json.Marshal(struct {
		Synthetic  bool            `json:"synthetic"`
		Sensor     string          `json:"sensor_id"`
		Definition json.RawMessage `json:"definition"`
	}{Synthetic: true, Sensor: config.ID, Definition: snapshot.Definition})
	hash := sha256.Sum256(data)
	return data, hex.EncodeToString(hash[:]), nil
}
