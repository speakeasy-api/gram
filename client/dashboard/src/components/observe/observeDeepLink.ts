import {
  encodeGatewayServerFilter,
  encodeHostedServerFilter,
  encodeShadowServerFilter,
  type ParsedTargetFilter,
} from "@/components/observe/observeTargetFilters";
import { applyFilterAdd } from "@/pages/logs/log-filter-types";
import { parseFilters, serializeFilters } from "@/pages/logs/log-filter-url";
import { Operator } from "@gram/client/models/components/logfilter";

/**
 * The observe filter params both pages share. Anything listed here survives a
 * move between Insights and Logs; `q` and `af` deliberately do not, since a
 * free-text search is intent local to the page it was typed on.
 *
 * ObserveFilterBar's "Reset to default" clears exactly this set, so the two
 * read from one list rather than drifting apart.
 */
export const OBSERVE_FILTER_PARAMS = [
  "server",
  "user",
  "source",
  "role",
  "hookTypes",
  "status",
  "account_type",
  "client",
  "range",
  "from",
  "to",
  "label",
] as const;

/** The attribute a tool-scoped drill-down filters on. */
export const TOOL_NAME_ATTRIBUTE_PATH = "gram.tool.name";

export type ObserveDeepLinkScope = {
  /** A row on a server, gateway, or shadow-server panel. */
  target?: ParsedTargetFilter;
  /** Skill and local-tool rows have no server identity; they narrow by type. */
  targetTypes?: string[];
  toolName?: string;
  statuses?: string[];
  userEmail?: string;
  hookSource?: string;
  clientKey?: string;
};

function appendCsv(
  params: URLSearchParams,
  key: string,
  value: string | undefined,
): void {
  if (!value) return;
  const existing = params.get(key);
  const values = existing ? existing.split(",").filter(Boolean) : [];
  if (values.includes(value)) return;
  params.set(key, [...values, value].join(","));
}

function encodeTarget(target: ParsedTargetFilter): string | undefined {
  switch (target.type) {
    case "hosted":
      return encodeHostedServerFilter(target.id);
    case "shadow":
      return encodeShadowServerFilter(target.id);
    case "gateway":
      return encodeGatewayServerFilter(target.id);
    default:
      return undefined;
  }
}

/** Copies the shared filter state forward, dropping everything page-local. */
export function carryObserveParams(current: URLSearchParams): URLSearchParams {
  const next = new URLSearchParams();
  for (const key of OBSERVE_FILTER_PARAMS) {
    const value = current.get(key);
    if (value !== null) next.set(key, value);
  }
  return next;
}

/**
 * Builds a link to `base` carrying the current observe filters, narrowed by
 * `scope`.
 *
 * Everything that can ride as a first-class param does, because those map to
 * payload fields the server answers from the pre-aggregated trace summaries.
 * A tool name has no such field, so it has to go through the `af` attribute
 * chips — which drops the listing onto a raw log scan. That is the reason for
 * the split, not taste: put a tool name in `af` and the page warns the search
 * is slow; put a server there and it would be needlessly slow.
 */
export function buildObserveHref(
  base: string,
  current: URLSearchParams,
  scope: ObserveDeepLinkScope = {},
): string {
  const params = carryObserveParams(current);

  if (scope.target) appendCsv(params, "server", encodeTarget(scope.target));
  for (const type of scope.targetTypes ?? []) {
    appendCsv(params, "hookTypes", type);
  }
  for (const status of scope.statuses ?? []) {
    appendCsv(params, "status", status);
  }
  appendCsv(params, "user", scope.userEmail);
  appendCsv(params, "source", scope.hookSource);
  appendCsv(params, "client", scope.clientKey);

  if (scope.toolName) {
    // Merge rather than overwrite: a reader who already narrowed by one tool
    // and then drills into another expects both chips, not a silent swap.
    const merged = applyFilterAdd(parseFilters(current.get("af")), {
      path: TOOL_NAME_ATTRIBUTE_PATH,
      op: Operator.Eq,
      value: scope.toolName,
    });
    const serialized = serializeFilters(merged);
    if (serialized) params.set("af", serialized);
  }

  const query = params.toString();
  return query ? `${base}?${query}` : base;
}
