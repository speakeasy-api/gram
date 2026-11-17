import type { Gram } from "@gram/client";
import type { Assistant } from "@gram/client/models/components/assistant.js";
import type { TriggerInstance } from "@gram/client/models/components/triggerinstance.js";
import { computeBehaviorSection } from "./behaviors";
import { setSection } from "./sections";
import {
  SLACK_CONVERSATION_EVENT_TYPES,
  expandCapabilities,
  expandEvents,
  type SlackReplyMode,
} from "./slackCapabilities";
import { SLACK_TOOL_URN_PREFIX } from "./slackManifest";

export type AssistantEnv = { id: string; slug: string };

function envNameFor(assistantName: string): string {
  return `${assistantName} env`;
}

/** The assistant's Slack trigger, when it has one. */
export function findSlackTrigger(
  triggers: readonly TriggerInstance[],
  assistantId: string,
): TriggerInstance | undefined {
  return triggers.find(
    (t) =>
      t.targetKind === "assistant" &&
      t.targetRef === assistantId &&
      t.definitionSlug === "slack",
  );
}

/**
 * A reply mode governs messages and mentions, so those never stay listed as
 * extra event types next to it.
 */
function slackTriggerConfig(
  existing: Record<string, unknown> | null | undefined,
  replyMode: SlackReplyMode,
  extraEventTypes?: string[],
): Record<string, unknown> {
  const current = Array.isArray(existing?.event_types)
    ? (existing.event_types as string[])
    : [];
  const eventTypes = (extraEventTypes ?? current).filter(
    (t) => !SLACK_CONVERSATION_EVENT_TYPES.includes(t),
  );
  return { ...existing, event_types: eventTypes, routing: replyMode };
}

/**
 * The environment that holds the assistant's own credentials. Found from
 * what it is already bound to, else created.
 */
async function ensureAssistantEnv(
  sdk: Gram,
  organizationId: string,
  assistant: Assistant,
  triggers: readonly TriggerInstance[],
): Promise<AssistantEnv> {
  const list = await sdk.environments.list();
  const envs = list.environments;

  const slackTrigger = findSlackTrigger(triggers, assistant.id);
  const byTrigger = slackTrigger?.environmentId
    ? envs.find((e) => e.id === slackTrigger.environmentId)
    : undefined;
  if (byTrigger) return { id: byTrigger.id, slug: byTrigger.slug };

  const boundSlugs = new Set(
    [...assistant.toolsets, ...(assistant.mcpServers ?? [])]
      .map((ref) => ref.environmentSlug)
      .filter((slug): slug is string => !!slug),
  );
  if (boundSlugs.size === 1) {
    const [slug] = boundSlugs;
    const bound = envs.find((e) => e.slug === slug);
    if (bound) return { id: bound.id, slug: bound.slug };
  }

  // Names aren't unique across assistants, so a new environment is created
  // rather than adopting one that only shares the name.
  const taken = new Set(envs.map((e) => e.name));
  let name = envNameFor(assistant.name);
  for (let n = 2; taken.has(name); n++) {
    name = `${envNameFor(assistant.name)} ${n}`;
  }
  const created = await sdk.environments.create({
    createEnvironmentForm: {
      name,
      description: `Shared credentials for ${assistant.name}.`,
      entries: [],
      organizationId,
    },
  });
  return { id: created.id, slug: created.slug };
}

/**
 * Rewrites the instructions' Behavior section from the tools the assistant
 * now has, so Slack etiquette follows the Slack tools it was given.
 */
async function recomputeBehaviorSection(
  sdk: Gram,
  assistant: Assistant,
): Promise<Assistant> {
  let urns: string[] = [];
  if (assistant.toolsets.length > 0) {
    const list = await sdk.toolsets.list();
    const bySlug = new Map(list.toolsets.map((t) => [t.slug, t]));
    const set = new Set<string>();
    for (const ref of assistant.toolsets) {
      for (const t of bySlug.get(ref.toolsetSlug)?.tools ?? []) {
        set.add(t.toolUrn);
      }
    }
    urns = [...set];
  }
  const next = setSection(
    assistant.instructions,
    "Behavior",
    computeBehaviorSection(urns),
  );
  if (next === assistant.instructions) return assistant;
  return sdk.assistants.update({
    updateAssistantForm: { id: assistant.id, instructions: next },
  });
}

/**
 * The assistant's Slack toolset: attached and has Slack tools. Unless
 * `includeShared`, one another assistant also uses doesn't count, so setup
 * never rewrites a toolset it doesn't own.
 */
async function findSlackToolset(
  sdk: Gram,
  assistant: Assistant,
  includeShared = false,
): Promise<{ slug: string; toolUrns: string[] } | undefined> {
  const [toolsets, assistants] = await Promise.all([
    sdk.toolsets.list(),
    sdk.assistants.list(),
  ]);
  const attached = new Set(assistant.toolsets.map((t) => t.toolsetSlug));
  const shared = new Set<string>();
  for (const other of assistants.assistants) {
    if (other.id === assistant.id) continue;
    for (const ref of other.toolsets) shared.add(ref.toolsetSlug);
  }
  const found = toolsets.toolsets.find(
    (t) =>
      attached.has(t.slug) &&
      (includeShared || !shared.has(t.slug)) &&
      t.tools.some((tool) => tool.toolUrn?.startsWith(SLACK_TOOL_URN_PREFIX)),
  );
  return found
    ? { slug: found.slug, toolUrns: found.tools.map((t) => t.toolUrn) }
    : undefined;
}

export type SlackSetupChoice = {
  capabilities: string[];
  events: string[];
  /** When it replies; undefined means it never listens to Slack. */
  replyMode: SlackReplyMode | undefined;
};

/**
 * Saves the Slack setup: a per-assistant Slack toolset with only the chosen
 * tools, bound to the assistant's environment, and a Slack trigger when it
 * should listen. Returns the trigger's webhook URL for the app manifest.
 */
export async function applySlackSetup(
  sdk: Gram,
  organizationId: string,
  assistant: Assistant,
  triggers: readonly TriggerInstance[],
  choice: SlackSetupChoice,
): Promise<{ assistant: Assistant; env: AssistantEnv; webhookUrl?: string }> {
  const env = await ensureAssistantEnv(
    sdk,
    organizationId,
    assistant,
    triggers,
  );
  const toolUrns = expandCapabilities(choice.capabilities);
  const existing = await findSlackToolset(sdk, assistant);

  let current = assistant;
  if (toolUrns.length > 0) {
    const upserted = existing
      ? await sdk.toolsets.updateBySlug({
          slug: existing.slug,
          updateToolsetRequestBody: { toolUrns },
        })
      : await sdk.toolsets.create({
          createToolsetRequestBody: {
            name: await freeToolsetName(sdk, `${assistant.name} Slack`),
            description: `Slack capabilities for ${assistant.name}.`,
            toolUrns,
            defaultEnvironmentSlug: env.slug,
          },
        });
    current = await sdk.assistants.update({
      updateAssistantForm: {
        id: assistant.id,
        toolsets: assistant.toolsets
          .filter((t) => t.toolsetSlug !== upserted.slug)
          .concat([{ toolsetSlug: upserted.slug, environmentSlug: env.slug }]),
      },
    });
  } else if (existing) {
    current = await sdk.assistants.update({
      updateAssistantForm: {
        id: assistant.id,
        toolsets: assistant.toolsets.filter(
          (t) => t.toolsetSlug !== existing.slug,
        ),
      },
    });
  }
  current = await recomputeBehaviorSection(sdk, current);

  // The Slack trigger refuses an environment that doesn't declare its
  // signing secret, so the keys go in first, empty until the app exists.
  await declareEnvKeys(sdk, env.slug, [
    ...(choice.replyMode ? ["SLACK_SIGNING_SECRET"] : []),
    "SLACK_BOT_TOKEN",
    "SLACK_USER_TOKEN",
  ]);

  const trigger = findSlackTrigger(triggers, assistant.id);
  const eventTypes = expandEvents(choice.events);
  let webhookUrl: string | undefined;
  if (choice.replyMode) {
    if (trigger) {
      const updated = await sdk.triggers.update({
        updateTriggerInstanceForm: {
          id: trigger.id,
          config: slackTriggerConfig(
            trigger.config as Record<string, unknown> | null,
            choice.replyMode,
            eventTypes,
          ),
          status: "active",
        },
      });
      webhookUrl = updated.webhookUrl ?? trigger.webhookUrl;
    } else {
      const created = await sdk.triggers.create({
        createTriggerInstanceForm: {
          name: "Slack",
          definitionSlug: "slack",
          config: slackTriggerConfig(null, choice.replyMode, eventTypes),
          environmentId: env.id,
          targetKind: "assistant",
          targetRef: assistant.id,
          targetDisplay: assistant.name,
        },
      });
      webhookUrl = created.webhookUrl;
    }
  } else if (trigger) {
    await sdk.triggers.delete({ id: trigger.id });
  }

  return { assistant: current, env, webhookUrl };
}

/** Takes Slack off the assistant: its Slack tools and its Slack trigger. */
export async function removeSlack(
  sdk: Gram,
  assistant: Assistant,
  triggers: readonly TriggerInstance[],
): Promise<Assistant> {
  // Detaching a shared toolset leaves it in place for the others.
  const existing = await findSlackToolset(sdk, assistant, true);
  let current = assistant;
  if (existing) {
    current = await sdk.assistants.update({
      updateAssistantForm: {
        id: assistant.id,
        toolsets: assistant.toolsets.filter(
          (t) => t.toolsetSlug !== existing.slug,
        ),
      },
    });
    current = await recomputeBehaviorSection(sdk, current);
  }
  const trigger = findSlackTrigger(triggers, assistant.id);
  if (trigger) await sdk.triggers.delete({ id: trigger.id });
  return current;
}

/** A toolset name no other toolset in the project uses. */
async function freeToolsetName(sdk: Gram, wanted: string): Promise<string> {
  const list = await sdk.toolsets.list();
  const taken = new Set(list.toolsets.map((t) => t.name));
  if (!taken.has(wanted)) return wanted;
  for (let n = 2; ; n++) {
    const candidate = `${wanted} ${n}`;
    if (!taken.has(candidate)) return candidate;
  }
}

/** Writes values into the environment, leaving other entries alone. */
export async function saveEnvEntries(
  sdk: Gram,
  envSlug: string,
  entries: { name: string; value: string }[],
): Promise<void> {
  if (entries.length === 0) return;
  await sdk.environments.updateBySlug({
    slug: envSlug,
    updateEnvironmentRequestBody: {
      entriesToRemove: [],
      entriesToUpdate: entries,
    },
  });
}

/** Adds empty entries for keys the environment doesn't list yet. */
async function declareEnvKeys(
  sdk: Gram,
  envSlug: string,
  keys: string[],
): Promise<void> {
  const list = await sdk.environments.list();
  const env = list.environments.find((e) => e.slug === envSlug);
  const present = new Set((env?.entries ?? []).map((e) => e.name));
  await saveEnvEntries(
    sdk,
    envSlug,
    keys.filter((k) => !present.has(k)).map((name) => ({ name, value: "" })),
  );
}
