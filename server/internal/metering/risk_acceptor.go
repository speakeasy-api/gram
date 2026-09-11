package metering

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/metering/chrepo"
	meteringrepo "github.com/speakeasy-api/gram/server/internal/metering/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/outbox"
	"github.com/speakeasy-api/gram/server/internal/streams"
)

type transactionDB interface {
	meteringrepo.DBTX
	Begin(context.Context) (pgx.Tx, error)
}

// RiskMeterAcceptor retains the first complete risk reading and atomically
// enqueues that immutable envelope for the canonical meter-reading topic.
type RiskMeterAcceptor struct {
	logger *slog.Logger
	db     *pgxpool.Pool
}

// NewRiskMeterAcceptor creates the durable risk-reading candidate subscriber.
func NewRiskMeterAcceptor(logger *slog.Logger, db *pgxpool.Pool) *RiskMeterAcceptor {
	return &RiskMeterAcceptor{
		logger: logger.With(attr.SlogComponent("risk-meter-acceptor")),
		db:     db,
	}
}

var _ streams.Handler[*meteringv1.RiskMeterReading] = (*RiskMeterAcceptor)(nil)

// Handle accepts a serialized candidate. Invalid candidates are poison messages
// and are acknowledged without entering either the receipt ledger or outbox.
func (a *RiskMeterAcceptor) Handle(ctx context.Context, candidate *meteringv1.RiskMeterReading, _ gcp.MessageMetadata) error {
	reading := new(meteringv1.MeterReading)
	if candidate == nil || proto.Unmarshal(candidate.GetReading(), reading) != nil {
		a.logger.ErrorContext(ctx, "skipping unprocessable risk meter reading", attr.SlogReason("invalid_envelope"))
		return nil
	}
	if _, reason := meterReadingRow(reading, time.Now().UTC()); reason != "" || !isRiskMeterReading(reading) {
		if reason == "" {
			reason = "not_registered_risk_meter"
		}
		a.logger.ErrorContext(ctx, "skipping unprocessable risk meter reading", attr.SlogReason(reason), attr.SlogMessageID(reading.GetId()))
		return nil
	}

	if _, err := canonicalRiskReading(ctx, a.db, reading); err != nil {
		return fmt.Errorf("accept risk meter reading: %w", err)
	}
	return nil
}

func isRiskMeterReading(reading *meteringv1.MeterReading) bool {
	definition, ok := LookupDefinition(MeterID(reading.GetMeterId()), reading.GetMeterVersion())
	if !ok {
		return false
	}
	switch definition {
	case RiskGitleaks(), RiskPresidio(), RiskPromptInjection(), RiskPromptPolicy(), RiskCustomRules(), RiskCLIDestructive():
		return true
	default:
		return false
	}
}

// canonicalRiskReading returns the first complete envelope retained for a risk
// reading identity. The winner atomically enqueues it, including when a legacy
// canonical-topic delivery is the first observer during cutover.
func canonicalRiskReading(ctx context.Context, db transactionDB, candidate *meteringv1.MeterReading) (*meteringv1.MeterReading, error) {
	id, err := uuid.Parse(candidate.GetId())
	if err != nil {
		return nil, fmt.Errorf("parse risk reading id: %w", err)
	}
	projectID, err := uuid.Parse(candidate.GetProjectId())
	if err != nil {
		return nil, fmt.Errorf("parse risk reading project id: %w", err)
	}
	projectArg := uuid.NullUUID{UUID: projectID, Valid: true}
	params := meteringrepo.GetRiskMeterReadingAcceptanceParams{
		ID:             id,
		OrganizationID: candidate.GetOrganizationId(),
		ProjectID:      projectArg,
	}
	if envelope, getErr := meteringrepo.New(db).GetRiskMeterReadingAcceptance(ctx, params); getErr == nil {
		return unmarshalAcceptedRiskReading(envelope, candidate)
	} else if !errors.Is(getErr, pgx.ErrNoRows) {
		return nil, fmt.Errorf("get accepted risk reading: %w", getErr)
	}

	row, reason := meterReadingRow(candidate, time.Now().UTC())
	if reason != "" || !isRiskMeterReading(candidate) {
		return nil, fmt.Errorf("invalid risk meter reading: %s", reason)
	}
	rows := []chrepo.ReadingRow{row}
	if err := enrichBillingUserRows(ctx, db, rows, true); err != nil {
		return nil, err
	}
	frozen, ok := proto.Clone(candidate).(*meteringv1.MeterReading)
	if !ok {
		return nil, fmt.Errorf("clone risk meter reading")
	}
	frozen.SetAttributes(rows[0].Attributes)
	envelope, err := proto.Marshal(frozen)
	if err != nil {
		return nil, fmt.Errorf("marshal risk meter reading: %w", err)
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin risk meter acceptance: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	queries := meteringrepo.New(tx)
	insertedEnvelope, err := queries.InsertRiskMeterReadingAcceptance(ctx, meteringrepo.InsertRiskMeterReadingAcceptanceParams{
		ID:             id,
		OrganizationID: candidate.GetOrganizationId(),
		ProjectID:      projectID,
		Envelope:       envelope,
	})
	won := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("insert risk meter acceptance: %w", err)
	}
	if !won {
		insertedEnvelope, err = queries.GetRiskMeterReadingAcceptance(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("reuse accepted risk meter reading: %w", err)
		}
	}
	accepted, err := unmarshalAcceptedRiskReading(insertedEnvelope, candidate)
	if err != nil {
		return nil, err
	}
	if won {
		if _, err := outbox.Publish(ctx, tx, accepted.GetOrganizationId(), outbox.Message{Proto: accepted, PublicID: id, Attributes: nil}); err != nil {
			return nil, fmt.Errorf("enqueue accepted risk meter reading: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit risk meter acceptance: %w", err)
	}
	return accepted, nil
}

func unmarshalAcceptedRiskReading(envelope []byte, candidate *meteringv1.MeterReading) (*meteringv1.MeterReading, error) {
	reading := new(meteringv1.MeterReading)
	if err := proto.Unmarshal(envelope, reading); err != nil {
		return nil, fmt.Errorf("unmarshal accepted risk meter reading: %w", err)
	}
	if reading.GetId() != candidate.GetId() ||
		reading.GetOrganizationId() != candidate.GetOrganizationId() ||
		reading.GetProjectId() != candidate.GetProjectId() {
		return nil, fmt.Errorf("accepted risk meter reading scope does not match candidate")
	}
	return reading, nil
}
