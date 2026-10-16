package demoseed

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/sigint/chrepo"
	"github.com/speakeasy-api/gram/server/internal/sigint/evaluation"
)

const (
	// sigintReadingDays is how many trailing days of readings each enabled
	// sensor gets: more than a year, so annual trend examples have a full cycle.
	sigintReadingDays = 400

	// sigintReadingMembers is the member count the reading formulas below are
	// written for: three signals or levels per enabled sensor.
	sigintReadingMembers = 3

	// Every ninth message is an assistant reply, which each sensor's default
	// user-only match expression excludes, so its evaluations are
	// not_applicable and coverage shows outside-scope messages.
	sigintAssistantReplyEvery = 9

	// Every 23rd message failed for every sensor, so coverage shows failures.
	sigintFailedEvery = 23

	// Every 17th answered message first failed and was answered on retry. The
	// failed attempt stays stored as a superseded version of the same reading,
	// so the analytics collapse is visible in the demo: it counts once.
	sigintRetriedEvery = 17

	// sigintMessagesPerConversation groups consecutive messages into one
	// conversation.
	sigintMessagesPerConversation = 4

	// sigintFailureReason is the evaluator's code for a classifier refusal.
	sigintFailureReason = "classifier_rejected"
)

// sigintMessageSources alternate the surface each message came from.
var sigintMessageSources = [2]string{"claude-code", "cursor"}

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
	ids := make(map[uuid.UUID]struct{}, len(rows))
	for _, row := range rows {
		ids[row.ID] = struct{}{}
	}
	return s.chRunChecks(ctx, ch, []chCheck{{
		cond:     `(SELECT count() FROM sigint_readings FINAL WHERE organization_id = @org) != ` + strconv.Itoa(len(ids)),
		settings: chFinalSettings,
		msg:      "demo seed postflight: expected 400 retrospective readings for each enabled sensor, retries collapsed",
	}, {
		cond:     `(SELECT countIf(status = 'not_applicable') = 0 OR countIf(status = 'failed') = 0 FROM sigint_readings FINAL WHERE organization_id = @org)`,
		settings: chFinalSettings,
		msg:      "demo seed postflight: expected not_applicable and failed sensor outcomes for coverage",
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
		for n := range sigintReadingDays {
			reading := s.sigintReading(sensor, members, n)
			switch {
			case n%sigintAssistantReplyEvery == 4:
				reading.ConversationRole = "assistant"
				rows = append(rows, sigintOutcome(reading, chrepo.StatusNotApplicable, ""))
			case n%sigintFailedEvery == 5:
				rows = append(rows, sigintOutcome(reading, chrepo.StatusFailed, sigintFailureReason))
			case n%sigintRetriedEvery == 3:
				failed := sigintOutcome(reading, chrepo.StatusFailed, sigintFailureReason)
				failed.EvaluationAttemptID = s.det("sigint-attempt-failed-" + strconv.Itoa(n))
				failed.EvaluatedAt = reading.EvaluatedAt.Add(-time.Hour)
				failed.InsertedAt = failed.EvaluatedAt
				rows = append(rows, failed, reading)
			default:
				rows = append(rows, reading)
			}
		}
	}
	return rows, nil
}

// sigintOutcome turns a reading into the outcome of the same evaluation: a
// decision without an answer, so its result columns are empty.
func sigintOutcome(reading chrepo.ReadingRow, status, reason string) chrepo.ReadingRow {
	reading.Status = status
	reading.FailureReason = reason
	reading.ResultType = ""
	reading.Probabilities = nil
	reading.SelectedSignalID = nil
	reading.SelectedSignalSlug = ""
	reading.ExpectedIndex = nil
	reading.Confidence = nil
	return reading
}

func (s *seeder) sigintReading(sensor demoSigintSensor, members []demoSigintMembership, n int) chrepo.ReadingRow {
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
	// The classifier reports confidence for every choice and score.
	if sensor.mode != "multi_label" {
		confidence = new(0.6 + 0.3*math.Cos(day*2*math.Pi/17))
	}

	definition := sha256.Sum256([]byte(s.name("sigint-definition-" + sensor.key)))
	departments := [2]string{"engineering", "support"}
	regions := [2]string{"us-east", "eu-west"}
	return chrepo.ReadingRow{
		ID:                         s.det("sigint-reading-" + sensor.key + "-" + strconv.Itoa(n)),
		EvaluationAttemptID:        s.det("sigint-attempt-" + strconv.Itoa(n)),
		OrganizationID:             s.orgID(),
		ProjectID:                  s.projectID(),
		SensorID:                   s.det("sigint-sensor-" + sensor.key),
		SensorSlug:                 sensor.slug,
		EventKind:                  evaluation.ConversationMessageKind,
		EventID:                    s.name("sigint-source-" + strconv.Itoa(n)),
		EventOccurredAt:            s.now.UTC().AddDate(0, 0, -n).Add(-time.Hour),
		EvaluatedAt:                s.now.UTC(),
		InsertedAt:                 s.now.UTC(),
		Status:                     chrepo.StatusAnswered,
		FailureReason:              "",
		DefinitionHash:             hex.EncodeToString(definition[:]),
		ConfiguredModel:            "demo-classifier",
		Models:                     nil,
		CompilerVersion:            "demo-compiler-v1",
		Provider:                   "demo",
		InputStrategy:              sensor.inputStrategy,
		InputRequestedMessageCount: requested,
		InputMessageCount:          supplied,
		InputTruncated:             false,
		EventContextType:           "",
		ConversationID:             new(s.det("sigint-conversation-" + strconv.Itoa(n/sigintMessagesPerConversation))),
		ConversationRole:           "user",
		ToolName:                   "",
		ToolCallID:                 "",
		MCPServerID:                nil,
		ActorUserID:                "",
		ActorExternalUserID:        "",
		ActorUserEmail:             s.email(demoUsers[n%len(demoUsers)].handle),
		BillingUserID:              "",
		Source:                     sigintMessageSources[n%len(sigintMessageSources)],
		UserAccountID:              nil,
		AccountType:                "",
		BillingMode:                "",
		AssistantID:                nil,
		Replayed:                   new(true),
		Attributes:                 map[string]string{"department": departments[n%2], "region": regions[n%2], "environment": "production"},
		ResultType:                 sigintReadingResultTypes[sensor.mode],
		Probabilities:              probabilities,
		SelectedSignalID:           selectedID,
		SelectedSignalSlug:         selectedSlug,
		ExpectedIndex:              expectedIndex,
		Confidence:                 confidence,
	}
}
