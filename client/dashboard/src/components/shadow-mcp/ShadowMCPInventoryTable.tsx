import { ShadowAISetupEmptyState } from "@/components/setup-empty-state";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { Page } from "@/components/page-layout";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { useShadowMCPInventoryInfinite } from "@gram/client/react-query/shadowMCPInventory.js";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { type Column, type SortDescriptor, Table } from "@/components/ui/Table";
import { sortTableData } from "@/components/ui/Table/sorting";
import { useMemo, useState } from "react";
import { formatShortDate } from "@/components/access/shadow-mcp-utils";
import { TableRowContextMenu } from "@/components/table-row-context-menu";
import { cn } from "@/lib/utils";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import { useNavigate } from "react-router";
import {
  canMakeShadowMCPServerOfficial,
  SHADOW_MCP_OFFICIAL_STATE_LABEL,
  shadowMCPOfficialServerDescription,
  shadowMCPOfficialState,
  shadowMCPMakeOfficialHref,
} from "./shadowMCPOfficialServer";
import {
  type DecideAccessTarget,
  DecideAccessSheet,
  POLICIES_UNAVAILABLE_REASON,
} from "@/components/mcp-approvals/DecideAccessSheet";
import {
  ShadowMCPInventoryReviewCell,
  ShadowMCPInventoryServerCell,
  ShadowMCPInventoryUsageCell,
} from "./ShadowMCPInventoryCells";
import { ReviewRequestSheet } from "@/components/mcp-approvals/ReviewRequestSheet";
import {
  matchesReviewFilter,
  observedDate,
  reviewSortRank,
} from "./shadowMCPInventoryReview";
import {
  defineFilters,
  type FilterValue,
  useFilterState,
} from "@/components/filters";
import {
  shadowMCPBlockingPolicyDisposition,
  shadowMCPInventoryStatus,
  shadowMCPInventoryStatusBadgeVariant,
  shadowMCPInventoryStatusDescription,
  shadowMCPInventoryStatusLabel,
  type ShadowMCPPolicy,
} from "./shadowMCPInventoryStatus";

const REVIEW_FILTER_OPTIONS = [
  { value: "requested", label: "Awaiting decision" },
  { value: "approved", label: "Approved" },
  { value: "denied", label: "Denied" },
  { value: "superseded", label: "Superseded" },
  { value: "none", label: "No review" },
];

const INVENTORY_FILTERS = defineFilters([
  { id: "review", label: "Review", kind: "select" },
]);

const INVENTORY_PAGE_LIMIT = 50;

function InventoryStatusCell({ server }: { server: ShadowMCPInventoryServer }) {
  const status = shadowMCPInventoryStatus(server);

  // An onboarded server has left the triage queue: the organization serves
  // it now, which says more than the access verdict on the direct URL. A
  // pending request still wins, because someone is waiting on an answer.
  const officialState = shadowMCPOfficialState(server, status === "pending");
  if (officialState) {
    return (
      <SimpleTooltip tooltip={shadowMCPOfficialServerDescription(server)}>
        <span tabIndex={0} className="inline-flex cursor-help">
          <Badge variant="success">
            <Badge.Text>
              {SHADOW_MCP_OFFICIAL_STATE_LABEL[officialState]}
            </Badge.Text>
          </Badge>
        </span>
      </SimpleTooltip>
    );
  }

  // The badge is the verdict; the description is the mechanism behind it,
  // which a one-word badge cannot carry. "Restricted" covers five different
  // postures — selected users only, mixed, blocked for some, and two ways a
  // denial still leaves standing access — and "Observed" can hide a decision
  // lying dormant for want of a blocking policy, which is the state that
  // should worry an admin most. Triaging those apart is the column's job, so
  // the detail stays on the row rather than only on the server page. It sits
  // in a tooltip to keep the column scannable, on a focusable span so it is
  // reachable by keyboard and announced through aria-describedby rather than
  // being hover-only.
  return (
    <SimpleTooltip tooltip={shadowMCPInventoryStatusDescription(server)}>
      <span tabIndex={0} className="inline-flex cursor-help">
        <Badge variant={shadowMCPInventoryStatusBadgeVariant(status)}>
          <Badge.Text>{shadowMCPInventoryStatusLabel(status)}</Badge.Text>
        </Badge>
      </span>
    </SimpleTooltip>
  );
}

function InventoryEmptyState() {
  return (
    <ShadowAISetupEmptyState
      heading="No MCP servers observed yet"
      description="Shadow AI reveals MCP servers observed in agent traffic or raised in an access request. Install and run the device agent to discover servers on your organization's devices."
    />
  );
}

export function ShadowMCPInventoryTable({
  className,
  enabled = true,
  members,
  onOpenServer,
  policiesUnavailable = false,
  projectID,
  roles,
  shadowMCPPolicies,
}: {
  className?: string;
  enabled?: boolean;
  members: AccessMember[];
  onOpenServer?: (server: ShadowMCPInventoryServer) => void;
  /** The policy list failed to load: the inventory still shows, deciding waits. */
  policiesUnavailable?: boolean;
  projectID: string;
  roles: Role[];
  shadowMCPPolicies: ShadowMCPPolicy[];
}): JSX.Element {
  // React Query owns the pages: each "Load more" appends the next cursor's
  // page, and a decision elsewhere invalidates and refetches every loaded
  // page, so the list never mixes stale and fresh rows.
  const queryEnabled = enabled && projectID.length > 0;
  const inventoryQuery = useShadowMCPInventoryInfinite(
    { projectId: projectID, limit: INVENTORY_PAGE_LIMIT },
    undefined,
    {
      enabled: queryEnabled,
      // The dashboard default throws every non-auth failure to the error
      // boundary; the table has its own failure state and a retry.
      throwOnError: false,
    },
  );
  // The row action and the stdio review's button wait while the policy list
  // is unavailable, the way the server page's button does.
  const decideDisabledReason = policiesUnavailable
    ? POLICIES_UNAVAILABLE_REASON
    : undefined;
  const [search, setSearch] = useState("");
  // Unsorted by default, the rows keep the server's order: open requests
  // first, then the most recently called. That order is the one the cursor
  // pages through, so each "Load more" appends below the rows already shown
  // instead of scattering them through a client-side sort of a partial list.
  const [sort, setSort] = useState<SortDescriptor | null>(null);
  const [decideTarget, setDecideTarget] = useState<DecideAccessTarget | null>(
    null,
  );
  const [reviewSheetServer, setReviewSheetServer] =
    useState<ShadowMCPInventoryServer | null>(null);
  // The sheet words its form and picks its write path from the policy set's
  // disposition; row rendering no longer touches it.
  const disposition = shadowMCPBlockingPolicyDisposition(shadowMCPPolicies);
  const { values, setValue, clearValue, clearAll } =
    useFilterState(INVENTORY_FILTERS);
  const routes = useRoutes();
  const navigate = useNavigate();

  // A row can move to a later page between fetches when its usage changes;
  // keep its first appearance so the table never renders a duplicate key.
  const servers = useMemo(() => {
    // A disabled query keeps its cached pages; a disabled table shows none.
    if (!queryEnabled) return [];
    const seen = new Set<string>();
    const rows: ShadowMCPInventoryServer[] = [];
    for (const page of inventoryQuery.data?.pages ?? []) {
      for (const server of page.result.servers) {
        if (seen.has(server.canonicalServerUrl)) continue;
        seen.add(server.canonicalServerUrl);
        rows.push(server);
      }
    }
    return rows;
  }, [inventoryQuery.data, queryEnabled]);

  const loadMoreServers = async () => {
    if (!inventoryQuery.hasNextPage || inventoryQuery.isFetchingNextPage) {
      return;
    }
    await inventoryQuery.fetchNextPage();
  };

  const isStdio = (server: ShadowMCPInventoryServer) =>
    server.targetKind === "stdio_command";

  const openRow = (server: ShadowMCPInventoryServer) => {
    // Local commands have no server page; their review lives in a sheet.
    if (isStdio(server)) {
      setReviewSheetServer(server);
      return;
    }
    onOpenServer?.(server);
  };

  const openDecide = (server: ShadowMCPInventoryServer) => {
    if (isStdio(server)) {
      setReviewSheetServer(server);
      return;
    }
    setDecideTarget({
      canonicalServerUrl: server.canonicalServerUrl,
      displayName:
        server.serverName || server.urlHost || server.canonicalServerUrl,
      approvalRequestId: server.approvalRequest?.id,
      // A pending legacy bypass request rides along so the sheet promotes it
      // into the review and the decision drains it too.
      pendingBypassRequestId: server.latestRequest?.id,
    });
  };

  // One action per row leads to the official server: open it when it
  // exists, start making it when it can exist. A local command has no URL
  // to serve, so it gets neither.
  const officialServerActions = (server: ShadowMCPInventoryServer) => {
    const official = server.officialServer;
    if (official) {
      return [
        {
          label: "Open official server",
          onClick: () =>
            void navigate(
              routes.mcp.details.overview.href(
                mcpServerRouteParam({
                  id: official.mcpServerId,
                  slug: official.slug,
                }),
              ),
            ),
        },
      ];
    }
    if (!canMakeShadowMCPServerOfficial(server)) return [];
    return [
      {
        label: "Make official",
        onClick: () =>
          void navigate(
            shadowMCPMakeOfficialHref(
              routes.shadowAI.mcps.detail.href(server.serverSlug),
            ),
          ),
      },
    ];
  };

  const columns: Column<ShadowMCPInventoryServer>[] = [
    {
      key: "server",
      header: "Server",
      sortable: true,
      sortValue: (server) =>
        (server.serverName || server.urlHost || server.canonicalServerUrl)
          .trim()
          .toLowerCase(),
      width: "2fr",
      render: (server) => <ShadowMCPInventoryServerCell server={server} />,
    },
    {
      key: "status",
      header: "Status",
      sortable: true,
      sortValue: (server) => {
        const status = shadowMCPInventoryStatus(server);
        const officialState = shadowMCPOfficialState(
          server,
          status === "pending",
        );
        return officialState
          ? SHADOW_MCP_OFFICIAL_STATE_LABEL[officialState]
          : shadowMCPInventoryStatusLabel(status);
      },
      width: "0.9fr",
      render: (server) =>
        server.targetKind === "stdio_command" ? (
          <Text muted small>
            —
          </Text>
        ) : (
          <InventoryStatusCell server={server} />
        ),
    },
    {
      key: "review",
      header: "Review",
      sortable: true,
      sortValue: reviewSortRank,
      width: "0.8fr",
      render: (server) => <ShadowMCPInventoryReviewCell server={server} />,
    },
    {
      key: "lastCalled",
      header: "Last called",
      sortable: true,
      sortValue: (server) => server.lastCalled?.getTime() ?? 0,
      width: "0.7fr",
      render: (server) => (
        <Text variant="small">{formatShortDate(server.lastCalled)}</Text>
      ),
    },
    {
      key: "lastSeen",
      header: "Last seen",
      sortable: true,
      sortValue: (server) => observedDate(server.lastSeen)?.getTime() ?? 0,
      width: "0.7fr",
      render: (server) => (
        <Text variant="small">
          {formatShortDate(observedDate(server.lastSeen))}
        </Text>
      ),
    },
    {
      key: "usage",
      header: "Usage",
      sortable: true,
      sortValue: (server) => server.observedUseCount,
      width: "0.5fr",
      render: (server) => <ShadowMCPInventoryUsageCell server={server} />,
    },
  ];

  const normalizedSearch = search.trim().toLowerCase();
  const reviewFilter = values.review ?? undefined;
  const filteredServers = useMemo(() => {
    return servers.filter((server) => {
      if (!matchesReviewFilter(server, reviewFilter)) return false;
      if (normalizedSearch.length === 0) return true;
      return [
        server.serverName,
        server.urlHost,
        server.canonicalServerUrl,
      ].some((value) => value?.toLowerCase().includes(normalizedSearch));
    });
  }, [normalizedSearch, reviewFilter, servers]);
  const sortedServers = sortTableData(
    filteredServers,
    columns,
    sort,
  ) as ShadowMCPInventoryServer[];
  const reviewFilterLabel = REVIEW_FILTER_OPTIONS.find(
    (option) => option.value === reviewFilter,
  )?.label;
  const noResultsMessage =
    normalizedSearch.length > 0
      ? `No servers matching “${search.trim()}”`
      : reviewFilterLabel
        ? `No servers with review state “${reviewFilterLabel}”`
        : undefined;

  if (inventoryQuery.isLoading) {
    return <SkeletonTable />;
  }

  if (inventoryQuery.isError && servers.length === 0) {
    return (
      <div className="bg-background flex min-h-32 flex-col items-center justify-center gap-3 px-4 py-8 text-center">
        <div className="space-y-1">
          <Text variant="body" className="font-medium">
            Shadow MCP inventory could not be loaded
          </Text>
          <Text muted small className="max-w-md">
            It may be a temporary problem. Try again.
          </Text>
        </div>
        <Button
          variant="secondary"
          size="sm"
          onClick={() => void inventoryQuery.refetch()}
        >
          <Button.Text>Retry</Button.Text>
        </Button>
      </div>
    );
  }

  if (servers.length === 0) {
    return <InventoryEmptyState />;
  }

  return (
    <div className={cn("flex flex-col gap-4", className)}>
      <ReviewRequestSheet
        request={
          reviewSheetServer?.approvalRequest
            ? {
                id: reviewSheetServer.approvalRequest.id,
                targetRaw: reviewSheetServer.canonicalServerUrl,
                requesterCount:
                  reviewSheetServer.approvalRequest.requesterCount,
              }
            : null
        }
        open={reviewSheetServer !== null}
        onOpenChange={(open) => {
          if (!open) {
            setReviewSheetServer(null);
          }
        }}
        onDecide={(request) => {
          setDecideTarget({
            targetKind: "stdio_command",
            canonicalServerUrl: request.targetRaw,
            displayName: request.targetRaw,
            approvalRequestId: request.id,
          });
        }}
        decideDisabledReason={decideDisabledReason}
      />
      <DecideAccessSheet
        target={decideTarget}
        open={decideTarget !== null}
        onOpenChange={(open) => {
          if (!open) {
            setDecideTarget(null);
          }
        }}
        disposition={disposition}
        members={members}
        roles={roles}
        onDecided={() => {
          // The sheet invalidates the inventory, which refetches every
          // loaded page. A decision handed off from the stdio review sheet resolves that
          // review; leaving the sheet open underneath would return the admin
          // to a stale pending view of a request that was just decided.
          setReviewSheetServer(null);
        }}
      />
      <Page.Toolbar>
        <Page.Toolbar.Search
          onChange={setSearch}
          placeholder="Search loaded servers..."
          value={search}
        />
        <Page.Toolbar.Filters
          schema={INVENTORY_FILTERS}
          values={values}
          optionsById={{ review: REVIEW_FILTER_OPTIONS }}
          onChange={setValue as (id: string, value: FilterValue) => void}
          onClear={clearValue as (id: string) => void}
          onClearAll={clearAll}
        />
      </Page.Toolbar>
      {/* The table grows with its rows and the page scrolls, as on the
        other dashboard list pages. A scrolling table body would carry the
        Load more row, which sits at the body's bottom edge, up over the
        rows as the body scrolls. */}
      <Table columns={columns} className="overflow-x-auto">
        <Table.Header columns={columns} sort={sort} onSortChange={setSort} />
        <Table.Body
          columns={columns}
          data={sortedServers}
          handleLoadMore={loadMoreServers}
          hasMore={Boolean(inventoryQuery.hasNextPage)}
          isLoading={inventoryQuery.isFetchingNextPage}
          noResultsMessage={noResultsMessage}
          onRowClick={openRow}
          rowKey={(row) => row.canonicalServerUrl}
          renderRow={(row, rowElement) => (
            <TableRowContextMenu
              key={row.canonicalServerUrl}
              actions={[
                {
                  label: "Decide access",
                  onClick: () => openDecide(row),
                  disabled: policiesUnavailable,
                  description: decideDisabledReason,
                },
                ...officialServerActions(row),
              ]}
            >
              {rowElement}
            </TableRowContextMenu>
          )}
        />
      </Table>
      {inventoryQuery.isFetchNextPageError ? (
        <div className="flex items-center justify-between border p-3">
          <Text destructive small>
            More servers could not be loaded.
          </Text>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => void inventoryQuery.fetchNextPage()}
          >
            <Button.Text>Retry</Button.Text>
          </Button>
        </div>
      ) : null}
    </div>
  );
}
