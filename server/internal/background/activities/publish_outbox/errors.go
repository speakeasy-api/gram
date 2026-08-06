package publish_outbox

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

// isPermanent reports whether a publish failure is worth retrying. A permanent
// failure is dead-lettered immediately instead of burning its retry budget.
//
// Only failures this package marks with oops.Permanent qualify. Notably,
// topics.ErrUnknownTopic is NOT permanent: writers validate topic names
// against the same generated registry, so an unknown topic at publish time
// means this drainer's binary predates the writer's — a rolling deploy in
// progress. Retrying carries the row across the skew window; a drainer that
// stays stale anyway still dead-letters it through the attempts cap.
// Everything else that reaches here (Pub/Sub unavailable, deadline exceeded,
// quota) is transient by nature: the relay talks to a single service over the
// network, so there is no equivalent of Svix's permanent 4xx.
func isPermanent(err error) bool {
	if err == nil {
		return false
	}

	return errors.Is(err, oops.ErrPermanent)
}

// unmarshalAttributes decodes the stored Pub/Sub attribute map. A row with an
// undecodable map cannot be republished faithfully — its trace context and any
// filter discriminators are gone — so this is permanent rather than publishing
// a message that would silently lose them.
func unmarshalAttributes(raw []byte) (map[string]string, error) {
	if len(raw) == 0 {
		return map[string]string{}, nil
	}

	var attrs map[string]string
	if err := json.Unmarshal(raw, &attrs); err != nil {
		return nil, fmt.Errorf("unmarshal attributes: %w", err)
	}

	return attrs, nil
}
