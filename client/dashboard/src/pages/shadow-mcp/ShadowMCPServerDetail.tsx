import { formatShortDate } from "@/components/access/shadow-mcp-utils";
import { InlineEditableText } from "@/components/inline-editable-text";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { MetricCard } from "@/components/ui/MetricCard";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { type Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import {
  type ActiveInventoryAction,
  type InventoryActionMode,
  type ReviewDecision,
  ShadowMCPInventoryActionSheet,
  type ShadowMCPPolicy,
} from "@/components/shadow-mcp/ShadowMCPInventoryActions";
import {
  eligibleShadowMCPAllowRulePolicies,
  shadowMCPBlockingPolicyDisposition,
  shadowMCPInventoryStatus,
  shadowMCPInventoryStatusBadgeVariant,
  shadowMCPInventoryStatusDescription,
  shadowMCPInventoryStatusLabel,
  shadowMCPPolicyState,
  type ShadowMCPPolicyDisposition,
  type ShadowMCPPolicyState,
} from "@/components/shadow-mcp/shadowMCPInventoryStatus";
import { ALLOW_RULE_POLICY_REQUIRED } from "@/components/shadow-mcp/shadowMCPInventoryActionItems";
import { useProject } from "@/contexts/Auth";
import { formatPlatform } from "@/lib/formatPlatform";
import { encodeCrumb } from "@/pages/costs/taxonomy";
import { HookSourceIcon } from "@/pages/hooks/HookSourceIcon";
import { useRoutes } from "@/routes";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import type { ShadowMCPInventoryUser } from "@gram/client/models/components/shadowmcpinventoryuser.js";
import type { ShadowMCPInventoryUserSource } from "@gram/client/models/components/shadowmcpinventoryusersource.js";
import { Dimension } from "@gram/client/models/components/queryfilter.js";
import { useDeleteShadowMCPInventoryPolicyBypassMutation } from "@gram/client/react-query/deleteShadowMCPInventoryPolicyBypass.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useResolveShadowMCPInventoryRequestMutation } from "@gram/client/react-query/resolveShadowMCPInventoryRequest.js";
import { useRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
import { useBlockShadowMCPInventoryServerMutation } from "@gram/client/react-query/blockShadowMCPInventoryServer.js";
import { useUnblockShadowMCPInventoryServerMutation } from "@gram/client/react-query/unblockShadowMCPInventoryServer.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { invalidateAllShadowMCPInventory } from "@gram/client/react-query/shadowMCPInventory.js";
import {
  invalidateAllShadowMCPInventoryServer,
  useShadowMCPInventoryServer,
} from "@gram/client/react-query/shadowMCPInventoryServer.js";
import { useUpdateShadowMCPInventoryServerNameMutation } from "@gram/client/react-query/updateShadowMCPInventoryServerName.js";
import {
  invalidateAllShadowMCPInventoryUsers,
  useShadowMCPInventoryUsers,
} from "@gram/client/react-query/shadowMCPInventoryUsers.js";
import { useUpsertShadowMCPInventoryPolicyBypassMutation } from "@gram/client/react-query/upsertShadowMCPInventoryPolicyBypass.js";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router";
import { toast } from "sonner";

const USERS_PAGE_LIMIT = 50;
const FIRST_PAGE_CURSOR = "";

type UsersPage = {
  cursor: string;
  nextCursor?: string;
  users: ShadowMCPInventoryUser[];
};

const EMPTY_USER_PAGES: UsersPage[] = [];

function usageCountLabel(count: number) {
  return `${count} ${count === 1 ? "call" : "calls"}`;
}

function sourceLabel(source: string) {
  return formatPlatform(source) || "Unknown";
}

function UserSources({
  sources,
}: {
  sources: ShadowMCPInventoryUserSource[] | undefined;
}) {
  const orderedSources = [...(sources ?? [])].sort((left, right) => {
    const countDifference = right.observedUseCount - left.observedUseCount;
    if (countDifference !== 0) return countDifference;

    return sourceLabel(left.source).localeCompare(sourceLabel(right.source));
  });

  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
      {orderedSources.map((source) => (
        <div className="flex items-center gap-1.5" key={source.source}>
          <HookSourceIcon source={source.source} className="size-4 shrink-0" />
          <span className="whitespace-nowrap font-medium">
            {sourceLabel(source.source)}
          </span>
          <Badge variant="neutral">
            <Badge.Text>{source.observedUseCount}</Badge.Text>
          </Badge>
        </div>
      ))}
    </div>
  );
}

function actionModeForServer(
  server: ShadowMCPInventoryServer,
  disposition: ShadowMCPPolicyDisposition | null,
): InventoryActionMode {
  if (server.requestCount > 0) return "review";
  if (disposition === "allow_all") {
    return server.access === "blocked" ? "unblock" : "block";
  }
  if (server.access === "allowed") return "edit";
  return "add";
}

function actionLabel(mode: InventoryActionMode) {
  switch (mode) {
    case "review":
      return "Review Request";
    case "add":
      return "Add Allow Rule";
    case "edit":
      return "Edit Rule";
    case "delete":
      return "Delete Rule";
    case "block":
      return "Block Server";
    case "unblock":
      return "Unblock Server";
  }
}

function ServerStatus({
  disposition,
  policyState,
  server,
}: {
  disposition: ShadowMCPPolicyDisposition | null;
  policyState: ShadowMCPPolicyState;
  server: ShadowMCPInventoryServer;
}) {
  const status = shadowMCPInventoryStatus(server, policyState);

  return (
    <div className="space-y-1">
      <Badge variant={shadowMCPInventoryStatusBadgeVariant(status)}>
        <Badge.Text>{shadowMCPInventoryStatusLabel(status)}</Badge.Text>
      </Badge>
      <Text muted small>
        {shadowMCPInventoryStatusDescription(server, policyState, disposition)}
      </Text>
    </div>
  );
}

function MetaItem({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-baseline gap-2">
      <span className="text-eyebrow">{label}</span>
      <Text variant="small" className="font-medium">
        {value}
      </Text>
    </div>
  );
}

function ServerSummary({
  disposition,
  policyState,
  server,
}: {
  disposition: ShadowMCPPolicyDisposition | null;
  policyState: ShadowMCPPolicyState;
  server: ShadowMCPInventoryServer;
}) {
  return (
    <div className="space-y-4">
      <MetricCard.Group>
        <MetricCard
          size="sm"
          label="Requests"
          value={server.requestCount}
          tone={server.requestCount > 0 ? "destructive" : "neutral"}
          description={
            server.requestCount === 1 ? "pending request" : "pending requests"
          }
        />
        <MetricCard
          size="sm"
          tone="information"
          label="Users"
          value={server.userCount}
          description={
            server.userCount === 1 ? "observed user" : "observed users"
          }
        />
        <MetricCard
          size="sm"
          tone="information"
          label="Observed use"
          value={server.observedUseCount}
          description={server.observedUseCount === 1 ? "call" : "calls"}
        />
        <MetricCard
          size="sm"
          tone="information"
          label="Allowed policies"
          value={server.allowedPolicyIds.length}
          description={
            server.allowedPolicyIds.length === 1 ? "policy" : "policies"
          }
        />
      </MetricCard.Group>
      <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-2">
        <ServerStatus
          disposition={disposition}
          policyState={policyState}
          server={server}
        />
        <div className="flex flex-wrap items-baseline gap-x-6 gap-y-1">
          <MetaItem
            label="Last called"
            value={formatShortDate(server.lastCalled)}
          />
          <MetaItem
            label="Last seen"
            value={formatShortDate(server.lastSeen)}
          />
          <MetaItem
            label="First seen"
            value={formatShortDate(server.firstSeen)}
          />
        </div>
      </div>
    </div>
  );
}

function TopUsersTable({
  onOpenUser,
  onLoadMore,
  users,
  hasMore,
  isLoading,
}: {
  onOpenUser: (user: ShadowMCPInventoryUser) => void;
  onLoadMore: () => void;
  users: ShadowMCPInventoryUser[];
  hasMore: boolean;
  isLoading: boolean;
}) {
  const columns: Column<ShadowMCPInventoryUser>[] = [
    {
      key: "user",
      header: "User",
      render: (user) => <Text variant="small">{user.userKey}</Text>,
      width: "1fr",
    },
    {
      key: "sources",
      header: "Sources",
      render: (user) => <UserSources sources={user.sources} />,
      width: "1fr",
    },
    {
      key: "calls",
      header: "Calls",
      render: (user) => (
        <Text variant="small">{usageCountLabel(user.observedUseCount)}</Text>
      ),
      width: "0.6fr",
    },
    {
      key: "lastCalled",
      header: "Last called",
      render: (user) => (
        <Text variant="small">{formatShortDate(user.lastCalled)}</Text>
      ),
      width: "0.6fr",
    },
  ];

  if (users.length === 0) {
    return (
      <div className="bg-muted/20 flex min-h-32 flex-col items-center justify-center border border-dashed px-6 py-8 text-center">
        <Text variant="body" className="font-medium">
          No user activity
        </Text>
        <Text muted small className="mt-1 max-w-md">
          Users will appear here after this Shadow MCP server is called.
        </Text>
      </div>
    );
  }

  return (
    <Table columns={columns}>
      <Table.Header columns={columns} />
      <Table.Body
        columns={columns}
        data={users}
        handleLoadMore={onLoadMore}
        hasMore={hasMore}
        isLoading={isLoading}
        isRowClickable={(user) => Boolean(user.email)}
        onRowClick={onOpenUser}
        rowKey={(row) => row.userKey}
      />
    </Table>
  );
}

function DetailActionButtons({
  allowRuleUnavailableMessage,
  canManageAllowRules,
  disabled,
  disposition,
  onOpenAction,
  server,
}: {
  allowRuleUnavailableMessage: string;
  canManageAllowRules: boolean;
  disabled: boolean;
  disposition: ShadowMCPPolicyDisposition | null;
  onOpenAction: (mode: InventoryActionMode) => void;
  server: ShadowMCPInventoryServer;
}) {
  const primaryMode = actionModeForServer(server, disposition);
  const isAllowAll = disposition === "allow_all";
  const primaryRequiresAllowRule =
    !isAllowAll && (primaryMode === "add" || primaryMode === "edit");
  const hasVisibleAllowRuleAction =
    primaryRequiresAllowRule || (!isAllowAll && server.access === "allowed");
  const primaryDisabled =
    disabled || (primaryRequiresAllowRule && !canManageAllowRules);

  return (
    <div className="flex flex-col items-end gap-1">
      <div className="flex items-center gap-2">
        <Button
          disabled={primaryDisabled}
          onClick={() => onOpenAction(primaryMode)}
          variant={primaryMode === "block" ? "destructive-primary" : "primary"}
        >
          <Button.Text>{actionLabel(primaryMode)}</Button.Text>
        </Button>
        {!isAllowAll &&
          server.access === "allowed" &&
          primaryMode !== "edit" && (
            <Button
              disabled={disabled || !canManageAllowRules}
              onClick={() => onOpenAction("edit")}
              variant="tertiary"
            >
              <Button.Text>{actionLabel("edit")}</Button.Text>
            </Button>
          )}
        {!isAllowAll && server.access === "allowed" && (
          <Button
            disabled={disabled}
            onClick={() => onOpenAction("delete")}
            variant="tertiary"
          >
            <Button.LeftIcon>
              <Icon name="trash-2" />
            </Button.LeftIcon>
            <Button.Text>Delete Rule</Button.Text>
          </Button>
        )}
      </div>
      {hasVisibleAllowRuleAction && !canManageAllowRules && (
        <Text muted small>
          {allowRuleUnavailableMessage}
        </Text>
      )}
    </div>
  );
}

export default function ShadowMCPServerDetail(): JSX.Element {
  const { serverSlug = "" } = useParams<{ serverSlug: string }>();
  const project = useProject();
  const routes = useRoutes();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const policiesQuery = useRiskListPolicies();
  const membersQuery = useMembers();
  const rolesQuery = useRoles();
  const policyState = policiesQuery.isError
    ? "unavailable"
    : shadowMCPPolicyState(policiesQuery.data?.policies);
  let shadowMCPPolicies: ShadowMCPPolicy[] = [];
  if (!policiesQuery.isError) {
    shadowMCPPolicies = eligibleShadowMCPAllowRulePolicies(
      policiesQuery.data?.policies,
    );
  }
  const disposition = shadowMCPBlockingPolicyDisposition(shadowMCPPolicies);
  const allowAllPolicy =
    disposition === "allow_all"
      ? (shadowMCPPolicies.find(
          (policy) => policy.shadowMcpDisposition === "allow_all",
        ) ?? null)
      : null;
  const allowRuleUnavailableMessage = policiesQuery.isError
    ? "Policy status is unavailable. Refresh the page to try again."
    : ALLOW_RULE_POLICY_REQUIRED;
  const queryEnabled = project.id.length > 0 && serverSlug.length > 0;
  const [usersCursor, setUsersCursor] = useState<string | undefined>(undefined);
  const [userPages, setUserPages] = useState<UsersPage[]>([]);
  const serverQuery = useShadowMCPInventoryServer(
    {
      projectId: project.id,
      serverSlug,
    },
    undefined,
    { enabled: queryEnabled },
  );
  const server = serverQuery.data;
  const serverDisplayName =
    server?.serverName || server?.urlHost || "Shadow MCP Server";
  const serverURL = server?.canonicalServerUrl ?? "";
  const usersQueryEnabled = queryEnabled && serverURL.length > 0;
  const usersScope = usersQueryEnabled ? `${project.id}:${serverURL}` : "";
  const [usersPaginationScope, setUsersPaginationScope] = useState(usersScope);
  const hasActiveUsersPagination = usersPaginationScope === usersScope;
  const activeUsersCursor = hasActiveUsersPagination ? usersCursor : undefined;
  const activeUserPages = hasActiveUsersPagination
    ? userPages
    : EMPTY_USER_PAGES;
  const usersRequest = activeUsersCursor
    ? {
        projectId: project.id,
        serverUrl: serverURL,
        limit: USERS_PAGE_LIMIT,
        cursor: activeUsersCursor,
      }
    : { projectId: project.id, serverUrl: serverURL, limit: USERS_PAGE_LIMIT };
  const usersQuery = useShadowMCPInventoryUsers(usersRequest, undefined, {
    enabled: usersQueryEnabled,
  });
  const upsertPolicyBypass = useUpsertShadowMCPInventoryPolicyBypassMutation();
  const blockInventoryServer = useBlockShadowMCPInventoryServerMutation();
  const unblockInventoryServer = useUnblockShadowMCPInventoryServerMutation();
  const deletePolicyBypass = useDeleteShadowMCPInventoryPolicyBypassMutation();
  const resolveInventoryRequest = useResolveShadowMCPInventoryRequestMutation();
  const updateServerName = useUpdateShadowMCPInventoryServerNameMutation();
  const [activeAction, setActiveAction] =
    useState<ActiveInventoryAction | null>(null);
  const [isSubmittingAction, setIsSubmittingAction] = useState(false);
  const isSubmitting =
    isSubmittingAction ||
    upsertPolicyBypass.isPending ||
    deletePolicyBypass.isPending ||
    resolveInventoryRequest.isPending ||
    blockInventoryServer.isPending ||
    unblockInventoryServer.isPending;
  const pageLoading =
    policiesQuery.isLoading ||
    membersQuery.isLoading ||
    rolesQuery.isLoading ||
    serverQuery.isLoading;
  const onOpenUser = (user: ShadowMCPInventoryUser) => {
    if (!user.email) return;

    void navigate(
      `${routes.costs.href()}/${encodeCrumb({ dim: Dimension.Email, value: user.email })}`,
    );
  };

  useEffect(() => {
    setUsersPaginationScope(usersScope);
    setUsersCursor(undefined);
    setUserPages([]);
  }, [usersScope]);

  useEffect(() => {
    if (!hasActiveUsersPagination || !usersQueryEnabled || !usersQuery.data) {
      return;
    }

    const pageCursor = activeUsersCursor ?? FIRST_PAGE_CURSOR;
    setUserPages((currentPages) => {
      const page: UsersPage = {
        cursor: pageCursor,
        nextCursor: usersQuery.data.nextCursor,
        users: usersQuery.data.users,
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
    activeUsersCursor,
    hasActiveUsersPagination,
    usersQueryEnabled,
    usersQuery.data,
  ]);

  const loadedUsers = useMemo(
    () => activeUserPages.flatMap((page) => page.users),
    [activeUserPages],
  );
  const latestUsersPage = activeUserPages[activeUserPages.length - 1];
  const nextUsersCursor =
    latestUsersPage?.nextCursor ?? usersQuery.data?.nextCursor;
  const hasLoadedUserPages = activeUserPages.length > 0;
  const displayedUsers =
    loadedUsers.length > 0 ? loadedUsers : (usersQuery.data?.users ?? []);
  const isLoadingMoreUsers = Boolean(
    hasLoadedUserPages && (usersQuery.isFetching || usersQuery.isLoading),
  );

  const loadMoreUsers = () => {
    if (!nextUsersCursor || isLoadingMoreUsers) {
      return;
    }

    if (activeUsersCursor === nextUsersCursor && usersQuery.error) {
      void usersQuery.refetch();
      return;
    }

    setUsersCursor(nextUsersCursor);
  };

  const refreshInventory = async () => {
    await Promise.all([
      invalidateAllShadowMCPInventory(queryClient),
      invalidateAllShadowMCPInventoryServer(queryClient),
      invalidateAllShadowMCPInventoryUsers(queryClient),
    ]);
    setUsersCursor(undefined);
    setUserPages([]);
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
      if (action.mode === "block" || action.mode === "unblock") {
        if (!allowAllPolicy) {
          throw new Error("no allow_all shadow MCP policy available");
        }
        const target = {
          projectId: project.id,
          serverUrl: action.server.canonicalServerUrl,
          policyId: allowAllPolicy.id,
        };
        if (action.mode === "block") {
          await blockInventoryServer.mutateAsync({
            request: { blockShadowMCPInventoryServerRequestBody: target },
          });
        } else {
          await unblockInventoryServer.mutateAsync({ request: target });
        }
        toast.success(
          action.mode === "block"
            ? `Blocked server: ${label}`
            : `Unblocked server: ${label}`,
        );
      } else if (action.mode === "delete") {
        await deletePolicyBypass.mutateAsync({
          request: {
            projectId: project.id,
            serverUrl: action.server.canonicalServerUrl,
          },
        });
        toast.success(`Removed allow rule for: ${label}`);
      } else if (action.mode === "review") {
        await resolveInventoryRequest.mutateAsync({
          request: {
            resolveShadowMCPInventoryRequestForm: {
              decision,
              policyIds: decision === "allow" ? policyIDs : undefined,
              projectId: project.id,
              serverUrl: action.server.canonicalServerUrl,
            },
          },
        });
        toast.success(
          decision === "allow"
            ? `Request approved for: ${label}`
            : `Request denied for: ${label}`,
        );
      } else {
        await upsertPolicyBypass.mutateAsync({
          request: {
            shadowMCPInventoryPolicyBypassForm: {
              policyIds: policyIDs,
              projectId: project.id,
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

  const saveServerName = async (name: string) => {
    if (!server) return false;

    try {
      await updateServerName.mutateAsync({
        request: {
          updateShadowMCPInventoryServerNameForm: {
            projectId: project.id,
            serverUrl: server.canonicalServerUrl,
            name,
          },
        },
      });
      await Promise.all([
        invalidateAllShadowMCPInventoryServer(queryClient),
        invalidateAllShadowMCPInventory(queryClient),
      ]);
      return true;
    } catch {
      toast.error("Unable to update Shadow MCP server name");
      return false;
    }
  };

  let serverNameTitle = <span className="truncate">{serverDisplayName}</span>;
  if (server) {
    serverNameTitle = (
      <InlineEditableText
        value={serverDisplayName}
        onSubmit={saveServerName}
        inputLabel="Shadow MCP server name"
        editTitle="Rename Shadow MCP server"
        maxLength={255}
        editorClassName="w-[24rem] max-w-full"
        inputClassName="text-lg font-semibold"
      >
        {serverNameTitle}
      </InlineEditableText>
    );
  }

  const openAction = (mode: InventoryActionMode) => {
    if (!server) return;
    setActiveAction({ mode, server });
  };

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{
            ["shadow-mcp"]: "Shadow MCP",
            [serverSlug]: server?.serverName || server?.urlHost,
          }}
        />
      </Page.Header>
      <Page.Body fullHeight className="pb-8">
        <RequireScope scope="org:admin" level="page">
          <Page.Section>
            <Page.Section.Title>{serverNameTitle}</Page.Section.Title>
            <Page.Section.Description>
              {server?.canonicalServerUrl || serverSlug}
            </Page.Section.Description>
            <Page.Section.CTA>
              {server && (
                <DetailActionButtons
                  allowRuleUnavailableMessage={allowRuleUnavailableMessage}
                  canManageAllowRules={shadowMCPPolicies.length > 0}
                  disabled={isSubmitting}
                  disposition={disposition}
                  onOpenAction={openAction}
                  server={server}
                />
              )}
            </Page.Section.CTA>
            <Page.Section.Body>
              {pageLoading ? (
                <SkeletonTable />
              ) : serverQuery.error || !server ? (
                <div className="bg-background flex min-h-32 flex-col items-center justify-center gap-1 px-4 py-8 text-center">
                  <Text variant="body" className="font-medium">
                    Shadow MCP server could not be loaded
                  </Text>
                  <Text muted small className="max-w-md">
                    Refresh the page or try again later.
                  </Text>
                </div>
              ) : (
                <div className="flex min-h-0 flex-col gap-6">
                  <ShadowMCPInventoryActionSheet
                    action={activeAction}
                    disposition={disposition}
                    isSubmitting={isSubmitting}
                    members={membersQuery.data?.members ?? []}
                    onOpenChange={(open) => {
                      if (!open) {
                        setActiveAction(null);
                      }
                    }}
                    onSubmit={submitInventoryAction}
                    open={activeAction !== null}
                    policyUnavailableMessage={allowRuleUnavailableMessage}
                    roles={rolesQuery.data?.roles ?? []}
                    shadowMCPPolicies={shadowMCPPolicies}
                  />
                  <ServerSummary
                    disposition={disposition}
                    policyState={policyState}
                    server={server}
                  />
                  <section className="min-h-0 space-y-3">
                    <div>
                      <Text variant="subheading">Top users</Text>
                      <Text muted small>
                        Users with observed calls to this Shadow MCP server.
                      </Text>
                    </div>
                    {usersQuery.isLoading && !hasLoadedUserPages ? (
                      <SkeletonTable />
                    ) : usersQuery.error && !hasLoadedUserPages ? (
                      <div className="bg-background flex min-h-24 flex-col items-center justify-center gap-1 px-4 py-6 text-center">
                        <Text variant="body" className="font-medium">
                          Users could not be loaded
                        </Text>
                      </div>
                    ) : (
                      <TopUsersTable
                        hasMore={Boolean(nextUsersCursor)}
                        isLoading={isLoadingMoreUsers}
                        onLoadMore={loadMoreUsers}
                        onOpenUser={onOpenUser}
                        users={displayedUsers}
                      />
                    )}
                  </section>
                </div>
              )}
            </Page.Section.Body>
          </Page.Section>
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
