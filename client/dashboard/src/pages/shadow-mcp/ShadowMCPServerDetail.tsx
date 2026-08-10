import { formatShortDate } from "@/components/access/shadow-mcp-utils";
import { InlineEditableText } from "@/components/inline-editable-text";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { type Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import {
  ApprovalReview,
  RefreshEvidenceButton,
} from "@/components/mcp-approvals/ApprovalReview";
import {
  type DecideAccessTarget,
  DecideAccessSheet,
} from "@/components/mcp-approvals/DecideAccessSheet";
import {
  eligibleShadowMCPAllowRulePolicies,
  shadowMCPBlockingPolicyDisposition,
  shadowMCPInventoryStatus,
  shadowMCPInventoryStatusBadgeVariant,
  shadowMCPInventoryStatusDescription,
  shadowMCPInventoryStatusLabel,
  shadowMCPPolicyState,
  type ShadowMCPPolicy,
  type ShadowMCPPolicyDisposition,
  type ShadowMCPPolicyState,
} from "@/components/shadow-mcp/shadowMCPInventoryStatus";
import { useProject } from "@/contexts/Auth";
import { formatPlatform } from "@/lib/formatPlatform";
import { encodeCrumb } from "@/pages/costs/taxonomy";
import { HookSourceIcon } from "@/pages/hooks/HookSourceIcon";
import { useRoutes } from "@/routes";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import type { ShadowMCPInventoryUser } from "@gram/client/models/components/shadowmcpinventoryuser.js";
import type { ShadowMCPInventoryUserSource } from "@gram/client/models/components/shadowmcpinventoryusersource.js";
import { Dimension } from "@gram/client/models/components/queryfilter.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { useEnsureMcpServerReviewMutation } from "@gram/client/react-query/ensureMcpServerReview.js";
import { invalidateAllShadowMCPInventory } from "@gram/client/react-query/shadowMCPInventory.js";
import {
  invalidateAllShadowMCPInventoryServer,
  useShadowMCPInventoryServer,
} from "@gram/client/react-query/shadowMCPInventoryServer.js";
import { useUpdateShadowMCPInventoryServerNameMutation } from "@gram/client/react-query/updateShadowMCPInventoryServerName.js";
import { useShadowMCPInventoryUsers } from "@gram/client/react-query/shadowMCPInventoryUsers.js";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
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
    <ServerStatus
      disposition={disposition}
      policyState={policyState}
      server={server}
    />
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
  disabled,
  onOpenDecide,
  server,
  projectSlug,
}: {
  disabled: boolean;
  onOpenDecide: () => void;
  server: ShadowMCPInventoryServer;
  projectSlug: string;
}) {
  const pendingReview = server.approvalRequest?.status === "requested";

  return (
    <div className="flex items-center gap-2">
      {server.approvalRequest && (
        <RefreshEvidenceButton
          requestId={server.approvalRequest.id}
          projectSlug={projectSlug}
          ready={!disabled}
        />
      )}
      <Button
        disabled={disabled}
        onClick={onOpenDecide}
        variant={pendingReview ? "primary" : "secondary"}
      >
        <Button.Text>
          {pendingReview ? "Review Request" : "Decide Access"}
        </Button.Text>
      </Button>
    </div>
  );
}

/**
 * Resolves the server's evidence dossier the moment the page needs it: the
 * ensure call opens one (gathering evidence) when none exists and returns
 * the existing review otherwise. Evidence is a property of the server, so
 * reading it never requires asking for access or deciding anything first.
 */
function EnsureServerReview({
  canonicalServerUrl,
}: {
  canonicalServerUrl: string;
}) {
  const project = useProject();
  const queryClient = useQueryClient();
  const ensure = useEnsureMcpServerReviewMutation();
  const ensureRef = useRef(false);

  useEffect(() => {
    if (ensureRef.current) return;
    ensureRef.current = true;
    void (async () => {
      try {
        await ensure.mutateAsync({
          request: {
            gramProject: project.slug,
            ensureServerReviewRequestBody: { target: canonicalServerUrl },
          },
        });
      } catch {
        toast.error("Evidence could not be gathered for this server");
        return;
      }
      await Promise.all([
        invalidateAllShadowMCPInventoryServer(queryClient),
        invalidateAllShadowMCPInventory(queryClient),
      ]);
    })();
  }, [canonicalServerUrl, ensure, project.slug, queryClient]);

  return (
    <div className="bg-muted/20 flex min-h-24 flex-col items-center justify-center border border-dashed px-6 py-8 text-center">
      <Text variant="body" className="font-medium">
        Gathering evidence
      </Text>
      <Text muted small className="mt-1 max-w-md">
        Collecting what this server declares about itself — its identity,
        capabilities, and package health. This takes a few seconds on first
        visit.
      </Text>
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
  const updateServerName = useUpdateShadowMCPInventoryServerNameMutation();
  const [decideTarget, setDecideTarget] = useState<DecideAccessTarget | null>(
    null,
  );
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

  const openDecide = () => {
    if (!server) return;
    setDecideTarget({
      canonicalServerUrl: server.canonicalServerUrl,
      displayName: serverDisplayName,
      approvalRequestId: server.approvalRequest?.id,
    });
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
                  disabled={decideTarget !== null}
                  onOpenDecide={openDecide}
                  server={server}
                  projectSlug={project.slug}
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
                  <DecideAccessSheet
                    target={decideTarget}
                    open={decideTarget !== null}
                    onOpenChange={(open) => {
                      if (!open) {
                        setDecideTarget(null);
                      }
                    }}
                    disposition={disposition}
                    members={membersQuery.data?.members ?? []}
                    roles={rolesQuery.data?.roles ?? []}
                  />
                  <ServerSummary
                    disposition={disposition}
                    policyState={policyState}
                    server={server}
                  />
                  <section className="space-y-3">
                    <div>
                      <Text variant="subheading">Access review</Text>
                      <Text muted small>
                        The evidence, requesters, and decision history for this
                        server. Decisions here are what allow or block it.
                      </Text>
                    </div>
                    {server.approvalRequest ? (
                      <ApprovalReview requestId={server.approvalRequest.id} />
                    ) : (
                      <EnsureServerReview
                        canonicalServerUrl={server.canonicalServerUrl}
                      />
                    )}
                  </section>
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
