package platformmcp

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/authz"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

// chatEvidenceParticipantLimit bounds metadata independently of transcript bytes.
const chatEvidenceParticipantLimit = 16

// ChatEvidenceParticipantFilter selects observed messages, not their executors.
type ChatEvidenceParticipantFilter struct {
	// UserID selects an exact recorded or explicitly resolved internal identity.
	UserID string `json:"user_id,omitempty"`
	// Provider is required with ProviderUserID, instead of UserID.
	Provider string `json:"provider,omitempty"`
	// ProviderUserID selects an exact captured provider identity.
	ProviderUserID string `json:"provider_user_id,omitempty"`
	// ProviderTeamID optionally disambiguates a provider workspace.
	ProviderTeamID string `json:"provider_team_id,omitempty"`
}

// ChatEvidenceEventTime is a claim in captured content, not a verified event.
type ChatEvidenceEventTime struct {
	// At preserves the provider timestamp independently of stored ordering.
	At time.Time `json:"at"`
	// Provider identifies the recognized envelope convention.
	Provider string `json:"provider"`
	// Path identifies the exact field from which the claim was extracted.
	Path string `json:"path"`
	// Trust explicitly distinguishes content claims from verified provider data.
	Trust string `json:"trust"`
}

// ChatEvidenceParticipant separates observed identity from ownership and access.
type ChatEvidenceParticipant struct {
	// Provider identifies the recorded or recognized provider convention.
	Provider string `json:"provider"`
	// ProviderUserID is the exact sanitized provider identity.
	ProviderUserID string `json:"provider_user_id"`
	// ProviderTeamID is a recorded workspace hint when available.
	ProviderTeamID string `json:"provider_team_id,omitempty"`
	// UserID is the internal identity in a recorded participant snapshot only.
	UserID string `json:"user_id,omitempty"`
	// DisplayName is the recorded snapshot name, never refreshed implicitly.
	DisplayName string `json:"display_name,omitempty"`
	// Provenance is recorded_participant_snapshot or captured_content_claim.
	Provenance string `json:"provenance"`
	// RecordedAt is when the participant snapshot was stored, when one exists.
	RecordedAt *time.Time `json:"recorded_at,omitempty"`
	// CurrentIdentity is optional, separately authorized current directory data.
	CurrentIdentity *ChatEvidenceCurrentIdentity `json:"current_identity,omitempty"`
}

// ChatEvidenceCurrentIdentity cannot establish a historical actor's identity.
type ChatEvidenceCurrentIdentity struct {
	// UserID is the currently confirmed organization-local mapping, if any.
	UserID string `json:"user_id,omitempty"`
	// DisplayName is the current directory snapshot name.
	DisplayName string `json:"display_name,omitempty"`
	// MappingConfirmedAt dates the current mapping independently of the message.
	MappingConfirmedAt *time.Time `json:"mapping_confirmed_at,omitempty"`
	// DirectoryObservedAt dates the directory snapshot independently of the message.
	DirectoryObservedAt time.Time `json:"directory_observed_at"`
}

type chatEvidenceAttributionInput struct {
	Content string
	Role    string
	Source  string
}

type chatEvidenceAttribution struct {
	Participants []ChatEvidenceParticipant
	Truncated    bool
	EventTimes   []ChatEvidenceEventTime
}

type chatEvidenceSlackIdentity struct {
	// ProviderUserID is an identity already observed in an admitted message.
	ProviderUserID string `json:"provider_user_id"`
	// ProviderTeamID is the captured workspace hint, empty when unavailable.
	ProviderTeamID string `json:"provider_team_id"`
}

func validParticipantFilter(filter *ChatEvidenceParticipantFilter) bool {
	if filter == nil {
		return true
	}
	for _, value := range []string{filter.UserID, filter.Provider, filter.ProviderUserID, filter.ProviderTeamID} {
		if !validEvidenceFilter(value, 128) {
			return false
		}
	}
	if filter.UserID != "" {
		return filter.Provider == "" && filter.ProviderUserID == "" && filter.ProviderTeamID == ""
	}
	return filter.Provider != "" && filter.ProviderUserID != ""
}

func matchesEvidenceParticipant(participants []ChatEvidenceParticipant, filter *ChatEvidenceParticipantFilter) bool {
	for _, p := range participants {
		if filter.UserID != "" {
			if p.UserID == filter.UserID || (p.CurrentIdentity != nil && p.CurrentIdentity.UserID == filter.UserID) {
				return true
			}
		} else if p.Provider == filter.Provider && p.ProviderUserID == filter.ProviderUserID && (filter.ProviderTeamID == "" || p.ProviderTeamID == filter.ProviderTeamID) {
			return true
		}
	}
	return false
}

func (s *ChatSearchService) authorizeParticipantResolution(ctx context.Context, principal Principal, resolve bool) error {
	if !resolve {
		return nil
	}
	if err := s.engine.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: principal.OrganizationID, Dimensions: nil}); err != nil {
		return &ExternalAuthorizationError{RequiredScope: string(authz.ScopeOrgAdmin), RequestAccessURL: "", cause: err}
	}
	return nil
}

// loadEvidenceAttributions reads only bounded, already admitted message IDs.
// Directory data is fetched only after the caller's separate org:admin check.
func (s *ChatSearchService) loadEvidenceAttributions(ctx context.Context, org string, project uuid.UUID, inputs map[string]chatEvidenceAttributionInput, resolve bool, filter *ChatEvidenceParticipantFilter) (map[string]chatEvidenceAttribution, error) {
	output := make(map[string]chatEvidenceAttribution, len(inputs))
	ids := make([]uuid.UUID, 0, len(inputs))
	for id := range inputs {
		ids = append(ids, uuid.MustParse(id))
		output[id] = chatEvidenceAttribution{Participants: []ChatEvidenceParticipant{}, Truncated: false, EventTimes: []ChatEvidenceEventTime{}}
	}
	if len(ids) == 0 {
		return output, nil
	}
	queries := platformrepo.New(s.db)
	selection := ChatEvidenceParticipantFilter{UserID: "", Provider: "", ProviderUserID: "", ProviderTeamID: ""}
	if filter != nil {
		selection = *filter
	}
	rows, err := queries.ListPlatformChatEvidenceParticipants(ctx, platformrepo.ListPlatformChatEvidenceParticipantsParams{OrganizationID: org, ProjectID: uuid.NullUUID{UUID: project, Valid: true}, MessageIds: ids, ParticipantRowLimit: chatEvidenceParticipantLimit + 1, ParticipantUserID: selection.UserID, ParticipantProvider: selection.Provider, ParticipantProviderUserID: selection.ProviderUserID, ParticipantProviderTeamID: selection.ProviderTeamID})
	if err != nil {
		return nil, fmt.Errorf("read captured message participants: %w", err)
	}
	sanitized := make(map[string]string)
	redact := func(value string) (string, error) {
		if safe, ok := sanitized[value]; ok {
			return safe, nil
		}
		safe, err := s.redact(ctx, value, 0)
		if err != nil {
			return "", err
		}
		sanitized[value] = safe
		return safe, nil
	}
	for _, row := range rows {
		id := row.MessageID.String()
		attribution := output[id]
		if len(attribution.Participants) == chatEvidenceParticipantLimit {
			attribution.Truncated = true
			output[id] = attribution
			continue
		}
		participant := ChatEvidenceParticipant{Provider: row.Provider, ProviderUserID: row.ProviderUserID, ProviderTeamID: row.ProviderTeamID, UserID: row.UserID, DisplayName: row.DisplayName, Provenance: "recorded_participant_snapshot", RecordedAt: new(row.CreatedAt.Time.UTC()), CurrentIdentity: nil}
		for _, field := range []*string{&participant.Provider, &participant.ProviderUserID, &participant.ProviderTeamID, &participant.UserID, &participant.DisplayName} {
			*field, err = redact(*field)
			if err != nil {
				return nil, err
			}
		}
		attribution.Participants = append(attribution.Participants, participant)
		output[id] = attribution
	}
	identities := map[chatEvidenceSlackIdentity]bool{}
	for id, input := range inputs {
		attribution := output[id]
		// Parse only the original content's leading envelope. Canonical JSON
		// leaves and tool results cannot manufacture provider provenance.
		header := slackEvidenceHeader(input.Content, input.Role, input.Source)
		if header != "" {
			safe, err := redact(header)
			if err != nil {
				return nil, err
			}
			sender, eventTime := parseSlackEvidenceHeader(safe)
			if eventTime != nil {
				attribution.EventTimes = append(attribution.EventTimes, *eventTime)
			}
			if sender != "" && !slices.ContainsFunc(attribution.Participants, func(p ChatEvidenceParticipant) bool { return p.Provider == "slack" && p.ProviderUserID == sender }) {
				if len(attribution.Participants) == chatEvidenceParticipantLimit {
					attribution.Truncated = true
				} else {
					attribution.Participants = append(attribution.Participants, ChatEvidenceParticipant{Provider: "slack", ProviderUserID: sender, ProviderTeamID: "", UserID: "", DisplayName: "", Provenance: "captured_content_claim", RecordedAt: nil, CurrentIdentity: nil})
				}
			}
		}
		for _, participant := range attribution.Participants {
			if participant.Provider == "slack" && slackEvidenceUserID.MatchString(participant.ProviderUserID) && !strings.Contains(participant.ProviderTeamID, "[") {
				identities[chatEvidenceSlackIdentity{ProviderUserID: participant.ProviderUserID, ProviderTeamID: participant.ProviderTeamID}] = true
			}
		}
		output[id] = attribution
	}
	if !resolve || len(identities) == 0 {
		return output, nil
	}
	userIDs := make([]string, 0, len(identities))
	teamIDs := make([]string, 0, len(identities))
	for identity := range identities {
		userIDs = append(userIDs, identity.ProviderUserID)
		teamIDs = append(teamIDs, identity.ProviderTeamID)
	}
	resolved, err := queries.ResolvePlatformChatEvidenceSlackIdentities(ctx, platformrepo.ResolvePlatformChatEvidenceSlackIdentitiesParams{OrganizationID: org, ProjectID: project, ProviderUserIds: userIDs, ProviderTeamIds: teamIDs})
	if err != nil {
		return nil, fmt.Errorf("resolve observed Slack identities: %w", err)
	}
	current := make(map[chatEvidenceSlackIdentity]*ChatEvidenceCurrentIdentity)
	for _, row := range resolved {
		user, err := s.redact(ctx, row.UserID, 0)
		if err != nil {
			return nil, err
		}
		name, err := s.redact(ctx, row.DisplayName, 0)
		if err != nil {
			return nil, err
		}
		var confirmed *time.Time
		if row.MappingConfirmedAt.Valid {
			confirmed = new(row.MappingConfirmedAt.Time.UTC())
		}
		current[chatEvidenceSlackIdentity{ProviderUserID: row.ProviderUserID, ProviderTeamID: row.ProviderTeamID}] = &ChatEvidenceCurrentIdentity{UserID: user, DisplayName: name, MappingConfirmedAt: confirmed, DirectoryObservedAt: row.DirectoryObservedAt.Time.UTC()}
	}
	for id, attribution := range output {
		for i := range attribution.Participants {
			p := &attribution.Participants[i]
			if p.Provider == "slack" {
				p.CurrentIdentity = current[chatEvidenceSlackIdentity{ProviderUserID: p.ProviderUserID, ProviderTeamID: p.ProviderTeamID}]
			}
		}
		output[id] = attribution
	}
	return output, nil
}

var slackEvidenceUserID = regexp.MustCompile(`^[UW][A-Z0-9]{8,32}$`)
var slackEvidenceEventID = regexp.MustCompile(`^[a-zA-Z0-9-]+:Ev[A-Z0-9]{8,32}$`)

// Only the Slack adapter's leading, bounded header is recognized. Assistant
// prose, nested quotes, other adapters and arbitrary JSON timestamps are not.
func slackEvidenceHeader(content, role, source string) string {
	if role != "user" || source != "assistants" {
		return ""
	}
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "<message-context>\n") {
		return ""
	}
	end := strings.Index(content, "</message-context>")
	if end < 0 || end > 4096 {
		return ""
	}
	return content[len("<message-context>\n"):end]
}

func parseSlackEvidenceHeader(header string) (string, *ChatEvidenceEventTime) {
	fields := make(map[string]string)
	for line := range strings.SplitSeq(header, "\n") {
		key, value, ok := strings.Cut(line, ": ")
		if !ok || (key != "EventID" && key != "EventType" && key != "UserID" && key != "Timestamp") {
			continue
		}
		if _, duplicate := fields[key]; duplicate {
			return "", nil
		}
		fields[key] = strings.TrimSpace(value)
	}
	if !slackEvidenceUserID.MatchString(fields["UserID"]) || !slackEvidenceEventID.MatchString(fields["EventID"]) || (fields["EventType"] != "message" && fields["EventType"] != "app_mention") {
		return "", nil
	}
	seconds, fraction, _ := strings.Cut(fields["Timestamp"], ".")
	sec, err := strconv.ParseInt(seconds, 10, 64)
	if err != nil || sec <= 0 || sec > 253402300799 || len(fraction) > 9 {
		return fields["UserID"], nil
	}
	if strings.Trim(fraction, "0123456789") != "" {
		return fields["UserID"], nil
	}
	nanos, err := strconv.ParseInt(fraction+strings.Repeat("0", 9-len(fraction)), 10, 64)
	if err != nil {
		return fields["UserID"], nil
	}
	return fields["UserID"], &ChatEvidenceEventTime{At: time.Unix(sec, nanos).UTC(), Provider: "slack", Path: "content.leading_message_context.Timestamp", Trust: "captured_content_claim"}
}
