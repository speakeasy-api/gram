import { mcpServerRouteParam } from "@/lib/sources";
import { mcpServerTabHref } from "@/pages/mcp/x/MCPServerDetailsRouting";
import type { useRoutes } from "@/routes";
import { logsSearch, PROBLEM_PARAM } from "./filters";
import type { ProjectSuspectedProblem } from "./types";

/** The page that lists the problem, opened on it where that page can. */
export function connectionProblemHref(
  routes: ReturnType<typeof useRoutes>,
  problem: ProjectSuspectedProblem,
  asOf: Date,
): string {
  const open = `?${PROBLEM_PARAM}=${encodeURIComponent(problem.id)}`;
  if (problem.server) {
    return `${mcpServerTabHref(routes, mcpServerRouteParam(problem.server), "sessions")}${open}`;
  }
  const issuer = problem.userSessionIssuerId;
  if (issuer && problem.part === "speakeasy") {
    return `${routes.mcpSessions.provider.overview.href(issuer)}${open}`;
  }
  if (issuer) {
    return `${routes.mcpSessions.provider.connectionLogs.href(issuer)}${logsSearch(problem.filters, asOf)}`;
  }
  return routes.mcpSessions.href();
}
