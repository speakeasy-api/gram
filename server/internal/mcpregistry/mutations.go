package mcpregistry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

var ErrConflict = errors.New("registry entry conflict")
var ErrStageAStructure = errors.New("stage A endpoint structure is immutable")

var tokenSyntax = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$`)

func Token(e Entry) string { return e.UpdatedAt.UTC().Format(time.RFC3339Nano) }

func (s *Service) Create(ctx context.Context, data json.RawMessage) (Entry, error) {
	if issues := s.validator.Validate(data); len(issues) > 0 {
		return Entry{}, &InvalidError{Issues: issues}
	}
	row, err := repo.New(s.db).CreateEntry(ctx, repo.CreateEntryParams{Data: data, StoredRecordLimit: StoredRecordByteLimit})
	if errors.Is(err, pgx.ErrNoRows) {
		return Entry{}, storedSizeError()
	}
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) && pgerr.Code == pgerrcode.UniqueViolation && pgerr.ConstraintName == "mcp_registry_entries_name_key" {
		return Entry{}, ErrConflict
	}
	return entry(row, err)
}

func (s *Service) Save(ctx context.Context, id uuid.UUID, token string, data json.RawMessage) (Entry, error) {
	return s.mutate(ctx, id, token, data, nil)
}
func (s *Service) SetPublished(ctx context.Context, id uuid.UUID, token string, published bool) (Entry, error) {
	return s.mutate(ctx, id, token, nil, &published)
}
func (s *Service) mutate(ctx context.Context, id uuid.UUID, token string, data json.RawMessage, published *bool) (Entry, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Entry{}, fmt.Errorf("begin registry mutation: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	q := repo.New(tx)
	old, err := entry(q.LockEntry(ctx, id))
	if err != nil {
		return Entry{}, err
	}
	parsed, err := time.Parse(time.RFC3339Nano, token)
	if err != nil || !tokenSyntax.MatchString(token) || !parsed.Equal(old.UpdatedAt) {
		return Entry{}, ErrConflict
	}
	var result Entry
	if published == nil {
		if issues := s.validator.Validate(data); len(issues) > 0 {
			return Entry{}, &InvalidError{Issues: issues}
		}
		if recordName(old.Data) != recordName(data) {
			return Entry{}, &InvalidError{Issues: []Issue{{Path: "/server/name", Message: "name is immutable"}}}
		}
		if err := checkStructure(old.Data, data); err != nil {
			return Entry{}, err
		}
		row, updateErr := q.UpdateEntry(ctx, repo.UpdateEntryParams{ID: id, Data: data, StoredRecordLimit: StoredRecordByteLimit})
		if errors.Is(updateErr, pgx.ErrNoRows) {
			return Entry{}, storedSizeError()
		}
		result, err = entry(row, updateErr)
	} else {
		if *published {
			if len(old.Data) > StoredRecordByteLimit {
				return Entry{}, storedSizeError()
			}
			if issues := s.validator.ValidateStored(old.Data); len(issues) > 0 {
				return Entry{}, &InvalidError{Issues: issues}
			}
		}
		result, err = entry(q.SetEntryPublished(ctx, repo.SetEntryPublishedParams{ID: id, Published: *published}))
	}
	if err != nil {
		return Entry{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Entry{}, fmt.Errorf("commit registry mutation: %w", err)
	}
	return result, nil
}

// Freeze all positional endpoint inputs, even when no inherited evidence or
// installations exist. Removing evidence never grants permission to rewrite URLs.
func checkStructure(old, updated json.RawMessage) error {
	a, errA := endpointRemotes(old)
	b, errB := endpointRemotes(updated)
	if errA != nil || errB != nil || len(a) != len(b) {
		return fmt.Errorf("%w: /server/remotes", ErrStageAStructure)
	}
	for i, r := range a {
		for _, key := range []string{"type", "url", "variables"} {
			if !equalJSON(r[key], b[i][key]) {
				return fmt.Errorf("%w: /server/remotes/%d/%s", ErrStageAStructure, i, key)
			}
		}
	}
	return nil
}

// Match the exact JSON keys used by validation and storage, not Go's
// case-insensitive struct fields. Unknown metadata must not shadow endpoints.
func endpointRemotes(data json.RawMessage) ([]map[string]json.RawMessage, error) {
	var root, server map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("decode registry record: %w", err)
	}
	if raw, ok := root["server"]; ok {
		if err := json.Unmarshal(raw, &server); err != nil {
			return nil, fmt.Errorf("decode registry server: %w", err)
		}
	}
	var remotes []map[string]json.RawMessage
	if raw, ok := server["remotes"]; ok {
		if err := json.Unmarshal(raw, &remotes); err != nil {
			return nil, fmt.Errorf("decode registry remotes: %w", err)
		}
	}
	return remotes, nil
}

func equalJSON(a, b json.RawMessage) bool {
	if len(a) == 0 || len(b) == 0 {
		return len(a) == len(b)
	}
	var x, y any
	da, db := json.NewDecoder(bytes.NewReader(a)), json.NewDecoder(bytes.NewReader(b))
	da.UseNumber()
	db.UseNumber()
	if da.Decode(&x) != nil || db.Decode(&y) != nil {
		return false
	}
	x, okX := normalizeNumbers(x)
	y, okY := normalizeNumbers(y)
	return okX && okY && reflect.DeepEqual(x, y)
}

// Normalize decimal coefficients without expanding exponents or using floats.
// Work and storage stay linear in the JSON size. The exponent bound exceeds
// PostgreSQL numeric's range, including compensation by an 8 MiB coefficient.
func normalizeNumbers(value any) (any, bool) {
	switch v := value.(type) {
	case json.Number:
		mantissa, exponent, _ := strings.Cut(strings.ToLower(string(v)), "e")
		var power int64
		if exponent != "" {
			var err error
			power, err = strconv.ParseInt(exponent, 10, 64)
			if err != nil || power < -(16<<20) || power > 16<<20 {
				return nil, false
			}
		}
		negative := strings.HasPrefix(mantissa, "-")
		mantissa = strings.TrimPrefix(mantissa, "-")
		if dot := strings.IndexByte(mantissa, '.'); dot >= 0 {
			power -= int64(len(mantissa) - dot - 1)
			mantissa = mantissa[:dot] + mantissa[dot+1:]
		}
		mantissa = strings.TrimLeft(mantissa, "0")
		if mantissa == "" {
			return json.Number("0"), true
		}
		coefficient := strings.TrimRight(mantissa, "0")
		power += int64(len(mantissa) - len(coefficient))
		if negative {
			coefficient = "-" + coefficient
		}
		return json.Number(coefficient + "e" + strconv.FormatInt(power, 10)), true
	case []any:
		for i, child := range v {
			normalized, ok := normalizeNumbers(child)
			if !ok {
				return nil, false
			}
			v[i] = normalized
		}
	case map[string]any:
		for key, child := range v {
			normalized, ok := normalizeNumbers(child)
			if !ok {
				return nil, false
			}
			v[key] = normalized
		}
	}
	return value, true
}

// Match PostgreSQL's exact, case-sensitive server/name JSON path.
func recordName(raw json.RawMessage) string {
	var doc map[string]json.RawMessage
	if json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	var server map[string]json.RawMessage
	if json.Unmarshal(doc["server"], &server) != nil {
		return ""
	}
	var name string
	if json.Unmarshal(server["name"], &name) != nil {
		return ""
	}
	return name
}

func storedSizeError() error {
	return &InvalidError{Issues: []Issue{{Path: "", Message: "stored record exceeds serialized byte limit"}}}
}
