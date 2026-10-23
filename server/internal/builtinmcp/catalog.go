// Package builtinmcp defines code-owned MCP servers that exist in every project.
package builtinmcp

import (
	"net/url"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/platformtools"
)

const Slack = "slack"

var namespace = uuid.NewSHA1(uuid.NameSpaceURL, []byte("https://app.getgram.ai/builtin/mcp"))

// Definition is immutable product metadata, not a provisioned server record.
type Definition struct {
	Key                 string
	Name                string
	Description         string
	Tools               []platformtools.ToolDescriptor
	CredentialVariables []string
}

// ID is stable across project renames and settings changes.
func ID(projectID uuid.UUID, key string) uuid.UUID {
	return uuid.NewSHA1(namespace, []byte(projectID.String()+"/"+key))
}

func URL(base *url.URL, projectID uuid.UUID, key string) string {
	return base.JoinPath("mcp", "builtin", projectID.String(), key).String()
}

// The allowlist is deliberate: adding a global platform tool must not silently
// expose it through a built-in. Signed file-download URLs remain internal.
var slackHandlers = []string{
	"add_bookmark",
	"add_reaction",
	"add_reminder",
	"archive_channel",
	"complete_reminder",
	"create_canvas",
	"create_channel",
	"create_channel_canvas",
	"delete_canvas",
	"delete_file",
	"delete_message",
	"delete_reminder",
	"delete_scheduled_message",
	"edit_bookmark",
	"edit_canvas",
	"get_channel_info",
	"get_file_info",
	"get_permalink",
	"get_reactions",
	"get_reminder",
	"get_team_dnd",
	"get_team_info",
	"get_user_dnd",
	"get_user_presence",
	"get_user_profile_fields",
	"invite_to_channel",
	"join_channel",
	"leave_channel",
	"list_bookmarks",
	"list_channel_members",
	"list_emoji",
	"list_files",
	"list_pins",
	"list_reactions",
	"list_reminders",
	"list_scheduled_messages",
	"list_user_conversations",
	"list_usergroup_members",
	"list_usergroups",
	"lookup_canvas_sections",
	"lookup_user_by_email",
	"mark_conversation",
	"me_message",
	"open_conversation",
	"pin_message",
	"post_ephemeral",
	"read_channel_messages",
	"read_thread_messages",
	"read_user_profile",
	"remove_bookmark",
	"remove_canvas_access",
	"remove_from_channel",
	"remove_reaction",
	"rename_channel",
	"schedule_message",
	"search_channels",
	"search_messages_and_files",
	"search_users",
	"send_message",
	"set_canvas_access",
	"set_channel_purpose",
	"set_channel_topic",
	"set_thread_status",
	"unarchive_channel",
	"unpin_message",
	"update_message",
	"upload_file",
}

func Lookup(key string) (Definition, bool) {
	if key != Slack {
		var empty Definition
		return empty, false
	}
	byHandler := make(map[string]platformtools.ToolDescriptor)
	for _, tool := range platformtools.ListPlatformTools() {
		if tool.SourceSlug == Slack {
			byHandler[tool.HandlerName] = tool
		}
	}
	tools := make([]platformtools.ToolDescriptor, 0, len(slackHandlers))
	for _, handler := range slackHandlers {
		tool, ok := byHandler[handler]
		if !ok {
			panic("builtinmcp: missing Slack executor " + handler)
		}
		tools = append(tools, tool)
	}
	return Definition{Key: Slack, Name: "Slack", Description: "Send messages, manage channels, and read conversations in your Slack workspace.", Tools: tools, CredentialVariables: []string{"SLACK_BOT_TOKEN", "SLACK_USER_TOKEN", "SLACK_TOKEN"}}, true
}

func All() []Definition {
	slack, _ := Lookup(Slack)
	return []Definition{slack}
}
