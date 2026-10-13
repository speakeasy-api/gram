import type { AiScanTarget } from "@gram/client/models/components/aiscantarget.js";
import { TARGET_CATEGORIES } from "@/pages/device-agent/ai-scan-target-draft";

/**
 * Assistant context for the Shadow AI targets tab. The library has no
 * assistant tool behind it, so the facts the assistant can answer from are
 * spelled out here: how big the library is, by kind and by origin, and which
 * targets the organization added itself. Shadow MCP activity is the one
 * related thing it can query.
 */
export function shadowAITargetsInsightsContext(data: {
  targets: AiScanTarget[];
  listVersion: number;
}): string {
  const builtIn = data.targets.filter((target) => target.origin === "default");
  const custom = data.targets.filter((target) => target.origin !== "default");
  const byKind = (targets: AiScanTarget[]) =>
    TARGET_CATEGORIES.map(
      (category) =>
        `${category.plural.toLowerCase()} ${targets.filter((target) => target.category === category.value).length}`,
    ).join(", ");
  return [
    "Page: Guardrails, Shadow AI targets tab — the organization's Shadow AI scan target library: the AI tools its device agents probe for.",
    `Library version ${data.listVersion}.`,
    `Built-in targets: ${builtIn.length} (${byKind(builtIn)}).`,
    `Targets this organization added: ${custom.length}${custom.length > 0 ? ` (${custom.map((target) => target.displayName).join(", ")})` : ""}.`,
    "The library is not queryable through tools; answer library questions from these facts.",
    "Shadow MCP activity is queryable: listRiskResultsForAgent with source shadow_mcp (its match is a server URL or command and safe to name).",
    "Never echo match_redacted values verbatim for other sources. Refer to findings by rule_id and source.",
  ].join(" ");
}
