import { SkeletonTable } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { useDeleteShadowMCPInventoryPolicyBypassMutation } from "@gram/client/react-query/deleteShadowMCPInventoryPolicyBypass.js";
import { useResolveShadowMCPInventoryRequestMutation } from "@gram/client/react-query/resolveShadowMCPInventoryRequest.js";
import {
  invalidateAllShadowMCPInventory,
  useShadowMCPInventory,
} from "@gram/client/react-query/shadowMCPInventory.js";
import { useUpsertShadowMCPInventoryPolicyBypassMutation } from "@gram/client/react-query/upsertShadowMCPInventoryPolicyBypass.js";
import {
  Badge,
  Button,
  type Column,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Icon,
  type SortDescriptor,
  Table,
  sortTableData,
} from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { formatShortDate } from "@/components/access/shadow-mcp-utils";
import { Checkbox } from "@/components/ui/checkbox";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { cn } from "@/lib/utils";
import {
  shadowMCPInventoryStatus,
  shadowMCPInventoryStatusBadgeVariant,
  shadowMCPInventoryStatusDescription,
  shadowMCPInventoryStatusLabel,
  type ShadowMCPPolicyState,
} from "./shadowMCPInventoryStatus";

const INVENTORY_PAGE_LIMIT = 50;
const FIRST_PAGE_CURSOR = "";

type ShadowMCPPolicy = Pick<
  RiskPolicy,
  "audienceType" | "audiencePrincipalUrns" | "id" | "name"
>;

type InventoryPage = {
  cursor: string;
  nextCursor?: string;
  servers: ShadowMCPInventoryServer[];
};

const EMPTY_INVENTORY_PAGES: InventoryPage[] = [];
type InventoryActionMode = "review" | "add" | "edit" | "delete";
type ReviewDecision = "approve" | "deny";
type ActiveInventoryAction = {
  mode: InventoryActionMode;
  server: ShadowMCPInventoryServer;
};

function usageCountLabel(count: number) {
  return `${count} ${count === 1 ? "call" : "calls"}`;
}

function userCountLabel(count: number) {
  return `${count} ${count === 1 ? "user" : "users"}`;
}

function InventoryServerCell({ server }: { server: ShadowMCPInventoryServer }) {
  return (
    <div className="min-w-0 space-y-1">
      <div className="flex gap-2 items-center">
        <Type variant="small" className="truncate font-medium">
          {server.serverName || server.urlHost}
        </Type>
        {server.requestCount > 0 && (
          <Badge variant="warning" size="sm" background={false}>
            <Badge.LeftIcon>
              <Icon name="shield-alert" />
            </Badge.LeftIcon>
            <Badge.Text>
              {server.requestCount} Access Request
              {server.requestCount > 1 && "s"}
            </Badge.Text>
          </Badge>
        )}
      </div>
      <Type variant="small" className="text-muted-foreground truncate text-xs">
        {server.canonicalServerUrl}
      </Type>
    </div>
  );
}

function InventoryStatusCell({
  policyState,
  server,
}: {
  policyState: ShadowMCPPolicyState;
  server: ShadowMCPInventoryServer;
}) {
  const status = shadowMCPInventoryStatus(server, policyState);

  return (
    <div className="space-y-1">
      <Badge variant={shadowMCPInventoryStatusBadgeVariant(status)}>
        <Badge.Text>{shadowMCPInventoryStatusLabel(status)}</Badge.Text>
      </Badge>
      <Type variant="small" className="text-muted-foreground text-xs">
        {shadowMCPInventoryStatusDescription(server, policyState)}
      </Type>
    </div>
  );
}

function UsageCell({ server }: { server: ShadowMCPInventoryServer }) {
  return (
    <div className="space-y-1">
      <Type variant="small">{usageCountLabel(server.observedUseCount)}</Type>
      <Type variant="small" className="text-muted-foreground text-xs">
        {userCountLabel(server.userCount)}
      </Type>
    </div>
  );
}

function InventoryEmptyState() {
  return (
    <div className="bg-muted/20 flex flex-col items-center justify-center rounded-xl border border-dashed px-8 py-16 text-center">
      <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
        <Icon name="shield-check" className="text-muted-foreground h-6 w-6" />
      </div>
      <Type variant="subheading" className="mb-1">
        No Shadow MCP servers
      </Type>
      <Type small muted className="mb-4 max-w-md">
        Inventory URLs will appear here after hook startup captures configured
        Shadow MCP servers.
      </Type>
    </div>
  );
}

function humanizePrincipalURN(principalURN: string) {
  if (principalURN === "user:all") {
    return "Everyone";
  }

  const segments = principalURN.split(":").filter(Boolean);
  const label = segments[segments.length - 1] ?? principalURN;
  return label
    .replace(/[_-]+/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase());
}

function policyAudienceLabel(policy: ShadowMCPPolicy) {
  if (policy.audienceType === "everyone") {
    return "Everyone";
  }

  const principalLabels =
    policy.audiencePrincipalUrns.map(humanizePrincipalURN);
  if (principalLabels.length <= 2) {
    return principalLabels.join(", ");
  }

  return `${principalLabels.slice(0, 2).join(", ")} + ${principalLabels.length - 2} more`;
}

function actionSheetTitle(mode: InventoryActionMode) {
  switch (mode) {
    case "review":
      return "Review Request";
    case "add":
      return "Add Allow Rule";
    case "edit":
      return "Edit Rule";
    case "delete":
      return "Delete Rule";
  }
}

function actionSheetDescription(mode: InventoryActionMode) {
  switch (mode) {
    case "review":
      return "Resolve the pending Shadow MCP request for this server.";
    case "add":
      return "Allow this Shadow MCP server for selected policies.";
    case "edit":
      return "Change which policies allow this Shadow MCP server.";
    case "delete":
      return "Remove the allow decision for this Shadow MCP server.";
  }
}

function actionSheetSubmitLabel(
  mode: InventoryActionMode,
  decision: ReviewDecision,
) {
  if (mode === "review") {
    return decision === "approve" ? "Approve Request" : "Deny Request";
  }
  if (mode === "delete") {
    return "Delete Rule";
  }
  if (mode === "edit") {
    return "Save Changes";
  }
  return "Add Allow Rule";
}

function initialPolicyIDsForAction(
  action: ActiveInventoryAction,
  shadowMCPPolicies: ShadowMCPPolicy[],
) {
  const shadowMCPPolicyIDs = shadowMCPPolicies.map((policy) => policy.id);
  if (action.server.allowedPolicyIds.length > 0) {
    return action.server.allowedPolicyIds.filter((policyID) =>
      shadowMCPPolicyIDs.includes(policyID),
    );
  }
  if (
    action.mode === "review" &&
    action.server.latestRequest &&
    shadowMCPPolicyIDs.includes(action.server.latestRequest.policyId)
  ) {
    return [action.server.latestRequest.policyId];
  }
  return shadowMCPPolicyIDs;
}

function InventoryActionMenu({
  disabled,
  onOpenAction,
  server,
}: {
  disabled: boolean;
  onOpenAction: (
    mode: InventoryActionMode,
    server: ShadowMCPInventoryServer,
  ) => void;
  server: ShadowMCPInventoryServer;
}) {
  const hasRequest = server.requestCount > 0;
  const hasAllowDecision = server.access === "allowed";

  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger asChild>
        <Button
          aria-label={`Open actions for ${server.serverName || server.urlHost}`}
          disabled={disabled}
          size="xs"
          variant="tertiary"
        >
          <Icon name="ellipsis" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {hasRequest && (
          <DropdownMenuItem
            onSelect={() => {
              window.setTimeout(() => onOpenAction("review", server), 0);
            }}
          >
            Review Request
          </DropdownMenuItem>
        )}
        {!hasRequest && !hasAllowDecision && (
          <DropdownMenuItem
            onSelect={() => {
              window.setTimeout(() => onOpenAction("add", server), 0);
            }}
          >
            Add Allow Rule
          </DropdownMenuItem>
        )}
        {hasAllowDecision && (
          <>
            <DropdownMenuItem
              onSelect={() => {
                window.setTimeout(() => onOpenAction("edit", server), 0);
              }}
            >
              Edit Rule
            </DropdownMenuItem>
            <DropdownMenuItem
              onSelect={() => {
                window.setTimeout(() => onOpenAction("delete", server), 0);
              }}
            >
              Delete Rule
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function PolicySelection({
  disabled,
  onSelectionChange,
  policies,
  selectedPolicyIDs,
}: {
  disabled: boolean;
  onSelectionChange: (policyIDs: string[]) => void;
  policies: ShadowMCPPolicy[];
  selectedPolicyIDs: string[];
}) {
  const selectedPolicyIDSet = new Set(selectedPolicyIDs);

  return (
    <section className="border-border space-y-3 rounded-md border p-3">
      <Type variant="small" className="font-medium">
        Policies
      </Type>
      <div className="space-y-2">
        {policies.map((policy) => {
          const checked = selectedPolicyIDSet.has(policy.id);
          return (
            <label
              key={policy.id}
              className="hover:bg-muted/40 flex cursor-pointer items-start gap-3 rounded-sm px-3 py-2.5 transition-colors"
            >
              <Checkbox
                checked={checked}
                disabled={disabled}
                onCheckedChange={(nextChecked) => {
                  if (nextChecked) {
                    onSelectionChange([...selectedPolicyIDs, policy.id]);
                    return;
                  }
                  onSelectionChange(
                    selectedPolicyIDs.filter(
                      (policyID) => policyID !== policy.id,
                    ),
                  );
                }}
              />
              <span className="min-w-0 flex-1">
                <Type variant="small" className="truncate font-medium">
                  {policy.name}
                </Type>
                <Type muted small>
                  Policy applies to {policyAudienceLabel(policy)}
                </Type>
              </span>
            </label>
          );
        })}
      </div>
    </section>
  );
}

function ShadowMCPInventoryActionSheet({
  action,
  shadowMCPPolicies,
  isSubmitting,
  onOpenChange,
  onSubmit,
  open,
}: {
  action: ActiveInventoryAction | null;
  shadowMCPPolicies: ShadowMCPPolicy[];
  isSubmitting: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (input: {
    action: ActiveInventoryAction;
    decision: ReviewDecision;
    policyIDs: string[];
  }) => Promise<void>;
  open: boolean;
}) {
  const [decision, setDecision] = useState<ReviewDecision>("approve");
  const [selectedPolicyIDs, setSelectedPolicyIDs] = useState<string[]>([]);

  useEffect(() => {
    if (!action || !open) {
      setDecision("approve");
      setSelectedPolicyIDs([]);
      return;
    }
    setDecision("approve");
    setSelectedPolicyIDs(initialPolicyIDsForAction(action, shadowMCPPolicies));
  }, [action, shadowMCPPolicies, open]);

  if (!action) return null;

  const server = action.server;
  const canChoosePolicies =
    action.mode !== "delete" &&
    (action.mode !== "review" || decision === "approve");
  const needsPolicySelection = canChoosePolicies;
  const canSubmit =
    !isSubmitting &&
    (action.mode === "delete" ||
      (action.mode === "review" && decision === "deny") ||
      selectedPolicyIDs.length > 0);

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="sm:max-w-xl">
        <SheetHeader>
          <SheetTitle>{actionSheetTitle(action.mode)}</SheetTitle>
          <SheetDescription>
            {actionSheetDescription(action.mode)}
          </SheetDescription>
        </SheetHeader>

        <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-4">
          <section className="border-border rounded-md border px-4 py-3">
            <Type variant="small" className="font-medium">
              {server.serverName || server.urlHost}
            </Type>
            <Type muted small className="mt-1 break-all">
              {server.canonicalServerUrl}
            </Type>
            {server.latestRequest && action.mode === "review" && (
              <div className="mt-4 grid grid-cols-2 gap-4">
                <div className="min-w-0">
                  <Type muted small>
                    Requester
                  </Type>
                  <Type variant="body" className="mt-1 truncate text-sm">
                    {server.latestRequest.requesterEmail}
                  </Type>
                </div>
                <div>
                  <Type muted small>
                    Requested
                  </Type>
                  <Type variant="body" className="mt-1 text-sm">
                    {formatShortDate(server.latestRequest.requestedAt)}
                  </Type>
                </div>
              </div>
            )}
          </section>

          {action.mode === "review" && (
            <RadioGroup
              value={decision}
              onValueChange={(value) => setDecision(value as ReviewDecision)}
              className="border-border grid grid-cols-2 gap-4 rounded-md border p-3"
            >
              <label
                className={cn(
                  "flex cursor-pointer items-start gap-3 rounded-sm border border-transparent px-3 py-2.5 transition-colors",
                  decision === "approve" && "border-border bg-card shadow-xs",
                )}
              >
                <RadioGroupItem value="approve" className="mt-1.5" />
                <span>
                  <Badge variant="success">
                    <Badge.Text>Approve</Badge.Text>
                  </Badge>
                  <Type muted small>
                    Add an allow decision.
                  </Type>
                </span>
              </label>
              <label
                className={cn(
                  "flex cursor-pointer items-start gap-3 rounded-sm border border-transparent px-3 py-2.5 transition-colors",
                  decision === "deny" && "border-border bg-card shadow-xs",
                )}
              >
                <RadioGroupItem value="deny" className="mt-1.5" />
                <span>
                  <Badge variant="destructive">
                    <Badge.Text>Deny</Badge.Text>
                  </Badge>
                  <Type muted small>
                    Resolve the request.
                  </Type>
                </span>
              </label>
            </RadioGroup>
          )}

          {needsPolicySelection && (
            <PolicySelection
              disabled={isSubmitting}
              onSelectionChange={setSelectedPolicyIDs}
              policies={shadowMCPPolicies}
              selectedPolicyIDs={selectedPolicyIDs}
            />
          )}

          {action.mode === "delete" && (
            <Type muted small>
              This removes the current allow decision for the URL.
            </Type>
          )}
        </div>

        <SheetFooter>
          <Button
            className="w-full"
            disabled={!canSubmit}
            onClick={() => {
              void onSubmit({ action, decision, policyIDs: selectedPolicyIDs });
            }}
            variant={
              action.mode === "delete" ? "destructive-primary" : "primary"
            }
          >
            <Button.LeftIcon>
              {isSubmitting && (
                <Icon name="loader-circle" className="animate-spin" />
              )}
            </Button.LeftIcon>
            <Button.Text>
              {actionSheetSubmitLabel(action.mode, decision)}
            </Button.Text>
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}

export function ShadowMCPInventoryTable({
  shadowMCPPolicies,
  className,
  enabled = true,
  policyState,
  projectID,
}: {
  shadowMCPPolicies: ShadowMCPPolicy[];
  className?: string;
  enabled?: boolean;
  policyState: ShadowMCPPolicyState;
  projectID: string;
}): JSX.Element {
  const queryClient = useQueryClient();
  const inventoryScope = enabled && projectID.length > 0 ? projectID : "";
  const [cursor, setCursor] = useState<string | undefined>(undefined);
  const [pages, setPages] = useState<InventoryPage[]>([]);
  const [paginationScope, setPaginationScope] = useState(inventoryScope);
  const hasActivePagination = paginationScope === inventoryScope;
  const activeCursor = hasActivePagination ? cursor : undefined;
  const activePages = hasActivePagination ? pages : EMPTY_INVENTORY_PAGES;
  const inventoryRequest = activeCursor
    ? {
        projectId: projectID,
        limit: INVENTORY_PAGE_LIMIT,
        cursor: activeCursor,
      }
    : { projectId: projectID, limit: INVENTORY_PAGE_LIMIT };
  const inventoryQuery = useShadowMCPInventory(inventoryRequest, undefined, {
    enabled: enabled && projectID.length > 0,
  });
  const upsertPolicyBypass = useUpsertShadowMCPInventoryPolicyBypassMutation();
  const deletePolicyBypass = useDeleteShadowMCPInventoryPolicyBypassMutation();
  const resolveInventoryRequest = useResolveShadowMCPInventoryRequestMutation();
  const [sort, setSort] = useState<SortDescriptor | null>({
    id: "lastCalled",
    direction: "desc",
  });
  const [activeAction, setActiveAction] =
    useState<ActiveInventoryAction | null>(null);
  const [isSubmittingAction, setIsSubmittingAction] = useState(false);
  const isSubmitting =
    isSubmittingAction ||
    upsertPolicyBypass.isPending ||
    deletePolicyBypass.isPending ||
    resolveInventoryRequest.isPending;
  const isActionPending = isSubmitting || activeAction !== null;

  useEffect(() => {
    setPaginationScope(inventoryScope);
    setCursor(undefined);
    setPages([]);
  }, [inventoryScope]);

  useEffect(() => {
    if (
      !hasActivePagination ||
      !enabled ||
      projectID.length === 0 ||
      !inventoryQuery.data
    ) {
      return;
    }

    const pageCursor = activeCursor ?? FIRST_PAGE_CURSOR;
    setPages((currentPages) => {
      const page: InventoryPage = {
        cursor: pageCursor,
        nextCursor: inventoryQuery.data.nextCursor,
        servers: inventoryQuery.data.servers,
      };
      const existingPageIndex = currentPages.findIndex(
        (currentPage) => currentPage.cursor === pageCursor,
      );

      if (existingPageIndex === -1) {
        return [...currentPages, page];
      }

      return currentPages.map((currentPage, index) =>
        index === existingPageIndex ? page : currentPage,
      );
    });
  }, [
    activeCursor,
    enabled,
    hasActivePagination,
    inventoryQuery.data,
    projectID,
  ]);

  const refreshInventory = async () => {
    setCursor(undefined);
    setPages([]);
    await invalidateAllShadowMCPInventory(queryClient);
  };

  const loadedServers = useMemo(() => {
    return activePages.flatMap((page) => page.servers);
  }, [activePages]);

  const latestPage = activePages[activePages.length - 1];
  const canUseInventoryQueryData =
    enabled && projectID.length > 0 && hasActivePagination;
  const nextCursor =
    latestPage?.nextCursor ??
    (canUseInventoryQueryData ? inventoryQuery.data?.nextCursor : undefined);
  const hasLoadedPages = activePages.length > 0;
  const isInitialLoading = inventoryQuery.isLoading && !hasLoadedPages;
  const isInitialError = Boolean(inventoryQuery.error && !hasLoadedPages);
  const isLoadingMore = Boolean(
    hasLoadedPages && (inventoryQuery.isFetching || inventoryQuery.isLoading),
  );

  const loadMoreServers = () => {
    if (!nextCursor || isLoadingMore) {
      return;
    }

    if (activeCursor === nextCursor && inventoryQuery.error) {
      void inventoryQuery.refetch();
      return;
    }

    setCursor(nextCursor);
  };

  const submitInventoryAction = async ({
    action,
    decision,
    policyIDs,
  }: {
    action: ActiveInventoryAction;
    decision: ReviewDecision;
    policyIDs: string[];
  }) => {
    const label = action.server.serverName ?? action.server.canonicalServerUrl;
    setIsSubmittingAction(true);
    try {
      if (action.mode === "delete") {
        await deletePolicyBypass.mutateAsync({
          request: {
            projectId: projectID,
            serverUrl: action.server.canonicalServerUrl,
          },
        });
        toast.success(`Removed allow rule for: ${label}`);
      } else if (action.mode === "review") {
        await resolveInventoryRequest.mutateAsync({
          request: {
            resolveShadowMCPInventoryRequestForm: {
              decision,
              policyIds: decision === "approve" ? policyIDs : undefined,
              projectId: projectID,
              serverUrl: action.server.canonicalServerUrl,
            },
          },
        });
        toast.success(
          decision === "approve"
            ? `Request approved for: ${label}`
            : `Request denied for: ${label}`,
        );
      } else {
        await upsertPolicyBypass.mutateAsync({
          request: {
            shadowMCPInventoryPolicyBypassForm: {
              policyIds: policyIDs,
              projectId: projectID,
              serverUrl: action.server.canonicalServerUrl,
            },
          },
        });
        toast.success(`Allow rule saved for: ${label}`);
      }
      await refreshInventory();
      setActiveAction(null);
    } catch {
      toast.error(`Unable to update allow rule for: ${label}`);
    } finally {
      setIsSubmittingAction(false);
    }
  };

  const renderActionsCell = (server: ShadowMCPInventoryServer) => {
    return (
      <InventoryActionMenu
        disabled={isActionPending}
        onOpenAction={(mode, selectedServer) =>
          setActiveAction({ mode, server: selectedServer })
        }
        server={server}
      />
    );
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
      render: (server) => <InventoryServerCell server={server} />,
    },
    {
      key: "status",
      header: "Status",
      sortable: true,
      sortValue: (server) =>
        shadowMCPInventoryStatusLabel(
          shadowMCPInventoryStatus(server, policyState),
        ),
      width: "0.9fr",
      render: (server) => (
        <InventoryStatusCell policyState={policyState} server={server} />
      ),
    },
    {
      key: "lastCalled",
      header: "Last called",
      sortable: true,
      sortValue: (server) => server.lastCalled?.getTime() ?? 0,
      width: "0.7fr",
      render: (server) => (
        <Type variant="small">{formatShortDate(server.lastCalled)}</Type>
      ),
    },
    {
      key: "lastSeen",
      header: "Last seen",
      sortable: true,
      sortValue: (server) => server.lastSeen.getTime(),
      width: "0.7fr",
      render: (server) => (
        <Type variant="small">{formatShortDate(server.lastSeen)}</Type>
      ),
    },
    {
      key: "usage",
      header: "Usage",
      sortable: true,
      sortValue: (server) => server.observedUseCount,
      width: "0.5fr",
      render: (server) => <UsageCell server={server} />,
    },
    {
      key: "actions",
      header: "",
      width: "0.3fr",
      render: renderActionsCell,
    },
  ];

  const servers =
    loadedServers.length > 0
      ? loadedServers
      : canUseInventoryQueryData
        ? (inventoryQuery.data?.servers ?? [])
        : [];
  const sortedServers = sortTableData(
    servers,
    columns,
    sort,
  ) as ShadowMCPInventoryServer[];

  if (isInitialLoading) {
    return <SkeletonTable />;
  }

  if (isInitialError) {
    return (
      <div className="bg-background flex min-h-32 flex-col items-center justify-center gap-1 px-4 py-8 text-center">
        <Type variant="body" className="font-medium">
          Shadow MCP inventory could not be loaded
        </Type>
        <Type muted small className="max-w-md">
          Refresh the page or try again later.
        </Type>
      </div>
    );
  }

  if (servers.length === 0) {
    return <InventoryEmptyState />;
  }

  return (
    <div className={cn("min-h-0 shrink overflow-hidden", className)}>
      <ShadowMCPInventoryActionSheet
        action={activeAction}
        shadowMCPPolicies={shadowMCPPolicies}
        isSubmitting={isSubmitting}
        onOpenChange={(open) => {
          if (!open) {
            setActiveAction(null);
          }
        }}
        onSubmit={submitInventoryAction}
        open={activeAction !== null}
      />
      <Table
        columns={columns}
        className="h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-x-auto overflow-y-hidden"
      >
        <Table.Header columns={columns} sort={sort} onSortChange={setSort} />
        <Table.Body
          columns={columns}
          data={sortedServers}
          handleLoadMore={loadMoreServers}
          hasMore={Boolean(nextCursor)}
          isLoading={isLoadingMore}
          rowKey={(row) => row.canonicalServerUrl}
          className="min-h-0 content-start overflow-y-auto"
        />
      </Table>
    </div>
  );
}
