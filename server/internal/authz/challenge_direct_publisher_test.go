package authz

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"

	authzv1 "github.com/speakeasy-api/gram/infra/gen/gram/authz/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// directCHPublisher inserts ChallengeRow messages into ClickHouse inside
// Publish. It satisfies gcp.Publisher so tests that assert on authz_challenges
// can exercise the full Log → Publish path without running the streams
// consumer.
type directCHPublisher struct {
	conn clickhouse.Conn
}

func (p *directCHPublisher) Publish(ctx context.Context, msg *authzv1.ChallengeRow) gcp.PublishResult {
	row, err := challengeRowFromProto(msg)
	if err != nil {
		return &errPublishResult{err: fmt.Errorf("decode challenge row: %w", err)}
	}
	if err := authzrepo.New(p.conn).InsertChallenge(ctx, row); err != nil {
		return &errPublishResult{err: err}
	}
	return gcp.NewSuccessPublishResult()
}

func (p *directCHPublisher) Stop(context.Context) error { return nil }

// errPublishResult mirrors infra/pkg/gcp's unexported errPublishResult so the
// direct publisher can surface insert failures through PublishResult.Get.
type errPublishResult struct {
	err error
}

func (e *errPublishResult) Ready() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (e *errPublishResult) Get(context.Context) (string, error) {
	return "", e.err
}

// NewDirectChallengePublisher returns a ChallengePublisher that inserts into
// ClickHouse synchronously inside Publish. For tests that assert on
// authz_challenges without running the streams consumer.
func NewDirectChallengePublisher(t *testing.T, logger *slog.Logger, conn clickhouse.Conn) *ChallengePublisher {
	t.Helper()
	return NewChallengePublisher(
		logger,
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		&directCHPublisher{conn: conn},
	)
}
