//nolint:exhaustruct // Optional feedback values use documented zero values.
package platformmcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

const (
	feedbackConnectionHourlyLimit   = 3
	feedbackOrganizationHourlyLimit = 10
	feedbackRetention               = 180 * 24 * time.Hour
	feedbackDeliveryQueued          = "queued"
)

var (
	ErrFeedbackInvalid     = errors.New("invalid platform mcp feedback")
	ErrFeedbackConflict    = errors.New("platform mcp feedback idempotency conflict")
	ErrFeedbackRateLimited = errors.New("platform mcp feedback rate limited")
	ErrFeedbackUnavailable = errors.New("platform mcp feedback unavailable")
	ErrFeedbackForbidden   = errors.New("platform mcp feedback connection is no longer active")
)

type FeedbackInput struct {
	Category        string
	Rating          *int
	Success         *bool
	ToolName        string
	FailureCategory string
	Note            string
	IdempotencyKey  string
}

type FeedbackResult struct {
	TrackingID    string
	DeliveryState string
	ExpiresAt     time.Time
	Replayed      bool
}

// FeedbackService stores a local, bounded feedback record. Delivery stays queued:
// this slice deliberately does not compose an external feedback destination.
type FeedbackService struct {
	db  *pgxpool.Pool
	now func() time.Time
}

func NewFeedbackService(db *pgxpool.Pool) *FeedbackService {
	return &FeedbackService{db: db, now: time.Now}
}

func (s *FeedbackService) Submit(ctx context.Context, principal Principal, input FeedbackInput) (FeedbackResult, error) {
	if s == nil || s.db == nil {
		return FeedbackResult{}, ErrFeedbackUnavailable
	}
	if err := validateFeedbackInput(principal, input); err != nil {
		return FeedbackResult{}, err
	}

	connectionID, generation, err := principalConnection(principal)
	if err != nil {
		return FeedbackResult{}, ErrFeedbackInvalid
	}
	inputHash := feedbackInputHash(input)
	now := s.now().UTC()

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return FeedbackResult{}, fmt.Errorf("begin platform mcp feedback: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := repo.New(tx)
	// A caller that presents a connection must still prove it is live and its
	// own. A connection-less surface has none to prove; its identity was
	// established upstream and the row is attributed by subject instead.
	if connectionID.Valid {
		connection, err := q.GetActivePlatformMCPConnectionForFeedbackForUpdate(ctx, repo.GetActivePlatformMCPConnectionForFeedbackForUpdateParams{
			ID:             connectionID.UUID,
			OrganizationID: principal.OrganizationID,
		})
		if errors.Is(err, pgx.ErrNoRows) || err == nil && (connection.SubjectUrn != userSubjectURN(principal.UserID) || connection.ActiveGeneration != generation.UUID) {
			return FeedbackResult{}, ErrFeedbackForbidden
		}
		if err != nil {
			return FeedbackResult{}, fmt.Errorf("resolve active platform mcp feedback connection: %w", err)
		}
	}
	if err := q.LockPlatformMCPFeedbackOrganization(ctx, principal.OrganizationID); err != nil {
		return FeedbackResult{}, fmt.Errorf("lock platform mcp feedback organization: %w", err)
	}
	if _, err := q.DeleteExpiredPlatformMCPFeedback(ctx, principal.OrganizationID); err != nil {
		return FeedbackResult{}, fmt.Errorf("delete expired platform mcp feedback: %w", err)
	}
	if err := q.LockPlatformMCPFeedbackSubmission(ctx, repo.LockPlatformMCPFeedbackSubmissionParams{
		OrganizationID: principal.OrganizationID,
		SubjectUrn:     userSubjectURN(principal.UserID),
		IdempotencyKey: input.IdempotencyKey,
	}); err != nil {
		return FeedbackResult{}, fmt.Errorf("lock platform mcp feedback submission: %w", err)
	}

	existing, err := q.GetPlatformMCPFeedbackByIdempotencyKey(ctx, repo.GetPlatformMCPFeedbackByIdempotencyKeyParams{
		OrganizationID: principal.OrganizationID,
		SubjectUrn:     userSubjectURN(principal.UserID),
		IdempotencyKey: input.IdempotencyKey,
	})
	switch {
	case err == nil:
		if existing.InputHash != inputHash {
			return FeedbackResult{}, ErrFeedbackConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return FeedbackResult{}, fmt.Errorf("commit platform mcp feedback replay: %w", err)
		}
		return FeedbackResult{TrackingID: existing.ID.String(), DeliveryState: existing.DeliveryState, ExpiresAt: existing.ExpiresAt.Time, Replayed: true}, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return FeedbackResult{}, fmt.Errorf("load platform mcp feedback replay: %w", err)
	}

	since := pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true}
	// The per-caller hourly limit follows whatever identifies the caller.
	// Without it, a connection-less surface would be metered only by the far
	// larger organization bucket.
	callerCount, err := q.CountRecentPlatformMCPFeedbackByCaller(ctx, repo.CountRecentPlatformMCPFeedbackByCallerParams{
		OrganizationID: principal.OrganizationID,
		ConnectionID:   connectionID,
		SubjectUrn:     userSubjectURN(principal.UserID),
		Since:          since,
	})
	if err != nil {
		return FeedbackResult{}, fmt.Errorf("count platform mcp feedback by caller: %w", err)
	}
	if callerCount >= feedbackConnectionHourlyLimit {
		return FeedbackResult{}, ErrFeedbackRateLimited
	}
	organizationCount, err := q.CountRecentPlatformMCPFeedbackByOrganization(ctx, repo.CountRecentPlatformMCPFeedbackByOrganizationParams{
		OrganizationID: principal.OrganizationID,
		Since:          since,
	})
	if err != nil {
		return FeedbackResult{}, fmt.Errorf("count platform mcp feedback by organization: %w", err)
	}
	if organizationCount >= feedbackOrganizationHourlyLimit {
		return FeedbackResult{}, ErrFeedbackRateLimited
	}

	created, err := q.CreatePlatformMCPFeedback(ctx, repo.CreatePlatformMCPFeedbackParams{
		OrganizationID:       principal.OrganizationID,
		SubjectUrn:           userSubjectURN(principal.UserID),
		ConnectionID:         connectionID,
		ConnectionGeneration: generation,
		Category:             input.Category,
		Rating:               optionalFeedbackRating(input.Rating),
		Success:              optionalFeedbackSuccess(input.Success),
		ToolName:             optionalFeedbackText(input.ToolName),
		FailureCategory:      optionalFeedbackText(input.FailureCategory),
		Note:                 optionalFeedbackText(input.Note),
		IdempotencyKey:       input.IdempotencyKey,
		InputHash:            inputHash,
		ExpiresAt:            pgtype.Timestamptz{Time: now.Add(feedbackRetention), Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return FeedbackResult{}, ErrFeedbackForbidden
	}
	if err != nil {
		return FeedbackResult{}, fmt.Errorf("create platform mcp feedback: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return FeedbackResult{}, fmt.Errorf("commit platform mcp feedback: %w", err)
	}
	return FeedbackResult{TrackingID: created.ID.String(), DeliveryState: created.DeliveryState, ExpiresAt: created.ExpiresAt.Time}, nil
}

func validateFeedbackInput(principal Principal, input FeedbackInput) error {
	if principal.UserID == "" || principal.OrganizationID == "" || input.IdempotencyKey == "" || !feedbackSafeText(input.IdempotencyKey, 128) || !validFeedbackCategory(input.Category) || !validOptionalFeedbackRating(input.Rating) || !validOptionalFeedbackTool(input.ToolName) || !validOptionalFeedbackFailureCategory(input.FailureCategory) || !feedbackNoteSafeText(input.Note) {
		return ErrFeedbackInvalid
	}
	return nil
}

func validFeedbackCategory(value string) bool {
	switch value {
	case "success", "failure", "confusing_guidance", "missing_capability", "incorrect_result", "authorization_problem", "setup_problem", "performance", "other":
		return true
	default:
		return false
	}
}

func validOptionalFeedbackRating(value *int) bool {
	return value == nil || (*value >= 1 && *value <= 5)
}

func validOptionalFeedbackTool(value string) bool {
	if value == "" {
		return true
	}
	_, ok := knownPlatformMCPToolNames[value]
	return ok
}

func validOptionalFeedbackFailureCategory(value string) bool {
	if value == "" {
		return true
	}
	switch value {
	case "rate_limited", "authorization", "invalid_input", "unavailable", "unknown", string(ReadinessNeedsProviderSetup), string(ReadinessNeedsGramAuthorization), string(ReadinessNeedsConfiguration), string(ReadinessAuthFailed), string(ReadinessUnreachable), string(ReadinessUnsupported), string(ReadinessUnauthorized), string(ReadinessGuideUnavailable), string(ReadinessDegraded):
		return true
	default:
		return false
	}
}

func feedbackSafeText(value string, maxRunes int) bool {
	if len(value) == 0 {
		return true
	}
	if len([]rune(value)) > maxRunes || strings.ContainsAny(value, "\r\n") || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return false
	}
	lower := strings.ToLower(value)
	for _, disallowed := range []string{"http://", "https://", "www.", "authorization:", "bearer ", "password", "secret", "api_key", "api-key", "token", "@", "{", "}"} {
		if strings.Contains(lower, disallowed) {
			return false
		}
	}
	return true
}

// feedbackNoteSafeText applies the deliberately stricter retention policy for
// free-form notes. Idempotency keys use feedbackSafeText but may be UUID-shaped;
// notes are never allowed to carry identifiers, URLs, or session material.
func feedbackNoteSafeText(value string) bool {
	if !feedbackSafeText(value, 500) {
		return false
	}
	if hasEmbeddedURIScheme(value) || hasUnsafeFeedbackPath(value) {
		return false
	}
	for _, word := range strings.FieldsFunc(value, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '-'
	}) {
		lowerWord := strings.ToLower(word)
		if lowerWord == "cookie" || lowerWord == "set-cookie" || lowerWord == "session" || hasSensitiveFeedbackToken(word) {
			return false
		}
		if _, err := uuid.Parse(word); err == nil {
			return false
		}
	}
	return true
}

func hasEmbeddedURIScheme(value string) bool {
	for _, word := range strings.FieldsFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) && r != ':' && r != '-' && r != '_'
	}) {
		if parsed, err := url.ParseRequestURI(word); err == nil && parsed.Scheme != "" && (parsed.Opaque != "" || parsed.Host != "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "") {
			return true
		}
	}
	return false
}

func hasUnsafeFeedbackPath(value string) bool {
	for word := range strings.FieldsSeq(value) {
		trimmed := strings.Trim(word, "([{\"'")
		trimmed = strings.TrimRight(trimmed, ".,;:!?)]}\"")
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "//") || net.ParseIP(trimmed) != nil {
			return true
		}
		if parsed, err := url.Parse("//" + trimmed); err == nil && parsed.Host != "" && unsafeFeedbackHost(parsed) {
			return true
		}
		if dot := strings.LastIndexByte(trimmed, '.'); dot > 0 && strings.IndexFunc(trimmed[:dot], unicode.IsLetter) >= 0 && onlyLetters(trimmed[dot+1:]) && len([]rune(trimmed[dot+1:])) > 1 {
			return true
		}
	}
	return false
}

func unsafeFeedbackHost(parsed *url.URL) bool {
	host := parsed.Hostname()
	if host == "" {
		return false
	}
	isIP := net.ParseIP(host) != nil
	isDottedHost := strings.Contains(host, ".")
	if isIP {
		return true
	}
	if parsed.Port() != "" {
		return isDottedHost
	}
	return isDottedHost && (parsed.Path != "" || parsed.RawQuery != "")
}

func onlyLetters(value string) bool {
	return value != "" && strings.IndexFunc(value, func(r rune) bool { return !unicode.IsLetter(r) }) == -1
}

func hasSensitiveFeedbackToken(value string) bool {
	runes := []rune(value)
	for _, marker := range []string{"cookie", "session"} {
		markerRunes := []rune(marker)
		for index := 0; index+len(markerRunes) <= len(runes); index++ {
			matches := true
			for offset, markerRune := range markerRunes {
				if unicode.ToLower(runes[index+offset]) != markerRune {
					matches = false
					break
				}
			}
			if !matches {
				continue
			}
			before := index == 0 || !unicode.IsLetter(runes[index-1]) || unicode.IsUpper(runes[index])
			afterIndex := index + len(markerRunes)
			after := afterIndex == len(runes) || !unicode.IsLetter(runes[afterIndex]) || unicode.IsUpper(runes[afterIndex])
			if before && after {
				return true
			}
		}
	}
	return false
}

func feedbackInputHash(input FeedbackInput) string {
	rating := ""
	if input.Rating != nil {
		rating = fmt.Sprintf("%d", *input.Rating)
	}
	success := ""
	if input.Success != nil {
		success = fmt.Sprintf("%t", *input.Success)
	}
	digest := sha256.Sum256([]byte(strings.Join([]string{input.Category, rating, success, input.ToolName, input.FailureCategory, input.Note}, "\x00")))
	return hex.EncodeToString(digest[:])
}

func optionalFeedbackRating(value *int) pgtype.Int4 {
	if value == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: int32(*value), Valid: true} //nolint:gosec // Validation limits ratings to 1 through 5.
}

func optionalFeedbackSuccess(value *bool) pgtype.Bool {
	if value == nil {
		return pgtype.Bool{}
	}
	return pgtype.Bool{Bool: *value, Valid: true}
}

func optionalFeedbackText(value string) pgtype.Text {
	if value == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: value, Valid: true}
}

var knownPlatformMCPToolNames = map[string]struct{}{
	"get_platform_context":                  {},
	"list_projects":                         {},
	"list_project_mcps":                     {},
	"get_mcp":                               {},
	"search_mcp_catalog":                    {},
	"inspect_mcp_candidate":                 {},
	"register_catalog_mcp":                  {},
	"get_setup_handoff":                     {},
	"get_mcp_readiness":                     {},
	"get_mcp_repair_plan":                   {},
	"distribute_mcp_to_default_plugin":      {},
	"remove_mcp_from_default_plugin":        {},
	"register_platform_mcp_for_project":     {},
	"get_platform_mcp_onboarding_status":    {},
	"attach_platform_mcp_identity_provider": {},
	"add_platform_mcp_to_default_plugin":    {},
	"send_platform_mcp_feedback":            {},
}
