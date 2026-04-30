import { useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { defineFrontendTool, type FrontendTool } from "@gram-ai/elements";
import { Gram } from "@gram/client";
import { ToolCallMessagePartComponent } from "@assistant-ui/react";
import { useMemo } from "react";
import { z } from "zod";
import { useAssistantDraft } from "../useAssistantDraft";
import { computeBehaviorSection } from "../behaviors";
import { getIntegrationDoc, listIntegrationDocs } from "../docs";
import { setSection } from "../sections";
import {
  ProposeIdentityComponent,
  RequestEnvironmentSecretsComponent,
  ShowSlackAppGuideComponent,
  ShowWebhookUrlComponent,
} from "./components";

type DraftHandle = ReturnType<typeof useAssistantDraft>;

type ToolDeps = {
  sdk: Gram;
  organizationId: string;
  draft: DraftHandle;
};

type ToolResult<T extends Record<string, unknown> = Record<string, unknown>> =
  | ({ ok: true } & T)
  | { ok: false; error: string; [k: string]: unknown };

// AI SDK's modelMessageSchema rejects nested `undefined` values inside a
// tool-result's JSON output (jsonValueSchema = null|string|number|boolean|
// record|array — no undefined). Tool returns here spread optional SDK fields
// like `description`, `webhook_url`, etc. that are often undefined; those
// survive into convertToModelMessages and break standardizePrompt on the
// resumed turn. Round-trip through JSON to strip them at the boundary.
const stripUndefined = <T,>(value: T): T =>
  JSON.parse(JSON.stringify(value)) as T;

const okResult = <T extends Record<string, unknown>>(data: T): ToolResult<T> =>
  stripUndefined({ ok: true, ...data }) as ToolResult<T>;

const errResult = (
  message: string,
  extra?: Record<string, unknown>,
): ToolResult =>
  stripUndefined({ ok: false, error: message, ...(extra ?? {}) }) as ToolResult;

function withTimeout<T>(p: Promise<T>, ms: number, label: string): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const id = setTimeout(
      () => reject(new Error(`${label} timed out after ${ms}ms`)),
      ms,
    );
    p.then(
      (v) => {
        clearTimeout(id);
        resolve(v);
      },
      (e) => {
        clearTimeout(id);
        reject(e);
      },
    );
  });
}

function envNameFor(assistantName: string) {
  return `${assistantName} env`;
}

type EnvEnsureOutcome =
  | "existing"
  | "adopted-by-slug"
  | "adopted-by-name"
  | "recreated"
  | "created";

type EnvEnsureResult = {
  env: { id: string; slug: string };
  outcome: EnvEnsureOutcome;
  note?: string;
};

async function ensureAssistantEnv(
  deps: ToolDeps,
  preferredName?: string,
): Promise<EnvEnsureResult> {
  const { sdk, draft, organizationId } = deps;
  const cached = draft.assistantEnv;
  const name = preferredName ?? draft.assistant?.name ?? "Assistant";
  const envName = envNameFor(name);

  const list = await sdk.environments.list().catch(() => null);
  const envs = list?.environments ?? [];

  if (cached) {
    const hitById = cached.id
      ? envs.find((e) => e.id === cached.id)
      : undefined;
    if (hitById) {
      const v = { id: hitById.id, slug: hitById.slug };
      draft.setAssistantEnv(v);
      return { env: v, outcome: "existing" };
    }
    const hitBySlug = envs.find((e) => e.slug === cached.slug);
    if (hitBySlug) {
      const v = { id: hitBySlug.id, slug: hitBySlug.slug };
      draft.setAssistantEnv(v);
      return {
        env: v,
        outcome: cached.id ? "adopted-by-slug" : "existing",
      };
    }
  }

  const byName = envs.find((e) => e.name === envName);
  if (byName) {
    const v = { id: byName.id, slug: byName.slug };
    draft.setAssistantEnv(v);
    return {
      env: v,
      outcome: "adopted-by-name",
      note: cached
        ? `Previously tracked assistant env "${cached.slug}" was missing. Adopted existing env "${byName.slug}" that matches the assistant's name. Any toolsets or triggers still pointed at the old slug must be re-attached or reconfigured.`
        : `Adopted existing env "${byName.slug}" (matched by name) as the assistant's shared environment.`,
    };
  }

  const created = await sdk.environments.create({
    createEnvironmentForm: {
      name: envName,
      description: `Shared credentials for ${name}.`,
      entries: [],
      organizationId,
    },
  });
  const v = { id: created.id, slug: created.slug };
  draft.setAssistantEnv(v);
  draft.invalidateAll();
  return {
    env: v,
    outcome: cached ? "recreated" : "created",
    note: cached
      ? `Previous assistant env "${cached.slug}" no longer exists — a fresh env "${created.slug}" was created. Any toolsets or triggers still referencing "${cached.slug}" must be re-attached (attach_toolset) or reconfigured (update_trigger) to point at the new env.`
      : `Created shared environment "${created.slug}" for the assistant.`,
  };
}

async function renameAssistantEnv(
  deps: ToolDeps,
  newAssistantName: string,
): Promise<void> {
  const { sdk, draft } = deps;
  const env = draft.assistantEnv;
  if (!env) return;
  try {
    await sdk.environments.updateBySlug({
      slug: env.slug,
      updateEnvironmentRequestBody: {
        name: envNameFor(newAssistantName),
        entriesToRemove: [],
        entriesToUpdate: [],
      },
    });
    draft.invalidateAll();
  } catch {
    // non-fatal: rename is cosmetic, slug is stable
  }
}

async function ensureAssistant(
  deps: ToolDeps,
  payload: {
    name?: string;
    instructions?: string;
    model?: string;
    status?: "active" | "paused";
    warm_ttl_seconds?: number;
    max_concurrency?: number;
  },
) {
  const { sdk, draft } = deps;
  if (draft.assistantId) {
    const prevName = draft.assistant?.name;
    const updated = await sdk.assistants.update({
      updateAssistantForm: {
        id: draft.assistantId,
        ...(payload.name !== undefined ? { name: payload.name } : {}),
        ...(payload.instructions !== undefined
          ? { instructions: payload.instructions }
          : {}),
        ...(payload.model !== undefined ? { model: payload.model } : {}),
        ...(payload.status !== undefined ? { status: payload.status } : {}),
        ...(payload.warm_ttl_seconds !== undefined
          ? { warmTtlSeconds: payload.warm_ttl_seconds }
          : {}),
        ...(payload.max_concurrency !== undefined
          ? { maxConcurrency: payload.max_concurrency }
          : {}),
      },
    });
    draft.setAssistant(updated);
    if (payload.name && payload.name !== prevName) {
      await renameAssistantEnv(deps, updated.name);
    }
    draft.invalidateAll();
    return updated;
  }
  const created = await sdk.assistants.create({
    createAssistantForm: {
      name: payload.name ?? "Untitled assistant",
      instructions: payload.instructions ?? "You are a helpful assistant.",
      model: payload.model ?? "anthropic/claude-sonnet-4.6",
      status: payload.status,
      toolsets: [],
      ...(payload.warm_ttl_seconds !== undefined
        ? { warmTtlSeconds: payload.warm_ttl_seconds }
        : {}),
      ...(payload.max_concurrency !== undefined
        ? { maxConcurrency: payload.max_concurrency }
        : {}),
    },
  });
  draft.setAssistant(created);
  draft.invalidateAll();
  return created;
}

type AssistantForBehavior = {
  id: string;
  instructions: string;
  toolsets: { toolsetSlug: string }[];
};

async function recomputeBehaviorSection(
  deps: ToolDeps,
  assistant?: AssistantForBehavior,
): Promise<void> {
  const { sdk, draft } = deps;
  const a = assistant ?? draft.assistant;
  if (!a) return;
  let urns: string[] = [];
  if (a.toolsets.length > 0) {
    const list = await sdk.toolsets.list().catch(() => null);
    const summaries = list?.toolsets ?? [];
    const bySlug = new Map(summaries.map((t) => [t.slug, t]));
    const set = new Set<string>();
    for (const ref of a.toolsets) {
      const ts = bySlug.get(ref.toolsetSlug);
      if (!ts) continue;
      for (const t of ts.tools) set.add(t.toolUrn);
    }
    urns = [...set];
  }
  const behaviorBody = computeBehaviorSection(urns);
  const nextInstructions = setSection(a.instructions, "Behavior", behaviorBody);
  if (nextInstructions === a.instructions) return;
  const updated = await sdk.assistants.update({
    updateAssistantForm: {
      id: a.id,
      instructions: nextInstructions,
    },
  });
  draft.setAssistant(updated);
  draft.invalidateAll();
}

async function currentEnvEntryNames(
  deps: ToolDeps,
  envSlug: string,
): Promise<Set<string>> {
  const list = await deps.sdk.environments.list().catch(() => null);
  const env = list?.environments.find((e) => e.slug === envSlug);
  return new Set((env?.entries ?? []).map((e) => e.name));
}

async function upsertEnvEntries(
  deps: ToolDeps,
  envSlug: string,
  entries: { name: string; value: string }[],
): Promise<void> {
  if (entries.length === 0) return;
  try {
    await deps.sdk.environments.updateBySlug({
      slug: envSlug,
      updateEnvironmentRequestBody: {
        entriesToRemove: [],
        entriesToUpdate: entries,
      },
    });
  } catch (e) {
    throw new Error(
      `Failed to write to environment "${envSlug}" (it may have been deleted out of band). ` +
        `Create a fresh environment with create_environment and retry, or omit environment_slug to let the assistant recreate its shared env. ` +
        `Underlying error: ${e instanceof Error ? e.message : String(e)}`,
    );
  }
  deps.draft.invalidateAll();
}

type UpdateAssistantArgs = {
  name?: string;
  model?: string;
  status?: "active" | "paused";
  warm_ttl_seconds?: number;
  max_concurrency?: number;
};
type SetPersonalityArgs = { instructions: string };
type SetTasksArgs = { tasks: string };
type AttachToolsetArgs = {
  toolset_slug: string;
  environment_slug?: string;
};
type DetachToolsetArgs = { toolset_slug: string };
type CreateToolsetArgs = {
  name: string;
  description?: string;
  tool_urns?: string[];
  default_environment_slug?: string;
};
type AddToolsArgs = { toolset_slug: string; tool_urns: string[] };
type ListAvailableToolsArgs = { urn_prefix?: string; limit?: number };
type CreateEnvArgs = { name: string; description?: string };
type AddEnvKeysArgs = { keys: string[]; environment_slug?: string };
type SecretKeyArg = {
  name: string;
  label?: string;
  description?: string;
  sensitive?: boolean;
  placeholder?: string;
};
type RequestSecretsArgs = {
  reason?: string;
  keys: SecretKeyArg[];
  environment_slug?: string;
};
type CreateTriggerArgs = {
  name: string;
  definition_slug: string;
  config: Record<string, unknown>;
  environment_id?: string;
};
type UpdateTriggerArgs = {
  id: string;
  name?: string;
  config?: Record<string, unknown>;
  status?: "active" | "paused";
  environment_id?: string;
};
type ShowWebhookArgs = {
  trigger_name?: string;
  webhook_url: string;
  instructions?: string;
};
type ShowSlackGuideArgs = {
  app_name?: string;
  workspace_hint?: string;
  bot_scopes?: string[];
  bot_events?: string[];
  webhook_url?: string;
};
type ListIntegrationsArgs = { keywords?: string[] };
type ReadDocsArgs = { slug: string };
type FinishArgs = { message?: string };
type ProposeIdentityArgs = {
  goal?: string;
  name_suggestions: string[];
};
type PersonalityChoice =
  | {
      kind: "prebuilt";
      prebuilt: {
        slug: string;
        title: string;
        summary: string;
        instructions: string;
      };
    }
  | { kind: "custom_text"; custom_text: string }
  | { kind: "generate"; describe: string }
  | { kind: "random" };

function buildAssistantTools(deps: ToolDeps) {
  const { sdk, draft, organizationId } = deps;

  const triggerCreateInFlight = new Map<string, Promise<ToolResult>>();

  const propose_identity = defineFrontendTool<ProposeIdentityArgs, ToolResult>(
    {
      description:
        "Present a name + personality picker to the user. Use this as the FIRST step of creation, before any toolsets or triggers. Generate 4-6 unique first-name suggestions that fit the user's stated goal. Treat the assistant like a coworker: pick real first names from a mix of cultures. Never use the phrasing '[Owner]'s assistant', role titles ('Support Bot'), product names, or generic words ('Helper', 'Assistant'). Distinct names only — no duplicates, no variants of the same name. User picks a name (yours or their own) and a personality source (preset, short description, pasted instructions, or random). The tool returns their choice; you then synthesize the final instructions and call update_assistant.",
      parameters: z.object({
        goal: z
          .string()
          .optional()
          .describe(
            "One short sentence restating what the user said the assistant should do. Shown to the user as context above the picker.",
          ),
        name_suggestions: z
          .array(z.string().min(1).max(48))
          .min(3)
          .max(8)
          .describe(
            "Candidate first names. 4-6 is ideal. Unique real first names from varied cultures. Never '[Owner]'s assistant', role titles, product names, or generic words.",
          ),
      }),
      execute: async (_args, ctx) => {
        const toolCallId = ctx.toolCallId ?? "";
        type IdentityResult = {
          success: boolean;
          cancelled?: boolean;
          name?: string;
          personality?: PersonalityChoice;
        };
        const userInput: IdentityResult = await withTimeout(
          new Promise<IdentityResult>((resolve) => {
            draft.registerPending(toolCallId, (r) =>
              resolve(r as IdentityResult),
            );
          }),
          15 * 60 * 1000,
          "propose_identity",
        ).catch(
          (e): IdentityResult => ({
            success: false,
            cancelled: true,
            name: e instanceof Error ? e.message : "timeout",
          }),
        );

        if (!userInput.success || !userInput.name || !userInput.personality) {
          return okResult({
            cancelled: true,
            note: "User skipped the identity picker. Ask them directly for a name and the gist of the personality they want, then call update_assistant with sensible defaults. Do not re-call propose_identity unless they ask.",
          });
        }

        const p = userInput.personality;
        const name = userInput.name;
        try {
          if (p.kind === "prebuilt") {
            const hasInstructions = p.prebuilt.instructions.trim().length > 0;
            const current = draft.assistant?.instructions ?? "";
            const next = hasInstructions
              ? setSection(current, "Personality", p.prebuilt.instructions)
              : current;
            const a = await ensureAssistant(
              deps,
              hasInstructions ? { name, instructions: next } : { name },
            );
            await recomputeBehaviorSection(deps, a);
            return okResult({
              name,
              personality: {
                kind: "prebuilt" as const,
                slug: p.prebuilt.slug,
                title: p.prebuilt.title,
                summary: p.prebuilt.summary,
                body_set: hasInstructions,
              },
              note: hasInstructions
                ? `Saved name "${name}" and the "${p.prebuilt.title}" personality verbatim under # Personality. Now describe what this assistant does — call set_tasks with role-specific guidance derived from the user's stated goal. Do not call set_personality unless the user explicitly asks to change it.`
                : `Saved name "${name}". The "${p.prebuilt.title}" preset has no body — synthesize personality content (voice, tone, formatting habits, uncertainty handling) matching the title "${p.prebuilt.title}" and summary "${p.prebuilt.summary}", then call set_personality with the result. Then call set_tasks with role-specific guidance.`,
            });
          }
          if (p.kind === "custom_text") {
            const current = draft.assistant?.instructions ?? "";
            const next = setSection(current, "Personality", p.custom_text);
            const a = await ensureAssistant(deps, { name, instructions: next });
            await recomputeBehaviorSection(deps, a);
            return okResult({
              name,
              personality: { kind: "custom_text" as const },
              note: `Saved name "${name}" and the user's pasted personality verbatim under # Personality. Now call set_tasks with role-specific guidance. Do not modify # Personality.`,
            });
          }
          if (p.kind === "generate") {
            const a = await ensureAssistant(deps, { name });
            await recomputeBehaviorSection(deps, a);
            return okResult({
              name,
              personality: {
                kind: "generate" as const,
                description: p.describe,
              },
              note: `Saved name "${name}". The user described their desired personality: "${p.describe}". Expand that into a full personality (voice, tone, formatting habits, uncertainty handling) and call set_personality with the expanded text. Then call set_tasks with role-specific guidance.`,
            });
          }
          const a = await ensureAssistant(deps, { name });
          await recomputeBehaviorSection(deps, a);
          return okResult({
            name,
            personality: { kind: "random" as const },
            note: `Saved name "${name}". Invent a distinctive personality from scratch (voice, formatting habits, sign-off, uncertainty handling). Keep it professional-compatible. Call set_personality with the result, then set_tasks with role-specific guidance.`,
          });
        } catch (e) {
          return errResult(e instanceof Error ? e.message : "save failed");
        }
      },
    },
    "propose_identity",
  );

  const update_assistant = defineFrontendTool<UpdateAssistantArgs, ToolResult>(
    {
      description:
        "Update the assistant's name, model, status, warm TTL, or max concurrency. The system prompt is split into three managed sections — # Personality (set via set_personality), # Behavior (managed automatically based on attached tools), # Tasks (set via set_tasks) — and is NOT writable through this tool. The first call also creates the assistant if none exists yet (creation flow).",
      parameters: z.object({
        name: z
          .string()
          .min(1)
          .max(128)
          .optional()
          .describe("Display name for the assistant."),
        model: z
          .string()
          .optional()
          .describe(
            "OpenRouter-style model id, e.g. 'anthropic/claude-sonnet-4.6'.",
          ),
        status: z.enum(["active", "paused"]).optional(),
        warm_ttl_seconds: z
          .number()
          .int()
          .min(0)
          .optional()
          .describe(
            "Seconds to keep a warm runtime alive after the last request. 0 disables warm runtimes. Default 300.",
          ),
        max_concurrency: z
          .number()
          .int()
          .min(1)
          .optional()
          .describe("Maximum number of concurrent warm runtimes. Default 1."),
      }),
      execute: async (args) => {
        try {
          const a = await ensureAssistant(deps, args as UpdateAssistantArgs);
          const envResult = a.name
            ? await ensureAssistantEnv(deps, a.name)
            : null;
          const notes: string[] = [];
          if (envResult?.note) notes.push(envResult.note);
          return okResult({
            assistant: {
              id: a.id,
              name: a.name,
              model: a.model,
              status: a.status,
              instructions: a.instructions,
              toolsets: a.toolsets,
            },
            environment: envResult?.env ?? draft.assistantEnv ?? undefined,
            ...(notes.length > 0 ? { notes } : {}),
          });
        } catch (e) {
          return errResult(e instanceof Error ? e.message : "update failed");
        }
      },
    },
    "update_assistant",
  );

  const set_personality = defineFrontendTool<SetPersonalityArgs, ToolResult>(
    {
      description:
        "Replace the assistant's # Personality section — voice, tone, addressing style, formatting habits, uncertainty handling. Personality is voice-only: do not include role-specific guidance about what the assistant does (use set_tasks) and do not include behavior bullets (managed automatically based on attached tools). Pass the body WITHOUT a leading '# Personality' heading. Do not use any H1 (`# Foo`) inside the body — use H2 (`##`) or lower for sub-structure.",
      parameters: z.object({
        instructions: z
          .string()
          .min(1)
          .describe(
            "Personality body. No leading '# Personality' heading. No H1 sub-headings — use H2 or lower.",
          ),
      }),
      execute: async (args) => {
        const { instructions } = args as SetPersonalityArgs;
        try {
          const current = draft.assistant?.instructions ?? "";
          const next = setSection(current, "Personality", instructions);
          const updated = await ensureAssistant(deps, { instructions: next });
          return okResult({
            assistant: {
              id: updated.id,
              name: updated.name,
              instructions: updated.instructions,
            },
          });
        } catch (e) {
          return errResult(
            e instanceof Error ? e.message : "set personality failed",
          );
        }
      },
    },
    "set_personality",
  );

  const set_tasks = defineFrontendTool<SetTasksArgs, ToolResult>(
    {
      description:
        "Replace the assistant's # Tasks section — what it actually does on each run: how it interprets incoming events, which tools it tends to use, what its output looks like, when to stay silent. This is the role/goal-specific guidance derived from the user's stated goal. Do not include personality (use set_personality) or behavior (managed automatically). Pass the body WITHOUT a leading '# Tasks' heading. Do not use any H1 (`# Foo`) inside the body — use H2 (`##`) or lower for sub-structure.",
      parameters: z.object({
        tasks: z
          .string()
          .min(1)
          .describe(
            "Tasks/role body. The job description for this assistant. No leading '# Tasks' heading. No H1 sub-headings — use H2 or lower.",
          ),
      }),
      execute: async (args) => {
        const { tasks } = args as SetTasksArgs;
        try {
          const current = draft.assistant?.instructions ?? "";
          const next = setSection(current, "Tasks", tasks);
          const updated = await ensureAssistant(deps, { instructions: next });
          return okResult({
            assistant: {
              id: updated.id,
              name: updated.name,
              instructions: updated.instructions,
            },
          });
        } catch (e) {
          return errResult(e instanceof Error ? e.message : "set tasks failed");
        }
      },
    },
    "set_tasks",
  );

  const attach_toolset = defineFrontendTool<AttachToolsetArgs, ToolResult>(
    {
      description:
        "Attach an existing toolset to the assistant so it can call those tools at runtime. By default the toolset is bound to the assistant's shared environment; pass environment_slug only to override. Replaces any prior reference to the same toolset_slug.",
      parameters: z.object({
        toolset_slug: z.string(),
        environment_slug: z
          .string()
          .optional()
          .describe(
            "Override the assistant's shared environment for this toolset. Omit in almost all cases — the assistant's env is used by default.",
          ),
      }),
      execute: async (args) => {
        const { toolset_slug, environment_slug } = args as AttachToolsetArgs;
        try {
          const a = await ensureAssistant(deps, {});
          const notes: string[] = [];
          let boundSlug = environment_slug;
          if (!boundSlug) {
            const envResult = await ensureAssistantEnv(deps, a.name);
            boundSlug = envResult.env.slug;
            if (envResult.note) notes.push(envResult.note);
          }
          const next = a.toolsets
            .filter((t) => t.toolsetSlug !== toolset_slug)
            .concat([
              {
                toolsetSlug: toolset_slug,
                environmentSlug: boundSlug,
              },
            ]);
          const updated = await sdk.assistants.update({
            updateAssistantForm: { id: a.id, toolsets: next },
          });
          draft.setAssistant(updated);
          draft.invalidateAll();
          await recomputeBehaviorSection(deps, updated);
          return okResult({
            toolsets: updated.toolsets,
            environment_slug: boundSlug,
            ...(notes.length > 0 ? { notes } : {}),
          });
        } catch (e) {
          return errResult(e instanceof Error ? e.message : "attach failed");
        }
      },
    },
    "attach_toolset",
  );

  const detach_toolset = defineFrontendTool<DetachToolsetArgs, ToolResult>(
    {
      description:
        "Remove a toolset from the assistant. Does not delete the toolset itself.",
      parameters: z.object({ toolset_slug: z.string() }),
      execute: async (args) => {
        const { toolset_slug } = args as DetachToolsetArgs;
        try {
          if (!draft.assistantId || !draft.assistant) {
            return errResult("No assistant exists yet. Create one first.");
          }
          const next = draft.assistant.toolsets.filter(
            (t) => t.toolsetSlug !== toolset_slug,
          );
          const updated = await sdk.assistants.update({
            updateAssistantForm: { id: draft.assistantId, toolsets: next },
          });
          draft.setAssistant(updated);
          draft.invalidateAll();
          await recomputeBehaviorSection(deps, updated);
          return okResult({ toolsets: updated.toolsets });
        } catch (e) {
          return errResult(e instanceof Error ? e.message : "detach failed");
        }
      },
    },
    "detach_toolset",
  );

  const list_toolsets = defineFrontendTool<Record<string, never>, ToolResult>(
    {
      description:
        "List all toolsets in the current project. Returns slug, name, description, default environment, and tool count for each.",
      parameters: z.object({}),
      execute: async () => {
        try {
          const result = await sdk.toolsets.list();
          return okResult({
            toolsets: result.toolsets.map((t) => ({
              slug: t.slug,
              name: t.name,
              description: t.description,
              default_environment_slug: t.defaultEnvironmentSlug,
              tool_count: t.tools.length,
              tool_names: t.tools.map((tool) => tool.name),
            })),
          });
        } catch (e) {
          return errResult(e instanceof Error ? e.message : "list failed");
        }
      },
    },
    "list_toolsets",
  );

  const create_toolset = defineFrontendTool<CreateToolsetArgs, ToolResult>(
    {
      description:
        "Create a new toolset. Optionally seed it with tool URNs from list_available_tools and a default environment. Returns the new slug.",
      parameters: z.object({
        name: z.string().min(1).max(128),
        description: z.string().optional(),
        tool_urns: z.array(z.string()).optional(),
        default_environment_slug: z.string().optional(),
      }),
      execute: async (args) => {
        const { name, description, tool_urns, default_environment_slug } =
          args as CreateToolsetArgs;
        try {
          const created = await sdk.toolsets.create({
            createToolsetRequestBody: {
              name,
              description,
              toolUrns: tool_urns,
              defaultEnvironmentSlug: default_environment_slug,
            },
          });
          draft.invalidateAll();
          return okResult({
            slug: created.slug,
            name: created.name,
            tool_count: created.tools.length,
          });
        } catch (e) {
          return errResult(
            e instanceof Error ? e.message : "create toolset failed",
          );
        }
      },
    },
    "create_toolset",
  );

  const add_tools_to_toolset = defineFrontendTool<AddToolsArgs, ToolResult>(
    {
      description:
        "Add tool URNs to an existing toolset. Pass new URNs you want appended; existing URNs are preserved.",
      parameters: z.object({
        toolset_slug: z.string(),
        tool_urns: z.array(z.string()).min(1),
      }),
      execute: async (args) => {
        const { toolset_slug, tool_urns } = args as AddToolsArgs;
        try {
          const current = await sdk.toolsets.getBySlug({ slug: toolset_slug });
          const merged = Array.from(
            new Set([...(current.toolUrns ?? []), ...tool_urns]),
          );
          const updated = await sdk.toolsets.updateBySlug({
            slug: toolset_slug,
            updateToolsetRequestBody: { toolUrns: merged },
          });
          draft.invalidateAll();
          await recomputeBehaviorSection(deps);
          return okResult({
            slug: updated.slug,
            tool_count: updated.tools.length,
            tool_urns: updated.toolUrns,
          });
        } catch (e) {
          return errResult(e instanceof Error ? e.message : "add tools failed");
        }
      },
    },
    "add_tools_to_toolset",
  );

  const list_available_tools = defineFrontendTool<
    ListAvailableToolsArgs,
    ToolResult
  >(
    {
      description:
        "List tool URNs available to add to toolsets in the current project. Optional urn_prefix filter (e.g. 'tools:http:slack:').",
      parameters: z.object({
        urn_prefix: z.string().optional(),
        limit: z.number().int().min(1).max(200).optional(),
      }),
      execute: async (args) => {
        const { urn_prefix, limit } = args as ListAvailableToolsArgs;
        try {
          const res = await sdk.tools.list({ urnPrefix: urn_prefix, limit });
          return okResult({
            tools: res.tools.map((t) => {
              const def =
                t.httpToolDefinition ??
                t.functionToolDefinition ??
                t.externalMcpToolDefinition ??
                t.platformToolDefinition;
              const prompt = t.promptTemplate;
              return {
                name: def?.name ?? prompt?.name,
                description: def?.description ?? prompt?.description,
                tool_urn:
                  t.httpToolDefinition?.toolUrn ??
                  t.functionToolDefinition?.toolUrn ??
                  t.externalMcpToolDefinition?.toolUrn ??
                  t.platformToolDefinition?.toolUrn,
              };
            }),
            next_cursor: res.nextCursor,
          });
        } catch (e) {
          return errResult(
            e instanceof Error ? e.message : "list tools failed",
          );
        }
      },
    },
    "list_available_tools",
  );

  const list_environments = defineFrontendTool<
    Record<string, never>,
    ToolResult
  >(
    {
      description:
        "List all environments in the project. Returns id, slug, name, description, entry names, and which entries have populated values (vs empty stubs). Use populated_entry_names to gate downstream prompts — e.g. skip show_slack_app_guide once SLACK_BOT_TOKEN is populated. Use the id when attaching an environment to a trigger.",
      parameters: z.object({}),
      execute: async () => {
        try {
          const res = await sdk.environments.list();
          return okResult({
            environments: res.environments.map((e) => ({
              id: e.id,
              slug: e.slug,
              name: e.name,
              description: e.description,
              entry_names: e.entries.map((entry) => entry.name),
              populated_entry_names: e.entries
                .filter((entry) => entry.value !== "<EMPTY>")
                .map((entry) => entry.name),
            })),
          });
        } catch (e) {
          return errResult(e instanceof Error ? e.message : "list envs failed");
        }
      },
    },
    "list_environments",
  );

  const create_environment = defineFrontendTool<CreateEnvArgs, ToolResult>(
    {
      description:
        "FALLBACK: create a brand-new environment. Use only when the assistant's shared env is missing or you need a separate env (e.g. the user deleted it and subsequent tool calls failed with an env-not-found error). In normal flow, assistant tools manage the shared env automatically — you should not need this. After creating, bind it with attach_toolset(environment_slug=…) or create_trigger(environment_id=…).",
      parameters: z.object({
        name: z.string().min(1),
        description: z.string().optional(),
      }),
      execute: async (args) => {
        const { name, description } = args as CreateEnvArgs;
        try {
          const created = await sdk.environments.create({
            createEnvironmentForm: {
              name,
              description: description ?? "",
              entries: [],
              organizationId,
            },
          });
          draft.invalidateAll();
          return okResult({
            id: created.id,
            slug: created.slug,
            name: created.name,
            description: created.description,
          });
        } catch (e) {
          return errResult(
            e instanceof Error ? e.message : "create env failed",
          );
        }
      },
    },
    "create_environment",
  );

  const add_environment_keys = defineFrontendTool<AddEnvKeysArgs, ToolResult>(
    {
      description:
        "Declare required variable names on the assistant's shared environment. Inserts any that don't yet exist with an empty value so the environment always advertises what it needs. Use this to extend the env before (or alongside) request_environment_secrets — even when you can't fill every slot yet. Idempotent: keys already present are left alone. Pass environment_slug ONLY as an escape hatch when targeting a specific env (e.g. you just created one with create_environment).",
      parameters: z.object({
        keys: z
          .array(z.string().min(1))
          .min(1)
          .describe(
            "Variable names as they should appear in the environment, e.g. SLACK_BOT_TOKEN.",
          ),
        environment_slug: z
          .string()
          .optional()
          .describe(
            "Override the assistant's shared env. Omit in almost all cases — a missing env is recreated automatically.",
          ),
      }),
      execute: async (args) => {
        const { keys, environment_slug } = args as AddEnvKeysArgs;
        try {
          const notes: string[] = [];
          let slug = environment_slug;
          if (!slug) {
            const a = await ensureAssistant(deps, {});
            const envResult = await ensureAssistantEnv(deps, a.name);
            slug = envResult.env.slug;
            if (envResult.note) notes.push(envResult.note);
          }
          const existing = await currentEnvEntryNames(deps, slug);
          const toAdd = keys
            .filter((k) => !existing.has(k))
            .map((name) => ({ name, value: "" }));
          await upsertEnvEntries(deps, slug, toAdd);
          return okResult({
            environment_slug: slug,
            added: toAdd.map((e) => e.name),
            already_present: keys.filter((k) => existing.has(k)),
            ...(notes.length > 0 ? { notes } : {}),
          });
        } catch (e) {
          return errResult(
            e instanceof Error ? e.message : "add env keys failed",
          );
        }
      },
    },
    "add_environment_keys",
  );

  const request_environment_secrets = defineFrontendTool<
    RequestSecretsArgs,
    ToolResult
  >(
    {
      description:
        "Render a form so the user can enter sensitive credentials on the assistant's shared environment. Use this whenever you need an API key, signing secret, bot token, or any value the user must supply themselves. The keys are declared on the environment with empty values immediately (so the env always advertises what it needs) and the real values are saved when the user submits. You only see whether the user submitted — never plaintext. Pass environment_slug ONLY as an escape hatch when targeting a specific env (e.g. you just created one with create_environment).",
      parameters: z.object({
        reason: z
          .string()
          .optional()
          .describe(
            "One short sentence shown to the user explaining why these are needed.",
          ),
        keys: z
          .array(
            z.object({
              name: z
                .string()
                .describe(
                  "Variable name as it should appear in the environment, e.g. SLACK_BOT_TOKEN.",
                ),
              label: z
                .string()
                .optional()
                .describe("Friendly label shown above the input."),
              description: z
                .string()
                .optional()
                .describe("Help text shown under the label."),
              sensitive: z
                .boolean()
                .optional()
                .describe(
                  "If true, the input is masked. Default false. Always set true for tokens, secrets, passwords.",
                ),
              placeholder: z.string().optional(),
            }),
          )
          .min(1),
        environment_slug: z
          .string()
          .optional()
          .describe(
            "Override the assistant's shared env. Omit in almost all cases — a missing env is recreated automatically.",
          ),
      }),
      execute: async (args, ctx) => {
        const { keys, environment_slug } = args as RequestSecretsArgs;
        const toolCallId = ctx.toolCallId ?? "";

        let envSlug: string;
        const preNotes: string[] = [];
        try {
          if (environment_slug) {
            envSlug = environment_slug;
          } else {
            const a = await ensureAssistant(deps, {});
            const envResult = await ensureAssistantEnv(deps, a.name);
            envSlug = envResult.env.slug;
            if (envResult.note) preNotes.push(envResult.note);
          }
          const existing = await currentEnvEntryNames(deps, envSlug);
          const stubs = keys
            .filter((k) => !existing.has(k.name))
            .map((k) => ({ name: k.name, value: "" }));
          await upsertEnvEntries(deps, envSlug, stubs);
        } catch (e) {
          return errResult(
            e instanceof Error ? e.message : "prepare env failed",
          );
        }

        type FormResult = {
          success: boolean;
          cancelled?: boolean;
          saved_keys?: string[];
          values?: Record<string, string>;
          error?: string;
        };

        const userInput: FormResult = await withTimeout(
          new Promise<FormResult>((resolve) => {
            draft.registerPending(toolCallId, (r) => resolve(r as FormResult));
          }),
          15 * 60 * 1000,
          "request_environment_secrets",
        ).catch(
          (e): FormResult => ({
            success: false,
            cancelled: true,
            error: e instanceof Error ? e.message : "timeout",
          }),
        );

        if (!userInput.success) {
          return okResult({
            cancelled: true,
            saved: false,
            environment_slug: envSlug,
            declared_keys: keys.map((k) => k.name),
            notes: [
              "User chose to skip entering these secrets. The keys are declared on the environment with empty values so the env advertises what it needs. Acknowledge briefly, continue with setup, and remind them they can fill the values later from the Environments page. Do NOT retry this tool in this turn unless the user explicitly asks.",
              ...preNotes,
            ],
          });
        }

        try {
          const entries = Object.entries(userInput.values ?? {})
            .filter(([, v]) => String(v).length > 0)
            .map(([name, value]) => ({ name, value: String(value) }));
          await upsertEnvEntries(deps, envSlug, entries);
          return okResult({
            saved: true,
            environment_slug: envSlug,
            saved_keys: entries.map((e) => e.name),
            declared_keys: keys.map((k) => k.name),
            ...(preNotes.length > 0 ? { notes: preNotes } : {}),
          });
        } catch (e) {
          return errResult(e instanceof Error ? e.message : "save failed", {
            environment_slug: envSlug,
          });
        }
      },
    },
    "request_environment_secrets",
  );

  const list_trigger_definitions = defineFrontendTool<
    Record<string, never>,
    ToolResult
  >(
    {
      description:
        "List trigger definitions available to attach to assistants (e.g. 'slack', 'cron'). Returns slug, kind (webhook|schedule), title, description, JSON config schema, and required env variables.",
      parameters: z.object({}),
      execute: async () => {
        try {
          const res = await sdk.triggers.listDefinitions();
          return okResult({
            definitions: res.definitions.map((d) => ({
              slug: d.slug,
              kind: d.kind,
              title: d.title,
              description: d.description,
              config_schema: d.configSchema,
              env_requirements: d.envRequirements,
            })),
          });
        } catch (e) {
          return errResult(e instanceof Error ? e.message : "list defs failed");
        }
      },
    },
    "list_trigger_definitions",
  );

  const list_triggers = defineFrontendTool<Record<string, never>, ToolResult>(
    {
      description:
        "List trigger instances bound to the assistant being configured. Returns id, name, definition_slug, status, target, and webhook_url (when applicable). Only returns triggers whose target is the current assistant.",
      parameters: z.object({}),
      execute: async () => {
        try {
          const res = await sdk.triggers.list();
          const assistantId = draft.assistantId;
          const scoped = assistantId
            ? res.triggers.filter(
                (t) =>
                  t.targetKind === "assistant" && t.targetRef === assistantId,
              )
            : [];
          return okResult({
            triggers: scoped.map((t) => ({
              id: t.id,
              name: t.name,
              definition_slug: t.definitionSlug,
              status: t.status,
              environment_id: t.environmentId,
              target_kind: t.targetKind,
              target_ref: t.targetRef,
              target_display: t.targetDisplay,
              webhook_url: t.webhookUrl,
              config: t.config,
            })),
          });
        } catch (e) {
          return errResult(
            e instanceof Error ? e.message : "list triggers failed",
          );
        }
      },
    },
    "list_triggers",
  );

  const create_trigger = defineFrontendTool<CreateTriggerArgs, ToolResult>(
    {
      description:
        "Create a trigger instance pointed at the current assistant. The assistant must already exist (call update_assistant first if needed). The trigger is bound to the assistant's shared environment by default — omit environment_id in almost all cases. For Slack triggers the env can be empty at creation time (Gram's webhook answers Slack's url_verification challenge without a signing secret), but SLACK_BOT_TOKEN and SLACK_SIGNING_SECRET must be populated before real events fire. For cron triggers the config must include a 5-field cron string in 'schedule'. After creation: if SLACK_BOT_TOKEN is NOT yet populated on the assistant's env (check via list_environments → populated_entry_names), pass webhook_url to show_slack_app_guide so the manifest pre-fills event_subscriptions.request_url. Otherwise the bot already exists — skip the guide and use show_webhook_url (or nothing, if the trigger is just being reconfigured).",
      parameters: z.object({
        name: z.string().min(1),
        definition_slug: z.string().describe("e.g. 'slack' or 'cron'."),
        config: z
          .record(z.string(), z.any())
          .describe(
            "Trigger config matching the definition's configSchema, e.g. { event_types: ['app_mention'] } for slack or { schedule: '0 9 * * *' } for cron.",
          ),
        environment_id: z
          .string()
          .optional()
          .describe(
            "Override the assistant's shared environment for this trigger. Omit in almost all cases — the assistant's env is used by default.",
          ),
      }),
      execute: async (args) => {
        const { name, definition_slug, config, environment_id } =
          args as CreateTriggerArgs;
        const run = async (): Promise<ToolResult> => {
          try {
            const a = await ensureAssistant(deps, {});
            const notes: string[] = [];
            let boundEnvId = environment_id;
            if (!boundEnvId) {
              const envResult = await ensureAssistantEnv(deps, a.name);
              boundEnvId = envResult.env.id;
              if (envResult.note) notes.push(envResult.note);
            }
            const existingList = await sdk.triggers.list().catch(() => null);
            const duplicate = existingList?.triggers.find(
              (t) =>
                t.targetKind === "assistant" &&
                t.targetRef === a.id &&
                t.definitionSlug === definition_slug &&
                t.name === name,
            );
            if (duplicate) {
              return okResult({
                id: duplicate.id,
                name: duplicate.name,
                definition_slug: duplicate.definitionSlug,
                status: duplicate.status,
                webhook_url: duplicate.webhookUrl,
                config: duplicate.config,
                environment_id: duplicate.environmentId,
                notes: [
                  "Trigger with this name already exists for this assistant; returning the existing one instead of creating a duplicate.",
                  ...notes,
                ],
              });
            }
            const created = await sdk.triggers.create({
              createTriggerInstanceForm: {
                name,
                definitionSlug: definition_slug,
                config,
                environmentId: boundEnvId,
                targetKind: "assistant",
                targetRef: a.id,
                targetDisplay: a.name,
              },
            });
            draft.invalidateAll();
            return okResult({
              id: created.id,
              name: created.name,
              definition_slug: created.definitionSlug,
              status: created.status,
              webhook_url: created.webhookUrl,
              config: created.config,
              environment_id: created.environmentId,
              ...(notes.length > 0 ? { notes } : {}),
            });
          } catch (e) {
            return errResult(
              e instanceof Error ? e.message : "create trigger failed",
            );
          }
        };
        const key = `${draft.assistantId ?? "new"}:${definition_slug}:${name}`;
        const inflight = triggerCreateInFlight.get(key);
        if (inflight) return inflight;
        const p = run();
        triggerCreateInFlight.set(key, p);
        try {
          return await p;
        } finally {
          triggerCreateInFlight.delete(key);
        }
      },
    },
    "create_trigger",
  );

  const update_trigger = defineFrontendTool<UpdateTriggerArgs, ToolResult>(
    {
      description:
        "Update an existing trigger instance (name, config, status). Use the trigger id from list_triggers or create_trigger.",
      parameters: z.object({
        id: z.string(),
        name: z.string().optional(),
        config: z.record(z.string(), z.any()).optional(),
        status: z.enum(["active", "paused"]).optional(),
        environment_id: z.string().optional(),
      }),
      execute: async (args) => {
        const { id, name, config, status, environment_id } =
          args as UpdateTriggerArgs;
        try {
          const updated = await sdk.triggers.update({
            updateTriggerInstanceForm: {
              id,
              name,
              config,
              status,
              environmentId: environment_id,
            },
          });
          draft.invalidateAll();
          return okResult({
            id: updated.id,
            name: updated.name,
            status: updated.status,
            webhook_url: updated.webhookUrl,
          });
        } catch (e) {
          return errResult(
            e instanceof Error ? e.message : "update trigger failed",
          );
        }
      },
    },
    "update_trigger",
  );

  const show_webhook_url = defineFrontendTool<ShowWebhookArgs, ToolResult>(
    {
      description:
        "Display a webhook URL prominently to the user with copy button and instructions. Use this after creating any webhook-kind trigger (e.g. slack) so the user can paste the URL into the source service. The user does not need to do anything in the chat to confirm — this is purely informational.",
      parameters: z.object({
        trigger_name: z.string().optional(),
        webhook_url: z.string(),
        instructions: z
          .string()
          .optional()
          .describe(
            "What the user should do with this URL (e.g. 'Paste into Slack Event Subscriptions Request URL and click Verify').",
          ),
      }),
      execute: async (args) => {
        const { webhook_url } = args as ShowWebhookArgs;
        return okResult({ shown: true, webhook_url });
      },
    },
    "show_webhook_url",
  );

  const show_slack_app_guide = defineFrontendTool<
    ShowSlackGuideArgs,
    ToolResult
  >(
    {
      description:
        "Display a Slack app creation guide with a deep link that pre-fills the Slack app manifest. Use this ONLY when the user needs to create a brand-new Slack app for this assistant — i.e. SLACK_BOT_TOKEN is not yet populated on the assistant's env (check list_environments → populated_entry_names). If the bot token is already populated, the app already exists and was already installed; do not call this tool. The component derives the manifest automatically: name from the assistant's name, bot scopes from the slack platform tools attached to the assistant, and bot_events from the assistant's slack triggers. Pass the webhook_url from the Slack trigger you already created so Slack verifies it on Create. Only pass app_name / bot_scopes / bot_events to override the derived defaults. The component walks the user through Create → Install-to-Workspace → copying the xoxb- bot token and signing secret. Pure UI — no return value the model needs to act on.",
      parameters: z.object({
        app_name: z
          .string()
          .optional()
          .describe(
            "Override the Slack app display name. Defaults to the assistant's name.",
          ),
        workspace_hint: z.string().optional(),
        bot_scopes: z
          .array(z.string())
          .optional()
          .describe(
            "Extra bot scopes to add on top of the ones derived from attached slack tools. Omit unless you know a tool needs a scope the catalog doesn't cover.",
          ),
        bot_events: z
          .array(z.string())
          .optional()
          .describe(
            "Extra Slack manifest bot_events (dotted form, e.g. 'message.channels') to add on top of those derived from the assistant's slack triggers. Omit in the normal flow.",
          ),
        webhook_url: z
          .string()
          .optional()
          .describe(
            "Webhook URL of the slack trigger to pre-fill in the manifest's request_url so Slack verifies it on Create.",
          ),
      }),
      execute: async () => okResult({ shown: true }),
    },
    "show_slack_app_guide",
  );

  const list_integrations = defineFrontendTool<
    ListIntegrationsArgs,
    ToolResult
  >(
    {
      description:
        "List Gram integrations (packaged toolsets) the user can install. Returns name, summary, keywords, and tool names. Use this to discover what an assistant could do.",
      parameters: z.object({
        keywords: z.array(z.string()).optional(),
      }),
      execute: async (args) => {
        const { keywords } = args as ListIntegrationsArgs;
        try {
          const res = await sdk.integrations.list({ keywords });
          const items = res.integrations ?? [];
          return okResult({
            integrations: items.map((i) => ({
              name: i.packageName,
              title: i.packageTitle,
              summary: i.packageSummary,
              keywords: i.packageKeywords,
              tool_names: i.toolNames,
            })),
          });
        } catch (e) {
          return errResult(
            e instanceof Error ? e.message : "list integrations failed",
          );
        }
      },
    },
    "list_integrations",
  );

  const list_docs = defineFrontendTool<Record<string, never>, ToolResult>(
    {
      description:
        "List integration onboarding guides authored for this onboarding agent. Returns slug, title, summary for each doc the agent can read via read_docs. Always start by calling this when the user mentions an integration you might not have docs for.",
      parameters: z.object({}),
      execute: async () => okResult({ docs: listIntegrationDocs() }),
    },
    "list_docs",
  );

  const read_docs = defineFrontendTool<ReadDocsArgs, ToolResult>(
    {
      description:
        "Read the markdown onboarding guide for a specific integration (e.g. 'slack', 'cron'). These docs explain the exact step-by-step setup including credential collection, scopes, and trigger configuration. Always read the relevant doc before guiding the user through setup of a new integration.",
      parameters: z.object({
        slug: z
          .string()
          .describe("Integration slug from list_docs, e.g. 'slack' or 'cron'."),
      }),
      execute: async (args) => {
        const { slug } = args as ReadDocsArgs;
        const doc = getIntegrationDoc(slug);
        if (!doc) {
          return errResult(`No doc found for slug "${slug}"`, {
            available: listIntegrationDocs().map((d) => d.slug),
          });
        }
        return okResult({
          slug: doc.slug,
          title: doc.title,
          body: doc.body,
        });
      },
    },
    "read_docs",
  );

  const finish_onboarding = defineFrontendTool<FinishArgs, ToolResult>(
    {
      description:
        "Signal that the assistant is fully configured and the user is happy. Returns a summary of the assistant. Call only after the user confirms they're done.",
      parameters: z.object({
        message: z
          .string()
          .optional()
          .describe("Optional summary message to display to the user."),
      }),
      execute: async (args) => {
        const { message } = args as FinishArgs;
        if (!draft.assistantId) {
          return errResult("No assistant has been created yet.");
        }
        try {
          const a = await sdk.assistants.get({ id: draft.assistantId });
          return okResult({
            assistant: {
              id: a.id,
              name: a.name,
              status: a.status,
              toolsets: a.toolsets,
            },
            message: message ?? "All set! Your assistant is configured.",
          });
        } catch (e) {
          return errResult(e instanceof Error ? e.message : "summary failed");
        }
      },
    },
    "finish_onboarding",
  );

  return {
    propose_identity,
    update_assistant,
    set_personality,
    set_tasks,
    attach_toolset,
    detach_toolset,
    list_toolsets,
    create_toolset,
    add_tools_to_toolset,
    list_available_tools,
    list_environments,
    create_environment,
    add_environment_keys,
    request_environment_secrets,
    list_trigger_definitions,
    list_triggers,
    create_trigger,
    update_trigger,
    show_webhook_url,
    show_slack_app_guide,
    list_integrations,
    list_docs,
    read_docs,
    finish_onboarding,
  };
}

type OnboardingTools = ReturnType<typeof buildAssistantTools>;

export function useOnboardingTools(): {
  frontendTools: Record<string, FrontendTool<Record<string, unknown>, unknown>>;
  components: Record<string, ToolCallMessagePartComponent>;
  toolsRequiringApproval: string[];
} {
  const sdk = useSdkClient();
  const session = useSession();
  const draft = useAssistantDraft();
  const organizationId = session.activeOrganizationId;

  const frontendTools = useMemo<OnboardingTools>(
    () => buildAssistantTools({ sdk, organizationId, draft }),
    [sdk, organizationId, draft],
  );

  const components = useMemo<Record<string, ToolCallMessagePartComponent>>(
    () => ({
      propose_identity: ProposeIdentityComponent,
      request_environment_secrets: RequestEnvironmentSecretsComponent,
      show_webhook_url: ShowWebhookUrlComponent,
      show_slack_app_guide: ShowSlackAppGuideComponent,
    }),
    [],
  );

  return {
    frontendTools: frontendTools as unknown as Record<
      string,
      FrontendTool<Record<string, unknown>, unknown>
    >,
    components,
    toolsRequiringApproval: [],
  };
}
