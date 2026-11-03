import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { PageTabsList, PageTabsTrigger, Tabs } from "@/components/ui/Tabs";
import { useRoutes } from "@/routes";
import { Link } from "react-router";
import { useOrgMemoryEnabled } from "./useOrgMemoryEnabled";

export type AgentSessionsTab = "sessions" | "memory";

/**
 * Agent Sessions' two views: the sessions themselves, and the Organizational
 * Memory built from them. They are separate routes so a memory link can be
 * shared on its own, which is why each page renders the strip rather than
 * the strip owning them. Outside the rollout there is only one view, so no
 * strip is drawn.
 */
export function AgentSessionsTabs({
  active,
}: {
  active: AgentSessionsTab;
}): JSX.Element | null {
  const routes = useRoutes();
  const enabled = useOrgMemoryEnabled();
  if (!enabled) return null;

  return (
    // The page header already closes with its own bottom margin; the strip
    // pulls up into it so the tabs sit with the title.
    <Tabs value={active} className="-mt-6 mb-4 w-full">
      <div className="overflow-x-auto border-b">
        <PageTabsList className="h-auto gap-6 bg-transparent p-0">
          <PageTabsTrigger value="sessions" asChild>
            <Link to={routes.agentSessions.href()}>Sessions</Link>
          </PageTabsTrigger>
          <PageTabsTrigger value="memory" asChild>
            <Link
              to={routes.agentSessions.memory.href()}
              className="inline-flex items-center gap-2"
            >
              Organizational Memory
              <ReleaseStageBadge stage="preview" noTooltip />
            </Link>
          </PageTabsTrigger>
        </PageTabsList>
      </div>
    </Tabs>
  );
}
