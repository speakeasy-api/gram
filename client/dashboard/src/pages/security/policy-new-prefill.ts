import type { PolicyAction, RuleCategory } from "./policy-data";
import { AVAILABLE_CATEGORIES } from "./policy-form";

/** A new standard policy's starting values, carried in the URL so another page
 *  (the catalog install's "customize now") can open the full editor pre-filled. */
export interface PolicyNewPrefill {
  categories?: Set<RuleCategory>;
  mcpServerIds: string[];
  action?: PolicyAction;
  score?: number;
  name?: string;
}

const ACTIONS: ReadonlySet<PolicyAction> = new Set([
  "flag",
  "warn",
  "block",
  "quarantine",
]);

function list(value: string | null): string[] {
  return (value ?? "")
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
}

/** Reads the prefill from the new-policy URL, dropping anything unrecognized.
 *  `category` is the older single-category form and still works. */
export function parsePolicyNewPrefill(
  params: URLSearchParams,
): PolicyNewPrefill {
  const categories = [
    ...list(params.get("categories")),
    ...list(params.get("category")),
  ].filter((category): category is RuleCategory =>
    AVAILABLE_CATEGORIES.has(category as RuleCategory),
  );
  const action = params.get("action");
  const score = Number(params.get("score"));
  const name = params.get("name")?.trim();
  return {
    ...(categories.length > 0 ? { categories: new Set(categories) } : {}),
    mcpServerIds: list(params.get("mcp_server_id")),
    ...(action && ACTIONS.has(action as PolicyAction)
      ? { action: action as PolicyAction }
      : {}),
    ...(params.has("score") && score >= 0.1 && score <= 10 ? { score } : {}),
    ...(name ? { name } : {}),
  };
}

/** Query string for the new-policy page, opening the standard editor with the
 *  given starting values. */
export function policyNewPrefillQuery(prefill: PolicyNewPrefill): string {
  const params = new URLSearchParams({ kind: "standard" });
  if (prefill.categories && prefill.categories.size > 0) {
    params.set("categories", [...prefill.categories].join(","));
  }
  if (prefill.mcpServerIds.length > 0) {
    params.set("mcp_server_id", prefill.mcpServerIds.join(","));
  }
  if (prefill.action) params.set("action", prefill.action);
  if (prefill.score !== undefined) params.set("score", String(prefill.score));
  if (prefill.name) params.set("name", prefill.name);
  return `?${params.toString()}`;
}
