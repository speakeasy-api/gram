import type { Assistant } from "@gram/client/models/components/assistant.js";
import type { Environment } from "@gram/client/models/components/environment.js";
import type { ToolsetEntry } from "@gram/client/models/components/toolsetentry.js";
import type { TriggerInstance } from "@gram/client/models/components/triggerinstance.js";
import { findSlackTrigger } from "./assistantSetup";
import {
  SLACK_CAPABILITY_GROUPS,
  SLACK_EVENT_GROUPS,
  isSlackReplyMode,
  type SlackReplyMode,
} from "./slackCapabilities";
import { SLACK_TOOL_URN_PREFIX } from "./slackManifest";

export type SlackCredentialKey =
  | "SLACK_SIGNING_SECRET"
  | "SLACK_BOT_TOKEN"
  | "SLACK_USER_TOKEN";

/** What the assistant's Slack connection looks like right now. */
export type SlackConnection = {
  /** Has Slack tools or listens to Slack. */
  connected: boolean;
  trigger: TriggerInstance | undefined;
  replyMode: SlackReplyMode | undefined;
  capabilities: string[];
  events: string[];
  env: Environment | undefined;
  /** Credentials it needs, in the order Slack's settings show them. */
  required: SlackCredentialKey[];
  missing: SlackCredentialKey[];
};

export function slackConnection(
  assistant: Assistant,
  triggers: readonly TriggerInstance[],
  toolsets: readonly ToolsetEntry[],
  environments: readonly Environment[],
): SlackConnection {
  const trigger = findSlackTrigger(triggers, assistant.id);
  const bySlug = new Map(toolsets.map((t) => [t.slug, t]));
  const slackRef = assistant.toolsets.find((ref) =>
    bySlug
      .get(ref.toolsetSlug)
      ?.tools.some((t) => t.toolUrn?.startsWith(SLACK_TOOL_URN_PREFIX)),
  );
  const urns = new Set(
    (slackRef ? bySlug.get(slackRef.toolsetSlug)?.tools : [])?.map(
      (t) => t.toolUrn,
    ),
  );
  const capabilities = SLACK_CAPABILITY_GROUPS.filter((g) =>
    g.toolUrns.some((u) => urns.has(u)),
  ).map((g) => g.slug);

  const config = (trigger?.config ?? {}) as Record<string, unknown>;
  const eventTypes = Array.isArray(config.event_types)
    ? (config.event_types as string[])
    : [];
  const events = SLACK_EVENT_GROUPS.filter((g) =>
    g.eventTypes.some((t) => eventTypes.includes(t)),
  ).map((g) => g.slug);

  const env =
    environments.find((e) => e.id === trigger?.environmentId) ??
    environments.find((e) => e.slug === slackRef?.environmentSlug);

  const required: SlackCredentialKey[] = [];
  if (trigger) required.push("SLACK_SIGNING_SECRET");
  if (slackRef || trigger) required.push("SLACK_BOT_TOKEN");
  // Slack only lets a user token search messages and files.
  if (urns.has(`${SLACK_TOOL_URN_PREFIX}search_messages_and_files`)) {
    required.push("SLACK_USER_TOKEN");
  }
  const set = new Set(
    (env?.entries ?? []).filter((e) => e.hasValue).map((e) => e.name),
  );

  return {
    connected: !!trigger || !!slackRef,
    trigger,
    replyMode: isSlackReplyMode(config.routing) ? config.routing : undefined,
    capabilities,
    events,
    env,
    required,
    missing: required.filter((key) => !set.has(key)),
  };
}

/** Where each credential lives in Slack's app settings. */
export const SLACK_CREDENTIALS: Record<
  SlackCredentialKey,
  { label: string; where: string; placeholder: string }
> = {
  SLACK_SIGNING_SECRET: {
    label: "Signing secret",
    where: "Basic Information → App Credentials",
    placeholder: "",
  },
  SLACK_BOT_TOKEN: {
    label: "Bot token",
    where: "OAuth & Permissions → Bot User OAuth Token",
    placeholder: "xoxb-…",
  },
  SLACK_USER_TOKEN: {
    label: "User token",
    where: "OAuth & Permissions → User OAuth Token",
    placeholder: "xoxp-…",
  },
};
