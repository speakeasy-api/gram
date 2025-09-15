package openapi

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
)

type logBuffer struct {
	msgs []repo.BatchLogEventsParams
}

type LogHandler struct {
	mut                *sync.Mutex
	attrDeploymentID   uuid.UUID
	attrProjectID      uuid.UUID
	attrOpenAPIAssetID uuid.UUID
	attrEvent          string
	buffer             *logBuffer
	level              slog.Leveler
}

func NewLogHandler() *LogHandler {
	ptr := &atomic.Pointer[[]repo.BatchLogEventsParams]{}
	ptr.Store(&[]repo.BatchLogEventsParams{})

	return &LogHandler{
		mut:                &sync.Mutex{},
		level:              slog.LevelInfo,
		attrDeploymentID:   uuid.Nil,
		attrProjectID:      uuid.Nil,
		attrOpenAPIAssetID: uuid.Nil,
		attrEvent:          "",
		buffer:             &logBuffer{msgs: []repo.BatchLogEventsParams{}},
	}

}

var _ slog.Handler = (*LogHandler)(nil)

func (l *LogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= l.level.Level()
}

func (l *LogHandler) Handle(ctx context.Context, record slog.Record) error {
	event := l.attrEvent
	projectID := l.attrProjectID
	deploymentID := l.attrDeploymentID
	openAPIAssetID := l.attrOpenAPIAssetID

	record.Attrs(func(a slog.Attr) bool {
		switch {
		case a.Key == string(attr.EventKey) && a.Value.Kind() == slog.KindString:
			event = a.Value.String()
		case a.Key == string(attr.ProjectIDKey) && a.Value.Kind() == slog.KindString:
			if id, err := uuid.Parse(a.Value.String()); err == nil {
				projectID = id
			}
		case a.Key == string(attr.DeploymentIDKey) && a.Value.Kind() == slog.KindString:
			if id, err := uuid.Parse(a.Value.String()); err == nil {
				deploymentID = id
			}
		case a.Key == string(attr.DeploymentOpenAPIIDKey) && a.Value.Kind() == slog.KindString:
			if id, err := uuid.Parse(a.Value.String()); err == nil {
				openAPIAssetID = id
			}
		}

		return true
	})

	if event == "" {
		event = fmt.Sprintf("log:%s", strings.ToLower(record.Level.String()))
	}

	if record.Message == "" || event == "" || projectID == uuid.Nil || deploymentID == uuid.Nil {
		return nil
	}

	l.mut.Lock()
	msg := repo.BatchLogEventsParams{
		DeploymentID:   deploymentID,
		ProjectID:      projectID,
		Event:          event,
		Message:        record.Message,
		AttachmentID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		AttachmentType: conv.ToPGTextEmpty(""),
	}
	if openAPIAssetID != uuid.Nil {
		msg.AttachmentID = uuid.NullUUID{UUID: openAPIAssetID, Valid: true}
		msg.AttachmentType = conv.ToPGText("openapi")
	}
	l.buffer.msgs = append(l.buffer.msgs, msg)
	l.mut.Unlock()

	return nil
}

func (l *LogHandler) clone() *LogHandler {
	clone := *l
	return &clone
}

func (l *LogHandler) Flush(ctx context.Context, db *pgxpool.Pool) (int64, error) {
	l.mut.Lock()
	msgs := make([]repo.BatchLogEventsParams, len(l.buffer.msgs))
	copy(msgs, l.buffer.msgs)
	l.buffer.msgs = []repo.BatchLogEventsParams{}
	l.mut.Unlock()

	n, err := repo.New(db).BatchLogEvents(ctx, msgs)
	if err != nil {
		return n, fmt.Errorf("flush log events: %w", err)
	}

	return n, nil
}

func (l *LogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := l.clone()

	for _, a := range attrs {
		switch {
		case a.Key == string(attr.EventKey) && a.Value.Kind() == slog.KindString:
			clone.attrEvent = a.Value.String()
		case a.Key == string(attr.ProjectIDKey) && a.Value.Kind() == slog.KindString:
			if id, err := uuid.Parse(a.Value.String()); err == nil {
				clone.attrProjectID = id
			}
		case a.Key == string(attr.DeploymentIDKey) && a.Value.Kind() == slog.KindString:
			if id, err := uuid.Parse(a.Value.String()); err == nil {
				clone.attrDeploymentID = id
			}
		case a.Key == string(attr.DeploymentOpenAPIIDKey) && a.Value.Kind() == slog.KindString:
			if id, err := uuid.Parse(a.Value.String()); err == nil {
				clone.attrOpenAPIAssetID = id
			}
		}
	}
	return clone
}

func (l *LogHandler) WithGroup(name string) slog.Handler {
	panic("groups are not supported")
}
