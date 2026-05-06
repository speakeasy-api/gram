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
  // search_messages_and_files (search.all) requires a USER token scope
  // (search:read) that Slack will not grant on a bot token, so it is omitted
  // from the bot-scope manifest deliberately.
};

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

const BASELINE_BOT_SCOPES: readonly string[] = [
  "chat:write",
  "im:write",
  "users:read",
];

// Slack manifests are static and the user can't easily edit them after
// install, so subscribe to every event the trigger service supports up
// front. The trigger config's `event_types` filters at delivery time.
const ALL_EVENT_BOT_EVENTS: readonly string[] = Array.from(
  new Set(
    Object.values(SLACK_EVENT_BINDINGS).flatMap((b) =>
      Array.from(b.bot_events),
    ),
  ),
).sort();
const ALL_EVENT_SCOPES: readonly string[] = Array.from(
  new Set(
    Object.values(SLACK_EVENT_BINDINGS).flatMap((b) => Array.from(b.scopes)),
  ),
).sort();

const SLACK_DISPLAY_NAME_LIMIT = 35;

export type SlackManifestInput = {
  appName: string;
  toolUrns?: readonly string[];
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
  botEvents: string[];
  unmappedToolUrns: string[];
  searchToolNeedsUserToken: boolean;
};

function uniqueSorted(values: Iterable<string>): string[] {
  return Array.from(new Set(values)).sort();
}

function handlerFromUrn(urn: string): string | null {
  if (!urn.startsWith(SLACK_TOOL_URN_PREFIX)) return null;
  return urn.slice(SLACK_TOOL_URN_PREFIX.length);
}

export function buildSlackManifest(
  input: SlackManifestInput,
): SlackManifestResult {
  const displayName = (input.appName.trim() || "Gram Assistant").slice(
    0,
    SLACK_DISPLAY_NAME_LIMIT,
  );

  const scopes = new Set<string>([...BASELINE_BOT_SCOPES, ...ALL_EVENT_SCOPES]);
  const botEvents = new Set<string>(ALL_EVENT_BOT_EVENTS);
  const unmappedToolUrns: string[] = [];
  let searchToolNeedsUserToken = false;

  for (const urn of input.toolUrns ?? []) {
    const handler = handlerFromUrn(urn);
    if (!handler) continue;
    const mapped = SLACK_TOOL_SCOPES[handler];
    if (mapped) {
      for (const s of mapped) scopes.add(s);
      continue;
    }
    if (handler === "search_messages_and_files") {
      searchToolNeedsUserToken = true;
      continue;
    }
    unmappedToolUrns.push(urn);
  }

  for (const s of input.extraScopes ?? []) scopes.add(s);
  for (const e of input.extraBotEvents ?? []) botEvents.add(e);

  const sortedScopes = uniqueSorted(scopes);
  const sortedEvents = uniqueSorted(botEvents);

  const manifest: Record<string, unknown> = {
    _metadata: { major_version: 1, minor_version: 1 },
    display_information: { name: displayName },
    features: {
      bot_user: { display_name: displayName, always_online: true },
    },
    oauth_config: { scopes: { bot: sortedScopes } },
  };
  if (input.webhookUrl) {
    manifest.settings = {
      event_subscriptions: {
        request_url: input.webhookUrl,
        bot_events: sortedEvents,
      },
    };
  } else if (sortedEvents.length > 0) {
    manifest.settings = {
      event_subscriptions: { bot_events: sortedEvents },
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
    botEvents: sortedEvents,
    unmappedToolUrns,
    searchToolNeedsUserToken,
  };
}

export type SlackContextSources = {
  attachedToolsetSlugs: readonly string[];
  toolsetsBySlug: ReadonlyMap<string, { toolUrns: readonly string[] }>;
  triggers: readonly {
    definitionSlug: string;
    targetKind?: string;
    targetRef?: string;
    config: { [k: string]: unknown };
  }[];
  assistantId?: string | null;
};

export function deriveSlackContext(sources: SlackContextSources): {
  toolUrns: string[];
  eventTypes: string[];
} {
  const toolUrns = new Set<string>();
  for (const slug of sources.attachedToolsetSlugs) {
    const ts = sources.toolsetsBySlug.get(slug);
    if (!ts) continue;
    for (const urn of ts.toolUrns) {
      if (urn.startsWith(SLACK_TOOL_URN_PREFIX)) toolUrns.add(urn);
    }
  }

  const eventTypes = new Set<string>();
  for (const trigger of sources.triggers) {
    if (trigger.definitionSlug !== "slack") continue;
    if (
      sources.assistantId &&
      trigger.targetKind === "assistant" &&
      trigger.targetRef !== sources.assistantId
    ) {
      continue;
    }
    const raw = trigger.config["event_types"];
    if (!Array.isArray(raw)) continue;
    for (const e of raw) {
      if (typeof e === "string" && e.length > 0) eventTypes.add(e);
    }
  }

  return {
    toolUrns: Array.from(toolUrns).sort(),
    eventTypes: Array.from(eventTypes).sort(),
  };
}
