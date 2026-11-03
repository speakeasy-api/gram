import { ResourceListPage } from "@/components/page-templates";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { formatCompact } from "@/lib/format";
import { useChatDetailSheet } from "@/pages/chatLogs/useChatDetailSheet";
import { useRoutes } from "@/routes";
import { Navigate, Outlet, useLocation, useSearchParams } from "react-router";
import { AgentSessionsTabs } from "./AgentSessionsTabs";
import type { DigestLevel } from "./memoryFormat";
import { OrgMemoryTimeline } from "./OrgMemoryTimeline";
import {
  ORG_MEMORY_OVERVIEW_DAYS,
  useOrgMemoryStatus,
} from "./useOrgMemoryEnabled";

const LEVELS: { value: DigestLevel; label: string }[] = [
  { value: "month", label: "Months" },
  { value: "week", label: "Weeks" },
  { value: "day", label: "Days" },
];

function parseLevel(value: string | null): DigestLevel {
  return value === "month" || value === "day" ? value : "week";
}

/** Agent Sessions and its Organizational Memory tab share this route root. */
export function AgentSessionsRoot(): JSX.Element {
  return <Outlet />;
}

/** The retired Org Memory page now lives in Agent Sessions. */
export function OrgMemoryRedirect(): JSX.Element {
  const routes = useRoutes();
  const location = useLocation();
  return (
    <Navigate
      to={`${routes.agentSessions.memory.href()}${location.search}`}
      replace
    />
  );
}

/**
 * Organizational Memory: what a project's agents can recall about past team
 * sessions. It shows the same digests, masked summaries and memos agents read
 * through the Platform MCP, newest period first, and every item links back to
 * the session it came from. Everything shown is already masked.
 */
export function OrgMemoryPage(): JSX.Element {
  const routes = useRoutes();
  const project = useProject();
  const { status, overview } = useOrgMemoryStatus();
  const [searchParams, setSearchParams] = useSearchParams();
  const level = parseLevel(searchParams.get("level"));
  const selectedId = searchParams.get("digest");
  const { openChat, sheet } = useChatDetailSheet();
  // Memory needs memory:read; opening the cited transcript needs chat:read.
  const { hasAnyScope } = useRBAC();
  const canOpenSessions = hasAnyScope(["chat:read"], project.id);

  const enabled = status === "enabled";

  if (status === "disabled") {
    return <Navigate to={routes.agentSessions.href()} replace />;
  }

  const select = (next: { level?: DigestLevel; digest?: string | null }) => {
    setSearchParams(
      (prev) => {
        const params = new URLSearchParams(prev);
        if (next.level) params.set("level", next.level);
        if (next.digest === null) params.delete("digest");
        else if (next.digest) params.set("digest", next.digest);
        return params;
      },
      { replace: true },
    );
  };

  const stats = overview.data;
  return (
    <ResourceListPage
      scope="memory:read"
      resourceId={project.id}
      title="Agent Sessions"
      description="What your agents remember about the team's past sessions: day, week and month digests built from masked session summaries and the memos agents saved."
      belowHeader={<AgentSessionsTabs active="memory" />}
      metrics={
        stats
          ? [
              {
                key: "remembered",
                label: "Sessions remembered",
                value: formatCompact(stats.sessionsRemembered),
                description: `Last ${ORG_MEMORY_OVERVIEW_DAYS} days`,
                tone: "neutral",
                size: "sm",
              },
              {
                key: "memos",
                label: "Memos saved",
                value: formatCompact(stats.memosSaved),
                description: "Facts agents chose to keep",
                tone: "neutral",
                size: "sm",
              },
              {
                key: "recalls",
                label: "Agent recalls",
                value: formatCompact(stats.recalls),
                description: "Reads through the Platform MCP",
                tone: stats.recalls > 0 ? "success" : "neutral",
                size: "sm",
              },
              {
                key: "left-out",
                label: "Left out",
                value: formatCompact(stats.sessionsLeftOut),
                description: "Excluded or personal sessions",
                tone: "neutral",
                size: "sm",
              },
            ]
          : undefined
      }
      metricsLoading={status === "loading"}
      toolbarActions={
        <SegmentedControl
          ariaLabel="Digest level"
          value={level}
          onChange={(value) => select({ level: value, digest: null })}
          options={LEVELS}
        />
      }
      isLoading={!enabled}
    >
      {/* Keyed by project so kept-previous pages never cross projects. */}
      <OrgMemoryTimeline
        key={project.id}
        level={level}
        selectedId={selectedId}
        onSelect={(digest, nextLevel) =>
          select({ digest, level: nextLevel ?? level })
        }
        onOpenSession={canOpenSessions ? openChat : undefined}
      />
      {sheet}
    </ResourceListPage>
  );
}
