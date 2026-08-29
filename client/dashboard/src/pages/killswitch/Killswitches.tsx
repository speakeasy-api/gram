import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { useSession } from "@/contexts/Auth";
import { useOrgRoutes } from "@/routes";
import { useQueryClient } from "@tanstack/react-query";
import { useMembers } from "@gram/client/react-query/members.js";
import { useKillswitchCapabilities } from "@gram/client/react-query/killswitchCapabilities.js";
import { useKillswitchMCPServers } from "@gram/client/react-query/killswitchMCPServers.js";
import {
  invalidateAllKillswitches,
  useKillswitchesInfinite,
} from "@gram/client/react-query/killswitches.js";
import { useCreateKillswitchMutation } from "@gram/client/react-query/createKillswitch.js";
import { usePreviewKillswitchOverlapsMutation } from "@gram/client/react-query/previewKillswitchOverlaps.js";
import type { KillswitchCapabilityKey } from "@gram/client/models/components/killswitchcapabilitykey.js";
import type { KillswitchListStatus } from "@gram/client/models/components/killswitchliststatus.js";
import type { KillswitchSummary } from "@gram/client/models/components/killswitchsummary.js";
import { Link, useSearchParams } from "react-router";
import { lazy, Suspense, useEffect, useMemo, useState } from "react";
import { FeatureRequestModal } from "@/components/FeatureRequestModal";
import { capitalize } from "@/lib/utils";
import {
  draftToSchedule,
  draftToScope,
  nextScheduleBoundaryDelay,
  scheduleLabel,
  scopeLabel,
  type EditorDraft,
} from "./killswitch-view-model";

const EMPTY: never[] = [];
const KillswitchEditorSheet = lazy(() =>
  import("./KillswitchEditorSheet").then((module) => ({
    default: module.KillswitchEditorSheet,
  })),
);
const statuses: KillswitchListStatus[] = [
  "active",
  "scheduled",
  "expired",
  "lifted",
];

export default function Killswitches(): JSX.Element {
  const session = useSession();
  const security = { sessionHeaderGramSession: session.session };
  const routes = useOrgRoutes();
  const queryClient = useQueryClient();
  const [params, setParams] = useSearchParams();
  const [editorOpen, setEditorOpen] = useState(false);
  const [requestOpen, setRequestOpen] = useState(false);
  const userId = params.get("user") || undefined;
  const statusParam = params.get("status");
  const status = statuses.includes(statusParam as KillswitchListStatus)
    ? (statusParam as KillswitchListStatus)
    : undefined;
  const capabilityParam = params.get("capability");
  const capabilityKey =
    capabilityParam === "mcp_tool_calls"
      ? (capabilityParam as KillswitchCapabilityKey)
      : undefined;

  const listQuery = useKillswitchesInfinite(
    security,
    { userId, status, capabilityKey, limit: 25 },
    { initialPageParam: undefined, throwOnError: false },
  );
  const membersQuery = useMembers(undefined, security, { throwOnError: false });
  const capabilitiesQuery = useKillswitchCapabilities(security, undefined, {
    enabled: editorOpen,
    throwOnError: false,
  });
  const serversQuery = useKillswitchMCPServers(security, undefined, {
    throwOnError: false,
  });
  const createMutation = useCreateKillswitchMutation();
  const previewMutation = usePreviewKillswitchOverlapsMutation();

  const items = useMemo(
    () => listQuery.data?.pages.flatMap((page) => page.result.items) ?? [],
    [listQuery.data],
  );
  const members = membersQuery.data?.members ?? EMPTY;
  const capabilities = capabilitiesQuery.data?.capabilities ?? EMPTY;
  const comingSoon = capabilitiesQuery.data?.comingSoon ?? EMPTY;
  const servers = serversQuery.data?.servers ?? EMPTY;
  const memberNames = useMemo(
    () => new Map(members.map((member) => [member.id, member.name])),
    [members],
  );
  const serverNames = useMemo(
    () => new Map(servers.map((server) => [server.id, server.name])),
    [servers],
  );
  const readError = listQuery.error ?? membersQuery.error ?? serversQuery.error;
  const editorCatalogError = capabilitiesQuery.error;
  const editorCatalogLoading = capabilitiesQuery.isLoading;
  const refetchList = listQuery.refetch;

  useEffect(() => {
    const delay = nextScheduleBoundaryDelay(items.map((item) => item.schedule));
    if (delay == null) return;
    const timer = window.setTimeout(() => void refetchList(), delay);
    return () => window.clearTimeout(timer);
  }, [items, refetchList]);

  const setFilter = (key: string, value: string) => {
    setParams((current) => {
      const next = new URLSearchParams(current);
      if (value) next.set(key, value);
      else next.delete(key);
      return next;
    });
  };

  const preview = (draft: EditorDraft) =>
    previewMutation.mutateAsync({
      security,
      request: {
        killswitchPreviewOverlapsRequest: {
          userId: draft.userId,
          capabilityKey: "mcp_tool_calls",
          scope: draftToScope(draft),
          schedule: draftToSchedule(draft),
        },
      },
    });

  const create = async (draft: EditorDraft, operationId: string) => {
    const receipt = await createMutation.mutateAsync({
      security,
      request: {
        killswitchCreateRequest: {
          userId: draft.userId,
          capabilityKey: "mcp_tool_calls",
          scope: draftToScope(draft),
          schedule: draftToSchedule(draft),
          externalNote: draft.externalNote,
          internalNote: draft.internalNote,
          operationId,
        },
      },
    });
    await invalidateAllKillswitches(queryClient);
    return receipt;
  };

  return (
    <main className="mx-auto w-full max-w-[1270px] space-y-6 px-4 py-6 sm:px-8 sm:py-8">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold">Killswitch</h1>
          <p className="text-muted-foreground mt-1 text-sm">
            Turn off one capability for one team member across explicit MCP
            server scope and time.
          </p>
        </div>
        <Button
          className="w-full sm:w-auto"
          onClick={() => setEditorOpen(true)}
        >
          New killswitch
        </Button>
      </header>

      <div className="grid gap-3 border p-4 sm:grid-cols-3">
        <label className="space-y-1 text-sm">
          Member
          <select
            aria-label="Filter by member"
            className="border-input bg-background block h-9 w-full border px-2"
            value={userId ?? ""}
            onChange={(event) => setFilter("user", event.target.value)}
          >
            <option value="">All members</option>
            {members.map((member) => (
              <option key={member.id} value={member.id}>
                {member.name}
              </option>
            ))}
          </select>
        </label>
        <label className="space-y-1 text-sm">
          Status
          <select
            aria-label="Filter by status"
            className="border-input bg-background block h-9 w-full border px-2"
            value={status ?? ""}
            onChange={(event) => setFilter("status", event.target.value)}
          >
            <option value="">All statuses</option>
            {statuses.map((item) => (
              <option key={item} value={item}>
                {capitalize(item)}
              </option>
            ))}
          </select>
        </label>
        <label className="space-y-1 text-sm">
          Capability
          <select
            aria-label="Filter by capability"
            className="border-input bg-background block h-9 w-full border px-2"
            value={capabilityKey ?? ""}
            onChange={(event) => setFilter("capability", event.target.value)}
          >
            <option value="">All capabilities</option>
            <option value="mcp_tool_calls">MCP tool calls</option>
          </select>
        </label>
      </div>

      {readError ? (
        <Alert variant="error">
          <AlertTitle>Unable to load Killswitches</AlertTitle>
          <AlertDescription>
            {readError.message}
            <div className="mt-2">
              <Button
                size="sm"
                variant="secondary"
                onClick={() =>
                  void Promise.all([
                    listQuery.refetch(),
                    membersQuery.refetch(),
                    serversQuery.refetch(),
                  ])
                }
              >
                Try again
              </Button>
            </div>
          </AlertDescription>
        </Alert>
      ) : listQuery.isLoading ||
        membersQuery.isLoading ||
        serversQuery.isLoading ? (
        <div className="border p-8 text-center text-sm text-muted-foreground">
          Loading Killswitches…
        </div>
      ) : items.length === 0 ? (
        <div className="border border-dashed p-10 text-center">
          <h2 className="font-medium">No Killswitches match these filters</h2>
          <p className="text-muted-foreground mt-1 text-sm">
            Create one or clear a filter to see organization-wide restrictions.
          </p>
        </div>
      ) : (
        <ul className="grid gap-3">
          {items.map((item) => (
            <KillswitchRow
              key={item.id}
              item={item}
              memberName={memberNames.get(item.userId)}
              serverNames={serverNames}
              href={routes.killswitch.detail.href(item.id)}
            />
          ))}
        </ul>
      )}

      {listQuery.hasNextPage && (
        <div className="text-center">
          <Button
            variant="secondary"
            disabled={listQuery.isFetchingNextPage}
            onClick={() => void listQuery.fetchNextPage()}
          >
            {listQuery.isFetchingNextPage ? "Loading…" : "Load more"}
          </Button>
        </div>
      )}

      <section className="border border-dashed p-5">
        <h2 className="font-medium">More capabilities coming soon</h2>
        <p className="text-muted-foreground mt-1 text-sm">
          Request another curated capability without creating a Killswitch or
          promising availability.
        </p>
        <Button
          className="mt-3"
          variant="secondary"
          size="sm"
          onClick={() => setRequestOpen(true)}
        >
          Request a capability
        </Button>
      </section>

      {editorOpen && (
        <Suspense
          fallback={
            <p role="status" className="text-muted-foreground text-sm">
              Loading editor…
            </p>
          }
        >
          <KillswitchEditorSheet
            open
            onOpenChange={setEditorOpen}
            mode="create"
            members={members}
            servers={servers}
            capabilities={capabilities}
            comingSoon={comingSoon}
            capabilitiesLoading={editorCatalogLoading}
            capabilitiesError={editorCatalogError}
            onRetryCapabilities={() => {
              void capabilitiesQuery.refetch();
            }}
            onPreview={preview}
            onSubmit={create}
            onView={(id) => routes.killswitch.detail.goTo(id)}
          />
        </Suspense>
      )}
      <FeatureRequestModal
        isOpen={requestOpen}
        onClose={() => setRequestOpen(false)}
        title="Request a capability"
        description="Tell us which curated capability would be useful."
        actionType="killswitch_capability"
        requestInput={{
          label: "Capability",
          placeholder: "What should a Killswitch be able to turn off?",
          telemetryField: "capability",
        }}
      />
    </main>
  );
}

function KillswitchRow({
  item,
  memberName,
  serverNames,
  href,
}: {
  item: KillswitchSummary;
  memberName?: string;
  serverNames: ReadonlyMap<string, string>;
  href: string;
}): JSX.Element {
  return (
    <li className="grid gap-4 border p-4 text-sm md:grid-cols-[minmax(10rem,1fr)_minmax(14rem,2fr)_auto_minmax(12rem,1fr)_auto] md:items-center">
      <div>
        <div className="text-muted-foreground text-xs md:hidden">Member</div>
        <Link className="font-medium hover:underline" to={href}>
          {memberName ?? "Deleted member"}
        </Link>
      </div>
      <div>
        <div>{item.capabilityLabel}</div>
        <div className="text-muted-foreground">
          {scopeLabel(item.scope, serverNames)}
        </div>
      </div>
      <Badge variant={item.status === "active" ? "success" : "neutral"}>
        {capitalize(item.status)}
      </Badge>
      <div>{scheduleLabel(item.schedule)}</div>
      <Button className="w-full md:w-auto" variant="secondary" asChild>
        <Link to={href}>View details</Link>
      </Button>
    </li>
  );
}
