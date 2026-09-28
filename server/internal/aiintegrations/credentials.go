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
)

const (
	// credentialProbeTimeout bounds the whole save-time verification. A save
	// is an interactive request, so a provider that is slow to answer must
	// not hold the write open; an inconclusive probe lets the save through.
	credentialProbeTimeout = 20 * time.Second

	// credentialProbeLookback keeps each probe's window tiny. The probes only
	// need the provider's verdict on the credentials, not its data, and a
	// narrow window is the cheapest request each API accepts.
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
	// Err is safe to show organization members: it carries the provider's own
	// explanation of the refusal.
	Err error
}

// CredentialVerifier asks a provider whether it will honor the credentials a
// save is about to store, by making the cheapest read each sync schedule
// already performs. Catching a refused key here is what keeps it out of the
// poll loop: a save that never lands creates no config generation, so no
// schedule is enqueued, no failure streak starts, and the user gets the
// provider's own message immediately instead of minutes later in a dashboard
// poll error.
type CredentialVerifier struct {
	logger   *slog.Logger
	guardian *guardian.Policy
	// Base URL overrides exist for tests, which point the probes at a local
	// provider stub. Empty means the client default.
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

// Verify probes every sync schedule the provider runs and returns the ones it
// refused, in syncSchedulesFor order. Probes run concurrently because they are
// independent and a save waits on all of them.
//
// Only an outright refusal is reported. Anything else — an outage, a timeout,
// a transport failure — says nothing about the credentials, so it is logged
// and the save proceeds: a provider having a bad minute must not block an
// organization from configuring its integration.
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

// probe makes one schedule's cheapest read: the upper-bound or first-page
// fetch its sync already starts with, narrowed to a single record. Schedules
// with no probe (or a config missing the scope id a probe needs, which the
// store rejects separately) report no opinion.
func (v *CredentialVerifier) probe(ctx context.Context, schedule string, creds Credentials) error {
	now := v.now().UTC()
	since := now.Add(-credentialProbeLookback)

	switch schedule {
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
		return err //nolint:wrapcheck // Preserve HTTPError for the rejection classification below.

	case ScheduleAnthropicCompliance:
		if creds.ExternalOrganizationID == nil {
			return nil
		}
		_, err := v.anthropicClient(creds).ListActivities(ctx, anthropicapi.ListActivitiesParams{
			ActivityTypes:   []string{anthropicComplianceActivityCreated, anthropicComplianceActivityUpdated},
			OrganizationIDs: []string{*creds.ExternalOrganizationID},
			CreatedAtGTE:    since,
			AfterID:         "",
			BeforeID:        "",
			Limit:           1,
		})
		return err //nolint:wrapcheck // Preserve HTTPError for the rejection classification below.

	case ScheduleAnthropicAnalyticsUsage:
		_, err := v.anthropicClient(creds).GetUserUsageReport(ctx, analyticsProbeParams(now))
		return err //nolint:wrapcheck // Preserve HTTPError for the rejection classification below.

	case ScheduleAnthropicAnalyticsCost:
		_, err := v.anthropicClient(creds).GetUserCostReport(ctx, analyticsProbeParams(now))
		return err //nolint:wrapcheck // Preserve HTTPError for the rejection classification below.

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
	return err //nolint:wrapcheck // Preserve HTTPError for the rejection classification.
}

// providerRejectedCredentials reports the status a provider answered with when
// it refused the request over the configuration itself: a rejected key, a
// missing scope, or an API the key is not entitled to. The poll path's
// equivalent (pollRejectedByProvider) also counts 400 and 422, but a probe
// sends a fixed minimal request, so a request-shaped complaint says more about
// the probe than about the credentials and must not block a save.
func providerRejectedCredentials(err error) (int, bool) {
	status := 0
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

// credentialRejectionError turns a provider refusal into the message the save
// fails with. The provider's own error text is the whole point: it names the
// API, the status, and usually the missing entitlement, which is what the user
// needs to fix the key.
func credentialRejectionError(schedule string, cause error) error {
	return oops.E(oops.CodeInvalid, cause, "%s", fmt.Sprintf("provider rejected these credentials for the %s sync: %s", schedule, cause.Error()))
}
