import { useFleetParamUpdate } from "./useFleetParamUpdate";
import { agentRestrictionLabel } from "./fleet-model";
import { useEffect, useMemo } from "react";
import { useSearchParams } from "react-router";
import { KillswitchRecord } from "@/components/killswitch/KillswitchRecord";
import {
  nextScheduleBoundaryDelay,
  scopeLabel,
} from "@/components/killswitch/killswitch-view-model";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Table, type Column } from "@/components/ui/Table";
import { useSession } from "@/contexts/Auth";
import { useKillswitchesInfinite } from "@gram/client/react-query/killswitches.js";
import { useKillswitch } from "@gram/client/react-query/killswitch.js";
import { useKillswitchMCPServers } from "@gram/client/react-query/killswitchMCPServers.js";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { KillswitchSummary } from "@gram/client/models/components/killswitchsummary.js";

export function AgentRestrictions({
  agents,
  inventoryAvailable,
  agentId,
}: {
  agents: ManagedAgent[];
  inventoryAvailable: boolean;
  agentId?: string;
}): JSX.Element {
  const session = useSession();
  const security = { sessionHeaderGramSession: session.session };
  const [params] = useSearchParams();
  const update = useFleetParamUpdate();
  const selected = params.get("restriction");
  const list = useKillswitchesInfinite(
    security,
    {
      principalKind: "agent",
      agentId,
      limit: 25,
      gramSession: session.session,
    },
    {
      initialPageParam: undefined,
      throwOnError: false,
    },
  );
  const servers = useKillswitchMCPServers(
    security,
    { gramSession: session.session },
    { throwOnError: false },
  );
  const items = useMemo(
    () => list.data?.pages.flatMap((page) => page.result.items) ?? [],
    [list.data],
  );
  const refetchList = list.refetch;
  useEffect(() => {
    const delay = nextScheduleBoundaryDelay(items.map((item) => item.schedule));
    if (delay == null) return;
    const timer = window.setTimeout(() => void refetchList(), delay);
    return () => window.clearTimeout(timer);
  }, [items, refetchList]);
  const names = useMemo(
    () =>
      new Map(
        servers.data?.servers.map((server) => [
          server.id,
          `${server.name} (${server.projectName})`,
        ]) ?? [],
      ),
    [servers.data],
  );
  const select = (id?: string) => update({ restriction: id ?? null }, true);
  const columns: Column<KillswitchSummary>[] = [
    {
      key: "agent",
      header: "Registered agent",
      render: (item) => (
        <button
          className="text-left underline underline-offset-4"
          onClick={() => select(item.id)}
        >
          {agentRestrictionLabel(
            item.agentId ?? "",
            agents,
            inventoryAvailable,
          )}
        </button>
      ),
    },
    {
      key: "scope",
      header: "MCP servers",
      render: (item) => scopeLabel(item.scope, names),
    },
    {
      key: "status",
      header: "Restriction",
      render: (item) => (
        <Badge variant={item.status === "active" ? "destructive" : "neutral"}>
          <Badge.Text>{item.status}</Badge.Text>
        </Badge>
      ),
    },
    {
      key: "action",
      header: "",
      render: (item) => (
        <Button size="sm" variant="secondary" onClick={() => select(item.id)}>
          View restriction
        </Button>
      ),
    },
  ];
  return (
    <section className="space-y-4" aria-label="Agent restrictions">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h2 className="font-medium">Agent restrictions</h2>
          <p className="text-muted-foreground text-sm">
            Organization-wide · all projects and credential sessions
          </p>
        </div>
        <Button
          variant="tertiary"
          size="sm"
          onClick={() => void list.refetch()}
        >
          Refresh restrictions
        </Button>
      </div>
      {selected && (
        <AgentRestrictionRecord
          id={selected}
          expectedAgentId={agentId}
          agents={agents}
          inventoryAvailable={inventoryAvailable}
          onSelect={select}
          onClose={() => select()}
        />
      )}
      {list.error && (
        <div role="alert" className="border-destructive border p-3 text-sm">
          Couldn’t refresh agent restrictions.{" "}
          {items.length > 0 && "Previously loaded restrictions may be stale."}{" "}
          <Button
            variant="tertiary"
            size="sm"
            onClick={() => void list.refetch()}
          >
            Try again
          </Button>
        </div>
      )}
      {list.isLoading && <SkeletonTable />}
      {!list.isLoading && (!list.error || items.length > 0) && (
        <Table
          columns={columns}
          data={items}
          rowKey={(item) => item.id}
          noResultsMessage="No agent restrictions."
        />
      )}
      {servers.error && (
        <p className="text-muted-foreground text-sm">
          Server names are unavailable. Open a restriction to retry.
        </p>
      )}
      {list.data && (
        <div className="flex items-center justify-between">
          <span className="text-muted-foreground text-xs">
            {items.length} {list.hasNextPage && "loaded "}
            {items.length === 1 ? "restriction" : "restrictions"}
          </span>
          {list.hasNextPage && (
            <Button
              variant="secondary"
              size="sm"
              disabled={list.isFetchingNextPage}
              onClick={() => void list.fetchNextPage()}
            >
              Load more restrictions
            </Button>
          )}
        </div>
      )}
    </section>
  );
}
export function AgentRestrictionRecord({
  id,
  expectedAgentId,
  agents,
  inventoryAvailable,
  onSelect,
  onClose,
}: {
  id: string;
  expectedAgentId?: string;
  agents: ManagedAgent[];
  inventoryAvailable: boolean;
  onSelect: (id: string) => void;
  onClose: () => void;
}): JSX.Element {
  const session = useSession();
  const detail = useKillswitch(
    { sessionHeaderGramSession: session.session },
    { id, gramSession: session.session },
    { throwOnError: false },
  );
  if (detail.isLoading) return <SkeletonTable />;
  if (detail.error || !detail.data)
    return (
      <div role="alert" className="border p-4 text-sm">
        Couldn’t load this restriction.{" "}
        <Button variant="tertiary" onClick={() => void detail.refetch()}>
          Try again
        </Button>
        <Button variant="tertiary" onClick={onClose}>
          Close
        </Button>
      </div>
    );
  if (detail.data.principalKind !== "agent" || !detail.data.agentId)
    return (
      <div role="alert">
        This is not an agent restriction.{" "}
        <Button variant="tertiary" onClick={onClose}>
          Close
        </Button>
      </div>
    );
  const agentId = expectedAgentId ?? detail.data.agentId;
  if (detail.data.agentId !== agentId)
    return (
      <div role="alert">
        This restriction belongs to a different agent.{" "}
        <Button variant="tertiary" onClick={onClose}>
          Close
        </Button>
      </div>
    );
  const agent = agents.find((item) => item.id === agentId);
  return (
    <KillswitchRecord
      killswitchId={id}
      subjectAgent={{
        id: agentId,
        name: agentRestrictionLabel(agentId, agents, inventoryAvailable),
        canEdit: Boolean(agent && agent.lifecycle !== "revoked"),
      }}
      onSelectKillswitch={onSelect}
      onClose={onClose}
    />
  );
}
