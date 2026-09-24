//nolint:exhaustruct // Attribution projections intentionally omit documented optional fields.
package platformmcp

import (
	"context"
	"fmt"
	"strings"

	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

const maxSkillUsageRows = 25

type QuerySkillUsageInput struct {
	ProjectID string `json:"project_id" jsonschema:"project ID to summarize"`
	Window    string `json:"window,omitempty" jsonschema:"observation window: 1h or 24h (default); this tool looks back at most 24h"`
}

type SkillUsage struct {
	SkillName   string       `json:"skill_name"`
	Activations int64        `json:"activations"`
	ActiveUsers SubjectCount `json:"active_users"`
	Errors      string       `json:"errors"`
}

type QuerySkillUsageOutput struct {
	ProjectID string       `json:"project_id"`
	Envelope  DataEnvelope `json:"data"`
	Skills    []SkillUsage `json:"skills"`
	Truncated bool         `json:"truncated"`
}

func (s *DiagnosticsService) QuerySkillUsage(ctx context.Context, principal Principal, input QuerySkillUsageInput) (QuerySkillUsageOutput, error) {
	if s == nil || !s.valid() || s.references == nil || !s.sensitiveBudget.valid() || !s.volume.valid() {
		return QuerySkillUsageOutput{}, ErrUnavailable
	}
	if input.ProjectID == "" {
		return QuerySkillUsageOutput{}, fmt.Errorf("project_id is required")
	}
	if err := s.sensitiveBudget.Allow(ctx, principal); err != nil {
		return QuerySkillUsageOutput{}, err
	}
	now := s.now()
	window, err := resolveWindow(input.Window, now, drilldownWindowSpec)
	if err != nil {
		return QuerySkillUsageOutput{}, err
	}
	if _, err := s.reader.FindMCP(ctx, principal, FindMCPInput{ProjectID: input.ProjectID, Limit: 1}); err != nil {
		return QuerySkillUsageOutput{}, fmt.Errorf("resolve skill usage project: %w", err)
	}
	if err := s.volume.AllowRows(ctx, principal, maxSkillUsageRows); err != nil {
		return QuerySkillUsageOutput{}, err
	}
	rows, err := s.telemetry.GetSkillsSummary(ctx, telemetryrepo.GetSkillsSummaryParams{
		GramProjectID:        input.ProjectID,
		TimeStart:            window.start.UnixNano(),
		TimeEnd:              window.end.UnixNano(),
		Filters:              nil,
		TypesToInclude:       nil,
		Limit:                maxSkillUsageRows + 1,
		CanonicalIdentityOrg: s.canonicalIdentityOrg(ctx, principal.OrganizationID),
	})
	if err != nil {
		return QuerySkillUsageOutput{}, fmt.Errorf("read skill usage: %w", err)
	}
	watermark, err := s.telemetry.GetTelemetryWatermark(ctx, telemetryrepo.GetTelemetryWatermarkParams{GramProjectIDs: []string{input.ProjectID}})
	if err != nil {
		return QuerySkillUsageOutput{}, fmt.Errorf("read skill usage watermark: %w", err)
	}
	truncated := len(rows) > maxSkillUsageRows
	if truncated {
		rows = rows[:maxSkillUsageRows]
	}
	skills := make([]SkillUsage, 0, len(rows))
	for _, row := range rows {
		skills = append(skills, SkillUsage{
			SkillName:   row.SkillName,
			Activations: boundedCount(row.UseCount),
			ActiveUsers: NewSubjectCount(boundedCount(row.UniqueUsers)),
			Errors:      "not_recorded",
		})
	}
	return QuerySkillUsageOutput{
		ProjectID: input.ProjectID,
		Envelope:  newDataEnvelope(now, watermarkTime(watermark), window, len(rows) > 0),
		Skills:    skills,
		Truncated: truncated,
	}, nil
}

type ListSkillUsageUsersInput struct {
	ProjectID string `json:"project_id" jsonschema:"project ID that owns the skill"`
	SkillName string `json:"skill_name" jsonschema:"exact canonical skill name returned by query_skill_usage"`
	Window    string `json:"window,omitempty" jsonschema:"observation window: 1h or 24h (default); this tool looks back at most 24h"`
}

type SkillUsageUser struct {
	SubjectReference string `json:"subject_reference"`
	MaskedIdentity   string `json:"masked_identity"`
	Activity         string `json:"activity"`
	Errors           string `json:"errors"`
}

type ListSkillUsageUsersOutput struct {
	ProjectID string           `json:"project_id"`
	SkillName string           `json:"skill_name"`
	Envelope  DataEnvelope     `json:"data"`
	Users     []SkillUsageUser `json:"users"`
	Truncated bool             `json:"truncated"`
}

func (s *DiagnosticsService) ListSkillUsageUsers(ctx context.Context, principal Principal, input ListSkillUsageUsersInput) (ListSkillUsageUsersOutput, error) {
	if s == nil || !s.valid() || s.references == nil || !s.sensitiveBudget.valid() || !s.volume.valid() {
		return ListSkillUsageUsersOutput{}, ErrUnavailable
	}
	input.SkillName = strings.TrimSpace(input.SkillName)
	if input.ProjectID == "" || input.SkillName == "" {
		return ListSkillUsageUsersOutput{}, fmt.Errorf("project_id and skill_name are required")
	}
	if err := s.sensitiveBudget.Allow(ctx, principal); err != nil {
		return ListSkillUsageUsersOutput{}, err
	}
	now := s.now()
	window, err := resolveWindow(input.Window, now, drilldownWindowSpec)
	if err != nil {
		return ListSkillUsageUsersOutput{}, err
	}
	if _, err := s.reader.FindMCP(ctx, principal, FindMCPInput{ProjectID: input.ProjectID, Limit: 1}); err != nil {
		return ListSkillUsageUsersOutput{}, fmt.Errorf("resolve skill usage project: %w", err)
	}
	if err := s.volume.AllowRows(ctx, principal, maxSkillUsageRows); err != nil {
		return ListSkillUsageUsersOutput{}, err
	}
	if err := s.auditor.RecordUsageAttributionRead(ctx, principal, input.ProjectID, "skill", input.SkillName, "multiple", string(window.Window)); err != nil {
		return ListSkillUsageUsersOutput{}, fmt.Errorf("record skill usage users read: %w", err)
	}
	rows, err := s.telemetry.GetSkillBreakdown(ctx, telemetryrepo.GetSkillBreakdownParams{
		GramProjectID:        input.ProjectID,
		TimeStart:            window.start.UnixNano(),
		TimeEnd:              window.end.UnixNano(),
		Filters:              nil,
		SkillNames:           []string{input.SkillName},
		Limit:                maxSkillUsageRows + 1,
		CanonicalIdentityOrg: s.canonicalIdentityOrg(ctx, principal.OrganizationID),
	})
	if err != nil {
		return ListSkillUsageUsersOutput{}, fmt.Errorf("read skill usage users: %w", err)
	}
	users := make([]SkillUsageUser, 0, len(rows))
	for _, row := range rows {
		email := strings.TrimSpace(row.UserEmail)
		reference, err := s.references.EncodeScoped(principal, subjectKindUser, skillUsageUserScope(input.ProjectID, input.SkillName, window), FormatSubjectIdentity(SubjectIdentityEmail, email), now)
		if err != nil {
			return ListSkillUsageUsersOutput{}, fmt.Errorf("mint skill usage user reference: %w", err)
		}
		users = append(users, SkillUsageUser{
			SubjectReference: reference,
			MaskedIdentity:   maskSubject(email),
			Activity:         "observed",
			Errors:           "not_recorded",
		})
	}
	truncated := len(users) > maxSkillUsageRows
	if truncated {
		users = users[:maxSkillUsageRows]
	}
	watermark, err := s.telemetry.GetTelemetryWatermark(ctx, telemetryrepo.GetTelemetryWatermarkParams{GramProjectIDs: []string{input.ProjectID}})
	if err != nil {
		return ListSkillUsageUsersOutput{}, fmt.Errorf("read skill usage watermark: %w", err)
	}
	return ListSkillUsageUsersOutput{
		ProjectID: input.ProjectID,
		SkillName: input.SkillName,
		Envelope:  newDataEnvelope(now, watermarkTime(watermark), window, len(rows) > 0),
		Users:     users,
		Truncated: truncated,
	}, nil
}

type GetUserSkillStatusInput struct {
	ProjectID        string `json:"project_id" jsonschema:"project ID that owns the skill"`
	SkillName        string `json:"skill_name" jsonschema:"exact canonical skill name returned by query_skill_usage"`
	SubjectReference string `json:"subject_reference" jsonschema:"opaque subject reference returned by list_skill_usage_users"`
	Window           string `json:"window,omitempty" jsonschema:"observation window: 1h or 24h (default); must match the window used to mint the reference"`
}

type GetUserSkillStatusOutput struct {
	ProjectID      string       `json:"project_id"`
	SkillName      string       `json:"skill_name"`
	Envelope       DataEnvelope `json:"data"`
	MaskedIdentity string       `json:"masked_identity"`
	Activity       string       `json:"activity"`
	Errors         string       `json:"errors"`
}

func (s *DiagnosticsService) GetUserSkillStatus(ctx context.Context, principal Principal, input GetUserSkillStatusInput) (GetUserSkillStatusOutput, error) {
	if s == nil || !s.valid() || s.references == nil || !s.sensitiveBudget.valid() {
		return GetUserSkillStatusOutput{}, ErrUnavailable
	}
	input.SkillName = strings.TrimSpace(input.SkillName)
	if input.ProjectID == "" || input.SkillName == "" || input.SubjectReference == "" {
		return GetUserSkillStatusOutput{}, fmt.Errorf("project_id, skill_name, and subject_reference are required")
	}
	if err := s.sensitiveBudget.Allow(ctx, principal); err != nil {
		return GetUserSkillStatusOutput{}, err
	}
	now := s.now()
	window, err := resolveWindow(input.Window, now, drilldownWindowSpec)
	if err != nil {
		return GetUserSkillStatusOutput{}, err
	}
	if _, err := s.reader.FindMCP(ctx, principal, FindMCPInput{ProjectID: input.ProjectID, Limit: 1}); err != nil {
		return GetUserSkillStatusOutput{}, fmt.Errorf("resolve skill usage project: %w", err)
	}
	subject, err := s.references.DecodeScoped(input.SubjectReference, principal, subjectKindUser, skillUsageUserScope(input.ProjectID, input.SkillName, window), now)
	if err != nil {
		return GetUserSkillStatusOutput{}, ErrSubjectReferenceNotFound
	}
	identityKind, identifier, err := parseSubjectIdentity(subject)
	if err != nil || identityKind != SubjectIdentityEmail {
		return GetUserSkillStatusOutput{}, ErrSubjectReferenceNotFound
	}
	maskedIdentity := maskSubject(identifier)
	if err := s.auditor.RecordUsageAttributionRead(ctx, principal, input.ProjectID, "skill", input.SkillName, maskedIdentity, string(window.Window)); err != nil {
		return GetUserSkillStatusOutput{}, fmt.Errorf("record user skill status read: %w", err)
	}
	rows, err := s.telemetry.GetSkillBreakdown(ctx, telemetryrepo.GetSkillBreakdownParams{
		GramProjectID: input.ProjectID,
		TimeStart:     window.start.UnixNano(),
		TimeEnd:       window.end.UnixNano(),
		Filters: []telemetryrepo.AttributeFilter{{
			Path:   "user.email",
			Op:     "eq",
			Values: []string{identifier},
		}},
		SkillNames:           []string{input.SkillName},
		Limit:                1,
		CanonicalIdentityOrg: s.canonicalIdentityOrg(ctx, principal.OrganizationID),
	})
	if err != nil {
		return GetUserSkillStatusOutput{}, fmt.Errorf("read user skill activity: %w", err)
	}
	watermark, err := s.telemetry.GetTelemetryWatermark(ctx, telemetryrepo.GetTelemetryWatermarkParams{GramProjectIDs: []string{input.ProjectID}})
	if err != nil {
		return GetUserSkillStatusOutput{}, fmt.Errorf("read skill usage watermark: %w", err)
	}
	activity := SubjectStateInactive
	if len(rows) > 0 {
		activity = SubjectStateActive
	}
	return GetUserSkillStatusOutput{
		ProjectID:      input.ProjectID,
		SkillName:      input.SkillName,
		Envelope:       newDataEnvelope(now, watermarkTime(watermark), window, len(rows) > 0),
		MaskedIdentity: maskedIdentity,
		Activity:       activity,
		Errors:         "not_recorded",
	}, nil
}

func skillUsageUserScope(projectID, skillName string, window ResolvedWindow) string {
	return queryScope(projectID, skillName, string(window.Window))
}
