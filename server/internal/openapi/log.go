package openapi

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/internal/deployments/repo"
)

type LogHandler struct {
	mut              *sync.Mutex
	attrDeploymentID uuid.UUID
	attrProjectID    uuid.UUID
	attrEvent        string
	msgs             []repo.BatchLogEventsParams
	level            slog.Leveler
}

func NewLogHandler() *LogHandler {
	return &LogHandler{
		mut:              &sync.Mutex{},
		level:            slog.LevelInfo,
		attrDeploymentID: uuid.Nil,
		attrProjectID:    uuid.Nil,
		attrEvent:        "",
		msgs:             []repo.BatchLogEventsParams{},
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

	record.Attrs(func(attr slog.Attr) bool {
		switch {
		case attr.Key == "event" && attr.Value.Kind() == slog.KindString:
			event = attr.Value.String()
		case attr.Key == "project_id" && attr.Value.Kind() == slog.KindString:
			if id, err := uuid.Parse(attr.Value.String()); err == nil {
				projectID = id
			}
		case attr.Key == "deployment_id" && attr.Value.Kind() == slog.KindString:
			if id, err := uuid.Parse(attr.Value.String()); err == nil {
				deploymentID = id
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
	l.msgs = append(l.msgs, repo.BatchLogEventsParams{
		DeploymentID: deploymentID,
		ProjectID:    projectID,
		Event:        event,
		Message:      record.Message,
	})
	l.mut.Unlock()

	return nil
}

func (l *LogHandler) clone() *LogHandler {
	clone := *l

	l.mut.Lock()
	clone.msgs = make([]repo.BatchLogEventsParams, len(l.msgs))
	copy(clone.msgs, l.msgs)
	l.mut.Unlock()

	return &clone
}

func (l *LogHandler) Flush(ctx context.Context, db *pgxpool.Pool) (int64, error) {
	var msgs []repo.BatchLogEventsParams

	l.mut.Lock()
	msgs = make([]repo.BatchLogEventsParams, len(l.msgs))
	copy(msgs, l.msgs)
	l.msgs = []repo.BatchLogEventsParams{}
	l.mut.Unlock()

	return repo.New(db).BatchLogEvents(ctx, msgs)
}

func (l *LogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := l.clone()

	for _, attr := range attrs {
		switch {
		case attr.Key == "event" && attr.Value.Kind() == slog.KindString:
			clone.attrEvent = attr.Value.String()
		case attr.Key == "project_id" && attr.Value.Kind() == slog.KindString:
			if id, err := uuid.Parse(attr.Value.String()); err == nil {
				clone.attrProjectID = id
			}
		case attr.Key == "deployment_id" && attr.Value.Kind() == slog.KindString:
			if id, err := uuid.Parse(attr.Value.String()); err == nil {
				clone.attrDeploymentID = id
			}
		}
	}
	return clone
}

func (l *LogHandler) WithGroup(name string) slog.Handler {
	panic("groups are not supported")
}
