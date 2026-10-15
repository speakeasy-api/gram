package demoseed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/sigint/chrepo"
)

const (
	// sigintReadingDays is how many trailing days of readings each enabled
	// sensor gets: more than a year, so annual trend examples have a full cycle.
	sigintReadingDays = 400

	// sigintReadingCount is the expected landed row count: one reading per day
	// for each of the three enabled sensors.
	sigintReadingCount = sigintReadingDays * 3
)

// sigintReadingSensor is one enabled sensor from the Postgres seed, with the
// configured message context its readings report.
type sigintReadingSensor struct {
	key, slug, resultType, inputStrategy string
	requestedMessages                    uint8
}

// sigintReadingSensors mirror seed_01_org.go's enabled sensors, in mode order.
var sigintReadingSensors = []sigintReadingSensor{
	{key: "service-areas", slug: "service-areas", resultType: "multi_label", inputStrategy: "single_message", requestedMessages: 1},
	{key: "primary-service", slug: "primary-service", resultType: "choice", inputStrategy: "sliding_window", requestedMessages: 5},
	{key: "impact", slug: "customer-impact", resultType: "score", inputStrategy: "sliding_window", requestedMessages: 10},
}

// chSigintReadings regenerates synthetic retrospective sensor readings. They
// reference the configured sensors and their ordered members, with each
// sensor's message context. Annual and weekly variation supports trend
// examples without paid classifier calls or claiming these are real
// evaluations. The org-scoped purge in chPurge removes the previous generation.
func (s *seeder) chSigintReadings(ctx context.Context, ch driver.Conn) error {
	rows := make([]chrepo.ReadingRow, 0, sigintReadingCount)
	for n := range sigintReadingDays {
		for mode := range sigintReadingSensors {
			rows = append(rows, s.sigintReading(n, mode))
		}
	}
	if err := chrepo.New(ch).InsertReadings(ctx, rows); err != nil {
		return fmt.Errorf("insert demo sensor readings: %w", err)
	}
	return s.chRunChecks(ctx, ch, []chCheck{{
		cond:     `(SELECT count() FROM sigint_readings FINAL WHERE organization_id = @org) != ` + strconv.Itoa(sigintReadingCount),
		settings: chFinalSettings,
		msg:      "demo seed postflight: expected 400 retrospective readings for each configured sensor",
	}})
}

func (s *seeder) sigintReading(n, mode int) chrepo.ReadingRow {
	sensor := sigintReadingSensors[mode]
	day := float64(n)
	probability := 0.5 + 0.3*math.Sin(day*2*math.Pi/365) + 0.1*math.Sin(day*2*math.Pi/7)
	remainderShare := 0.5 + 0.3*math.Sin(day*2*math.Pi/7+1)
	distribution := [3]float64{probability, (1 - probability) * remainderShare, (1 - probability) * (1 - remainderShare)}
	selected := 0
	for i, value := range distribution {
		if value > distribution[selected] {
			selected = i
		}
	}
	signal := func(key string, p float64) chrepo.ProbabilityRow {
		return chrepo.ProbabilityRow{SignalID: s.det("sigint-signal-" + key), SignalSlug: key, Probability: p}
	}

	// Short conversations supply fewer messages than a window requests.
	supplied := uint8(1)
	var (
		probabilities []chrepo.ProbabilityRow
		selectedID    *uuid.UUID
		selectedSlug  string
		expectedIndex *float64
		confidence    *float64
	)
	switch mode {
	case 0:
		probabilities = []chrepo.ProbabilityRow{
			signal("api", probability),
			signal("database", 0.2+0.6*(1-probability)),
			signal("authentication", 0.3+0.2*math.Sin(day*2*math.Pi/7)),
		}
	case 1:
		options := [3]string{"api", "database", "worker"}
		for i, key := range options {
			probabilities = append(probabilities, signal(key, distribution[i]))
		}
		selectedID = new(s.det("sigint-signal-" + options[selected]))
		selectedSlug = options[selected]
		supplied, _ = conv.ClampedIntToUint8(min(5, 2+n%5))
	default:
		probabilities = []chrepo.ProbabilityRow{
			signal("low", (1-probability)*0.65),
			signal("medium", (1-probability)*0.35),
			signal("high", probability),
		}
		expectedIndex = new((1-probability)*0.35 + 2*probability)
		supplied, _ = conv.ClampedIntToUint8(min(10, 2+n%11))
	}
	// Some exclusive and ordered readings carry no classifier confidence.
	if mode != 0 && n%11 != 0 {
		confidence = new(0.6 + 0.3*math.Cos(day*2*math.Pi/17))
	}

	definition := sha256.Sum256([]byte(s.name("sigint-definition-" + strconv.Itoa(mode+1))))
	departments := [2]string{"engineering", "support"}
	regions := [2]string{"us-east", "eu-west"}
	return chrepo.ReadingRow{
		ID:                         s.det("sigint-reading-" + strconv.Itoa(mode+1) + "-" + strconv.Itoa(n)),
		EvaluationAttemptID:        s.det("sigint-attempt-" + strconv.Itoa(n)),
		OrganizationID:             s.orgID(),
		ProjectID:                  s.projectID(),
		SensorID:                   s.det("sigint-sensor-" + sensor.key),
		SensorSlug:                 sensor.slug,
		EventKind:                  "demo.event",
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
		InputRequestedMessageCount: sensor.requestedMessages,
		InputMessageCount:          supplied,
		InputTruncated:             false,
		EventContextType:           "",
		ConversationID:             nil,
		ConversationRole:           "",
		ToolName:                   "",
		ToolCallID:                 "",
		MCPServerID:                nil,
		ActorUserID:                "",
		ActorExternalUserID:        "",
		ActorUserEmail:             "",
		BillingUserID:              "",
		Source:                     "",
		UserAccountID:              nil,
		AccountType:                "",
		BillingMode:                "",
		AssistantID:                nil,
		Replayed:                   new(true),
		Attributes:                 map[string]string{"department": departments[n%2], "region": regions[n%2], "environment": "production"},
		ResultType:                 sensor.resultType,
		Probabilities:              probabilities,
		SelectedSignalID:           selectedID,
		SelectedSignalSlug:         selectedSlug,
		ExpectedIndex:              expectedIndex,
		Confidence:                 confidence,
	}
}
