package platformmcp

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	usersessionsgen "github.com/speakeasy-api/gram/server/gen/user_sessions"
)

// gatewayConnectionTrendDefaultWindow is the window a trend read covers when
// from is omitted.
const gatewayConnectionTrendDefaultWindow = 7 * 24 * time.Hour

var errGatewayConnectionTrendInvalid = errors.New("invalid gateway connection trend request")

// GatewayConnectionTrendInput names one gateway and the window to count.
type GatewayConnectionTrendInput struct {
	// ProjectID is the exact project the gateway belongs to.
	ProjectID string `json:"project_id" jsonschema:"ID of the project the gateway belongs to"`

	// GatewayID is the exact gateway whose connections are counted.
	GatewayID string `json:"gateway_id" jsonschema:"exact gateway ID"`

	// From is the window start; omitted means seven days before to.
	From string `json:"from,omitempty" jsonschema:"window start, RFC 3339; defaults to 7 days before to; at most 31 days before to"`

	// To is the window end, excluded; omitted means now.
	To string `json:"to,omitempty" jsonschema:"window end, RFC 3339, excluded; defaults to now"`

	// Bucket is hour or day; omitted means hour for windows of up to seven
	// days and day otherwise.
	Bucket string `json:"bucket,omitempty" jsonschema:"hour or day, aligned to UTC; defaults to hour for windows up to 7 days, day otherwise"`
}

// GatewayConnectionTrendBucket is one bucket of a gateway's connection trend.
type GatewayConnectionTrendBucket struct {
	// Start is the bucket start, inclusive.
	Start string `json:"start"`

	// End is the bucket end, exclusive.
	End string `json:"end"`

	// Created counts credentials first issued during the bucket.
	Created int64 `json:"created"`

	// Revoked counts credentials revoked before their deadline during the
	// bucket.
	Revoked int64 `json:"revoked"`

	// Active counts credentials unexpired, with no recorded revocation,
	// immediately before the bucket's end.
	Active int64 `json:"active" jsonschema:"credentials unexpired with no recorded revocation immediately before the bucket end; a recorded lifetime, not request activity, permission or tool discovery"`
}

// GatewayConnectionTrendOutput is one gateway's connection trend.
type GatewayConnectionTrendOutput struct {
	// ProjectID is the project the trend was read in.
	ProjectID string `json:"project_id"`

	// GatewayID is the gateway the trend was read for.
	GatewayID string `json:"gateway_id"`

	// From is the window start.
	From string `json:"from"`

	// To is the window end, excluded.
	To string `json:"to"`

	// Bucket is the bucket width.
	Bucket string `json:"bucket"`

	// Buckets is every bucket in the window, empty ones included.
	Buckets []GatewayConnectionTrendBucket `json:"buckets"`

	// ActiveAtStart counts credentials active immediately before from.
	ActiveAtStart int64 `json:"active_at_start"`

	// Created counts credentials first issued in the window.
	Created int64 `json:"created"`

	// Revoked counts credentials revoked in the window.
	Revoked int64 `json:"revoked"`

	// ActiveAtEnd counts credentials active immediately before to.
	ActiveAtEnd int64 `json:"active_at_end"`

	// ExcludedLegacyRows counts session records left out because they were
	// written before lineage was kept.
	ExcludedLegacyRows int64 `json:"excluded_legacy_rows" jsonschema:"session records, not connections, left out because they predate lineage"`

	// UnattributedRows counts session records on the gateway's current
	// sign-in server that record no gateway; absent without a sign-in server.
	UnattributedRows *int64 `json:"unattributed_rows,omitempty" jsonschema:"session records, not connections, on the gateway's current sign-in server that record no gateway"`
}

// Trend counts a gateway's connections through the session service, which
// authorizes the project and the gateway and scopes the read.
func (s *GatewayConnectionsService) Trend(ctx context.Context, principal Principal, input GatewayConnectionTrendInput) (GatewayConnectionTrendOutput, error) {
	var zero GatewayConnectionTrendOutput
	if !s.valid() {
		return zero, ErrUnavailable
	}
	projectID, err := uuid.Parse(strings.TrimSpace(input.ProjectID))
	if err != nil || projectID == uuid.Nil {
		return zero, errGatewayConnectionTrendInvalid
	}
	gatewayID, err := uuid.Parse(strings.TrimSpace(input.GatewayID))
	if err != nil || gatewayID == uuid.Nil {
		return zero, errGatewayConnectionTrendInvalid
	}
	to := time.Now().UTC()
	if input.To != "" {
		if to, err = time.Parse(time.RFC3339Nano, input.To); err != nil {
			return zero, errGatewayConnectionTrendInvalid
		}
	}
	from := to.Add(-gatewayConnectionTrendDefaultWindow)
	if input.From != "" {
		if from, err = time.Parse(time.RFC3339Nano, input.From); err != nil {
			return zero, errGatewayConnectionTrendInvalid
		}
	}
	bucket := input.Bucket
	if bucket == "" {
		bucket = "hour"
		if to.Sub(from) > gatewayConnectionTrendDefaultWindow {
			bucket = "day"
		}
	}

	ctx, project, err := s.inProject(ctx, principal, projectID)
	if err != nil {
		return zero, err
	}
	trend, err := s.sessions.GetTrend(ctx, &usersessionsgen.GetTrendPayload{
		MetaMcpServerID: gatewayID.String(),
		From:            from.UTC().Format(time.RFC3339Nano),
		To:              to.UTC().Format(time.RFC3339Nano),
		Bucket:          bucket,
		SessionToken:    nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	if err != nil {
		return zero, gatewayConnectionsError(err)
	}

	out := GatewayConnectionTrendOutput{
		ProjectID: project.ID.String(), GatewayID: gatewayID.String(),
		From: trend.From, To: trend.To, Bucket: trend.Bucket,
		Buckets:       make([]GatewayConnectionTrendBucket, 0, len(trend.Buckets)),
		ActiveAtStart: trend.ActiveAtStart, Created: trend.Created, Revoked: trend.Revoked, ActiveAtEnd: trend.ActiveAtEnd,
		ExcludedLegacyRows: trend.ExcludedLegacyRows, UnattributedRows: trend.UnattributedRows,
	}
	for _, b := range trend.Buckets {
		out.Buckets = append(out.Buckets, GatewayConnectionTrendBucket{Start: b.Start, End: b.End, Created: b.Created, Revoked: b.Revoked, Active: b.Active})
	}
	return out, nil
}
