package attemptlog

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
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
// Non-empty steps keep only attempts that ended at those steps, as the Connection Logs step filter does.
func SignIns(ctx context.Context, ch driver.Conn, s Scope, bucket Bucket, steps []string, now time.Time) ([]SignInBucket, error) {
	attempts, args, err := attemptsSQL(s, now)
	if err != nil {
		return nil, err
	}
	width := bucket.width()
	startOf := fmt.Sprintf("toStartOfInterval(a_started, INTERVAL %d SECOND, 'UTC')", int64(width/time.Second))
	// Placeholders bind in text order: the WITH list comes before the subquery.
	signIns, renewals := groupStrings(signInGroups), groupStrings(renewalGroups)
	if len(steps) > 0 {
		keep := func(g string) bool { return !slices.Contains(steps, g) }
		signIns, renewals = slices.DeleteFunc(signIns, keep), slices.DeleteFunc(renewals, keep)
	}
	args = append(append([]any{signIns, renewals}, args...), string(OutcomeInProgress))
	query := `
	WITH has(?, d_step) AS is_sign_in, has(?, d_step) AS is_renewal
	SELECT ` + startOf + ` AS b_start,
		sumIf(a_weight, is_sign_in AND d_outcome = '` + string(OutcomeConnected) + `') AS b_sign_in_ok,
		sumIf(a_weight, is_sign_in AND d_outcome != '` + string(OutcomeConnected) + `') AS b_sign_in_not,
		sumIf(a_weight, is_renewal AND d_outcome = '` + string(OutcomeConnected) + `') AS b_renewal_ok,
		sumIf(a_weight, is_renewal AND d_outcome != '` + string(OutcomeConnected) + `') AS b_renewal_not
	FROM (` + attempts + `)
	WHERE d_outcome != ? AND (is_sign_in OR is_renewal)
	GROUP BY b_start
	ORDER BY b_start` + readSettings
	rows, err := queryAll[SignInBucket](ctx, ch, "sign-in summary", query, args...)
	if err != nil {
		return nil, err
	}
	found := make(map[int64]SignInBucket, len(rows))
	for _, b := range rows {
		found[b.Start.UTC().Unix()] = b
	}

	first := s.From.UTC().Truncate(width)
	var buckets []SignInBucket
	for start := first; start.Before(s.To); start = start.Add(width) {
		b := found[start.Unix()]
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
	return queryAll[DeniedClient](ctx, ch, "denied clients", query, args...)
}

func groupStrings(groups []StepGroup) []string {
	out := make([]string, len(groups))
	for i, g := range groups {
		out[i] = string(g)
	}
	return out
}

// TimeCount is the weighted attempts that started in one interval.
type TimeCount struct {
	// Start is the interval's start.
	Start time.Time `ch:"t_start"`

	// Attempts is weighted attempts.
	Attempts uint64 `ch:"t_attempts"`
}

// CountOver counts the attempts matching the filters per interval of width, every interval in the range included.
func CountOver(ctx context.Context, ch driver.Conn, s Scope, f Filters, width time.Duration, now time.Time) ([]TimeCount, error) {
	if width < time.Second {
		return nil, errors.New("interval must be at least a second")
	}
	attempts, args, err := attemptsSQL(s, now)
	if err != nil {
		return nil, err
	}
	where, filterArgs := filterSQL(f)
	args = append(args, filterArgs...)
	interval := fmt.Sprintf("INTERVAL %d SECOND", int64(width/time.Second))
	query := `
	SELECT toDateTime(toStartOfInterval(a_started, ` + interval + `), 'UTC') AS t_start, toUInt64(sum(a_weight)) AS t_attempts
	FROM (` + attempts + `)
	WHERE ` + where + `
	GROUP BY t_start` + readSettings
	rows, err := queryAll[TimeCount](ctx, ch, "attempts over time", query, args...)
	if err != nil {
		return nil, err
	}
	found := make(map[int64]uint64, len(rows))
	for _, r := range rows {
		found[r.Start.Unix()] = r.Attempts
	}
	var out []TimeCount
	for start := s.From.UTC().Truncate(width); start.Before(s.To); start = start.Add(width) {
		out = append(out, TimeCount{Start: start, Attempts: found[start.Unix()]})
	}
	return out, nil
}

// maxReasons bounds the reasons CountByReason returns.
const maxReasons = 100

// CountByReason counts the attempts matching the filters per reason, most first.
func CountByReason(ctx context.Context, ch driver.Conn, s Scope, f Filters, now time.Time) ([]FacetValue, error) {
	attempts, args, err := attemptsSQL(s, now)
	if err != nil {
		return nil, err
	}
	where, filterArgs := filterSQL(f)
	args = append(append(args, filterArgs...), maxReasons)
	type reasonCount struct {
		Reason   string `ch:"x_reason"`
		Attempts uint64 `ch:"x_attempts"`
	}
	rows, err := queryAll[reasonCount](ctx, ch, "attempts by reason", `
	SELECT d_reason AS x_reason, toUInt64(sum(a_weight)) AS x_attempts
	FROM (`+attempts+`)
	WHERE `+where+` AND d_reason != ''
	GROUP BY x_reason
	ORDER BY x_attempts DESC, x_reason
	LIMIT ?`+readSettings, args...)
	if err != nil {
		return nil, err
	}
	out := make([]FacetValue, len(rows))
	for i, r := range rows {
		out[i] = FacetValue{Value: r.Reason, Attempts: r.Attempts}
	}
	return out, nil
}
