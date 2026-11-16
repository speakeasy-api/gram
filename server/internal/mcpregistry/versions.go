package mcpregistry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
)

// Server is the stable identity shared by a server's registry entries.
type Server struct {
	ID                   uuid.UUID
	Name                 string
	Visible              bool
	DefaultEntryID       *uuid.UUID
	CreatedAt, UpdatedAt time.Time
}

func (s *Service) GetServer(ctx context.Context, id uuid.UUID) (Server, error) {
	row, err := repo.New(s.db).GetServer(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Server{}, ErrNotFound
	}
	if err != nil {
		return Server{}, fmt.Errorf("get registry server: %w", err)
	}
	result := Server{ID: row.ID, Name: row.Name, Visible: row.Visible, DefaultEntryID: nil, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
	if row.DefaultEntryID.Valid {
		result.DefaultEntryID = &row.DefaultEntryID.UUID
	}
	return result, nil
}

// All entry writers acquire the parent first; linking is immutable after backfill.
func lockEntry(ctx context.Context, q *repo.Queries, id uuid.UUID) (Entry, error) {
	if _, err := q.LockEntryServer(ctx, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Entry{}, ErrNotFound
		}
		return Entry{}, fmt.Errorf("lock registry server: %w", err)
	}
	return entry(q.LockEntry(ctx, id))
}

// recordVersion matches PostgreSQL's exact, case-sensitive server/version JSON
// path. ok is false when the stored version is missing, null, or not a string,
// so invalid historical versions never compare equal to an empty version.
func recordVersion(raw json.RawMessage) (version string, ok bool) {
	var doc map[string]json.RawMessage
	if json.Unmarshal(raw, &doc) != nil {
		return "", false
	}
	var server map[string]json.RawMessage
	if json.Unmarshal(doc["server"], &server) != nil {
		return "", false
	}
	var value *string
	if json.Unmarshal(server["version"], &value) != nil || value == nil {
		return "", false
	}
	return *value, true
}
