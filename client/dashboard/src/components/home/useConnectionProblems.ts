import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { useProjectSuspectedProblems } from "@/pages/mcp-sessions/connection-health/api";
import { useConnectionHealthFlag } from "@/pages/mcp-sessions/connection-health/flag";
import type { ProjectSuspectedProblem } from "@/pages/mcp-sessions/connection-health/types";
import { useLogsEnabled } from "./useLogsEnabled";

/** Whether this viewer gets connection problem rows: the flag, logs on and `log:read`. */
export function useConnectionProblemsAccess(): "pending" | "on" | "off" {
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

/** The project's suspected MCP connection problems, for viewers who may read them. */
export function useConnectionProblems(limit: number): {
  access: "pending" | "on" | "off";
  problems: ProjectSuspectedProblem[];
  total: number;
  /** When the problems were read; links to Connection Logs end there. */
  asOf: Date;
  pending: boolean;
  error: boolean;
} {
  const access = useConnectionProblemsAccess();
  const on = access === "on";
  const query = useProjectSuspectedProblems({ limit, enabled: on });
  // Problems name other people: none with logging off or for own-scope readers.
  const data =
    on &&
    query.data?.loggingEnabled === true &&
    query.data.viewerScope === "all"
      ? query.data
      : undefined;
  return {
    access,
    problems: data?.problems ?? [],
    total: data?.problemsTotal ?? 0,
    asOf: new Date(query.dataUpdatedAt),
    pending: access === "pending" || (on && query.isPending),
    error: on && query.isError,
  };
}
