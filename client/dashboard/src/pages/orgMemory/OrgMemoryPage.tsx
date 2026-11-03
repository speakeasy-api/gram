import { ResourceListPage } from "@/components/page-templates";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { useProject } from "@/contexts/Auth";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { formatCompact } from "@/lib/format";
import { useChatDetailSheet } from "@/pages/chatLogs/useChatDetailSheet";
import { useRoutes } from "@/routes";
import { useOrgMemoryOverview } from "@gram/client/react-query/orgMemoryOverview.js";
import { Navigate, Outlet, useLocation, useSearchParams } from "react-router";
import { AgentSessionsTabs } from "./AgentSessionsTabs";
import type { DigestLevel } from "./memoryFormat";
import { OrgMemoryTimeline } from "./OrgMemoryTimeline";

const OVERVIEW_DAYS = 30;

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
  const flag = useFeatureFlag(FEATURE_FLAGS.orgMemory);
  const [searchParams, setSearchParams] = useSearchParams();
  const level = parseLevel(searchParams.get("level"));
  const selectedId = searchParams.get("digest");
  const { openChat, sheet } = useChatDetailSheet();

  const enabled = flag.status === "enabled";
  const overview = useOrgMemoryOverview({ days: OVERVIEW_DAYS }, undefined, {
    enabled,
    throwOnError: false,
  });

  if (flag.status !== "loading" && !enabled) {
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
      scope="chat:read"
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
                description: `Last ${OVERVIEW_DAYS} days`,
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
      metricsLoading={overview.isPending && enabled}
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
      <OrgMemoryTimeline
        level={level}
        selectedId={selectedId}
        onSelect={(digest, nextLevel) =>
          select({ digest, level: nextLevel ?? level })
        }
        onOpenSession={openChat}
      />
      {sheet}
    </ResourceListPage>
  );
}
