import { lazy, Suspense, useEffect, useMemo } from "react";
import { Link, useSearchParams } from "react-router";

import { FeatureRequestModal } from "@/components/FeatureRequestModal";
import {
  cleanKillswitchCreateRoute,
  closeKillswitchRecordRoute,
  MCP_TOOL_CALLS_CAPABILITY,
  openKillswitchCreateRoute,
  openKillswitchRecordRoute,
  parseKillswitchCreateRoute,
  selectedKillswitchId,
  type KillswitchCreateContext,
} from "@/components/killswitch/killswitch-routing";
import { mcpSessionsUserHref } from "@/components/killswitch/KillswitchUserStatus";
import {
  draftToSchedule,
  draftToScope,
  nextScheduleBoundaryDelay,
  scheduleLabel,
  scopeLabel,
  type EditorDraft,
} from "@/components/killswitch/killswitch-view-model";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { Button } from "@/components/ui/Button";
import { useSession } from "@/contexts/Auth";
import { useKillswitchAccess } from "@/hooks/useKillswitchAccess";
import { capitalize } from "@/lib/utils";
import { useOrgRoutes } from "@/routes";
import type { IdentityModel } from "@gram/client/models/components/identitymodel.js";
import type { KillswitchSummary } from "@gram/client/models/components/killswitchsummary.js";
import { useCreateKillswitchMutation } from "@gram/client/react-query/createKillswitch.js";
import { useKillswitchCapabilities } from "@gram/client/react-query/killswitchCapabilities.js";
import { useKillswitchMCPServers } from "@gram/client/react-query/killswitchMCPServers.js";
import {
  invalidateAllKillswitches,
  useKillswitchesInfinite,
} from "@gram/client/react-query/killswitches.js";
import { usePreviewKillswitchOverlapsMutation } from "@gram/client/react-query/previewKillswitchOverlaps.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import {
  IdentityPanel,
  IdentityPanelEmpty,
  IdentityPanelRow,
} from "./IdentityPanel";
import { retryFailed, useIdentityMember } from "./useIdentityQueries";

const EMPTY: never[] = [];
/** One page of rows; the rest are a page at a time behind "Load more". */
const KILLSWITCH_PAGE_SIZE = 25;

const KillswitchEditorSheet = lazy(() =>
  import("@/components/killswitch/KillswitchEditorSheet").then((module) => ({
    default: module.KillswitchEditorSheet,
  })),
);
const KillswitchRecord = lazy(() =>
  import("@/components/killswitch/KillswitchRecord").then((module) => ({
    default: module.KillswitchRecord,
  })),
);

/** Where a row sits in the reader's attention: in force, coming, or done. */
function statusAccent(
  status: KillswitchSummary["status"],
): "destructive" | "warning" | undefined {
  switch (status) {
    case "active":
      return "destructive";
    case "scheduled":
      return "warning";
    case "expired":
    case "lifted":
      return undefined;
  }
}

/**
 * This person's killswitches, and the controls that create them.
 *
 * Killswitches restrict one named person, so they are managed from that
 * person's page rather than from a roster of restrictions with a member filter
 * on it: the reader deciding whether to turn a capability off is already
 * looking at what that capability has been used for.
 */
export function IdentityKillswitches({
  identity,
}: {
  identity: IdentityModel;
}): JSX.Element | null {
  // Fail-closed, and the same gate the management API enforces. A reader who
  // cannot use killswitches is shown no panel at all rather than an empty one,
  // which would read as "this person has none".
  const access = useKillswitchAccess();
  if (!access.canAccess) return null;
  return <IdentityKillswitchesPanel identity={identity} />;
}

function IdentityKillswitchesPanel({
  identity,
}: {
  identity: IdentityModel;
}): JSX.Element {
  const session = useSession();
  const security = { sessionHeaderGramSession: session.session };
  const orgRoutes = useOrgRoutes();
  const queryClient = useQueryClient();
  const [params, setParams] = useSearchParams();
  const [requestOpen, setRequestOpen] = useState(false);

  // Killswitches key on the Gram user id. An identity with no member row —
  // an address an agent reported, an api-key subject — names nobody the
  // capability service can restrict, so there is nothing to list or create.
  const { member, query: membersQuery } = useIdentityMember(identity);
  const userId = member?.id ?? identity.userIds[0];

  const createRoute = parseKillswitchCreateRoute(params);
  const editorOpen = createRoute.open && !!userId;
  // Which record is open lives in the address, so the row that opened it can
  // be linked to, and so the retired killswitch URLs resolve onto it.
  const recordId = selectedKillswitchId(params);

  const listQuery = useKillswitchesInfinite(
    security,
    { userId, limit: KILLSWITCH_PAGE_SIZE, gramSession: session.session },
    {
      initialPageParam: undefined,
      enabled: !!userId,
      throwOnError: false,
    },
  );
  // Server names label every row's scope, so this is read with the list rather
  // than only when the editor opens.
  const serversQuery = useKillswitchMCPServers(
    security,
    { gramSession: session.session },
    { throwOnError: false },
  );
  const capabilitiesQuery = useKillswitchCapabilities(
    security,
    { gramSession: session.session },
    { enabled: editorOpen, throwOnError: false },
  );
  const createMutation = useCreateKillswitchMutation();
  const previewMutation = usePreviewKillswitchOverlapsMutation();

  const items: KillswitchSummary[] = useMemo(
    () => listQuery.data?.pages.flatMap((page) => page.result.items) ?? EMPTY,
    [listQuery.data],
  );
  const members = membersQuery.data?.members ?? EMPTY;
  const servers = serversQuery.data?.servers ?? EMPTY;
  const capabilities = capabilitiesQuery.data?.capabilities ?? EMPTY;
  const comingSoon = capabilitiesQuery.data?.comingSoon ?? EMPTY;
  const serverNames = useMemo(
    () => new Map(servers.map((server) => [server.id, server.name])),
    [servers],
  );

  // A row's status is a statement about now, so a list left open across the
  // moment a schedule starts or ends would keep asserting the previous one.
  const refetchList = listQuery.refetch;
  useEffect(() => {
    const delay = nextScheduleBoundaryDelay(items.map((item) => item.schedule));
    if (delay == null) return;
    const timer = window.setTimeout(() => void refetchList(), delay);
    return () => window.clearTimeout(timer);
  }, [items, refetchList]);

  const createContext: KillswitchCreateContext | undefined =
    editorOpen && userId
      ? {
          userId,
          // What the sender was looking at, kept only where the catalogue
          // confirms it — a stale link would otherwise preselect a capability
          // or a server that no longer exists.
          capabilityKey:
            createRoute.params.capabilityKey === MCP_TOOL_CALLS_CAPABILITY &&
            capabilities.some(
              (capability) =>
                capability.key === createRoute.params.capabilityKey,
            )
              ? MCP_TOOL_CALLS_CAPABILITY
              : undefined,
          originatingMcpServerId: servers.some(
            (server) => server.id === createRoute.params.originatingMcpServerId,
          )
            ? createRoute.params.originatingMcpServerId
            : undefined,
        }
      : undefined;

  const openRecord = (id: string) =>
    setParams((current) => openKillswitchRecordRoute(current, id));
  const closeRecord = () =>
    setParams((current) => closeKillswitchRecordRoute(current), {
      replace: true,
    });

  const setEditorOpen = (open: boolean) => {
    if (open) {
      setParams((current) => openKillswitchCreateRoute(current));
    } else {
      setParams((current) => cleanKillswitchCreateRoute(current), {
        replace: true,
      });
    }
  };

  const editorCatalogError =
    capabilitiesQuery.error ?? membersQuery.error ?? serversQuery.error;
  const editorCatalogLoading =
    capabilitiesQuery.isLoading ||
    membersQuery.isLoading ||
    serversQuery.isLoading;
  const retryEditorCatalog = retryFailed(
    capabilitiesQuery,
    membersQuery,
    serversQuery,
  );

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

  const listFailed = listQuery.isError || serversQuery.isError;
  const activeCount = items.filter((item) => item.status === "active").length;
  const scheduledCount = items.filter(
    (item) => item.status === "scheduled",
  ).length;
  // The list is cursor-paged and the API reports no total, so these counts are
  // only a count of this person's killswitches once every page is in hand.
  // While a cursor remains they count the rows on screen, and say so — a bare
  // "1 in force" under a truncated list is a claim about the person that the
  // data does not support.
  const countLine = `${activeCount.toLocaleString()} in force · ${scheduledCount.toLocaleString()} scheduled`;
  const loadedLine = listQuery.hasNextPage
    ? `${countLine} of the ${items.length.toLocaleString()} loaded so far`
    : countLine;
  // The explanatory line describes the control this panel offers. With no
  // member row that control is absent and the body says no capability can be
  // turned off for this person, so the offer is dropped rather than left to
  // contradict it.
  let footer: string | undefined;
  if (items.length > 0) footer = loadedLine;
  else if (userId) {
    footer =
      "Turns off one capability for this person, across an explicit MCP server scope and time.";
  }

  return (
    <>
      <IdentityPanel
        title={
          <span className="flex items-center gap-2">
            Killswitches
            <ReleaseStageBadge stage="beta" />
          </span>
        }
        action={
          userId && (
            <Button size="sm" onClick={() => setEditorOpen(true)}>
              New killswitch
            </Button>
          )
        }
        loading={listQuery.isLoading || serversQuery.isLoading}
        error={listFailed && items.length === 0}
        refreshFailed={listFailed && items.length > 0}
        onRetry={retryFailed(listQuery, serversQuery)}
        footer={footer}
      >
        {!userId ? (
          <IdentityPanelEmpty>
            No org member row resolves to this identity, so no capability can be
            turned off for them.
          </IdentityPanelEmpty>
        ) : items.length === 0 ? (
          <IdentityPanelEmpty>
            No killswitches for this person.
          </IdentityPanelEmpty>
        ) : (
          items.map((item) => (
            <Link
              key={item.id}
              to={{
                search: openKillswitchRecordRoute(params, item.id).toString(),
              }}
              className="hover:bg-muted/40 block transition-colors"
              aria-current={item.id === recordId ? "true" : undefined}
            >
              <IdentityPanelRow
                accent={statusAccent(item.status)}
                title={item.capabilityLabel}
                detail={`${scopeLabel(item.scope, serverNames)} · ${scheduleLabel(
                  item.schedule,
                )}`}
                trailing={capitalize(item.status)}
              />
            </Link>
          ))
        )}
        {listQuery.hasNextPage && (
          <div className="border-border border-b px-4 py-3 text-center">
            <Button
              variant="secondary"
              size="sm"
              disabled={listQuery.isFetchingNextPage}
              onClick={() => void listQuery.fetchNextPage()}
            >
              {listQuery.isFetchingNextPage ? "Loading…" : "Load more"}
            </Button>
          </div>
        )}
        <div className="border-border flex flex-wrap items-center justify-between gap-2 border-b px-4 py-3">
          <span className="text-muted-foreground text-xs">
            More capabilities coming soon.
          </span>
          <Button
            variant="tertiary"
            size="sm"
            onClick={() => setRequestOpen(true)}
          >
            Request a capability
          </Button>
        </div>
      </IdentityPanel>

      {recordId && userId && (
        <div className="md:col-span-2">
          <Suspense
            fallback={
              <p role="status" className="text-muted-foreground text-sm">
                Loading killswitch…
              </p>
            }
          >
            <KillswitchRecord
              key={recordId}
              killswitchId={recordId}
              subjectUserId={userId}
              onSelectKillswitch={openRecord}
              onClose={closeRecord}
            />
          </Suspense>
        </div>
      )}

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
            onView={openRecord}
            mcpSessionsHref={(id) =>
              mcpSessionsUserHref(orgRoutes.mcpSessions.href(), id)
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
          placeholder: "What should a killswitch be able to turn off?",
          telemetryField: "capability",
        }}
      />
    </>
  );
}
