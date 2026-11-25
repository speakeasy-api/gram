import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { getHttpStatusCode } from "@/lib/route-errors";
import { useProjectSuspectedProblems } from "@/pages/mcp-sessions/connection-health/api";
import { useConnectionHealthFlag } from "@/pages/mcp-sessions/connection-health/flag";
import type { ProjectSuspectedProblem } from "@/pages/mcp-sessions/connection-health/types";
import { useLogsEnabled } from "./useLogsEnabled";

/** Whether this viewer may ask for connection problems: the flag, logs on and `log:read`. */
function useConnectionProblemsEligibility(): "pending" | "on" | "off" {
  const flag = useConnectionHealthFlag();
  const project = useProject();
  const logs = useLogsEnabled();
  const { hasScope, isLoading } = useRBAC();
  if (flag === "pending" || isLoading) return "pending";
  return flag === "on" &&
    logs.enabled &&
    !!project.id &&
    hasScope("log:read", project.id)
    ? "on"
    : "off";
}

/**
 * The project's suspected MCP connection problems. `display` is "tall" once
 * the viewer is eligible, unless the API answers that they may not see them.
 */
export function useConnectionProblems(limit: number): {
  display: "pending" | "tall" | "base";
  problems: ProjectSuspectedProblem[];
  total: number;
  /** When the problems were read; links to Connection Logs end there. */
  asOf: Date;
  pending: boolean;
  error: boolean;
} {
  const eligibility = useConnectionProblemsEligibility();
  const on = eligibility === "on";
  const query = useProjectSuspectedProblems({ limit, enabled: on });
  // Not found: the server has connection health off for this organization.
  const off = on && query.isError && getHttpStatusCode(query.error) === 404;
  const error = on && query.isError && !off;
  // A failed refetch keeps the old data; never show rows from it.
  const answer = on && !query.isError ? query.data : undefined;
  // Problems name other people: none with logging off or for own-scope readers.
  const denied =
    off ||
    (answer !== undefined &&
      (!answer.loggingEnabled || answer.viewerScope !== "all"));
  const shown = answer && !denied ? answer : undefined;
  return {
    display:
      eligibility === "pending" ? "pending" : !on || denied ? "base" : "tall",
    problems: shown?.problems ?? [],
    total: shown?.problemsTotal ?? 0,
    asOf: new Date(query.dataUpdatedAt),
    pending: eligibility === "pending" || (on && query.isPending),
    error,
  };
}
