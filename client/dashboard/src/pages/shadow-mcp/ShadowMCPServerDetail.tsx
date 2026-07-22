import { formatShortDate } from "@/components/access/shadow-mcp-utils";
import { InlineEditableText } from "@/components/inline-editable-text";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import {
  type ActiveInventoryAction,
  type InventoryActionMode,
  type ReviewDecision,
  ShadowMCPInventoryActionSheet,
  type ShadowMCPPolicy,
} from "@/components/shadow-mcp/ShadowMCPInventoryActions";
import {
  eligibleShadowMCPAllowRulePolicies,
  shadowMCPInventoryStatus,
  shadowMCPInventoryStatusBadgeVariant,
  shadowMCPInventoryStatusDescription,
  shadowMCPInventoryStatusLabel,
  shadowMCPPolicyState,
  type ShadowMCPPolicyState,
} from "@/components/shadow-mcp/shadowMCPInventoryStatus";
import { ALLOW_RULE_POLICY_REQUIRED } from "@/components/shadow-mcp/shadowMCPInventoryActionItems";
import { SkeletonTable } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useProject } from "@/contexts/Auth";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import type { ShadowMCPInventoryUser } from "@gram/client/models/components/shadowmcpinventoryuser.js";
import { useDeleteShadowMCPInventoryPolicyBypassMutation } from "@gram/client/react-query/deleteShadowMCPInventoryPolicyBypass.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useResolveShadowMCPInventoryRequestMutation } from "@gram/client/react-query/resolveShadowMCPInventoryRequest.js";
import { useRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
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
import {
  Badge,
  Button,
  type Column,
  Icon,
  Table,
} from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { useParams } from "react-router";
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

function userCountLabel(count: number) {
  return `${count} ${count === 1 ? "user" : "users"}`;
}

function actionModeForServer(
  server: ShadowMCPInventoryServer,
): InventoryActionMode {
  if (server.requestCount > 0) return "review";
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
  }
}

function ServerStatus({
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
      <Type muted small>
        {shadowMCPInventoryStatusDescription(server, policyState)}
      </Type>
    </div>
  );
}

function DetailStat({
  emphasized = false,
  label,
  value,
}: {
  emphasized?: boolean;
  label: string;
  value: string;
}) {
  return (
    <div className="min-w-0">
      <Type muted small>
        {label}
      </Type>
      <Type
        variant={emphasized ? "body" : "small"}
        className="mt-1 truncate font-medium"
      >
        {value}
      </Type>
    </div>
  );
}

function ServerSummary({
  policyState,
  server,
}: {
  policyState: ShadowMCPPolicyState;
  server: ShadowMCPInventoryServer;
}) {
  return (
    <div className="border-border overflow-hidden rounded-md border">
      <div className="bg-muted/20 grid gap-4 p-4 md:grid-cols-4">
        <ServerStatus policyState={policyState} server={server} />
        <DetailStat
          emphasized
          label="Requests"
          value={`${server.requestCount} ${server.requestCount === 1 ? "request" : "requests"}`}
        />
        <DetailStat
          emphasized
          label="Users"
          value={userCountLabel(server.userCount)}
        />
        <DetailStat
          emphasized
          label="Allowed policies"
          value={`${server.allowedPolicyIds.length} ${server.allowedPolicyIds.length === 1 ? "policy" : "policies"}`}
        />
      </div>
      <div className="border-border grid gap-4 border-t p-4 md:grid-cols-4">
        <DetailStat
          label="Observed use"
          value={usageCountLabel(server.observedUseCount)}
        />
        <DetailStat
          label="Last called"
          value={formatShortDate(server.lastCalled)}
        />
        <DetailStat
          label="Last seen"
          value={formatShortDate(server.lastSeen)}
        />
        <DetailStat
          label="First seen"
          value={formatShortDate(server.firstSeen)}
        />
      </div>
    </div>
  );
}

function TopUsersTable({
  onLoadMore,
  users,
  hasMore,
  isLoading,
}: {
  onLoadMore: () => void;
  users: ShadowMCPInventoryUser[];
  hasMore: boolean;
  isLoading: boolean;
}) {
  const columns: Column<ShadowMCPInventoryUser>[] = [
    {
      key: "user",
      header: "User",
      render: (user) => <Type variant="small">{user.userKey}</Type>,
      width: "1fr",
    },
    {
      key: "calls",
      header: "Calls",
      render: (user) => (
        <Type variant="small">{usageCountLabel(user.observedUseCount)}</Type>
      ),
      width: "160px",
    },
    {
      key: "lastCalled",
      header: "Last called",
      render: (user) => (
        <Type variant="small">{formatShortDate(user.lastCalled)}</Type>
      ),
      width: "180px",
    },
  ];

  if (users.length === 0) {
    return (
      <div className="bg-muted/20 flex min-h-32 flex-col items-center justify-center rounded-md border border-dashed px-6 py-8 text-center">
        <Type variant="body" className="font-medium">
          No user activity
        </Type>
        <Type muted small className="mt-1 max-w-md">
          Users will appear here after this Shadow MCP server is called.
        </Type>
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
        rowKey={(row) => row.userKey}
      />
    </Table>
  );
}

function DetailActionButtons({
  allowRuleUnavailableMessage,
  canManageAllowRules,
  disabled,
  onOpenAction,
  server,
}: {
  allowRuleUnavailableMessage: string;
  canManageAllowRules: boolean;
  disabled: boolean;
  onOpenAction: (mode: InventoryActionMode) => void;
  server: ShadowMCPInventoryServer;
}) {
  const primaryMode = actionModeForServer(server);
  const primaryRequiresAllowRule =
    primaryMode === "add" || primaryMode === "edit";
  const hasVisibleAllowRuleAction =
    primaryRequiresAllowRule || server.access === "allowed";
  const primaryDisabled =
    disabled || (primaryRequiresAllowRule && !canManageAllowRules);

  return (
    <div className="flex flex-col items-end gap-1">
      <div className="flex items-center gap-2">
        <Button
          disabled={primaryDisabled}
          onClick={() => onOpenAction(primaryMode)}
          variant="primary"
        >
          <Button.Text>{actionLabel(primaryMode)}</Button.Text>
        </Button>
        {server.access === "allowed" && primaryMode !== "edit" && (
          <Button
            disabled={disabled || !canManageAllowRules}
            onClick={() => onOpenAction("edit")}
            variant="tertiary"
          >
            <Button.Text>{actionLabel("edit")}</Button.Text>
          </Button>
        )}
        {server.access === "allowed" && (
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
        <Type muted small>
          {allowRuleUnavailableMessage}
        </Type>
      )}
    </div>
  );
}

export default function ShadowMCPServerDetail(): JSX.Element {
  const { serverSlug = "" } = useParams<{ serverSlug: string }>();
  const project = useProject();
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
    resolveInventoryRequest.isPending;
  const pageLoading =
    policiesQuery.isLoading ||
    membersQuery.isLoading ||
    rolesQuery.isLoading ||
    serverQuery.isLoading;

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
      if (action.mode === "delete") {
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
                  <Type variant="body" className="font-medium">
                    Shadow MCP server could not be loaded
                  </Type>
                  <Type muted small className="max-w-md">
                    Refresh the page or try again later.
                  </Type>
                </div>
              ) : (
                <div className="flex min-h-0 flex-col gap-6">
                  <ShadowMCPInventoryActionSheet
                    action={activeAction}
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
                  <ServerSummary policyState={policyState} server={server} />
                  <section className="min-h-0 space-y-3">
                    <div>
                      <Type variant="subheading">Top users</Type>
                      <Type muted small>
                        Users with observed calls to this Shadow MCP server.
                      </Type>
                    </div>
                    {usersQuery.isLoading && !hasLoadedUserPages ? (
                      <SkeletonTable />
                    ) : usersQuery.error && !hasLoadedUserPages ? (
                      <div className="bg-background flex min-h-24 flex-col items-center justify-center gap-1 px-4 py-6 text-center">
                        <Type variant="body" className="font-medium">
                          Users could not be loaded
                        </Type>
                      </div>
                    ) : (
                      <TopUsersTable
                        hasMore={Boolean(nextUsersCursor)}
                        isLoading={isLoadingMoreUsers}
                        onLoadMore={loadMoreUsers}
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
