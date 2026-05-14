export const SLACK_TOOL_URN_PREFIX = "tools:platform:slack:";

export const SLACK_TOOL_SCOPES: Record<string, readonly string[]> = {
  send_message: ["chat:write", "chat:write.public", "im:write"],
  schedule_message: ["chat:write", "chat:write.public"],
  read_channel_messages: [
    "channels:history",
    "groups:history",
    "im:history",
    "mpim:history",
  ],
  read_thread_messages: [
    "channels:history",
    "groups:history",
    "im:history",
    "mpim:history",
  ],
  search_channels: ["channels:read", "groups:read", "im:read", "mpim:read"],
  read_user_profile: ["users:read", "users:read.email"],
  search_users: ["users:read", "users:read.email"],
  add_reaction: ["reactions:write"],
  remove_reaction: ["reactions:write"],
  get_reactions: ["reactions:read"],
  list_reactions: ["reactions:read"],
  list_emoji: ["emoji:read"],
};

// Always grant the full user-scope superset alongside bot scopes. Slack mints
// both xoxb- and xoxp- at install; the assistant uses whichever fits each
// call site. No per-tool gating — same rationale as ALL_BOT_SCOPES: adding a
// scope post-install forces a re-install.
export const SLACK_USER_SCOPES: readonly string[] = [
  "channels:history",
  "channels:read",
  "emoji:read",
  "files:read",
  "groups:history",
  "groups:read",
  "im:history",
  "im:read",
  "links:read",
  "mpim:history",
  "mpim:read",
  "pins:read",
  "reactions:read",
  "search:read",
  "users:read",
  "users:read.email",
];

export type SlackEventBinding = {
  bot_events: readonly string[];
  scopes: readonly string[];
};

export const SLACK_EVENT_BINDINGS: Record<string, SlackEventBinding> = {
  app_home_opened: { bot_events: ["app_home_opened"], scopes: [] },
  app_mention: {
    bot_events: ["app_mention"],
    scopes: ["app_mentions:read"],
  },
  app_uninstalled: { bot_events: ["app_uninstalled"], scopes: [] },
  channel_archive: {
    bot_events: ["channel_archive"],
    scopes: ["channels:read"],
  },
  channel_created: {
    bot_events: ["channel_created"],
    scopes: ["channels:read"],
  },
  channel_deleted: {
    bot_events: ["channel_deleted"],
    scopes: ["channels:read"],
  },
  channel_id_changed: {
    bot_events: ["channel_id_changed"],
    scopes: ["channels:read"],
  },
  channel_left: { bot_events: ["channel_left"], scopes: ["channels:read"] },
  channel_rename: {
    bot_events: ["channel_rename"],
    scopes: ["channels:read"],
  },
  channel_unarchive: {
    bot_events: ["channel_unarchive"],
    scopes: ["channels:read"],
  },
  emoji_changed: { bot_events: ["emoji_changed"], scopes: ["emoji:read"] },
  file_change: { bot_events: ["file_change"], scopes: ["files:read"] },
  file_created: { bot_events: ["file_created"], scopes: ["files:read"] },
  file_deleted: { bot_events: ["file_deleted"], scopes: ["files:read"] },
  file_public: { bot_events: ["file_public"], scopes: ["files:read"] },
  file_shared: { bot_events: ["file_shared"], scopes: ["files:read"] },
  file_unshared: { bot_events: ["file_unshared"], scopes: ["files:read"] },
  group_archive: { bot_events: ["group_archive"], scopes: ["groups:read"] },
  group_deleted: { bot_events: ["group_deleted"], scopes: ["groups:read"] },
  group_left: { bot_events: ["group_left"], scopes: ["groups:read"] },
  group_rename: { bot_events: ["group_rename"], scopes: ["groups:read"] },
  group_unarchive: {
    bot_events: ["group_unarchive"],
    scopes: ["groups:read"],
  },
  link_shared: { bot_events: ["link_shared"], scopes: ["links:read"] },
  member_joined_channel: {
    bot_events: ["member_joined_channel"],
    scopes: ["channels:read", "groups:read"],
  },
  member_left_channel: {
    bot_events: ["member_left_channel"],
    scopes: ["channels:read", "groups:read"],
  },
  message: {
    bot_events: [
      "message.channels",
      "message.groups",
      "message.im",
      "message.mpim",
    ],
    scopes: [
      "channels:history",
      "groups:history",
      "im:history",
      "mpim:history",
    ],
  },
  pin_added: { bot_events: ["pin_added"], scopes: ["pins:read"] },
  pin_removed: { bot_events: ["pin_removed"], scopes: ["pins:read"] },
  reaction_added: {
    bot_events: ["reaction_added"],
    scopes: ["reactions:read"],
  },
  reaction_removed: {
    bot_events: ["reaction_removed"],
    scopes: ["reactions:read"],
  },
  team_join: { bot_events: ["team_join"], scopes: ["users:read"] },
  tokens_revoked: { bot_events: ["tokens_revoked"], scopes: [] },
  user_change: { bot_events: ["user_change"], scopes: ["users:read"] },
};

// Slack manifests are effectively static post-install: adding a scope or
// event after the user has clicked Create requires deleting the app and
// going through OAuth again. So we always grant the full bot-token
// superset — every scope referenced by any platform tool or trigger event
// — at install time. Per-tool gating saves nothing and locks future
// capabilities behind a forced re-install. The trigger config's
// `event_types` still filters delivery, so over-subscribing to events is
// harmless.
const ALL_BOT_SCOPES: readonly string[] = Array.from(
  new Set([
    ...Object.values(SLACK_TOOL_SCOPES).flatMap((s) => Array.from(s)),
    ...Object.values(SLACK_EVENT_BINDINGS).flatMap((b) => Array.from(b.scopes)),
  ]),
).sort();
const ALL_EVENT_BOT_EVENTS: readonly string[] = Array.from(
  new Set(
    Object.values(SLACK_EVENT_BINDINGS).flatMap((b) =>
      Array.from(b.bot_events),
    ),
  ),
).sort();

const SLACK_DISPLAY_NAME_LIMIT = 35;

export type SlackManifestInput = {
  appName: string;
  webhookUrl?: string | undefined;
  extraScopes?: readonly string[];
  extraBotEvents?: readonly string[];
};

export type SlackManifestResult = {
  manifest: Record<string, unknown>;
  manifestJson: string;
  deepLink: string;
  displayName: string;
  scopes: string[];
  userScopes: string[];
  botEvents: string[];
};

function uniqueSorted(values: Iterable<string>): string[] {
  return Array.from(new Set(values)).sort();
}

export function buildSlackManifest(
  input: SlackManifestInput,
): SlackManifestResult {
  const displayName = (input.appName.trim() || "Gram Assistant").slice(
    0,
    SLACK_DISPLAY_NAME_LIMIT,
  );

  const scopes = new Set<string>(ALL_BOT_SCOPES);
  for (const s of input.extraScopes ?? []) scopes.add(s);

  const userScopes = new Set<string>(SLACK_USER_SCOPES);

  // Slack rejects an event_subscriptions block whose request_url is missing
  // or unverified. Only emit bot_events when we have a webhook to anchor them
  // to — otherwise produce a scope-only manifest that Slack will accept.
  const botEvents = new Set<string>();
  if (input.webhookUrl) {
    for (const e of ALL_EVENT_BOT_EVENTS) botEvents.add(e);
    for (const e of input.extraBotEvents ?? []) botEvents.add(e);
  }

  const sortedScopes = uniqueSorted(scopes);
  const sortedUserScopes = uniqueSorted(userScopes);
  const sortedEvents = uniqueSorted(botEvents);

  const oauthScopes: { bot: string[]; user?: string[] } = { bot: sortedScopes };
  if (sortedUserScopes.length > 0) oauthScopes.user = sortedUserScopes;

  const manifest: Record<string, unknown> = {
    _metadata: { major_version: 1, minor_version: 1 },
    display_information: { name: displayName },
    features: {
      bot_user: { display_name: displayName, always_online: true },
    },
    oauth_config: { scopes: oauthScopes },
  };
  if (input.webhookUrl) {
    // Slack interactivity (button clicks, modal submissions) uses a separate
    // `request_url` from event_subscriptions, even though Gram answers both
    // on the same trigger webhook. Without this Slack shows "This app is
    // not configured to handle interactive responses" the moment a user
    // clicks a Block Kit button. Always enable it when we have a webhook —
    // same rationale as the all-scopes superset: missing it post-install
    // forces a re-install.
    manifest.settings = {
      event_subscriptions: {
        request_url: input.webhookUrl,
        bot_events: sortedEvents,
      },
      interactivity: {
        is_enabled: true,
        request_url: input.webhookUrl,
      },
    };
  }

  const manifestJson = JSON.stringify(manifest);
  const deepLink = `https://api.slack.com/apps?new_app=1&manifest_json=${encodeURIComponent(manifestJson)}`;

  return {
    manifest,
    manifestJson,
    deepLink,
    displayName,
    scopes: sortedScopes,
    userScopes: sortedUserScopes,
    botEvents: sortedEvents,
  };
}
