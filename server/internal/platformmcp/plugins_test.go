package platformmcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
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
		Truncated: false,
	}

	require.ElementsMatch(t, []string{
		"project_id",
		"plugin", "id", "name", "slug", "is_default",
		"server_count", "skill_count",
		"audience", "all_members", "roles", "users",
		"publication",
		"servers", "display_name", "backend", "mcp_slug", "policy", "enabled",
		"skills", "name", "follows_latest",
		"truncated",
	}, decodeKeys(t, output))
}

// TestMatchesTargetNameRefusesPartialMatches pins what an exact target means.
// A prefix or substring match is what turns "the marketing plugin" into
// someone else's plugin, so only an id, a slug, or a whole name matches.
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
