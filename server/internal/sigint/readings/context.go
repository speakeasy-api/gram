// Package readings defines evaluation provenance and logical reading identity.
package readings

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// TriggerKind identifies why an evaluation was requested.
type TriggerKind string

const (
	// TriggerUnspecified is invalid for published evaluations.
	TriggerUnspecified TriggerKind = ""

	// TriggerStream evaluates a source event through its automatic subscription.
	TriggerStream TriggerKind = "stream"

	// TriggerManual evaluates targets within an explicitly requested run.
	TriggerManual TriggerKind = "manual"
)

// Context is immutable across attempts of a logical evaluation.
type Context struct {
	// TriggerKind distinguishes automatic evaluation from an explicit run.
	TriggerKind TriggerKind

	// RunID is nonzero for manual runs and zero for streaming evaluations.
	RunID uuid.UUID

	// RequestedAt anchors retention and partitioning independently of source time.
	RequestedAt time.Time
}

// readingNamespace is the stable UUIDv5 namespace for sensor readings.
var readingNamespace = uuid.NewSHA1(uuid.NameSpaceURL, []byte("gram:sigint:reading"))

// ParseContext validates the explicit provenance carried by an evaluation message.
func ParseContext(trigger, run, requestedAt string) (Context, error) {
	c := Context{TriggerKind: TriggerKind(trigger), RunID: uuid.Nil, RequestedAt: time.Time{}}
	var err error
	if run != "" {
		c.RunID, err = uuid.Parse(run)
		if err != nil {
			return c, fmt.Errorf("parse evaluation run ID: %w", err)
		}
	}
	c.RequestedAt, err = time.Parse(time.RFC3339Nano, requestedAt)
	if err != nil {
		return c, fmt.Errorf("parse evaluation request time: %w", err)
	}
	c.RequestedAt = c.RequestedAt.UTC()
	return c, c.Validate()
}

// Validate checks trigger/run consistency and storage-compatible request time.
func (c Context) Validate() error {
	switch c.TriggerKind {
	case TriggerStream:
		if c.RunID != uuid.Nil {
			return fmt.Errorf("streaming evaluation has a run ID")
		}
	case TriggerManual:
		if c.RunID == uuid.Nil {
			return fmt.Errorf("manual evaluation requires a run ID")
		}
	case TriggerUnspecified:
		return fmt.Errorf("evaluation trigger is required")
	default:
		return fmt.Errorf("unknown evaluation trigger")
	}
	// firstRequestYear excludes pre-epoch values unsupported by DateTime TTLs.
	const firstRequestYear = 1970
	// lastRequestYear leaves two years of headroom before DateTime's 2106 limit.
	const lastRequestYear = 2104
	year := c.RequestedAt.UTC().Year()
	if year < firstRequestYear || year >= lastRequestYear {
		return fmt.Errorf("unsupported evaluation request time")
	}
	return nil
}

// ReadingID derives a sensor result's UUIDv5 within a validated evaluation scope.
// Request time, attempt, content and configuration do not change the identity.
func (c Context) ReadingID(organization, project, kind, event, sensor string) uuid.UUID {
	identity := []string{"sigint-reading", organization, project, string(c.TriggerKind)}
	if c.TriggerKind == TriggerManual {
		identity = append(identity, c.RunID.String())
	}
	identity = append(identity, kind, event, sensor)
	encoded, _ := json.Marshal(identity)
	return uuid.NewSHA1(readingNamespace, encoded)
}
