import {
  defineFilters,
  useFilterState,
  type FilterValue,
  type OptionsById,
} from "@/components/filters";
import { Page } from "@/components/page-layout";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { type Column, Table } from "@/components/ui/Table";
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
import { mcpSessionsUserHref } from "@/components/killswitch/KillswitchUserStatus";
import {
  cleanKillswitchCreateRoute,
  MCP_TOOL_CALLS_CAPABILITY,
  openKillswitchCreateRoute,
  parseKillswitchCreateRoute,
  type KillswitchCreateContext,
} from "@/components/killswitch/killswitch-routing";
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

const KILLSWITCH_FILTERS = defineFilters([
  {
    id: "user",
    label: "Member",
    kind: "select",
    pinned: true,
    allLabel: "All members",
  },
  {
    id: "status",
    label: "Status",
    kind: "select",
    pinned: true,
    allLabel: "All statuses",
  },
  {
    id: "capability",
    label: "Capability",
    kind: "select",
    pinned: true,
    allLabel: "All capabilities",
  },
]);

const STATUS_FILTER_OPTIONS = statuses.map((status) => ({
  value: status,
  label: capitalize(status),
}));
const CAPABILITY_FILTER_OPTIONS = [
  { value: MCP_TOOL_CALLS_CAPABILITY, label: "MCP tool calls" },
];

export default function Killswitches(): JSX.Element {
  const session = useSession();
  const security = { sessionHeaderGramSession: session.session };
  const routes = useOrgRoutes();
  const queryClient = useQueryClient();
  const [params, setParams] = useSearchParams();
  const { values, setValue, clearValue, clearAll } =
    useFilterState(KILLSWITCH_FILTERS);
  const createRoute = parseKillswitchCreateRoute(params);
  const editorOpen = createRoute.open;
  const [requestOpen, setRequestOpen] = useState(false);
  const userId = values.user || undefined;
  const statusParam = values.status;
  const status = statuses.includes(statusParam as KillswitchListStatus)
    ? (statusParam as KillswitchListStatus)
    : undefined;
  const capabilityParam = values.capability;
  const capabilityKey =
    capabilityParam === MCP_TOOL_CALLS_CAPABILITY
      ? (capabilityParam as KillswitchCapabilityKey)
      : undefined;
  const toolbarValues = {
    ...values,
    status: status ?? null,
    capability: capabilityKey ?? null,
  };

  const listQuery = useKillswitchesInfinite(
    security,
    { userId, status, capabilityKey, limit: 25, gramSession: session.session },
    { initialPageParam: undefined, throwOnError: false },
  );
  const membersQuery = useMembers({ gramSession: session.session }, security, {
    throwOnError: false,
  });
  const capabilitiesQuery = useKillswitchCapabilities(
    security,
    { gramSession: session.session },
    {
      enabled: editorOpen,
      throwOnError: false,
    },
  );
  const serversQuery = useKillswitchMCPServers(
    security,
    { gramSession: session.session },
    { throwOnError: false },
  );
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
  const contextualUserId = createRoute.context?.userId;
  const contextualCapability = createRoute.context?.capabilityKey;
  const contextualServerId = createRoute.context?.originatingMcpServerId;
  const createContext: KillswitchCreateContext | undefined =
    editorOpen &&
    contextualUserId != null &&
    members.some((member) => member.id === contextualUserId)
      ? {
          userId: contextualUserId,
          capabilityKey:
            contextualCapability === MCP_TOOL_CALLS_CAPABILITY &&
            capabilities.some(
              (capability) => capability.key === contextualCapability,
            )
              ? MCP_TOOL_CALLS_CAPABILITY
              : undefined,
          originatingMcpServerId:
            contextualServerId != null &&
            servers.some((server) => server.id === contextualServerId)
              ? contextualServerId
              : undefined,
        }
      : undefined;
  const memberNames = useMemo(
    () => new Map(members.map((member) => [member.id, member.name])),
    [members],
  );
  const serverNames = useMemo(
    () => new Map(servers.map((server) => [server.id, server.name])),
    [servers],
  );
  const filterOptions: OptionsById = useMemo(
    () => ({
      user: members.map((member) => ({
        value: member.id,
        label: member.name,
      })),
      status: STATUS_FILTER_OPTIONS,
      capability: CAPABILITY_FILTER_OPTIONS,
    }),
    [members],
  );
  const columns: Column<KillswitchSummary>[] = [
    {
      key: "member",
      header: "Member",
      width: "1fr",
      render: (item) => (
        <Link
          className="font-medium hover:underline"
          to={routes.killswitch.detail.href(item.id)}
        >
          {memberNames.get(item.userId) ?? "Deleted member"}
        </Link>
      ),
    },
    {
      key: "capability",
      header: "Capability",
      width: "1.4fr",
      render: (item) => (
        <div>
          <div>{item.capabilityLabel}</div>
          <div className="text-muted-foreground">
            {scopeLabel(item.scope, serverNames)}
          </div>
        </div>
      ),
    },
    {
      key: "status",
      header: "Status",
      width: "140px",
      render: (item) => (
        <Badge variant={item.status === "active" ? "success" : "neutral"}>
          {capitalize(item.status)}
        </Badge>
      ),
    },
    {
      key: "schedule",
      header: "Schedule",
      width: "1.2fr",
      render: (item) => scheduleLabel(item.schedule),
    },
    {
      key: "actions",
      header: "",
      width: "auto",
      render: (item) => (
        <Button variant="secondary" size="sm" asChild>
          <Link to={routes.killswitch.detail.href(item.id)}>View details</Link>
        </Button>
      ),
    },
  ];
  const readError = listQuery.error ?? membersQuery.error ?? serversQuery.error;
  const editorCatalogError =
    capabilitiesQuery.error ?? membersQuery.error ?? serversQuery.error;
  const editorCatalogLoading =
    capabilitiesQuery.isLoading ||
    membersQuery.isLoading ||
    serversQuery.isLoading;
  const refetchList = listQuery.refetch;

  useEffect(() => {
    const delay = nextScheduleBoundaryDelay(items.map((item) => item.schedule));
    if (delay == null) return;
    const timer = window.setTimeout(() => void refetchList(), delay);
    return () => window.clearTimeout(timer);
  }, [items, refetchList]);

  const setEditorOpen = (open: boolean) => {
    if (open) {
      setParams((current) => openKillswitchCreateRoute(current));
    } else {
      setParams((current) => cleanKillswitchCreateRoute(current), {
        replace: true,
      });
    }
  };

  const retryEditorCatalog = () => {
    const retries: Promise<unknown>[] = [];
    if (capabilitiesQuery.error) retries.push(capabilitiesQuery.refetch());
    if (membersQuery.error) retries.push(membersQuery.refetch());
    if (serversQuery.error) retries.push(serversQuery.refetch());
    void Promise.allSettled(retries);
  };

  const preview = (draft: EditorDraft) =>
    previewMutation.mutateAsync({
      security,
      request: {
        killswitchPreviewOverlapsRequest: {
          userId: draft.userId,
          capabilityKey: MCP_TOOL_CALLS_CAPABILITY,
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
          capabilityKey: MCP_TOOL_CALLS_CAPABILITY,
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

      <Page.Toolbar>
        <Page.Toolbar.Filters
          schema={KILLSWITCH_FILTERS}
          values={toolbarValues}
          optionsById={filterOptions}
          onChange={setValue as (id: string, value: FilterValue) => void}
          onClear={clearValue as (id: string) => void}
          onClearAll={clearAll}
        />
      </Page.Toolbar>

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
      ) : (
        <div className="w-full overflow-x-auto">
          <Table
            columns={columns}
            data={items}
            rowKey={(item) => item.id}
            cellPadding="spacious"
            className="min-w-max"
            noResultsMessage={
              <div className="py-4 text-center">
                <h2 className="text-foreground font-medium">
                  No Killswitches match these filters
                </h2>
                <p className="mt-1">
                  Create one or clear a filter to see organization-wide
                  restrictions.
                </p>
              </div>
            }
          />
        </div>
      )}

      {!readError &&
        !listQuery.isLoading &&
        !membersQuery.isLoading &&
        !serversQuery.isLoading &&
        listQuery.hasNextPage && (
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
            createContext={createContext}
            members={members}
            servers={servers}
            capabilities={capabilities}
            comingSoon={comingSoon}
            capabilitiesLoading={editorCatalogLoading}
            capabilitiesError={editorCatalogError}
            onRetryCapabilities={retryEditorCatalog}
            onPreview={preview}
            onSubmit={create}
            onView={(id) => routes.killswitch.detail.goTo(id)}
            mcpSessionsHref={(userId) =>
              mcpSessionsUserHref(routes.mcpSessions.href(), userId)
            }
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
