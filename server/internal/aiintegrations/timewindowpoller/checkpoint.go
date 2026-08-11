package timewindowpoller

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

const pollCheckpointVersion = 1

type pollCheckpointJSON PollCheckpoint

// PollCheckpoint is the durable resume marker for time-window sync schedules.
// A completed checkpoint only carries Watermark. A partial page checkpoint
// also carries the fixed request window and the next provider page token.
type PollCheckpoint struct {
	V           int       `json:"v"`
	Watermark   time.Time `json:"watermark"`
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`
	PageToken   string    `json:"page_token,omitempty"`
}

func CompletedCheckpoint(watermark time.Time) PollCheckpoint {
	return PollCheckpoint{
		V:           pollCheckpointVersion,
		Watermark:   utcIfSet(watermark),
		WindowStart: time.Time{},
		WindowEnd:   time.Time{},
		PageToken:   "",
	}
}

func PartialCheckpoint(watermark, windowStart, windowEnd time.Time, pageToken string) PollCheckpoint {
	return PollCheckpoint{
		V:           pollCheckpointVersion,
		Watermark:   utcIfSet(watermark),
		WindowStart: utcIfSet(windowStart),
		WindowEnd:   utcIfSet(windowEnd),
		PageToken:   pageToken,
	}
}

func (c PollCheckpoint) MarshalText() ([]byte, error) {
	c = c.normalized()
	if err := c.validate(); err != nil {
		return nil, err
	}

	raw, err := json.Marshal(pollCheckpointJSON(c))
	if err != nil {
		return nil, fmt.Errorf("marshal poll checkpoint: %w", err)
	}

	out := make([]byte, base64.StdEncoding.EncodedLen(len(raw)))
	base64.StdEncoding.Encode(out, raw)
	return out, nil
}

func (c *PollCheckpoint) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		*c = CompletedCheckpoint(time.Time{})
		return nil
	}

	raw := make([]byte, base64.StdEncoding.DecodedLen(len(text)))
	n, err := base64.StdEncoding.Decode(raw, text)
	if err != nil {
		return fmt.Errorf("decode poll checkpoint: %w", err)
	}

	var decoded pollCheckpointJSON
	if err := json.Unmarshal(raw[:n], &decoded); err != nil {
		return fmt.Errorf("unmarshal poll checkpoint: %w", err)
	}
	checkpoint := PollCheckpoint(decoded)
	checkpoint = checkpoint.normalized()
	if err := checkpoint.validate(); err != nil {
		return err
	}

	*c = checkpoint
	return nil
}

func DecodeCheckpoint(encoded string, legacyWatermark time.Time) (PollCheckpoint, error) {
	if encoded == "" {
		return CompletedCheckpoint(legacyWatermark), nil
	}

	var checkpoint PollCheckpoint
	if err := checkpoint.UnmarshalText([]byte(encoded)); err != nil {
		return emptyCheckpoint(), err
	}
	return checkpoint, nil
}

func (c PollCheckpoint) Partial() bool {
	return c.PageToken != ""
}

func (c PollCheckpoint) normalized() PollCheckpoint {
	if c.V == 0 {
		c.V = pollCheckpointVersion
	}
	c.Watermark = utcIfSet(c.Watermark)
	c.WindowStart = utcIfSet(c.WindowStart)
	c.WindowEnd = utcIfSet(c.WindowEnd)
	return c
}

func (c PollCheckpoint) validate() error {
	if c.V != pollCheckpointVersion {
		return fmt.Errorf("unsupported poll checkpoint version %d", c.V)
	}
	if c.PageToken == "" {
		return nil
	}
	if c.WindowStart.IsZero() || c.WindowEnd.IsZero() {
		return fmt.Errorf("partial poll checkpoint missing window bounds")
	}
	if !c.WindowStart.Before(c.WindowEnd) {
		return fmt.Errorf("partial poll checkpoint window end must be after start")
	}
	return nil
}

func utcIfSet(t time.Time) time.Time {
	if t.IsZero() {
		return time.Time{}
	}
	return t.UTC()
}

func emptyCheckpoint() PollCheckpoint {
	return PollCheckpoint{
		V:           0,
		Watermark:   time.Time{},
		WindowStart: time.Time{},
		WindowEnd:   time.Time{},
		PageToken:   "",
	}
}
