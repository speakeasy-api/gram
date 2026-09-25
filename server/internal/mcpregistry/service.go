package mcpregistry

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
)

type Entry struct {
	ID                   uuid.UUID
	Data                 json.RawMessage
	Published            bool
	CreatedAt, UpdatedAt time.Time
}

type ListOptions struct {
	Query     string
	Published *bool
	Cursor    string
	Limit     int32
}

type Summary struct {
	ID        uuid.UUID
	Name      string
	Published bool
	UpdatedAt string
	Issues    []Issue
}

type Page struct {
	Entries    []Summary
	NextCursor string
}

// ListPageByteBudget bounds serialized JSONB bodies loaded for one staff page.
const ListPageByteBudget = 8 << 20

type Service struct {
	db        *pgxpool.Pool
	validator *Validator
}

var ErrNotFound = errors.New("registry entry not found")

var ErrInvalidCursor = errors.New("invalid registry cursor")

var ErrInvalidListOptions = errors.New("invalid registry list options")

func New(db *pgxpool.Pool, v *Validator) *Service {
	return &Service{db: db, validator: v}
}

func (s *Service) Ready(ctx context.Context) error {
	if s.validator == nil || s.validator.schema == nil {
		return errors.New("registry validator unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := repo.New(s.db).RegistryReady(ctx)
	if err != nil {
		return fmt.Errorf("check registry readiness: %w", err)
	}
	return nil
}

func entry(row repo.McpRegistryEntry, err error) (Entry, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return Entry{}, ErrNotFound
	}
	if err != nil {
		return Entry{}, err
	}
	return Entry{ID: row.ID, Data: row.Data, Published: row.Published, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Entry, error) {
	return entry(repo.New(s.db).GetEntry(ctx, id))
}

func (s *Service) GetByName(ctx context.Context, name string) (Entry, error) {
	return entry(repo.New(s.db).GetEntryByName(ctx, name))
}

type listCursor struct {
	Name      string    `json:"Name"`
	ID        uuid.UUID `json:"ID"`
	Query     string    `json:"Query"`
	Published *bool     `json:"Published"`
}

func samePublished(a, b *bool) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

func (s *Service) List(ctx context.Context, opts ListOptions) (Page, error) {
	if s.validator == nil || s.validator.schema == nil {
		return Page{}, errors.New("registry validator unavailable")
	}
	if opts.Limit == 0 {
		opts.Limit = 25
	}
	if opts.Limit < 0 || opts.Limit > 50 || len(opts.Query) > 1024 || strings.ContainsRune(opts.Query, '\x00') {
		return Page{}, ErrInvalidListOptions
	}
	c := listCursor{Name: "", ID: uuid.Nil, Query: "", Published: nil}
	if opts.Cursor != "" {
		if len(opts.Cursor) > 8192 {
			return Page{}, ErrInvalidCursor
		}
		raw, err := base64.RawURLEncoding.DecodeString(opts.Cursor)
		if err != nil {
			return Page{}, ErrInvalidCursor
		}
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		if d.Decode(&c) != nil {
			return Page{}, ErrInvalidCursor
		}
		var extra any
		if d.Decode(&extra) != io.EOF || c.ID == uuid.Nil || strings.ContainsRune(c.Name, '\x00') || c.Query != opts.Query || !samePublished(c.Published, opts.Published) {
			return Page{}, ErrInvalidCursor
		}
	}
	published := pgtype.Bool{Bool: false, Valid: false}
	if opts.Published != nil {
		published = pgtype.Bool{Bool: *opts.Published, Valid: true}
	}
	rows, err := repo.New(s.db).ListEntries(ctx, repo.ListEntriesParams{ByteBudget: ListPageByteBudget, Search: opts.Query, Published: published, HasCursor: opts.Cursor != "", LastName: c.Name, LastID: c.ID, PageLimit: opts.Limit + 1})
	if err != nil {
		return Page{}, fmt.Errorf("list registry entries: %w", err)
	}
	more := false
	page := Page{Entries: make([]Summary, 0, len(rows)), NextCursor: ""}
	for _, r := range rows {
		if len(page.Entries) == int(opts.Limit) || r.PageBytes > ListPageByteBudget {
			more = true
			break
		}
		var issues []Issue
		if r.DataBytes > ListPageByteBudget {
			// Keep invalid stored records discoverable for repair without loading them.
			issues = []Issue{{Path: "", Message: "record exceeds byte limit"}}
		} else {
			issues = s.validator.Validate(r.Data)
		}
		page.Entries = append(page.Entries, Summary{ID: r.ID, Name: r.Name, Published: r.Published, UpdatedAt: r.UpdatedAt.Time.UTC().Format(time.RFC3339Nano), Issues: issues})
	}
	if more {
		last := page.Entries[len(page.Entries)-1]
		raw, err := json.Marshal(listCursor{Name: last.Name, ID: last.ID, Query: opts.Query, Published: opts.Published})
		if err != nil {
			return Page{}, fmt.Errorf("encode registry cursor: %w", err)
		}
		page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return page, nil
}
