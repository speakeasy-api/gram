import { SLACK_TOOL_URN_PREFIX } from "./slackManifest";

const URN = (handler: string) => `${SLACK_TOOL_URN_PREFIX}${handler}`;

export type SlackCapabilityGroup = {
  slug: string;
  label: string;
  description: string;
  toolUrns: readonly string[];
};

export const SLACK_CAPABILITY_GROUPS: readonly SlackCapabilityGroup[] = [
  {
    slug: "send",
    label: "Send messages",
    description:
      "Post replies, schedule messages, and edit or delete its own posts.",
    toolUrns: [
      URN("send_message"),
      URN("schedule_message"),
      URN("update_message"),
      URN("delete_message"),
      URN("delete_scheduled_message"),
      URN("list_scheduled_messages"),
      URN("me_message"),
      URN("post_ephemeral"),
      URN("get_permalink"),
    ],
  },
  {
    slug: "read",
    label: "Read messages",
    description: "Read channel history and threads it has access to.",
    toolUrns: [URN("read_channel_messages"), URN("read_thread_messages")],
  },
  {
    slug: "search",
    label: "Search",
    description: "Find channels, messages, and people by keyword.",
    toolUrns: [
      URN("search_channels"),
      URN("search_messages_and_files"),
      URN("search_users"),
    ],
  },
  {
    slug: "people",
    label: "Look up people",
    description: "Read profiles, find people by email, and check who's around.",
    toolUrns: [
      URN("read_user_profile"),
      URN("lookup_user_by_email"),
      URN("list_user_conversations"),
      URN("get_user_presence"),
      URN("get_user_profile_fields"),
      URN("get_user_dnd"),
      URN("get_team_dnd"),
      URN("get_team_info"),
      URN("list_usergroups"),
      URN("list_usergroup_members"),
    ],
  },
  {
    slug: "react",
    label: "React with emoji",
    description: "Add and remove emoji reactions on messages.",
    toolUrns: [
      URN("add_reaction"),
      URN("remove_reaction"),
      URN("get_reactions"),
      URN("list_reactions"),
      URN("list_emoji"),
    ],
  },
  {
    slug: "channels",
    label: "Manage channels",
    description:
      "Join, leave, create, archive, rename, and invite people to channels.",
    toolUrns: [
      URN("open_conversation"),
      URN("create_channel"),
      URN("join_channel"),
      URN("leave_channel"),
      URN("invite_to_channel"),
      URN("set_channel_topic"),
      URN("set_channel_purpose"),
      URN("mark_conversation"),
      URN("archive_channel"),
      URN("unarchive_channel"),
      URN("rename_channel"),
      URN("remove_from_channel"),
      URN("get_channel_info"),
      URN("list_channel_members"),
    ],
  },
  {
    slug: "files",
    label: "Files",
    description: "Upload, list, and delete files.",
    toolUrns: [
      URN("upload_file"),
      URN("get_file_info"),
      URN("list_files"),
      URN("delete_file"),
    ],
  },
  {
    slug: "pins",
    label: "Pins & bookmarks",
    description: "Pin messages and manage channel bookmarks.",
    toolUrns: [
      URN("pin_message"),
      URN("unpin_message"),
      URN("list_pins"),
      URN("add_bookmark"),
      URN("edit_bookmark"),
      URN("remove_bookmark"),
      URN("list_bookmarks"),
    ],
  },
  {
    slug: "reminders",
    label: "Reminders",
    description: "Create, view, complete, and delete reminders.",
    toolUrns: [
      URN("add_reminder"),
      URN("complete_reminder"),
      URN("delete_reminder"),
      URN("get_reminder"),
      URN("list_reminders"),
    ],
  },
  {
    slug: "canvases",
    label: "Canvases",
    description: "Create and edit Slack canvases for richer docs.",
    toolUrns: [
      URN("create_canvas"),
      URN("edit_canvas"),
      URN("delete_canvas"),
      URN("lookup_canvas_sections"),
      URN("set_canvas_access"),
      URN("remove_canvas_access"),
      URN("create_channel_canvas"),
    ],
  },
];

export type SlackEventGroup = {
  slug: string;
  label: string;
  description: string;
  eventTypes: readonly string[];
  noisy?: boolean;
};

export const SLACK_EVENT_GROUPS: readonly SlackEventGroup[] = [
  {
    slug: "mentions",
    label: "When @-mentioned",
    description:
      "Wake up only when someone @-mentions the assistant in a channel.",
    eventTypes: ["app_mention"],
  },
  {
    slug: "messages",
    label: "Every message",
    description:
      "Wake up on every message in any channel or DM the assistant can see. Pair with a narrower filter — without one this fires constantly.",
    eventTypes: ["message"],
    noisy: true,
  },
  {
    slug: "reactions",
    label: "Emoji reactions",
    description: "Wake up when someone adds or removes an emoji reaction.",
    eventTypes: ["reaction_added", "reaction_removed"],
  },
  {
    slug: "channel-changes",
    label: "Channel changes",
    description:
      "Wake up when channels are created, renamed, archived, or unarchived.",
    eventTypes: [
      "channel_created",
      "channel_rename",
      "channel_archive",
      "channel_unarchive",
      "channel_deleted",
      "channel_id_changed",
      "channel_left",
      "group_archive",
      "group_deleted",
      "group_left",
      "group_rename",
      "group_unarchive",
    ],
  },
  {
    slug: "files",
    label: "Files & uploads",
    description: "Wake up when files are uploaded, shared, or removed.",
    eventTypes: [
      "file_created",
      "file_change",
      "file_deleted",
      "file_public",
      "file_shared",
      "file_unshared",
    ],
  },
  {
    slug: "people",
    label: "People & membership",
    description:
      "Wake up when people join channels, leave, or update their profile.",
    eventTypes: [
      "member_joined_channel",
      "member_left_channel",
      "team_join",
      "user_change",
    ],
  },
  {
    slug: "pins-and-links",
    label: "Pins & shared links",
    description:
      "Wake up when messages are pinned, unpinned, or links are shared.",
    eventTypes: ["pin_added", "pin_removed", "link_shared"],
  },
];

export function expandCapabilities(slugs: readonly string[]): string[] {
  const urns = new Set<string>();
  for (const g of SLACK_CAPABILITY_GROUPS) {
    if (!slugs.includes(g.slug)) continue;
    for (const u of g.toolUrns) urns.add(u);
  }
  return Array.from(urns);
}

export function expandEvents(slugs: readonly string[]): string[] {
  const types = new Set<string>();
  for (const g of SLACK_EVENT_GROUPS) {
    if (!slugs.includes(g.slug)) continue;
    for (const t of g.eventTypes) types.add(t);
  }
  return Array.from(types);
}

export function getCapabilityGroup(
  slug: string,
): SlackCapabilityGroup | undefined {
  return SLACK_CAPABILITY_GROUPS.find((g) => g.slug === slug);
}

export function getEventGroup(slug: string): SlackEventGroup | undefined {
  return SLACK_EVENT_GROUPS.find((g) => g.slug === slug);
}
