package platformmcp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/directory"
	pluginaudience "github.com/speakeasy-api/gram/server/internal/plugins/audience"
)

// TestListPluginsOutput_ProjectsOnlyAllowlistedFields pins the inventory's
// serialized shape. The projection is positive: a field not listed here is not
// served, so a future addition has to be made deliberately in this test before
// it can reach a caller. Nothing here carries a principal URN, a user id, a
// repository name, or a server URL.
func TestListPluginsOutput_ProjectsOnlyAllowlistedFields(t *testing.T) {
	t.Parallel()

	output := ListPluginsOutput{
		ProjectID: "00000000-0000-0000-0000-000000000001",
		Plugins: []Plugin{{
			ID:          "00000000-0000-0000-0000-0000000000a1",
			Name:        "Marketing",
			Slug:        "marketing",
			Description: "Tools the marketing team installs",
			IsDefault:   false,
			ServerCount: 3,
			SkillCount:  1,
			Audience:    PluginAudience{AllMembers: false, Roles: 2, Users: 4},
			Publication: PluginPublicationPublished,
		}},
		NextCursor: "opaque",
	}

	require.ElementsMatch(t, []string{
		"project_id",
		"plugins", "id", "name", "slug", "description", "is_default",
		"server_count", "skill_count",
		"audience", "all_members", "roles", "users",
		"publication",
		"next_cursor",
	}, decodeKeys(t, output))
}

func TestListPluginAudiencesOutput_ProjectsOnlyAllowlistedFields(t *testing.T) {
	t.Parallel()

	count := NewSubjectCount(8)
	output := ListPluginAudiencesOutput{
		ProjectID: "00000000-0000-0000-0000-000000000001",
		Audiences: []PluginAudienceOption{{
			Kind:        "role",
			DisplayName: "Engineering",
			MemberCount: &count,
			Reference:   "opaque",
		}},
		ReferencesExpireAt: "2026-09-04T12:10:00Z",
		Truncated:          false,
	}

	require.ElementsMatch(t, []string{
		"project_id", "audiences", "kind", "display_name", "member_count", "reference", "references_expire_at", "truncated",
	}, decodeKeys(t, output))
}

// TestGetPluginOutput_ProjectsOnlyAllowlistedFields does the same for one
// plugin's membership.
func TestGetPluginOutput_ProjectsOnlyAllowlistedFields(t *testing.T) {
	t.Parallel()

	output := GetPluginOutput{
		ProjectID: "00000000-0000-0000-0000-000000000001",
		Plugin: Plugin{
			ID:          "00000000-0000-0000-0000-0000000000a1",
			Name:        "Marketing",
			Slug:        "marketing",
			IsDefault:   true,
			ServerCount: 1,
			SkillCount:  1,
			Audience:    PluginAudience{AllMembers: true},
			Publication: PluginPublicationUnpublished,
		},
		Servers: []PluginServer{{
			DisplayName: "Billing",
			Backend:     "mcp_server",
			MCPSlug:     "billing",
			Policy:      "required",
			Enabled:     true,
		}},
		Skills: []PluginSkill{{
			Name:          "triage",
			FollowsLatest: true,
		}},
		AssignmentVersion: "opaque-version",
		Audiences: []PluginAudienceOption{{
			Kind:        "everyone",
			DisplayName: "Everyone",
			Reference:   "opaque-reference",
		}},
		AudienceDetailsComplete: true,
		AudiencesTruncated:      false,
		ReferencesExpireAt:      "2026-09-04T12:10:00Z",
		Truncated:               false,
	}

	require.ElementsMatch(t, []string{
		"project_id",
		"plugin", "id", "name", "slug", "is_default",
		"server_count", "skill_count",
		"audience", "all_members", "roles", "users",
		"publication",
		"servers", "display_name", "backend", "mcp_slug", "policy", "enabled",
		"skills", "name", "follows_latest",
		"assignment_version",
		"audiences", "kind", "display_name", "reference",
		"audience_details_complete", "audiences_truncated", "references_expire_at",
		"truncated",
	}, decodeKeys(t, output))
}

// TestMatchesTargetNameRefusesPartialMatches pins what an exact target means.
// A prefix or substring match is what turns "the marketing plugin" into
// someone else's plugin, so only an id, a slug, or a whole name matches.
func TestPluginAudienceReferenceIsEncryptedAndBound(t *testing.T) {
	t.Parallel()

	codec, err := newSubjectReferenceCodec("key-material")
	require.NoError(t, err)
	principal := testReferencePrincipal()
	projectID := "00000000-0000-0000-0000-000000000001"
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	principalURN := "role:organization:00000000-0000-0000-0000-0000000000a1"

	reference, err := codec.EncodeScoped(principal, subjectKindPluginAudience, projectID, principalURN, now)
	require.NoError(t, err)
	raw, err := base64.RawURLEncoding.DecodeString(reference)
	require.NoError(t, err)
	require.NotContains(t, string(raw), principalURN)
	require.NotContains(t, string(raw), principal.OrganizationID)

	resolved, err := codec.DecodeScoped(reference, principal, subjectKindPluginAudience, projectID, now)
	require.NoError(t, err)
	require.Equal(t, principalURN, resolved)

	otherOrganization := principal
	otherOrganization.OrganizationID = "org-2"
	_, err = codec.DecodeScoped(reference, otherOrganization, subjectKindPluginAudience, projectID, now)
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
	_, err = codec.DecodeScoped(reference, principal, subjectKindPluginAudience, "00000000-0000-0000-0000-000000000002", now)
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
	_, err = codec.DecodeScoped(reference, principal, subjectKindPluginAudience, projectID, now.Add(SubjectReferenceTTL))
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
}

func TestPluginAssignmentVersionCoversPluginAndCanonicalAssignmentSet(t *testing.T) {
	t.Parallel()

	key := []byte("version-key")
	projectID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	pluginID := uuid.MustParse("00000000-0000-0000-0000-0000000000a1")
	assignments := []string{"role:global:00000000-0000-0000-0000-0000000000b2", "*"}
	version := pluginAssignmentVersion(key, projectID, pluginID, assignments)

	require.Equal(t, version, pluginAssignmentVersion(key, projectID, pluginID, []string{"*", assignments[0], "*"}))
	require.NotEqual(t, version, pluginAssignmentVersion(key, projectID, pluginID, []string{"*"}))
	require.NotEqual(t, version, pluginAssignmentVersion(key, projectID, uuid.New(), assignments))
	require.NotEqual(t, version, pluginAssignmentVersion(key, uuid.New(), pluginID, assignments))
}

func TestPlatformAudienceDisplayNameRejectsSensitiveDirectoryAttributes(t *testing.T) {
	t.Parallel()

	visibleURN := directory.AttributePrincipal("department_name", "Engineering")
	visible, ok := platformAudienceDisplayName(pluginaudience.Audience{
		Kind:         "directory_attribute",
		DisplayName:  "department_name: Engineering",
		MemberCount:  nil,
		PrincipalURN: visibleURN,
	})
	require.True(t, ok)
	require.Equal(t, "department_name: Engineering", visible)

	sensitiveValue := "manager@example.com"
	hidden, ok := platformAudienceDisplayName(pluginaudience.Audience{
		Kind:         "directory_attribute",
		DisplayName:  "manager_email: " + sensitiveValue,
		MemberCount:  nil,
		PrincipalURN: directory.AttributePrincipal("manager_email", sensitiveValue),
	})
	require.False(t, ok)
	require.Empty(t, hidden)
}

func TestCurrentPluginAudiencesCanonicalizesAndHidesUnreviewedAssignments(t *testing.T) {
	t.Parallel()

	groupID := uuid.MustParse("00000000-0000-0000-0000-0000000000b2")
	available := []resolvedPluginAudience{
		{option: PluginAudienceOption{Kind: "everyone", DisplayName: "Everyone", Reference: "everyone-ref"}, principalURN: "*"},
		{option: PluginAudienceOption{Kind: "directory_group", DisplayName: "Engineering", Reference: "group-ref"}, principalURN: directory.GroupPrincipal(groupID)},
	}
	current, complete := currentPluginAudiences(available, []string{"user:private-user-id", "*", "directory_group:00000000-0000-0000-0000-0000000000B2"})

	require.False(t, complete)
	require.Equal(t, []PluginAudienceOption{
		{Kind: "everyone", DisplayName: "Everyone", Reference: "everyone-ref"},
		{Kind: "directory_group", DisplayName: "Engineering", Reference: "group-ref"},
	}, publicAudienceOptions(current))
	encoded, err := json.Marshal(publicAudienceOptions(current))
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-user-id")
	require.NotContains(t, string(encoded), "principal")
}

func TestProjectPluginAudiencesBoundsResultsAfterFiltering(t *testing.T) {
	t.Parallel()

	audiences := make([]pluginaudience.Audience, 0, maxPluginMembers+2)
	for index := range maxPluginMembers + 1 {
		principalURN := fmt.Sprintf("role:organization:%03d", index)
		audiences = append(audiences, pluginaudience.Audience{
			Kind:         "role",
			DisplayName:  fmt.Sprintf("Role %03d", index),
			MemberCount:  nil,
			PrincipalURN: principalURN,
		})
	}
	// A sensitive attribute does not consume an output slot or leak its value.
	audiences = append(audiences, pluginaudience.Audience{
		Kind:         "directory_attribute",
		DisplayName:  "manager_email: private@example.com",
		MemberCount:  nil,
		PrincipalURN: directory.AttributePrincipal("manager_email", "private@example.com"),
	})

	projected, truncated, err := projectPluginAudiences(audiences, nil, func(principalURN string) (string, error) {
		return "ref:" + principalURN, nil
	})
	require.NoError(t, err)
	require.True(t, truncated)
	require.Len(t, projected, maxPluginMembers)
	encoded, err := json.Marshal(publicAudienceOptions(projected))
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private@example.com")

	selectedURN := audiences[maxPluginMembers].PrincipalURN
	selected, selectedTruncated, err := projectPluginAudiences(audiences, []string{selectedURN}, func(principalURN string) (string, error) {
		return "ref:" + principalURN, nil
	})
	require.NoError(t, err)
	require.False(t, selectedTruncated)
	require.Len(t, selected, 1)
	require.Equal(t, selectedURN, selected[0].principalURN)
}

func TestPluginAssignmentVersionCanonicalizesDirectoryPrincipals(t *testing.T) {
	t.Parallel()

	key := []byte("version-key")
	projectID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	pluginID := uuid.MustParse("00000000-0000-0000-0000-0000000000a1")
	canonical := directory.GroupPrincipal(uuid.MustParse("00000000-0000-0000-0000-0000000000b2"))
	legacyCase := "directory_group:00000000-0000-0000-0000-0000000000B2"

	require.Equal(t,
		pluginAssignmentVersion(key, projectID, pluginID, []string{canonical}),
		pluginAssignmentVersion(key, projectID, pluginID, []string{legacyCase}),
	)
}

func TestMatchesTargetNameRefusesPartialMatches(t *testing.T) {
	t.Parallel()

	id := "00000000-0000-0000-0000-0000000000a1"
	for _, test := range []struct {
		name   string
		wanted string
		want   bool
	}{
		{name: "id", wanted: id, want: true},
		{name: "slug", wanted: "marketing", want: true},
		{name: "slug case insensitive", wanted: "MARKETING", want: true},
		{name: "exact name", wanted: "Marketing Tools", want: true},
		{name: "name case insensitive", wanted: "marketing tools", want: true},
		{name: "prefix of name that is also the slug", wanted: "Marketing", want: true},
		{name: "substring of name", wanted: "Tools", want: false},
		{name: "prefix of slug", wanted: "market", want: false},
		{name: "empty", wanted: "", want: false},
	} {
		got := matchesTargetName(id, "marketing", "Marketing Tools", test.wanted)
		require.Equal(t, test.want, got, test.name)
	}
}

// TestPluginCursorRefusesAnotherPrincipalsCursor keeps a page cursor bound to
// the connection and project that issued it: a cursor is a server-issued
// position, never a way to read another organization's listing.
func TestPluginCursorRefusesAnotherPrincipalsCursor(t *testing.T) {
	t.Parallel()

	codec, err := newPluginCursorCodec("test-cursor-key")
	require.NoError(t, err)

	projectID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	principal := Principal{OrganizationID: "org_1", ConnectionID: "conn_1", Generation: "gen_1"}
	after := uuid.MustParse("00000000-0000-0000-0000-0000000000a1")

	cursor, err := codec.Encode(pluginCursor{
		OrganizationID: principal.OrganizationID,
		Binding:        principalCursorBinding(principal),
		ProjectID:      projectID.String(),
		AfterPluginID:  after.String(),
	})
	require.NoError(t, err)

	decoded, err := codec.Decode(cursor, principal, projectID)
	require.NoError(t, err)
	require.Equal(t, after, decoded)

	_, err = codec.Decode(cursor, Principal{OrganizationID: "org_2", ConnectionID: "conn_1", Generation: "gen_1"}, projectID)
	require.ErrorIs(t, err, ErrPluginCursorInvalid)

	_, err = codec.Decode(cursor, principal, uuid.MustParse("00000000-0000-0000-0000-000000000002"))
	require.ErrorIs(t, err, ErrPluginCursorInvalid)

	_, err = codec.Decode(cursor, Principal{OrganizationID: "org_1", ConnectionID: "conn_2", Generation: "gen_2"}, projectID)
	require.ErrorIs(t, err, ErrPluginCursorInvalid)
}

// TestPluginCursorTreatsNoCursorAsTheFirstPage keeps an omitted cursor from
// being reported as tampering.
func TestPluginCursorTreatsNoCursorAsTheFirstPage(t *testing.T) {
	t.Parallel()

	codec, err := newPluginCursorCodec("test-cursor-key")
	require.NoError(t, err)

	after, err := codec.Decode("", Principal{OrganizationID: "org_1"}, uuid.MustParse("00000000-0000-0000-0000-000000000001"))
	require.NoError(t, err)
	require.Equal(t, uuid.Nil, after)
}
