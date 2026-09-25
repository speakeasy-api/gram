import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { restoreFleetFocus } from "./fleet-focus";
import { useMemo, useRef } from "react";
import { Link, useSearchParams } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { ResourceListPage } from "@/components/page-templates";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { useOrganization, useProject, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { useReadableAgents } from "@/hooks/useReadableAgents";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { useKillswitchAccess } from "@/hooks/useKillswitchAccess";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { useRoutes } from "@/routes";
import { useListChats } from "@gram/client/react-query/listChats.js";
import { useAssistantsList } from "@gram/client/react-query/assistantsList.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { buildFleetRows } from "./fleet-model";
import { FleetCollection } from "./FleetCollection";
import { FleetInspector } from "./FleetInspector";
import { AgentRestrictions } from "./AgentRestrictions";
import "./fleet.css";

const PAGE_SIZE = 50;
const SOURCES = [
  { value: "all", label: "All sources" },
  { value: "agent", label: "Registered agents" },
  { value: "assistant", label: "Assistants" },
  { value: "session", label: "Captured sessions" },
];
export default function Fleet(): JSX.Element {
  return (
    <RequireScope scope="project:read" level="page">
      <FleetPage />
    </RequireScope>
  );
}
function FleetPage(): JSX.Element {
  const project = useProject();
  const organization = useOrganization();
  const session = useSession();
  const sdk = useSdkClient();
  const routes = useRoutes();
  const { hasScope } = useRBAC();
  const access = useKillswitchAccess();
  const agentFlag = useFeatureFlag(FEATURE_FLAGS.agentManagement);
  const assistantFlag = useFeatureFlag(FEATURE_FLAGS.assistants);
  const [params, setParams] = useSearchParams();
  const q = params.get("q") ?? "";
  const source = params.get("source") ?? "all";
  const view = params.get("view") === "directory" ? "directory" : "list";
  const offset = Math.max(
    0,
    Number.parseInt(params.get("offset") ?? "0", 10) || 0,
  );
  const selected = params.get("selected");
  const restrictions = params.get("tab") === "restrictions";
  const originatingRow = useRef<string | null>(null);
  const update = (values: Record<string, string | null>, replace = false) =>
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous);
        for (const [key, value] of Object.entries(values)) {
          if (value == null) next.delete(key);
          else next.set(key, value);
        }
        return next;
      },
      { replace },
    );
  const agents = useReadableAgents(agentFlag.status === "enabled");
  const assistants = useAssistantsList(undefined, undefined, {
    enabled: assistantFlag.status === "enabled" && hasScope("project:read"),
    throwOnError: false,
    retry: false,
    refetchInterval: 30_000,
  });
  const members = useMembers(undefined, undefined, {
    enabled: hasScope("org:read"),
    throwOnError: false,
    retry: false,
  });
  const chats = useListChats(
    {
      limit: PAGE_SIZE,
      offset,
      search: q || undefined,
      sortBy: "last_message_timestamp",
      sortOrder: "desc",
    },
    undefined,
    { throwOnError: false, retry: false, refetchInterval: 30_000 },
  );
  const rows = useMemo(
    () =>
      buildFleetRows({
        agents: agents.data ?? [],
        assistants: assistants.data?.assistants ?? [],
        sessions: chats.data?.chats ?? [],
        members: members.data?.members ?? [],
        projectId: project.id,
      }),
    [agents.data, assistants.data, chats.data, members.data, project.id],
  );
  const filtered = rows.filter(
    (row) =>
      (source === "all" || row.source === source) &&
      (row.source === "session" ||
        `${row.title} ${row.personName} ${row.department}`
          .toLowerCase()
          .includes(q.toLowerCase())),
  );
  const agentIds = rows
    .flatMap((row) => (row.agent ? [row.agent.id] : []))
    .sort();
  const badges = useQuery({
    queryKey: [
      "fleet-agent-badges",
      organization.id,
      session.user.id,
      agentIds,
    ],
    enabled: access.canAccess && agentIds.length > 0,
    throwOnError: false,
    retry: false,
    refetchInterval: 15_000,
    queryFn: async ({ signal }) => {
      const batches: string[][] = [];
      for (let start = 0; start < agentIds.length; start += 100)
        batches.push(agentIds.slice(start, start + 100));
      const results = await Promise.all(
        batches.map((agentIds) =>
          sdk.killswitches.batchAgentBadges(
            { killswitchBatchAgentBadgesRequest: { agentIds } },
            { sessionHeaderGramSession: session.session },
            { signal },
          ),
        ),
      );
      return results.flatMap((result) => result.badges);
    },
  });
  const blocked = new Set(
    badges.data
      ?.filter((badge) => badge.affectedNow)
      .map((badge) => `agent:${badge.agentId}`) ?? [],
  );
  const selectedRow = filtered.find((row) => row.id === selected);
  const select = (id: string) => {
    originatingRow.current = id;
    update({ selected: id, detail: null, restriction: null });
  };
  const close = () => {
    const id = originatingRow.current ?? selected;
    update({ selected: null, detail: null, restriction: null });
    requestAnimationFrame(() => restoreFleetFocus(id));
  };
  const refresh = () => {
    void Promise.all([
      chats.refetch(),
      ...(agentFlag.status === "enabled" ? [agents.refetch()] : []),
      ...(assistantFlag.status === "enabled" ? [assistants.refetch()] : []),
      ...(hasScope("org:read") ? [members.refetch()] : []),
      ...(access.canAccess && agentIds.length ? [badges.refetch()] : []),
    ]);
  };
  const errors = [
    agents.error && "Registered agents",
    assistants.error && "Assistants",
    chats.error && "Captured sessions",
    members.error && "Directory profiles",
    badges.error && "MCP status",
  ].filter(Boolean);
  return (
    <ResourceListPage
      title="Fleet"
      area="Identity"
      description={`Agents and captured sessions in ${project.name}. Organization-wide identities are included.`}
      hideToolbar
      primaryAction={
        agentFlag.status === "enabled" ? (
          <Link to={routes.agents.href()}>
            <Button variant="secondary">Manage agent identities</Button>
          </Link>
        ) : undefined
      }
    >
      <div className="space-y-4">
        <Page.Toolbar>
          <Page.Toolbar.Row>
            <Page.Toolbar.Search
              value={q}
              onChange={(value) =>
                update({ q: value || null, offset: null }, true)
              }
              placeholder="Search Fleet"
              debounceMs={300}
            />
            <Page.Toolbar.Actions>
              <SegmentedControl
                value={view}
                onChange={(value) => update({ view: value })}
                options={[
                  {
                    value: "list",
                    label: "List",
                    tooltip: "Agents and sessions in a list",
                  },
                  {
                    value: "directory",
                    label: "Directory",
                    tooltip: "Browse by owner department and identity",
                  },
                ]}
              />
            </Page.Toolbar.Actions>
            <Page.Toolbar.Refresh
              onRefresh={refresh}
              isRefreshing={
                chats.isFetching || agents.isFetching || assistants.isFetching
              }
            />
          </Page.Toolbar.Row>
          <Page.Toolbar.Row>
            <Page.Toolbar.Leading>
              <div className="hidden md:block">
                <SegmentedControl
                  value={source}
                  onChange={(value) => update({ source: value, tab: null })}
                  options={SOURCES.map((item) => ({
                    ...item,
                    tooltip: `Show ${item.label.toLowerCase()}`,
                  }))}
                />
              </div>
              <div className="md:hidden">
                <Select
                  value={source}
                  onValueChange={(value) =>
                    update({ source: value, tab: null })
                  }
                >
                  <SelectTrigger aria-label="Fleet source">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {SOURCES.map((item) => (
                      <SelectItem key={item.value} value={item.value}>
                        {item.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </Page.Toolbar.Leading>
            <Page.Toolbar.Actions>
              {access.canAccess && (
                <Button
                  variant={restrictions ? "primary" : "secondary"}
                  onClick={() =>
                    update({
                      tab: restrictions ? null : "restrictions",
                      restriction: null,
                    })
                  }
                >
                  Agent restrictions
                </Button>
              )}
            </Page.Toolbar.Actions>
          </Page.Toolbar.Row>
        </Page.Toolbar>
        {errors.length > 0 && (
          <div role="alert" className="border-destructive border p-3 text-sm">
            Couldn’t refresh {errors.join(", ")}. Previously loaded data may be
            stale.{" "}
            <Button variant="tertiary" size="sm" onClick={refresh}>
              Try again
            </Button>
          </div>
        )}
        {!hasScope("chat:read") && (
          <p className="text-muted-foreground text-sm">
            Only your own captured sessions are shown. Other inventory follows
            your identity permissions.
          </p>
        )}
        {agentFlag.status !== "enabled" && (
          <p className="text-muted-foreground text-sm">
            Registered-agent inventory is unavailable. Captured sessions and
            permitted restrictions remain accessible.
          </p>
        )}
        {restrictions && access.canAccess && (
          <AgentRestrictions
            agents={agents.data ?? []}
            inventoryAvailable={agents.isSuccess}
          />
        )}
        {!restrictions && (
          <>
            <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
              <span>
                {filtered.length} loaded items · {chats.data?.total ?? "…"}{" "}
                captured sessions match the search
              </span>
              <span>
                Last refresh{" "}
                {chats.dataUpdatedAt
                  ? new Date(chats.dataUpdatedAt).toLocaleTimeString()
                  : "pending"}
              </span>
            </div>
            {(chats.isLoading || agents.isLoading || assistants.isLoading) &&
              !rows.length && <SkeletonTable />}
            <div
              className="fleet-workspace"
              data-selected={Boolean(selectedRow)}
            >
              <div className="fleet-collection">
                <h2
                  id="fleet-collection-heading"
                  tabIndex={-1}
                  className="sr-only"
                >
                  Fleet collection
                </h2>
                <FleetCollection
                  rows={filtered}
                  view={view}
                  selected={selected}
                  onSelect={select}
                  blocked={blocked}
                  projectName={project.name}
                />
              </div>
              {selectedRow && (
                <FleetInspector
                  row={selectedRow}
                  rows={rows}
                  onClose={close}
                  onSelect={select}
                  blocked={blocked.has(selectedRow.id)}
                />
              )}
            </div>
            {selected && !selectedRow && !chats.isLoading && (
              <p role="status" className="text-sm">
                The selected item is not in the loaded page or is no longer
                available.{" "}
                <Button variant="tertiary" size="sm" onClick={close}>
                  Clear selection
                </Button>
              </p>
            )}
            <div className="flex flex-wrap items-center justify-between gap-2 border-t pt-3">
              <span className="text-muted-foreground text-xs">
                Captured sessions: {chats.data?.chats.length ?? 0} loaded of{" "}
                {chats.data?.total ?? "…"}. Registered identities stay visible
                without a captured session.
              </span>
              <div className="flex gap-2">
                <Button
                  variant="secondary"
                  size="sm"
                  disabled={offset === 0 || chats.isFetching}
                  onClick={() =>
                    update({
                      offset: String(Math.max(0, offset - PAGE_SIZE)),
                      selected: null,
                    })
                  }
                >
                  Previous sessions
                </Button>
                <Button
                  variant="secondary"
                  size="sm"
                  disabled={
                    !chats.data ||
                    offset + PAGE_SIZE >= chats.data.total ||
                    chats.isFetching
                  }
                  onClick={() =>
                    update({
                      offset: String(offset + PAGE_SIZE),
                      selected: null,
                    })
                  }
                >
                  Next sessions
                </Button>
              </div>
            </div>
          </>
        )}
      </div>
    </ResourceListPage>
  );
}
