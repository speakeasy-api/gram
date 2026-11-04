import { useCallback } from "react";
import { useSearchParams } from "react-router";
import {
  TOOL_USAGE_TYPES_WITHOUT_LOCAL_TOOLS,
  toTargetTypes,
} from "@/components/observe/observeTargetFilters";
import type { GetToolUsageSummaryPayload } from "@gram/client/models/components/gettoolusagesummarypayload";

/**
 * Whether an identity page counts the identity's local tool calls in its
 * Tool calls chart and its Tool logs. Kept in the URL so a link keeps the
 * choice, and written only when local tools are hidden: showing them is the
 * default, so a default link carries no param.
 */
export const LOCAL_TOOLS_PARAM = "localTools";
const HIDE_LOCAL_TOOLS = "hide";

export function showsLocalTools(params: URLSearchParams): boolean {
  return params.get(LOCAL_TOOLS_PARAM) !== HIDE_LOCAL_TOOLS;
}

export function withShowLocalTools(
  params: URLSearchParams,
  show: boolean,
): URLSearchParams {
  const next = new URLSearchParams(params);
  if (show) {
    next.delete(LOCAL_TOOLS_PARAM);
    return next;
  }
  next.set(LOCAL_TOOLS_PARAM, HIDE_LOCAL_TOOLS);
  return next;
}

export function useShowLocalTools(): [boolean, (show: boolean) => void] {
  const [params, setParams] = useSearchParams();
  const setShow = useCallback(
    (show: boolean) => {
      setParams((prev) => withShowLocalTools(prev, show), { replace: true });
    },
    [setParams],
  );
  return [showsLocalTools(params), setShow];
}

/**
 * The body an agent's Tool calls card sends to the usage endpoints. With
 * local tools hidden it names every other target type, so the chart, the
 * call count and the top sources all leave them out together.
 */
export function agentToolUsagePayload({
  agentId,
  from,
  to,
  includeLocalTools,
}: {
  agentId: string;
  from: Date;
  to: Date;
  includeLocalTools: boolean;
}): GetToolUsageSummaryPayload {
  // The agent's bare id, not its `agent:` urn: the telemetry filter takes the
  // principal's id and builds the urn itself.
  const payload: GetToolUsageSummaryPayload = {
    from,
    to,
    userFilters: [{ kind: "agent_id", key: agentId }],
  };
  if (includeLocalTools) return payload;
  return {
    ...payload,
    targetTypes: toTargetTypes(TOOL_USAGE_TYPES_WITHOUT_LOCAL_TOOLS),
  };
}
