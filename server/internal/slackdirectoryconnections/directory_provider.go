package slackdirectoryconnections

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	slackapi "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/api"
)

// DirectoryMember is an observation, never evidence of ownership of a Gram person.
type DirectoryMember struct {
	UserID      string
	DisplayName string
	Email       string
	Status      string
	MemberType  string
	UpdatedAt   *time.Time
}

// SyncProgress contains counts only; directory profiles and credentials stay out of history.
type SyncProgress struct {
	Phase            string
	Pages            int
	Members          int
	ExcludedExternal int
	Bots             int
}

type DirectoryProvider interface {
	Fetch(context.Context, string, string, func(SyncProgress)) ([]DirectoryMember, error)
}

type directoryProvider struct{ api *slackapi.Client }

func NewDirectoryProvider(api *slackapi.Client) DirectoryProvider {
	return &directoryProvider{api: api}
}

// SyncError is safe for storage and workflow history. It never contains provider response text.
type SyncError struct {
	Code       string
	Retryable  bool
	Reconnect  bool
	RetryAfter time.Duration
}

func (e *SyncError) Error() string { return "Slack directory: " + e.Code }

type directoryUser struct {
	ID                 string `json:"id"`
	TeamID             string `json:"team_id"`
	Stranger           bool   `json:"is_stranger"`
	Deleted            *bool  `json:"deleted"`
	Bot                *bool  `json:"is_bot"`
	App                *bool  `json:"is_app_user"`
	Guest              *bool  `json:"is_restricted"`
	SingleChannelGuest *bool  `json:"is_ultra_restricted"`
	EmailConfirmed     *bool  `json:"is_email_confirmed"`
	Invited            bool   `json:"is_invited_user"`
	Updated            int64  `json:"updated"`
	Profile            struct {
		DisplayName string `json:"display_name"`
		RealName    string `json:"real_name"`
		Email       string `json:"email"`
		Team        string `json:"team"`
	} `json:"profile"`
}

type directoryPage struct {
	OK       bool            `json:"ok"`
	Error    string          `json:"error"`
	Members  []directoryUser `json:"members"`
	Metadata *struct {
		NextCursor *string `json:"next_cursor"`
	} `json:"response_metadata"`
}

func (p *directoryProvider) Fetch(ctx context.Context, token, team string, report func(SyncProgress)) ([]DirectoryMember, error) {
	members := make([]DirectoryMember, 0)
	cursors := map[string]bool{}
	users := map[string]int{}
	excluded := map[string]bool{}
	bots := 0
	if report == nil {
		report = func(SyncProgress) {}
	}
	cursor := ""
	for pageNumber := 1; pageNumber <= 10000; pageNumber++ {
		progress := SyncProgress{Phase: "fetching", Pages: pageNumber - 1, Members: len(members), ExcludedExternal: len(excluded), Bots: bots}
		report(progress)
		page, err := p.page(ctx, token, team, cursor, progress, report)
		if err != nil {
			return nil, err
		}
		for _, user := range page.Members {
			// Slack Connect users require both workspace and stranger checks. Enterprise
			// identifiers and profile emails do not establish workspace membership.
			// https://docs.slack.dev/apis/slack-connect/#determining-whether-a-user-is-external-must-be-done-implicitly
			if user.TeamID != team || user.Stranger || (user.Profile.Team != "" && user.Profile.Team != team) {
				excluded[user.TeamID+":"+user.ID] = true
				continue
			}
			if user.ID == "" {
				return nil, &SyncError{Code: "invalid_directory", Retryable: true, Reconnect: false, RetryAfter: 0}
			}
			status := "unknown"
			if user.Deleted != nil {
				if *user.Deleted {
					status = "deactivated"
				} else if user.Invited {
					status = "invited"
				} else {
					status = "active"
				}
			}
			memberType := "unknown"
			switch {
			case user.ID == "USLACKBOT" || conv.PtrValOr(user.Bot, false) || conv.PtrValOr(user.App, false):
				memberType = "bot"
			case conv.PtrValOr(user.SingleChannelGuest, false):
				memberType = "single_channel_guest"
			case conv.PtrValOr(user.Guest, false):
				memberType = "guest"
			case user.Bot != nil && user.Guest != nil:
				memberType = "person"
			}
			var updated *time.Time
			if user.Updated > 0 {
				updated = new(time.Unix(user.Updated, 0).UTC())
			}
			email := user.Profile.Email
			if user.EmailConfirmed != nil && !*user.EmailConfirmed {
				email = ""
			}
			member := DirectoryMember{UserID: user.ID, DisplayName: conv.Default(user.Profile.DisplayName, user.Profile.RealName), Email: email, Status: status, MemberType: memberType, UpdatedAt: updated}
			if index, exists := users[user.ID]; exists {
				if members[index].MemberType == "bot" {
					bots--
				}
				members[index] = member
			} else {
				users[user.ID] = len(members)
				members = append(members, member)
			}
			if member.MemberType == "bot" {
				bots++
			}
		}
		if len(members)+len(excluded) > 50000 {
			return nil, &SyncError{Code: "directory_too_large", Retryable: false, Reconnect: false, RetryAfter: 0}
		}
		report(SyncProgress{Phase: "fetching", Pages: pageNumber, Members: len(members), ExcludedExternal: len(excluded), Bots: bots})
		cursor = *page.Metadata.NextCursor
		if cursor == "" {
			return members, nil
		}
		if cursors[cursor] {
			return nil, &SyncError{Code: "invalid_directory", Retryable: true, Reconnect: false, RetryAfter: 0}
		}
		cursors[cursor] = true
	}
	return nil, &SyncError{Code: "directory_too_large", Retryable: false, Reconnect: false, RetryAfter: 0}
}

func (p *directoryProvider) page(ctx context.Context, token, team, cursor string, progress SyncProgress, report func(SyncProgress)) (*directoryPage, error) {
	for attempt := 0; ; attempt++ {
		page, delay, err := p.request(ctx, token, team, cursor)
		if err == nil {
			return page, nil
		}
		if delay == 0 || attempt == 3 {
			return nil, err
		}
		progress.Phase = "waiting_for_slack"
		report(progress)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("wait for Slack rate limit: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func (p *directoryProvider) request(ctx context.Context, token, team, cursor string) (*directoryPage, time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	q := url.Values{"limit": {"200"}, "cursor": {cursor}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.api.BaseURL()+"/users.list?"+q.Encode(), nil)
	if err != nil {
		return nil, 0, &SyncError{Code: "request_invalid", Retryable: false, Reconnect: false, RetryAfter: 0}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := p.api.HTTPClient().Do(req)
	if err != nil {
		return nil, 0, &SyncError{Code: "provider_unavailable", Retryable: true, Reconnect: false, RetryAfter: 0}
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })
	if resp.StatusCode == http.StatusTooManyRequests {
		seconds, err := strconv.Atoi(resp.Header.Get("Retry-After"))
		if err != nil || seconds < 1 {
			seconds = 60
		}
		// A longer wait cannot fit an activity attempt. Fail without retrying early.
		if seconds > 900 {
			return nil, 0, &SyncError{Code: "rate_limited", Retryable: true, Reconnect: false, RetryAfter: time.Duration(min(seconds, 86400)) * time.Second}
		}
		return nil, time.Duration(seconds) * time.Second, &SyncError{Code: "rate_limited", Retryable: true, Reconnect: false, RetryAfter: time.Duration(min(seconds, 86400)) * time.Second}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, 0, &SyncError{Code: "provider_unavailable", Retryable: true, Reconnect: false, RetryAfter: 0}
	}
	var page directoryPage
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&page); err != nil {
		return nil, 0, &SyncError{Code: "invalid_directory", Retryable: true, Reconnect: false, RetryAfter: 0}
	}
	if !page.OK {
		switch page.Error {
		case "invalid_auth", "token_expired", "token_revoked", "account_inactive", "not_authed", "missing_scope", "team_access_not_granted":
			return nil, 0, &SyncError{Code: "authorization_expired", Retryable: false, Reconnect: true, RetryAfter: 0}
		case "ratelimited", "rate_limited":
			return nil, time.Minute, &SyncError{Code: "rate_limited", Retryable: true, Reconnect: false, RetryAfter: time.Minute}
		default:
			return nil, 0, &SyncError{Code: "provider_unavailable", Retryable: true, Reconnect: false, RetryAfter: 0}
		}
	}
	if page.Members == nil || page.Metadata == nil || page.Metadata.NextCursor == nil {
		return nil, 0, &SyncError{Code: "invalid_directory", Retryable: true, Reconnect: false, RetryAfter: 0}
	}
	return &page, 0, nil
}
