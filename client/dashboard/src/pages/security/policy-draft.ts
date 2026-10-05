import type { RiskPreset } from "@gram/client/models/components/riskpreset.js";
import type { SuggestRiskPolicyResult } from "@gram/client/models/components/suggestriskpolicyresult.js";
import type { PolicyAction } from "./policy-data";
import type { PolicyMCPScopeValue } from "./policy-mcp-scope";

/** Where a draft should be enforced when it is not left to agent sessions:
 * the MCP servers to start the scope step with, and the server names that
 * were recognized in the administrator's description, if that is where the
 * selection came from. */
export type PolicyDraftMCPScope = {
  serverIds: string[];
  matchedNames: string[];
};

/** A prefilled starting point for the create wizard, carried in router state
 * from the intent screen. Mirrors the server's suggestion result so a preset
 * and a described draft feed the editors the same way. */
export type PolicyDraft = {
  presetId: string;
  policyType: "standard" | "prompt_based";
  name: string;
  action: PolicyAction;
  score: number;
  sources: string[];
  presidioEntities: string[];
  prompt: string;
  userMessage: string;
  mcpScope?: PolicyDraftMCPScope;
};

const ACTIONS: ReadonlySet<string> = new Set([
  "flag",
  "warn",
  "block",
  "quarantine",
]);

function toAction(value: string): PolicyAction {
  return ACTIONS.has(value) ? (value as PolicyAction) : "flag";
}

export function draftFromPreset(preset: RiskPreset): PolicyDraft {
  return {
    presetId: preset.id,
    policyType: preset.policyType,
    name: preset.label,
    action: toAction(preset.action),
    score: preset.score,
    sources: [...preset.sources],
    presidioEntities: [...preset.presidioEntities],
    prompt: preset.prompt,
    userMessage: preset.userMessage,
  };
}

export function draftFromSuggestion(
  suggestion: SuggestRiskPolicyResult,
): PolicyDraft {
  return {
    presetId: suggestion.presetId,
    policyType: suggestion.policyType,
    name: suggestion.name,
    action: toAction(suggestion.action),
    score: suggestion.score,
    sources: [...suggestion.sources],
    presidioEntities: [...suggestion.presidioEntities],
    prompt: suggestion.prompt,
    userMessage: suggestion.userMessage,
  };
}

// Mirrors ValidateMCPScopeSources in server/internal/risk/policycore: these
// detectors work on the session, not on individual MCP calls.
const SESSION_ONLY_SOURCES: Record<string, string> = {
  shadow_mcp:
    "It looks for servers outside your approved list, so it can't be limited to chosen servers.",
  account_identity:
    "It checks who is signed in to the session, so it can't be limited to MCP servers.",
};

/** Why a draft with these sources cannot be limited to MCP servers, or null
 * when it can. */
export function sessionOnlyReason(sources: readonly string[]): string | null {
  for (const source of sources) {
    const reason = SESSION_ONLY_SOURCES[source];
    if (reason) return reason;
  }
  return null;
}

/** MCP servers the description names. A server matches when its whole name or
 * slug appears in the text on word boundaries, so "GitHub" does not match a
 * description that only says "hub". */
export function matchMCPServers<
  T extends {
    id: string;
    name?: string | undefined;
    slug?: string | undefined;
  },
>(description: string, servers: readonly T[]): T[] {
  const text = ` ${description.toLowerCase().replace(/[^a-z0-9]+/g, " ")} `;
  return servers.filter((server) =>
    [server.name, server.slug].some((label) => {
      const needle = (label ?? "").toLowerCase().replace(/[^a-z0-9]+/g, " ");
      return needle.trim().length >= 3 && text.includes(` ${needle.trim()} `);
    }),
  );
}

/** The scope step's starting value for a draft, or null to leave it on the
 * editor's default. */
export function draftMCPScopeValue(
  draft: PolicyDraft | null | undefined,
): PolicyMCPScopeValue | null {
  if (!draft?.mcpScope || sessionOnlyReason(draft.sources)) return null;
  return {
    mode: "mcp",
    allServers: false,
    toolAnnotations: [],
    servers: draft.mcpScope.serverIds.map((mcpServerId) => ({ mcpServerId })),
  };
}

/** The wizard kind a draft opens in. */
export function draftKind(draft: PolicyDraft): "standard" | "prompt" {
  return draft.policyType === "prompt_based" ? "prompt" : "standard";
}

/** Router state is untyped; accept only a shape the editors can seed from. */
export function readPolicyDraft(state: unknown): PolicyDraft | null {
  if (!state || typeof state !== "object") return null;
  const candidate = (state as { draft?: unknown }).draft;
  if (!candidate || typeof candidate !== "object") return null;
  const draft = candidate as Partial<PolicyDraft>;
  if (
    (draft.policyType !== "standard" && draft.policyType !== "prompt_based") ||
    typeof draft.name !== "string"
  ) {
    return null;
  }
  return {
    presetId: typeof draft.presetId === "string" ? draft.presetId : "",
    policyType: draft.policyType,
    name: draft.name,
    action: toAction(typeof draft.action === "string" ? draft.action : "flag"),
    score: typeof draft.score === "number" ? draft.score : 5,
    sources: Array.isArray(draft.sources) ? draft.sources : [],
    presidioEntities: Array.isArray(draft.presidioEntities)
      ? draft.presidioEntities
      : [],
    prompt: typeof draft.prompt === "string" ? draft.prompt : "",
    userMessage: typeof draft.userMessage === "string" ? draft.userMessage : "",
    ...(readDraftMCPScope(draft.mcpScope) ?? {}),
  };
}

function readDraftMCPScope(
  value: unknown,
): { mcpScope: PolicyDraftMCPScope } | null {
  if (!value || typeof value !== "object") return null;
  const scope = value as Partial<PolicyDraftMCPScope>;
  const strings = (list: unknown): string[] =>
    Array.isArray(list)
      ? list.filter((item): item is string => typeof item === "string")
      : [];
  return {
    mcpScope: {
      serverIds: strings(scope.serverIds),
      matchedNames: strings(scope.matchedNames),
    },
  };
}
