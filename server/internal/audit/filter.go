package audit

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

// MaxExactFilterBytes bounds exact-match identifiers and open-ended vocabulary.
const MaxExactFilterBytes = 256

// MaxExactTimestampBytes bounds RFC3339 timestamps before parsing.
const MaxExactTimestampBytes = 64

// ExactFilter contains optional exact-match audit filters. Empty strings mean absent.
// Cursor validation belongs to the caller because feeds have different cursor formats.
type ExactFilter struct {
	ProjectID     string
	ActorID       string
	Action        string
	SubjectType   string
	SubjectID     string
	ActingSurface string
	From          string
	To            string
}

// ParsedExactFilter contains the typed project and half-open time bounds.
// Other exact-match values remain unchanged in the input after validation.
type ParsedExactFilter struct {
	ProjectID uuid.NullUUID
	From      *time.Time
	To        *time.Time
}

// ParseExactFilter validates unpadded, byte-bounded exact values, a canonical
// project UUID, and optional RFC3339 timestamps with from before to. It does not
// authorize project ownership, restrict future vocabulary, or validate cursors.
func ParseExactFilter(input ExactFilter) (ParsedExactFilter, error) {
	var result ParsedExactFilter
	for name, value := range map[string]string{
		"actor_id": input.ActorID, "action": input.Action, "subject_type": input.SubjectType,
		"subject_id": input.SubjectID, "acting_surface": input.ActingSurface, "from": input.From, "to": input.To,
	} {
		limit := MaxExactFilterBytes
		if name == "from" || name == "to" {
			limit = MaxExactTimestampBytes
		}
		if len(value) > limit || value != strings.TrimSpace(value) {
			return result, fmt.Errorf("provide an unpadded %s up to %d bytes", name, limit)
		}
	}
	if input.ProjectID != "" {
		id, err := uuid.Parse(input.ProjectID)
		if err != nil || id.String() != input.ProjectID {
			return result, errors.New("provide a canonical project UUID")
		}
		result.ProjectID = uuid.NullUUID{UUID: id, Valid: true}
	}
	from, to, err := conv.ParseOptionalTimeWindow(conv.PtrEmpty(input.From), conv.PtrEmpty(input.To))
	if err != nil {
		return result, errors.New("provide valid RFC3339 timestamps with from before to")
	}
	result.From, result.To = from, to
	return result, nil
}
