import type { CreateRiskPolicyRequestBody } from "@gram/client/models/components/createriskpolicyrequestbody.js";
import type { RiskMCPScope } from "@gram/client/models/components/riskmcpscope.js";
import type { PolicyAction, RuleCategory } from "../policy-data";
import {
  categoriesToPayload,
  FLAG_ONLY_CATEGORIES,
  type DetectorMode,
} from "../policy-form";

/** Detectors a server-scoped guardrail offers. The full editor has more; this
 *  is the set that makes sense for tool traffic through one MCP server. */
export const SERVER_GUARDRAIL_CATEGORIES: RuleCategory[] = [
  "secrets",
  "pii",
  "destructive_tool",
  "prompt_injection",
];

const DEFAULT_SERVER_GUARDRAIL_SCORE = 7;

export interface ServerTool {
  name: string;
  destructive: boolean;
}

export type ServerToolMode = "all" | "selected";
type ServerGuardrailAudience = "everyone" | "targeted";

export interface ServerGuardrailState {
  categories: Set<RuleCategory>;
  toolMode: ServerToolMode;
  selectedTools: string[];
  action: PolicyAction;
  userMessage: string;
  score: number;
  audienceType: ServerGuardrailAudience;
  audiencePrincipalUrns: Set<string>;
}

export function defaultServerGuardrailState(): ServerGuardrailState {
  return {
    categories: new Set<RuleCategory>(["secrets"]),
    toolMode: "all",
    selectedTools: [],
    action: "flag",
    userMessage: "",
    score: DEFAULT_SERVER_GUARDRAIL_SCORE,
    audienceType: "everyone",
    audiencePrincipalUrns: new Set(),
  };
}

/** Whether any selected detector only supports logging. Mirrors
 *  policycore.ValidateSourceAction on the server. */
export function hasFlagOnlyCategory(categories: Set<RuleCategory>): boolean {
  return [...categories].some((category) => FLAG_ONLY_CATEGORIES.has(category));
}

/** The action to submit: flag-only detectors force logging. */
export function effectiveAction(state: ServerGuardrailState): PolicyAction {
  return hasFlagOnlyCategory(state.categories) ? "flag" : state.action;
}

/** Pre-fills a catalog install from the tools' annotations: secrets and PII
 *  warn and ask for confirmation, and destructive tools are inspected when the
 *  server has any. The destructive detector only supports logging, so a preset
 *  that includes it logs. */
export function catalogPresetState(tools: ServerTool[]): ServerGuardrailState {
  const categories = new Set<RuleCategory>(["secrets", "pii"]);
  const destructive = tools.some((tool) => tool.destructive);
  if (destructive) {
    categories.add("destructive_tool");
  }
  return {
    ...defaultServerGuardrailState(),
    categories,
    action: destructive ? "flag" : "warn",
  };
}

export function destructiveToolNames(tools: ServerTool[]): string[] {
  return tools.filter((tool) => tool.destructive).map((tool) => tool.name);
}

export type ServerGuardrailValidation =
  | { ok: true }
  | { ok: false; message: string };

export function validateServerGuardrail(
  state: ServerGuardrailState,
): ServerGuardrailValidation {
  if (state.categories.size === 0) {
    return { ok: false, message: "Select at least one detector." };
  }
  if (state.toolMode === "selected" && state.selectedTools.length === 0) {
    return {
      ok: false,
      message: "Select at least one tool, or inspect all tools.",
    };
  }
  if (
    state.audienceType === "targeted" &&
    state.audiencePrincipalUrns.size === 0
  ) {
    return {
      ok: false,
      message: "Pick at least one user or role, or apply to everyone.",
    };
  }
  return { ok: true };
}

export function serverGuardrailScope(
  state: Pick<ServerGuardrailState, "toolMode" | "selectedTools">,
  mcpServerIds: string[],
): RiskMCPScope {
  const tools =
    state.toolMode === "selected" ? [...state.selectedTools].sort() : undefined;
  return {
    allServers: false,
    toolAnnotations: [],
    servers: mcpServerIds.map((mcpServerId) => ({
      mcpServerId,
      ...(tools === undefined ? {} : { tools }),
    })),
  };
}

export function defaultServerGuardrailName(serverName: string): string {
  return `${serverName.trim() || "MCP server"} guardrail`;
}

/** The create request for a guardrail scoped to the given servers. Tool
 *  requests and responses are always inspected (scanners do both), so no
 *  message-type field is sent. */
export function buildServerGuardrailRequest(
  state: ServerGuardrailState,
  options: {
    mcpServerIds: string[];
    name: string;
    mode: DetectorMode;
  },
): CreateRiskPolicyRequestBody {
  // An empty server list is read by the backend as an unrestricted scope, which
  // would turn a server guardrail into an org-wide policy.
  if (options.mcpServerIds.length === 0) {
    throw new Error("A server guardrail needs at least one MCP server.");
  }
  const { sources, presidioEntities, promptInjectionRules, disabledRules } =
    categoriesToPayload(state.categories, new Set(), new Set(), options.mode);
  const message = state.userMessage.trim();
  return {
    name: options.name.trim(),
    policyType: "standard",
    enabled: true,
    sources,
    presidioEntities,
    promptInjectionRules,
    disabledRules,
    action: effectiveAction(state),
    score: state.score,
    audienceType: state.audienceType,
    audiencePrincipalUrns:
      state.audienceType === "targeted" ? [...state.audiencePrincipalUrns] : [],
    ...(message === "" ? {} : { userMessage: message }),
    mcpScope: serverGuardrailScope(state, options.mcpServerIds),
  };
}
