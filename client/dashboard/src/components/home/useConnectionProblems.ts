import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { mcpServerRouteParam } from "@/lib/sources";
import { mcpServerTabHref } from "@/pages/mcp/x/MCPServerDetailsRouting";
import { useProjectSuspectedProblems } from "@/pages/mcp-sessions/connection-health/api";
import {
  logsSearch,
  PROBLEM_PARAM,
} from "@/pages/mcp-sessions/connection-health/filters";
import { useConnectionHealthFlag } from "@/pages/mcp-sessions/connection-health/flag";
import type {
  Outcome,
  StepGroup,
} from "@/pages/mcp-sessions/connection-health/types";
import type { useRoutes } from "@/routes";
import type { ProjectSuspectedProblem } from "@gram/client/models/components/projectsuspectedproblem.js";

/** Rows Needs attention holds when it spans two grid rows. */
export const TALL_ATTENTION_ROWS = 6;

/** Whether this viewer gets connection problem rows: the flag and `log:read`. */
export function useConnectionProblemsAccess(): "pending" | "on" | "off" {
  const flag = useConnectionHealthFlag();
  const project = useProject();
  const { hasScope, isLoading } = useRBAC();
  if (flag === "pending" || isLoading) return "pending";
  return flag === "on" && !!project.id && hasScope("log:read", project.id)
    ? "on"
    : "off";
}

/** The project's suspected MCP connection problems, for viewers who may read them. */
export function useConnectionProblems(): {
  access: "pending" | "on" | "off";
  problems: ProjectSuspectedProblem[];
  total: number;
  pending: boolean;
  error: boolean;
} {
  const access = useConnectionProblemsAccess();
  const on = access === "on";
  const query = useProjectSuspectedProblems({
    limit: TALL_ATTENTION_ROWS,
    enabled: on,
  });
  // Problems name other people: none with logging off or for own-scope readers.
  const data =
    on &&
    query.data?.loggingEnabled === true &&
    query.data.connectionViewerScope === "all"
      ? query.data
      : undefined;
  return {
    access,
    problems: data?.problems ?? [],
    total: data?.problemsTotal ?? 0,
    pending: access === "pending" || (on && query.isPending),
    error: on && query.isError,
  };
}

/** The page that lists the problem, opened on it where the page can. */
export function connectionProblemHref(
  routes: ReturnType<typeof useRoutes>,
  problem: ProjectSuspectedProblem,
  now: Date,
): string {
  const open = `?${PROBLEM_PARAM}=${encodeURIComponent(problem.id)}`;
  if (problem.server) {
    return `${mcpServerTabHref(routes, mcpServerRouteParam(problem.server), "sessions")}${open}`;
  }
  const issuer = problem.userSessionIssuerId;
  if (issuer && problem.suspectedProblemPart === "speakeasy") {
    return `${routes.mcpSessions.provider.overview.href(issuer)}${open}`;
  }
  if (issuer) {
    const { from, outcomes, steps, ...filters } = problem.filters;
    return `${routes.mcpSessions.provider.connectionLogs.href(issuer)}${logsSearch(
      {
        ...filters,
        from: from.toISOString(),
        outcomes: outcomes as Outcome[] | undefined,
        steps: steps as StepGroup[] | undefined,
      },
      now,
    )}`;
  }
  return routes.mcpSessions.href();
}
