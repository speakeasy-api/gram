package aiintegrations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	anthropicapi "github.com/speakeasy-api/gram/server/internal/thirdparty/anthropic"
	codexapi "github.com/speakeasy-api/gram/server/internal/thirdparty/codex"
	cursorapi "github.com/speakeasy-api/gram/server/internal/thirdparty/cursor"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/geminivault"
)

const (
	// A save is an interactive request, so a slow provider must not hold the
	// write open. Timing out is inconclusive and lets the save through.
	credentialProbeTimeout = 20 * time.Second

	// Probes want the provider's verdict, not its data, so the window stays as
	// narrow as each API accepts.
	credentialProbeLookback = time.Minute
)

// Credentials are the provider secrets and scope a save is about to store.
type Credentials struct {
	Provider               string
	APIKey                 string
	ExternalOrganizationID *string
}

// ScheduleRejection is one sync schedule whose provider refused the
// credentials outright.
type ScheduleRejection struct {
	Schedule string
	// Err carries the provider's own explanation, safe to show org members.
	Err error
}

// CredentialVerifier asks a provider whether it will honor the credentials a
// save is about to store, using the cheapest read each sync schedule already
// makes. A refused key that never lands creates no config generation, so the
// poll loop never starts for it.
type CredentialVerifier struct {
	logger   *slog.Logger
	guardian *guardian.Policy
	// Test overrides pointing the probes at a stub. Empty is the client default.
	cursorBaseURL    string
	anthropicBaseURL string
	codexBaseURL     string
	now              func() time.Time
}

func NewCredentialVerifier(logger *slog.Logger, guardianPolicy *guardian.Policy) *CredentialVerifier {
	return &CredentialVerifier{
		logger:           logger.With(attr.SlogComponent("aiintegrations.credentials")),
		guardian:         guardianPolicy,
		cursorBaseURL:    "",
		anthropicBaseURL: "",
		codexBaseURL:     "",
		now:              func() time.Time { return time.Now().UTC() },
	}
}

// Verify probes every sync schedule the provider runs, concurrently, and
// returns the refused ones in syncSchedulesFor order.
//
// Only an outright refusal counts. An outage, timeout or transport failure
// says nothing about the credentials, so it is logged and the save proceeds.
func (v *CredentialVerifier) Verify(ctx context.Context, creds Credentials) []ScheduleRejection {
	schedules := syncSchedulesFor(creds.Provider)

	ctx, cancel := context.WithTimeout(ctx, credentialProbeTimeout)
	defer cancel()

	rejected := make([]*ScheduleRejection, len(schedules))
	var group errgroup.Group
	for i, sched := range schedules {
		group.Go(func() error {
			err := v.probe(ctx, sched.schedule, creds)
			if err == nil {
				return nil
			}
			if status, ok := providerRejectedCredentials(err); ok {
				rejected[i] = &ScheduleRejection{
					Schedule: sched.schedule,
					Err:      credentialRejectionError(sched.schedule, err),
				}
				v.logger.InfoContext(ctx, "ai integration credentials rejected by provider",
					attr.SlogProvider(creds.Provider),
					attr.SlogAIIntegrationSyncSchedule(sched.schedule),
					attr.SlogHTTPResponseStatusCode(status),
				)
				return nil
			}
			v.logger.WarnContext(ctx, "ai integration credential check was inconclusive",
				attr.SlogProvider(creds.Provider),
				attr.SlogAIIntegrationSyncSchedule(sched.schedule),
				attr.SlogError(err),
			)
			return nil
		})
	}
	// Every probe reports through rejected; the group never fails.
	_ = group.Wait()

	rejections := make([]ScheduleRejection, 0, len(rejected))
	for _, rejection := range rejected {
		if rejection != nil {
			rejections = append(rejections, *rejection)
		}
	}
	return rejections
}

// probe makes one schedule's cheapest read: the fetch its sync already starts
// with, narrowed to a single record. A schedule with no probe, or one missing
// the scope id it needs, reports no opinion.
func (v *CredentialVerifier) probe(ctx context.Context, schedule string, creds Credentials) error {
	now := v.now().UTC()
	since := now.Add(-credentialProbeLookback)

	switch schedule {
	case ScheduleGeminiCompliance:
		if creds.ExternalOrganizationID == nil {
			return nil
		}
		// The sync's first read is FindExport, but listing exports cannot tell
		// a closed matter from an open one. Reading the matter exercises the
		// same token exchange and matter access, and also confirms the matter
		// is OPEN, which every export the sync creates requires.
		client, err := geminivault.NewClient(creds.APIKey, *creds.ExternalOrganizationID, v.guardian)
		if err != nil {
			return fmt.Errorf("create Gemini Vault credential probe client: %w", err)
		}
		return client.GetMatter(ctx) //nolint:wrapcheck // preserve typed provider status for rejection classification.
	case ScheduleCursor:
		client := cursorapi.New(v.guardian,
			cursorapi.WithAPIKey(creds.APIKey),
			cursorapi.WithBaseURL(v.cursorBaseURL),
			cursorapi.WithPageSize(1),
		)
		_, err := client.FetchUsageEventsPage(ctx, cursorapi.FetchUsageEventsPageParams{
			Start: since,
			End:   now,
			Page:  1,
		})
		return err //nolint:wrapcheck // providerRejectedCredentials needs the HTTPError.

	case ScheduleAnthropicCompliance:
		if creds.ExternalOrganizationID == nil {
			return nil
		}
		// The sync reads the activity feed and the chat list, which Anthropic
		// grants under separate scopes, so the key is probed against both.
		client := v.anthropicClient(creds)
		if _, err := client.ListActivities(ctx, anthropicapi.ListActivitiesParams{
			ActivityTypes:   []string{anthropicComplianceActivityCreated},
			OrganizationIDs: []string{*creds.ExternalOrganizationID},
			CreatedAtGTE:    since,
			CreatedAtLT:     time.Time{},
			AfterID:         "",
			BeforeID:        "",
			Limit:           1,
		}); err != nil {
			return err //nolint:wrapcheck // providerRejectedCredentials needs the HTTPError.
		}
		_, err := client.ListChats(ctx, anthropicapi.ListChatsParams{
			OrganizationIDs: []string{*creds.ExternalOrganizationID},
			OrderBy:         anthropicapi.ChatOrderByUpdatedAt,
			UpdatedAtGTE:    since,
			AfterID:         "",
			Limit:           1,
		})
		return err //nolint:wrapcheck // providerRejectedCredentials needs the HTTPError.

	case ScheduleAnthropicAnalyticsUsage:
		_, err := v.anthropicClient(creds).GetUserUsageReport(ctx, analyticsProbeParams(now))
		return err //nolint:wrapcheck // providerRejectedCredentials needs the HTTPError.

	case ScheduleAnthropicAnalyticsCost:
		_, err := v.anthropicClient(creds).GetUserCostReport(ctx, analyticsProbeParams(now))
		return err //nolint:wrapcheck // providerRejectedCredentials needs the HTTPError.

	case ScheduleCodexCompliance:
		if creds.ExternalOrganizationID == nil {
			return nil
		}
		client := codexapi.New(v.guardian, *creds.ExternalOrganizationID,
			codexapi.WithAPIKey(creds.APIKey),
			codexapi.WithBaseURL(v.codexBaseURL),
		)
		return listLogsProbe(ctx, client, codexComplianceCostsEventType, since)

	case ScheduleChatGPTCompliance, ScheduleCodexCloudSessions:
		if creds.ExternalOrganizationID == nil {
			return nil
		}
		client := codexapi.NewWorkspaceClient(v.guardian, *creds.ExternalOrganizationID,
			codexapi.WithAPIKey(creds.APIKey),
			codexapi.WithBaseURL(v.codexBaseURL),
		)
		eventType := chatgptConversationEventType
		if schedule == ScheduleCodexCloudSessions {
			eventType = codexCloudEventType
		}
		return listLogsProbe(ctx, client, eventType, since)

	default:
		return nil
	}
}

func (v *CredentialVerifier) anthropicClient(creds Credentials) *anthropicapi.Client {
	return anthropicapi.New(v.guardian,
		anthropicapi.WithAPIKey(creds.APIKey),
		anthropicapi.WithBaseURL(v.anthropicBaseURL),
	)
}

func listLogsProbe(ctx context.Context, client *codexapi.Client, eventType string, since time.Time) error {
	_, err := client.ListLogs(ctx, codexapi.ListLogsParams{
		EventType: eventType,
		After:     since,
		Limit:     1,
	})
	return err //nolint:wrapcheck // providerRejectedCredentials needs the HTTPError.
}

// providerRejectedCredentials reports the status a provider answered with when
// it refused the configuration itself: a bad key, a missing scope, an API the
// key is not entitled to. Unlike the poll path (pollRejectedByProvider), 400
// and 422 do not count — a probe sends a fixed minimal request, so a
// request-shaped complaint is about the probe, not the credentials. The
// exceptions are Gemini answers about the configuration itself: any 4xx from
// Google's token endpoint, and a matter that exists but is not open (status 0,
// since Vault answered 200).
func providerRejectedCredentials(err error) (int, bool) {
	if errors.Is(err, geminivault.ErrMatterNotOpen) {
		return 0, true
	}
	status := 0
	if providerErr, ok := errors.AsType[*geminivault.HTTPError](err); ok {
		if providerErr.CredentialsRejected() {
			return providerErr.StatusCode, true
		}
		status = providerErr.StatusCode
	}
	if cursorErr, ok := errors.AsType[*cursorapi.HTTPError](err); ok {
		status = cursorErr.StatusCode
	}
	if anthropicErr, ok := errors.AsType[*anthropicapi.HTTPError](err); ok {
		status = anthropicErr.StatusCode
	}
	if codexErr, ok := errors.AsType[*codexapi.HTTPError](err); ok {
		status = codexErr.StatusCode
	}

	switch status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return status, true
	default:
		return 0, false
	}
}

// credentialRejectionError turns a refusal into the message the save fails
// with. The provider's own text names the API, the status and usually the
// missing entitlement — what the user needs to fix the key.
func credentialRejectionError(schedule string, cause error) error {
	if schedule == ScheduleGeminiCompliance {
		if errors.Is(cause, geminivault.ErrMatterNotOpen) {
			return oops.E(oops.CodeInvalid, cause, "Google Vault matter is not open; reopen it in Google Vault or use an open matter")
		}
		if providerErr, ok := errors.AsType[*geminivault.HTTPError](cause); ok {
			if providerErr.CredentialsRejected() {
				return oops.E(oops.CodeUnauthorized, cause, "Google rejected the service account credentials; check the key, the domain-wide delegation scopes, and the delegated user")
			}
			switch providerErr.StatusCode {
			case 401:
				return oops.E(oops.CodeUnauthorized, cause, "Google Vault rejected the service account credentials")
			case 403:
				return oops.E(oops.CodeForbidden, cause, "Google Vault access denied; check domain-wide delegation, delegated user permissions, and matter access")
			default:
				return oops.E(oops.CodeInvalid, cause, "Google Vault rejected the configured matter; check the matter UUID and delegated user access")
			}
		}
	}

	return oops.E(oops.CodeInvalid, cause, "%s", fmt.Sprintf("provider rejected these credentials for the %s sync: %s", schedule, cause.Error()))
}
