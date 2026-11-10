import { isKnownEnumValue, requireKnownEnumValue } from "@/lib/enum";
import {
  RiskPolicyAudienceType,
  RiskPolicyPolicyType,
  RiskPolicyShadowMcpDisposition,
  type RiskPolicy,
} from "@gram/client/models/components/riskpolicy.js";
import {
  RiskPresetAction,
  type RiskPreset,
} from "@gram/client/models/components/riskpreset.js";
import {
  SuggestRiskPolicyResultAction,
  type SuggestRiskPolicyResult,
} from "@gram/client/models/components/suggestriskpolicyresult.js";
import type { PolicyAction } from "./policy-data";
import type { PolicyMCPScopeValue } from "./policy-mcp-scope";

/** Where a draft should be enforced when it is not left to agent sessions:
 * the MCP servers to start the scope step with, and the server names that
 * were recognized in the administrator's description, if that is where the
 * selection came from. */
type PolicyDraftMCPScope = {
  serverIds: string[];
  matchedNames: string[];
};

/** Where a draft came from, so the wizard can say so and send the
 * administrator back to that starting point with their description intact. */
type PolicyDraftOrigin =
  | { source: "preset" }
  | {
      source: "describe";
      description: string;
      suggestion: SuggestRiskPolicyResult;
      /** A use case picked over the suggestion on the review screen. */
      overridePresetId: string | null;
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
  /** The policy detects nothing until approved email domains are set. */
  requiresApprovedEmailDomains?: boolean;
  origin?: PolicyDraftOrigin;
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

// Sources the editors can round-trip through policyToCategories and
// categoriesToPayload. Unknown sources must not disappear during an edit.
const SUPPORTED_SOURCES: ReadonlySet<string> = new Set([
  "gitleaks",
  "presidio",
  "shadow_mcp",
  "destructive_tool",
  "cli_destructive",
  "prompt_injection",
  "account_identity",
]);

function supportsSources(sources: readonly string[]): boolean {
  return sources.every((source) => SUPPORTED_SOURCES.has(source));
}

function draftSources(sources: readonly string[]): string[] {
  if (!supportsSources(sources)) {
    throw new Error(
      "This policy's detectors are not supported by this version of Speakeasy. Refresh the page before editing.",
    );
  }
  return [...sources];
}

export function supportsPolicyDraft(source: {
  policyType: string;
  action: string;
  sources: readonly string[];
}): boolean {
  return (
    isKnownEnumValue(RiskPolicyPolicyType, source.policyType) &&
    // A draft opens in a detection editor, so require_approval is not one.
    ACTIONS.has(source.action) &&
    supportsSources(source.sources)
  );
}

export function supportsPolicyEditing(
  policy: Pick<
    RiskPolicy,
    | "policyType"
    | "action"
    | "audienceType"
    | "shadowMcpDisposition"
    | "sources"
  >,
): boolean {
  return (
    supportsPolicyDraft(policy) &&
    isKnownEnumValue(RiskPolicyAudienceType, policy.audienceType) &&
    (policy.shadowMcpDisposition === undefined ||
      isKnownEnumValue(
        RiskPolicyShadowMcpDisposition,
        policy.shadowMcpDisposition,
      ))
  );
}

export function draftFromPreset(preset: RiskPreset): PolicyDraft {
  return {
    presetId: preset.id,
    policyType: requireKnownEnumValue(RiskPolicyPolicyType, preset.policyType),
    name: preset.label,
    action: requireKnownEnumValue(RiskPresetAction, preset.action),
    score: preset.score,
    sources: draftSources(preset.sources),
    presidioEntities: [...preset.presidioEntities],
    prompt: preset.prompt,
    userMessage: preset.userMessage,
    requiresApprovedEmailDomains: preset.requiresApprovedEmailDomains,
    origin: { source: "preset" },
  };
}

export function draftFromSuggestion(
  suggestion: SuggestRiskPolicyResult,
): PolicyDraft {
  return {
    presetId: suggestion.presetId,
    policyType: requireKnownEnumValue(
      RiskPolicyPolicyType,
      suggestion.policyType,
    ),
    name: suggestion.name,
    action: requireKnownEnumValue(
      SuggestRiskPolicyResultAction,
      suggestion.action,
    ),
    score: suggestion.score,
    sources: draftSources(suggestion.sources),
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
    typeof draft.name !== "string" ||
    (draft.sources !== undefined &&
      (!Array.isArray(draft.sources) || !supportsSources(draft.sources)))
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
    ...readDraftMCPScope(draft.mcpScope),
    ...(draft.requiresApprovedEmailDomains === true
      ? { requiresApprovedEmailDomains: true }
      : {}),
    ...readDraftOrigin(draft.origin),
  };
}

function readDraftOrigin(value: unknown): { origin: PolicyDraftOrigin } | null {
  if (!value || typeof value !== "object") return null;
  const origin = value as Partial<Record<string, unknown>>;
  if (origin.source === "preset") return { origin: { source: "preset" } };
  if (origin.source !== "describe" || typeof origin.description !== "string") {
    return null;
  }
  const suggestion = readSuggestion(origin.suggestion);
  if (!suggestion) return null;
  return {
    origin: {
      source: "describe",
      description: origin.description,
      suggestion,
      overridePresetId:
        typeof origin.overridePresetId === "string"
          ? origin.overridePresetId
          : null,
    },
  };
}

/** A suggestion carried through router state, checked for the fields the
 * review screen reads. */
export function readSuggestion(value: unknown): SuggestRiskPolicyResult | null {
  if (!value || typeof value !== "object") return null;
  const s = value as Partial<SuggestRiskPolicyResult>;
  if (
    typeof s.presetId !== "string" ||
    (s.policyType !== "standard" && s.policyType !== "prompt_based") ||
    typeof s.name !== "string" ||
    typeof s.action !== "string" ||
    typeof s.score !== "number" ||
    typeof s.rationale !== "string" ||
    typeof s.confidence !== "number" ||
    !Array.isArray(s.sources) ||
    !Array.isArray(s.presidioEntities) ||
    !Array.isArray(s.alternatives)
  ) {
    return null;
  }
  return {
    presetId: s.presetId,
    policyType: s.policyType,
    name: s.name,
    action: s.action,
    score: s.score,
    sources: s.sources.filter((v): v is string => typeof v === "string"),
    presidioEntities: s.presidioEntities.filter(
      (v): v is string => typeof v === "string",
    ),
    prompt: typeof s.prompt === "string" ? s.prompt : "",
    userMessage: typeof s.userMessage === "string" ? s.userMessage : "",
    rationale: s.rationale,
    confidence: s.confidence,
    alternatives: s.alternatives.filter(
      (alt): alt is SuggestRiskPolicyResult["alternatives"][number] =>
        !!alt &&
        typeof alt === "object" &&
        typeof alt.presetId === "string" &&
        typeof alt.label === "string" &&
        typeof alt.confidence === "number",
    ),
  };
}

/** Router state that reopens the intent screen where the administrator left
 * it: the describe box filled in, and the review screen if they had one. */
export type PolicyStartRestore = {
  description: string;
  review: {
    suggestion: SuggestRiskPolicyResult;
    overridePresetId: string | null;
  } | null;
};

export function restoreFromDraft(draft: PolicyDraft): PolicyStartRestore {
  if (draft.origin?.source !== "describe") {
    return { description: "", review: null };
  }
  return {
    description: draft.origin.description,
    review: {
      suggestion: draft.origin.suggestion,
      overridePresetId: draft.origin.overridePresetId,
    },
  };
}

export function readPolicyStartRestore(
  state: unknown,
): PolicyStartRestore | null {
  if (!state || typeof state !== "object") return null;
  const restore = (state as { restore?: unknown }).restore;
  if (!restore || typeof restore !== "object") return null;
  const r = restore as Partial<Record<string, unknown>>;
  const description = typeof r.description === "string" ? r.description : "";
  const review =
    r.review && typeof r.review === "object"
      ? (r.review as Partial<Record<string, unknown>>)
      : null;
  const suggestion = review ? readSuggestion(review.suggestion) : null;
  return {
    description,
    review: suggestion
      ? {
          suggestion,
          overridePresetId:
            typeof review?.overridePresetId === "string"
              ? review.overridePresetId
              : null,
        }
      : null,
  };
}

const POLICY_START_STORAGE_PREFIX = "speakeasy:policy-start:";

/** The intent screen's description and draft review for a project, kept for
 * the browser session so leaving the page does not lose them. */
export function loadPolicyStartSession(
  projectSlug: string,
): PolicyStartRestore | null {
  try {
    const raw = window.sessionStorage.getItem(
      POLICY_START_STORAGE_PREFIX + projectSlug,
    );
    return raw ? readPolicyStartRestore({ restore: JSON.parse(raw) }) : null;
  } catch {
    return null;
  }
}

export function savePolicyStartSession(
  projectSlug: string,
  restore: PolicyStartRestore,
): void {
  try {
    const key = POLICY_START_STORAGE_PREFIX + projectSlug;
    if (!restore.description.trim() && !restore.review) {
      window.sessionStorage.removeItem(key);
      return;
    }
    window.sessionStorage.setItem(key, JSON.stringify(restore));
  } catch {
    // Storage can be unavailable; the screen then starts empty next time.
  }
}

export function clearPolicyStartSession(projectSlug: string): void {
  try {
    window.sessionStorage.removeItem(POLICY_START_STORAGE_PREFIX + projectSlug);
  } catch {
    // Nothing to clear when storage is unavailable.
  }
}

export function actionBadgeVariant(
  action: string,
): "neutral" | "warning" | "destructive" {
  if (action === "warn") return "warning";
  if (action === "block" || action === "quarantine") return "destructive";
  return "neutral";
}

const HOSTNAME =
  /^(?=.{1,253}$)(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$/i;

/** Entries of a comma- or space-separated domain list that are not hostnames. */
export function invalidEmailDomains(domains: readonly string[]): string[] {
  return domains.filter((domain) => !HOSTNAME.test(domain));
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
