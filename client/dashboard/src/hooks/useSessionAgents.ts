import { useMemo } from "react";
import { useReadableAgents } from "@/hooks/useReadableAgents";
import {
  resolveSessionAgents,
  type ResolvedSession,
} from "@/lib/session-agents";
import type { UserSession } from "@gram/client/models/components/usersession.js";

export function useSessionAgents(sessions: UserSession[]): ResolvedSession[] {
  const agents = useReadableAgents(
    sessions.some((session) => session.subjectType === "agent"),
  );
  return useMemo(
    () =>
      resolveSessionAgents(sessions, agents.isError ? [] : (agents.data ?? [])),
    [sessions, agents.data, agents.isError],
  );
}
