package memory

import (
	"encoding/json"
	"time"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/threadsource"
)

// provenance captures, for tracing, the conversational context a memory write
// came from: the origin thread's source surface, the external user who said
// it, the thread's correlation id, and when it was recorded. It exists to
// answer "why is the assistant remembering this?" — note that it records the
// conversation the write happened in, not necessarily the true origin of the
// fact (the assistant may have learned it from e.g. a web page mid-turn).
// All fields are optional: writes that happen outside an assistant thread
// carry none.
type provenance struct {
	Kind          *string
	UserID        *string
	CorrelationID *string
	Timestamp     *time.Time
}

// extractProvenance maps an origin thread's source surface onto memory
// provenance. The thread's correlation id is stored verbatim for every source
// kind — it uniformly encodes the source conversation (e.g.
// "slack:T123:C456:789.012") regardless of surface. A speaker is only
// recorded where one exists: slack and dashboard source refs carry the
// user_id of the human driving the turn; cron and wake turns are automated
// and have none.
//
// The kind set (threadsource.Kind*) is an open enum: source_kind is TEXT with
// no database constraint, and extractProvenance records any kind it is handed
// verbatim. Adding a new source surface (e.g. email) therefore requires no
// migration — its memories carry kind and correlation id only, until a case
// is added here to extract its speaker.
//
// Timestamp is the time of write: the triggering event's own timestamp is not
// available at Remember() time (the tool call carries only the thread
// principal), and the write happens within the same turn as the event, so the
// write time is a faithful proxy.
func extractProvenance(kind string, correlationID string, sourceRefJSON []byte) provenance {
	now := time.Now()
	out := provenance{
		Kind:          conv.PtrEmpty(kind),
		UserID:        nil,
		CorrelationID: conv.PtrEmpty(correlationID),
		Timestamp:     &now,
	}

	switch kind {
	case threadsource.KindSlack, threadsource.KindDashboard:
		// Both surfaces carry the speaking user under the same key in their
		// source ref payloads.
		var ref struct {
			UserID string `json:"user_id"`
		}
		if err := json.Unmarshal(sourceRefJSON, &ref); err == nil {
			out.UserID = conv.PtrEmpty(ref.UserID)
		}
	}

	return out
}
