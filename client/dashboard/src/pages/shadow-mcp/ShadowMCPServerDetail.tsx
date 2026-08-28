import { IdentityLink } from "@/components/identity-link";
import { identityRefForUserKey } from "@/lib/identity-urn";
import { formatShortDate } from "@/components/access/shadow-mcp-utils";
import { InlineEditableText } from "@/components/inline-editable-text";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { MetricCard, type MetricCardProps } from "@/components/ui/MetricCard";
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
  MoreToggle,
  useCollapsedPreview,
} from "@/components/ui/collapsible-preview";
import {
  eligibleShadowMCPAllowRulePolicies,
  shadowMCPBlockingPolicyDisposition,
  shadowMCPInventoryStatus,
  shadowMCPInventoryStatusDescription,
  shadowMCPInventoryStatusLabel,
  type ShadowMCPInventoryStatus,
  type ShadowMCPPolicy,
} from "@/components/shadow-mcp/shadowMCPInventoryStatus";
import { useProject } from "@/contexts/Auth";
import { formatPlatform } from "@/lib/formatPlatform";
import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import { useRoutes } from "@/routes";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import type { ShadowMCPInventoryUser } from "@gram/client/models/components/shadowmcpinventoryuser.js";
import type { ShadowMCPInventoryUserSource } from "@gram/client/models/components/shadowmcpinventoryusersource.js";
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
import { useEffect, useId, useMemo, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router";
import { toast } from "sonner";

const USERS_PAGE_LIMIT = 50;
const FIRST_PAGE_CURSOR = "";

// How many users the table shows before the rest collapses behind a toggle.
// A busy server's roster runs long, and it sits beside the evidence rather
// than owning the page, so it previews a handful and expands on demand.
const USERS_PREVIEW_COUNT = 5;

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

  // With one source, its count is the user's whole call count — already the
  // next column over. The split is only worth showing when there is a split.
  const showCounts = orderedSources.length > 1;

  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
      {orderedSources.map((source) => (
        <div className="flex items-center gap-1.5" key={source.source}>
          <AgentProviderIcon
            source={source.source}
            className="size-4 shrink-0"
          />
          <span className="whitespace-nowrap font-medium">
            {sourceLabel(source.source)}
          </span>
          {showCounts && (
            <Badge variant="neutral">
              <Badge.Text>{source.observedUseCount}</Badge.Text>
            </Badge>
          )}
        </div>
      ))}
    </div>
  );
}

// MetricCard defaults to the roomier p-6/gap-4 the deployment page uses. This
// strip shares a row with the review header and sits above dense hairline
// tables, so it matches their rhythm, and px-3 puts its labels on the same left
// rail as the fact lists below.
const SUMMARY_TILE_CLASS = "gap-2 px-3 py-3";

/** Day without the time, for a figure that has to read at a glance. */
const summaryDayFormatter = new Intl.DateTimeFormat(undefined, {
  month: "short",
  day: "numeric",
});

/** The clock time, demoted to the qualifier line under the day. */
const summaryTimeFormatter = new Intl.DateTimeFormat(undefined, {
  hour: "numeric",
  minute: "2-digit",
});

/**
 * How the status colors its figure. The badge variants and the metric tones
 * are the same vocabulary, but they are separate types — mapping them here
 * keeps the strip honest if either list grows.
 */
function statusTone(status: ShadowMCPInventoryStatus): MetricCardProps["tone"] {
  switch (status) {
    case "allowed":
      return "success";
    case "blocked":
      return "destructive";
    case "restricted":
      return "warning";
    case "pending":
      // Blue like the inventory badge and the review's own awaiting-decision
      // vocabulary; orange stays reserved for the partial-access family.
      return "information";
    case "observed":
      return "neutral";
  }
}

/**
 * The server at a glance: status, traffic, and recency as one bordered strip.
 *
 * Handed to the review as its `summary` so the two share a row. It replaced a
 * badge and two lines of prose in a full-width block, where the status sat
 * alone on the left and the figures had nowhere to go that did not read as an
 * island — giving every figure the same shape and its own column fills the
 * width by construction instead.
 */
function ServerSummary({ server }: { server: ShadowMCPInventoryServer }) {
  const status = shadowMCPInventoryStatus(server);

  return (
    <MetricCard.Group className="flex-wrap">
      <MetricCard
        label="Status"
        value={shadowMCPInventoryStatusLabel(status)}
        tone={statusTone(status)}
        size="xs"
        className={SUMMARY_TILE_CLASS}
        description={shadowMCPInventoryStatusDescription(server)}
      />
      {/* First-seen rides under the call count rather than taking a tile of
          its own: it is the span those calls happened over, not a figure
          anyone reads on its own. */}
      <MetricCard
        label="Calls"
        value={server.observedUseCount}
        tone="neutral"
        size="xs"
        className={SUMMARY_TILE_CLASS}
        description={`since ${summaryDayFormatter.format(server.firstSeen)}`}
      />
      <MetricCard
        label="People"
        value={server.userCount}
        tone="neutral"
        size="xs"
        className={SUMMARY_TILE_CLASS}
      />
      <MetricCard
        label="Last called"
        value={
          server.lastCalled
            ? summaryDayFormatter.format(server.lastCalled)
            : "Never"
        }
        tone="neutral"
        size="xs"
        className={SUMMARY_TILE_CLASS}
        description={
          server.lastCalled
            ? summaryTimeFormatter.format(server.lastCalled)
            : undefined
        }
      />
    </MetricCard.Group>
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
      render: (user) => (
        <Text variant="small">
          <IdentityLink identifier={identityRefForUserKey(user.userKey)}>
            {user.userKey}
          </IdentityLink>
        </Text>
      ),
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
      width: "0.5fr",
    },
    {
      // Wider than the call count it sits beside: this table shares a row
      // with the declared tools, and a timestamp that wraps costs every row
      // a second line.
      key: "lastCalled",
      header: "Last called",
      render: (user) => (
        <Text variant="small">{formatShortDate(user.lastCalled)}</Text>
      ),
      width: "0.9fr",
    },
  ];

  const { collapsible, expanded, toggle, visible } = useCollapsedPreview(
    users,
    USERS_PREVIEW_COUNT,
  );
  const tableId = useId();

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

  const collapsed = collapsible && !expanded;

  return (
    <div className="space-y-2">
      <div id={tableId}>
        <Table columns={columns}>
          <Table.Header columns={columns} />
          <Table.Body
            columns={columns}
            data={visible}
            handleLoadMore={onLoadMore}
            // While collapsed, don't auto-fetch the next page — expanding is the
            // signal that the reader wants the whole roster.
            hasMore={!collapsed && hasMore}
            isLoading={isLoading}
            isRowClickable={(user) => Boolean(user.email)}
            onRowClick={onOpenUser}
            rowKey={(row) => row.userKey}
          />
        </Table>
      </div>
      {collapsible && (
        <MoreToggle
          expanded={expanded}
          onToggle={toggle}
          collapsedLabel={`Show all ${users.length}${hasMore ? "+" : ""} users`}
          controlId={tableId}
        />
      )}
    </div>
  );
}

function DetailActionButtons({
  disabled,
  onOpenDecide,
  policiesUnavailable,
  server,
  projectSlug,
}: {
  disabled: boolean;
  onOpenDecide: () => void;
  /**
   * The policy list failed to load. Deciding writes grants against the
   * policies the sheet can see, so with none loaded an approval would record
   * a decision that enforces nothing — the button waits instead.
   */
  policiesUnavailable: boolean;
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
        disabled={disabled || policiesUnavailable}
        onClick={onOpenDecide}
        variant={pendingReview ? "primary" : "secondary"}
        size="sm"
        title={
          policiesUnavailable
            ? "Policy data could not be loaded — deciding now would not be enforced"
            : undefined
        }
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
  const [failed, setFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);
  // Keyed by server and project (and retry attempt), not a bare boolean, so
  // navigating from one unreviewed server to another gathers for the new one
  // instead of being swallowed by the first mount's guard.
  const startedRunRef = useRef<string | null>(null);
  // The run key repeats when the user leaves a server and comes back before
  // its first gather settles, so key equality alone cannot tell a stale
  // rejection from the current run's. Each started run gets its own token and
  // only the newest may mark failure.
  const activeRunRef = useRef<symbol | null>(null);

  useEffect(() => {
    const runKey = `${project.slug}:${canonicalServerUrl}:${attempt}`;
    if (startedRunRef.current === runKey) return;
    startedRunRef.current = runKey;
    const runToken = Symbol(runKey);
    activeRunRef.current = runToken;
    setFailed(false);
    void (async () => {
      try {
        await ensure.mutateAsync({
          request: {
            gramProject: project.slug,
            ensureServerReviewRequestBody: { target: canonicalServerUrl },
          },
        });
      } catch {
        // A failure only marks the run it belongs to: navigating away starts
        // a new run — even one that repeats this run's key — and a stale
        // rejection must not overwrite that run's state.
        if (activeRunRef.current === runToken) {
          setFailed(true);
        }
        return;
      }
      await Promise.all([
        invalidateAllShadowMCPInventoryServer(queryClient),
        invalidateAllShadowMCPInventory(queryClient),
      ]);
    })();
  }, [attempt, canonicalServerUrl, ensure, project.slug, queryClient]);

  if (failed) {
    return (
      <div className="bg-muted/20 flex min-h-24 flex-col items-center justify-center border border-dashed px-6 py-8 text-center">
        <Text variant="body" className="font-medium">
          Evidence could not be gathered
        </Text>
        <Text muted small className="mt-1 max-w-md">
          Collecting this server's evidence failed. It may be a temporary
          problem — try again.
        </Text>
        <Button
          className="mt-3"
          variant="secondary"
          onClick={() => setAttempt((current) => current + 1)}
        >
          <Button.Text>Retry</Button.Text>
        </Button>
      </div>
    );
  }

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

    // The employee detail route resolves a raw email segment, so no
    // name-slug lookup is needed here.
    void navigate(routes.employees.detail.href(encodeURIComponent(user.email)));
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

  // Built once and rendered wherever the review puts it: inside the evidence
  // for a server that has one, standalone under its own heading for a server
  // that does not.
  const usersPanel =
    usersQuery.isLoading && !hasLoadedUserPages ? (
      <SkeletonTable />
    ) : usersQuery.error && !hasLoadedUserPages ? (
      <div className="bg-background flex min-h-24 flex-col items-center justify-center gap-1 px-4 py-6 text-center">
        <Text variant="body" className="font-medium">
          Users could not be loaded
        </Text>
      </div>
    ) : (
      <TopUsersTable
        // Remount per project+server: the collapse state lives in the table,
        // and this route stays mounted when navigating between servers or
        // switching projects, so without a key the next roster opens already
        // expanded.
        key={`${project.id}/${serverSlug}`}
        hasMore={Boolean(nextUsersCursor)}
        isLoading={isLoadingMoreUsers}
        onLoadMore={loadMoreUsers}
        onOpenUser={onOpenUser}
        users={displayedUsers}
      />
    );

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
      // A pending legacy bypass request rides along so the sheet promotes it
      // into the review and the decision drains it too.
      pendingBypassRequestId: server.latestRequest?.id,
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
            {/* No area eyebrow: "SECURE" over a server under review reads as
                a verdict about the server, not as the app section. */}
            <Page.Section.Title area="">{serverNameTitle}</Page.Section.Title>
            <Page.Section.Description>
              {server?.canonicalServerUrl || serverSlug}
            </Page.Section.Description>
            <Page.Section.CTA>
              {server && (
                <DetailActionButtons
                  disabled={decideTarget !== null}
                  onOpenDecide={openDecide}
                  policiesUnavailable={policiesQuery.isError}
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
                <div className="flex min-h-0 flex-col gap-6 pb-8">
                  {/* pb-8: the last thing on this page is a disclosure
                      toggle, and a control flush against the bottom edge
                      reads as cut off rather than as the end of the page. */}
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
                  <section className="min-h-0 space-y-3">
                    {/* The review owns its own heading: the request's status
                        shares that row, and only the review has it. Observed
                        traffic goes in with the evidence rather than trailing
                        it as a section of its own — who calls the server is
                        one of the questions a reviewer is asking. */}
                    {server.approvalRequest ? (
                      <ApprovalReview
                        requestId={server.approvalRequest.id}
                        title="Access review"
                        usage={usersPanel}
                        summary={<ServerSummary server={server} />}
                      />
                    ) : (
                      <>
                        {/* No dossier yet, so the review has no header to
                            share a row with — the strip stands alone until
                            the gather lands and the two-column row appears. */}
                        <ServerSummary server={server} />
                        <EnsureServerReview
                          canonicalServerUrl={server.canonicalServerUrl}
                        />
                        <div>
                          <Text variant="subheading">
                            Who is currently using it?
                          </Text>
                          <Text muted small>
                            Users with observed calls to this Shadow MCP server.
                          </Text>
                        </div>
                        {usersPanel}
                      </>
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
