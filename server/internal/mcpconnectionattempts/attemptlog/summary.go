package attemptlog

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// Bucket is a bucket size for the sign-in summary.
type Bucket string

const (
	BucketHour     Bucket = "hour"
	BucketSixHours Bucket = "six_hours"
	BucketDay      Bucket = "day"
)

// width is the bucket's length. Buckets start on UTC boundaries of it.
func (b Bucket) width() time.Duration {
	switch b {
	case BucketSixHours:
		return 6 * time.Hour
	case BucketDay:
		return 24 * time.Hour
	case BucketHour:
		return time.Hour
	default:
		return time.Hour
	}
}

// hourlyRangeLimit is the longest range that defaults to hourly buckets: two days of hours fit a chart.
const hourlyRangeLimit = 48 * time.Hour

// DefaultBucket is hour for ranges up to 48 hours, otherwise day.
func DefaultBucket(from, to time.Time) Bucket {
	if to.Sub(from) <= hourlyRangeLimit {
		return BucketHour
	}
	return BucketDay
}

// SignInBucket counts sign-ins and renewals that started in one bucket.
type SignInBucket struct {
	// Start is the bucket start, UTC.
	Start time.Time `ch:"b_start"`

	// SignInsSucceeded and the rest are weighted attempts.
	SignInsSucceeded uint64 `ch:"b_sign_in_ok"`

	// SignInsDidNot are first sign-ins that did not connect.
	SignInsDidNot uint64 `ch:"b_sign_in_not"`

	// RenewalsSucceeded are renewals that connected.
	RenewalsSucceeded uint64 `ch:"b_renewal_ok"`

	// RenewalsDidNot are renewals that did not connect.
	RenewalsDidNot uint64 `ch:"b_renewal_not"`
}

// SignIns counts sign-ins and renewals per bucket, every bucket in the range included.
func SignIns(ctx context.Context, ch driver.Conn, s Scope, bucket Bucket, now time.Time) ([]SignInBucket, error) {
	attempts, args, err := attemptsSQL(s, now)
	if err != nil {
		return nil, err
	}
	width := bucket.width()
	startOf := fmt.Sprintf("toStartOfInterval(a_started, INTERVAL %d SECOND, 'UTC')", int64(width/time.Second))
	signIns, renewals := groupStrings(signInGroups), groupStrings(renewalGroups)
	// Placeholders bind in text order: the select list comes before the subquery.
	args = append(append([]any{signIns, signIns, renewals, renewals}, args...), string(OutcomeInProgress), signIns, renewals)
	query := `
	SELECT ` + startOf + ` AS b_start,
		sumIf(a_weight, has(?, d_step) AND d_outcome = '` + string(OutcomeConnected) + `') AS b_sign_in_ok,
		sumIf(a_weight, has(?, d_step) AND d_outcome != '` + string(OutcomeConnected) + `') AS b_sign_in_not,
		sumIf(a_weight, has(?, d_step) AND d_outcome = '` + string(OutcomeConnected) + `') AS b_renewal_ok,
		sumIf(a_weight, has(?, d_step) AND d_outcome != '` + string(OutcomeConnected) + `') AS b_renewal_not
	FROM (` + attempts + `)
	WHERE d_outcome != ? AND (has(?, d_step) OR has(?, d_step))
	GROUP BY b_start
	ORDER BY b_start` + readSettings
	result, err := ch.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query sign-in summary: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return result.Close() })

	found := map[int64]SignInBucket{}
	for result.Next() {
		var b SignInBucket
		if err := result.ScanStruct(&b); err != nil {
			return nil, fmt.Errorf("scan sign-in summary: %w", err)
		}
		found[b.Start.UTC().Unix()] = b
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("read sign-in summary: %w", err)
	}

	first := s.From.UTC().Truncate(width)
	var buckets []SignInBucket
	for start := first; start.Before(s.To); start = start.Add(width) {
		b, ok := found[start.Unix()]
		if !ok {
			b = SignInBucket{Start: start, SignInsSucceeded: 0, SignInsDidNot: 0, RenewalsSucceeded: 0, RenewalsDidNot: 0}
		}
		b.Start = start
		buckets = append(buckets, b)
	}
	return buckets, nil
}

// DeniedClient is an app refused at sign-in, per reason.
type DeniedClient struct {
	// App is the app family, empty when not on the list.
	App string `ch:"x_app"`

	// AppKey is a stored app key of the app.
	AppKey string `ch:"x_app_key"`

	// Reason is the reason code.
	Reason string `ch:"x_reason"`

	// Attempts is weighted attempts.
	Attempts uint64 `ch:"x_attempts"`

	// People is distinct known people.
	People uint64 `ch:"x_people"`

	// Networks is distinct networks among attempts with no known person.
	Networks uint64 `ch:"x_networks"`

	// LastAt is the latest refusal.
	LastAt time.Time `ch:"x_last"`
}

// Denied lists apps refused at sign-in for being the app they are, most recent first.
func Denied(ctx context.Context, ch driver.Conn, s Scope, now time.Time) ([]DeniedClient, error) {
	attempts, args, err := attemptsSQL(s, now)
	if err != nil {
		return nil, err
	}
	args = append(args, string(OutcomeBlocked), groupStrings(deniedGroups), deniedReasons, maxDeniedClients)
	query := `
	SELECT d_app AS x_app, a_app_key AS x_app_key, d_reason AS x_reason, sum(a_weight) AS x_attempts,
		toUInt64(uniqExactIf(a_subject, a_subject != '')) AS x_people,
		toUInt64(uniqExactIf(a_device, a_subject = '' AND a_device != '')) AS x_networks,
		max(d_at) AS x_last
	FROM (` + attempts + `)
	WHERE d_outcome = ? AND has(?, d_step) AND has(?, d_reason)
	GROUP BY x_app, x_app_key, x_reason
	ORDER BY x_last DESC
	LIMIT ?` + readSettings
	result, err := ch.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query denied clients: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return result.Close() })

	var clients []DeniedClient
	for result.Next() {
		var client DeniedClient
		if err := result.ScanStruct(&client); err != nil {
			return nil, fmt.Errorf("scan denied client: %w", err)
		}
		clients = append(clients, client)
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("read denied clients: %w", err)
	}
	return clients, nil
}

func groupStrings(groups []StepGroup) []string {
	out := make([]string, len(groups))
	for i, g := range groups {
		out[i] = string(g)
	}
	return out
}
