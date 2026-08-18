package openrouter

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const maxSpendResponseBytes = 2 << 20

// DailySpendSource identifies the OpenRouter endpoint that supplied a result.
type DailySpendSource string

const (
	// DailySpendSourceAnalytics is the arbitrary-range analytics query endpoint.
	DailySpendSourceAnalytics DailySpendSource = "analytics"

	// DailySpendSourceActivity is the per-day activity fallback endpoint.
	DailySpendSourceActivity DailySpendSource = "activity"
)

// DailySpendDay is the exact USD spend attributed to one completed UTC day.
type DailySpendDay struct {
	// Day is midnight UTC for the reported day.
	Day time.Time

	// SpendUSD is a non-negative, base-10 decimal without exponent notation.
	SpendUSD string
}

// DailySpendResult contains spend rows from one upstream source.
type DailySpendResult struct {
	// Days contains at most one row for each day in the requested range.
	Days []DailySpendDay

	// Source identifies whether the primary or fallback endpoint supplied Days.
	Source DailySpendSource
}

// SpendClient reads daily spend for platform-managed OpenRouter keys.
type SpendClient interface {
	// GetDailySpend returns spend in the half-open UTC range [startDay, endDay).
	GetDailySpend(ctx context.Context, keyHash string, startDay, endDay time.Time) (DailySpendResult, error)
}

var _ SpendClient = (*OpenRouter)(nil)

type analyticsSpendRequest struct {
	Metrics     []string               `json:"metrics"`
	Granularity string                 `json:"granularity"`
	Filters     []analyticsSpendFilter `json:"filters"`
	TimeRange   analyticsTimeRange     `json:"time_range"`
	Limit       int                    `json:"limit"`
}

type analyticsSpendFilter struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

type analyticsTimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type analyticsSpendResponse struct {
	Data *struct {
		Data     *[]analyticsSpendRow `json:"data"`
		Metadata *struct {
			Truncated bool `json:"truncated"`
		} `json:"metadata"`
		Warnings []json.RawMessage `json:"warnings"`
	} `json:"data"`
}

type analyticsSpendRow struct {
	DateDay      string          `json:"date__day"`
	CreatedAtDay string          `json:"created_at__day"`
	TotalUsage   json.RawMessage `json:"total_usage"`
}

type activitySpendResponse struct {
	Data *[]activitySpendRow `json:"data"`
}

type activitySpendRow struct {
	Date  string          `json:"date"`
	Usage json.RawMessage `json:"usage"`
}

type spendTransportError struct {
	cause error
}

func (e *spendTransportError) Error() string {
	return "OpenRouter request transport failed"
}

func (e *spendTransportError) Unwrap() error {
	return e.cause
}

// GetDailySpend first uses the arbitrary-range analytics API, then falls back
// to the retained per-day activity API when analytics cannot be trusted.
func (o *OpenRouter) GetDailySpend(ctx context.Context, keyHash string, startDay, endDay time.Time) (DailySpendResult, error) {
	startDay, endDay, err := validateSpendRequest(keyHash, startDay, endDay)
	if err != nil {
		return DailySpendResult{}, err
	}

	days, analyticsErr := o.getAnalyticsDailySpend(ctx, keyHash, startDay, endDay)
	if analyticsErr == nil {
		return DailySpendResult{Days: days, Source: DailySpendSourceAnalytics}, nil
	}

	days, activityErr := o.getActivityDailySpend(ctx, keyHash, startDay, endDay)
	if activityErr != nil {
		return DailySpendResult{}, errors.Join(
			fmt.Errorf("query OpenRouter analytics: %w", analyticsErr),
			fmt.Errorf("query OpenRouter activity fallback: %w", activityErr),
		)
	}

	return DailySpendResult{Days: days, Source: DailySpendSourceActivity}, nil
}

func validateSpendRequest(keyHash string, startDay, endDay time.Time) (time.Time, time.Time, error) {
	if len(keyHash) != 64 {
		return time.Time{}, time.Time{}, errors.New("query OpenRouter daily spend: key hash must be 64 hexadecimal characters")
	}
	if _, err := hex.DecodeString(keyHash); err != nil {
		return time.Time{}, time.Time{}, errors.New("query OpenRouter daily spend: key hash must be 64 hexadecimal characters")
	}

	startUTC := startDay.UTC()
	endUTC := endDay.UTC()
	if !startUTC.Equal(startOfUTCDay(startUTC)) || !endUTC.Equal(startOfUTCDay(endUTC)) {
		return time.Time{}, time.Time{}, errors.New("query OpenRouter daily spend: range bounds must be UTC midnights")
	}
	if !startUTC.Before(endUTC) {
		return time.Time{}, time.Time{}, errors.New("query OpenRouter daily spend: start day must precede end day")
	}

	return startUTC, endUTC, nil
}

func (o *OpenRouter) getAnalyticsDailySpend(ctx context.Context, keyHash string, startDay, endDay time.Time) ([]DailySpendDay, error) {
	requestBody := analyticsSpendRequest{
		Metrics:     []string{"total_usage"},
		Granularity: "day",
		Filters: []analyticsSpendFilter{{
			Field:    "api_key_id",
			Operator: "eq",
			Value:    keyHash,
		}},
		TimeRange: analyticsTimeRange{Start: startDay, End: endDay},
		Limit:     int(endDay.Sub(startDay) / (24 * time.Hour)),
	}

	var response analyticsSpendResponse
	if err := o.doSpendRequest(ctx, http.MethodPost, "/v1/analytics/query", requestBody, &response); err != nil {
		return nil, err
	}
	if response.Data == nil || response.Data.Data == nil || response.Data.Metadata == nil {
		return nil, errors.New("analytics response omitted required fields")
	}
	if response.Data.Metadata.Truncated {
		return nil, errors.New("analytics response was truncated")
	}
	if len(response.Data.Warnings) > 0 {
		return nil, errors.New("analytics response contained warnings")
	}

	days := make([]DailySpendDay, 0, len(*response.Data.Data))
	seen := make(map[time.Time]struct{}, len(*response.Data.Data))
	for _, row := range *response.Data.Data {
		day, err := parseAnalyticsDay(row.DateDay, row.CreatedAtDay)
		if err != nil {
			return nil, err
		}
		if day.Before(startDay) || !day.Before(endDay) {
			return nil, errors.New("analytics response contained a day outside the requested range")
		}
		if _, duplicate := seen[day]; duplicate {
			return nil, errors.New("analytics response contained duplicate daily rows")
		}

		spend, err := parseSpendAmount(row.TotalUsage)
		if err != nil {
			return nil, fmt.Errorf("parse analytics total usage: %w", err)
		}
		seen[day] = struct{}{}
		days = append(days, DailySpendDay{Day: day, SpendUSD: formatSpendAmount(spend)})
	}
	slices.SortFunc(days, func(left, right DailySpendDay) int {
		return left.Day.Compare(right.Day)
	})

	return days, nil
}

func (o *OpenRouter) getActivityDailySpend(ctx context.Context, keyHash string, startDay, endDay time.Time) ([]DailySpendDay, error) {
	days := make([]DailySpendDay, 0, int(endDay.Sub(startDay)/(24*time.Hour)))
	var requestErrors []error
	for day := startDay; day.Before(endDay); day = day.AddDate(0, 0, 1) {
		endpoint := "/v1/activity?" + url.Values{
			"api_key_hash": []string{keyHash},
			"date":         []string{day.Format(time.DateOnly)},
		}.Encode()

		var response activitySpendResponse
		if err := o.doSpendRequest(ctx, http.MethodGet, endpoint, nil, &response); err != nil {
			requestErrors = append(requestErrors, fmt.Errorf("fetch activity for %s: %w", day.Format(time.DateOnly), err))
			continue
		}
		if response.Data == nil {
			requestErrors = append(requestErrors, fmt.Errorf("parse activity for %s: response omitted required fields", day.Format(time.DateOnly)))
			continue
		}

		total := new(big.Rat)
		malformed := false
		for _, row := range *response.Data {
			rowDay, err := parseUTCDay(row.Date)
			if err != nil || !rowDay.Equal(day) {
				requestErrors = append(requestErrors, fmt.Errorf("parse activity for %s: response contained an invalid day", day.Format(time.DateOnly)))
				malformed = true
				break
			}
			usage, err := parseSpendAmount(row.Usage)
			if err != nil {
				requestErrors = append(requestErrors, fmt.Errorf("parse activity usage for %s: %w", day.Format(time.DateOnly), err))
				malformed = true
				break
			}
			total.Add(total, usage)
		}
		if !malformed {
			days = append(days, DailySpendDay{Day: day, SpendUSD: formatSpendAmount(total)})
		}
	}
	if len(requestErrors) > 0 {
		return nil, errors.Join(requestErrors...)
	}

	return days, nil
}

func (o *OpenRouter) doSpendRequest(ctx context.Context, method, path string, requestBody any, responseBody any) error {
	var body io.Reader
	if requestBody != nil {
		encoded, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, o.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+o.provisioningKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.orClient.Do(req)
	if err != nil {
		// Transport errors commonly echo the complete request URL. Activity
		// filters by key hash in its query string, so keep the cause available
		// to errors.Is without allowing its text into workflow logs.
		return fmt.Errorf("send request: %w", &spendTransportError{cause: err})
	}
	defer o11y.NoLogDefer(func() error {
		return resp.Body.Close()
	})

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected response status: %s", resp.Status)
	}

	encoded, err := io.ReadAll(io.LimitReader(resp.Body, maxSpendResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if len(encoded) > maxSpendResponseBytes {
		return errors.New("response exceeded size limit")
	}
	if err := json.Unmarshal(encoded, responseBody); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}

func parseAnalyticsDay(dateDay, createdAtDay string) (time.Time, error) {
	if dateDay == "" && createdAtDay == "" {
		return time.Time{}, errors.New("analytics response row omitted its day")
	}

	var parsed time.Time
	for _, value := range []string{dateDay, createdAtDay} {
		if value == "" {
			continue
		}
		day, err := parseUTCDay(value)
		if err != nil {
			return time.Time{}, errors.New("analytics response row contained an invalid day")
		}
		if !parsed.IsZero() && !parsed.Equal(day) {
			return time.Time{}, errors.New("analytics response row contained conflicting days")
		}
		parsed = day
	}

	return parsed, nil
}

func parseUTCDay(value string) (time.Time, error) {
	if day, err := time.Parse(time.DateOnly, value); err == nil {
		return day.UTC(), nil
	}
	instant, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse UTC day timestamp: %w", err)
	}
	instant = instant.UTC()
	if !instant.Equal(startOfUTCDay(instant)) {
		return time.Time{}, errors.New("timestamp is not midnight UTC")
	}
	return instant, nil
}

func startOfUTCDay(value time.Time) time.Time {
	year, month, day := value.UTC().Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func parseSpendAmount(raw json.RawMessage) (*big.Rat, error) {
	if len(raw) == 0 {
		return nil, errors.New("amount is missing")
	}

	value := string(raw)
	if raw[0] == '"' {
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, errors.New("amount is not a decimal")
		}
	}
	amount, err := parseDecimal(value)
	if err != nil {
		return nil, err
	}
	if amount.Sign() < 0 {
		return nil, errors.New("amount is negative")
	}
	return amount, nil
}

func parseDecimal(value string) (*big.Rat, error) {
	if value == "" || len(value) > 128 || strings.TrimSpace(value) != value {
		return nil, errors.New("amount is not a decimal")
	}

	mantissa := value
	exponent := 0
	if index := strings.IndexAny(value, "eE"); index >= 0 {
		if strings.ContainsAny(value[index+1:], "eE") {
			return nil, errors.New("amount is not a decimal")
		}
		mantissa = value[:index]
		parsed, err := strconv.Atoi(value[index+1:])
		if err != nil || parsed < -1000 || parsed > 1000 {
			return nil, errors.New("amount is not a decimal")
		}
		exponent = parsed
	}

	negative := strings.HasPrefix(mantissa, "-")
	if negative {
		mantissa = mantissa[1:]
	}
	if mantissa == "" || strings.HasPrefix(mantissa, "+") {
		return nil, errors.New("amount is not a decimal")
	}

	whole, fraction, hasPoint := strings.Cut(mantissa, ".")
	if whole == "" || (hasPoint && fraction == "") || !allDecimalDigits(whole) || (hasPoint && !allDecimalDigits(fraction)) {
		return nil, errors.New("amount is not a decimal")
	}
	digits := whole + fraction
	coefficient, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return nil, errors.New("amount is not a decimal")
	}
	if negative {
		coefficient.Neg(coefficient)
	}

	scale := len(fraction) - exponent
	if scale <= 0 {
		coefficient.Mul(coefficient, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-scale)), nil))
		return new(big.Rat).SetInt(coefficient), nil
	}
	denominator := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
	return new(big.Rat).SetFrac(coefficient, denominator), nil
}

func allDecimalDigits(value string) bool {
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func formatSpendAmount(amount *big.Rat) string {
	if amount.Sign() == 0 {
		return "0"
	}

	denominator := new(big.Int).Set(amount.Denom())
	twos, fives := 0, 0
	for denominator.Bit(0) == 0 {
		denominator.Rsh(denominator, 1)
		twos++
	}
	five := big.NewInt(5)
	quotient, remainder := new(big.Int), new(big.Int)
	for {
		quotient.QuoRem(denominator, five, remainder)
		if remainder.Sign() != 0 {
			break
		}
		denominator.Set(quotient)
		fives++
	}
	if denominator.Cmp(big.NewInt(1)) != 0 {
		panic("non-terminating spend decimal")
	}

	scale := max(twos, fives)
	coefficient := new(big.Int).Set(amount.Num())
	if missingTwos := scale - twos; missingTwos > 0 {
		coefficient.Mul(coefficient, new(big.Int).Exp(big.NewInt(2), big.NewInt(int64(missingTwos)), nil))
	}
	if missingFives := scale - fives; missingFives > 0 {
		coefficient.Mul(coefficient, new(big.Int).Exp(five, big.NewInt(int64(missingFives)), nil))
	}

	digits := coefficient.String()
	if scale == 0 {
		return digits
	}
	if len(digits) <= scale {
		digits = strings.Repeat("0", scale-len(digits)+1) + digits
	}
	point := len(digits) - scale
	formatted := strings.TrimRight(digits[:point]+"."+digits[point:], "0")
	return strings.TrimSuffix(formatted, ".")
}
