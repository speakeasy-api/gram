import { useSearchParams } from "react-router";
import { Page } from "@/components/page-layout";
import {
  defineFilters,
  useFilterState,
  type FilterValue,
} from "@/components/filters";
import { getPresetRange } from "@/elements";
import type { ListOrganizationRemoteSessionClientSessionsRequest } from "@gram/client/models/operations/listorganizationremotesessionclientsessions.js";
import { IdentityLink } from "@/components/identity-link";
import { RequireScope } from "@/components/require-scope";
import { TableRowContextMenu } from "@/components/table-row-context-menu";
import { Table, type Column } from "@/components/ui/Table";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { MoreActions, type Action } from "@/components/ui/MoreActions";
import { Text } from "@/components/ui/Text";
import { useRBAC } from "@/hooks/useRBAC";
import { useReadableAgents } from "@/hooks/useReadableAgents";
import type { RemoteSession } from "@gram/client/models/components/remotesession.js";
import {
  invalidateAllOrganizationRemoteSessionClientSessions,
  useOrganizationRemoteSessionClientSessionsInfinite,
} from "@gram/client/react-query/organizationRemoteSessionClientSessions.js";
import { useRefreshOrganizationRemoteSessionMutation } from "@gram/client/react-query/refreshOrganizationRemoteSession.js";
import { useRevokeOrganizationRemoteSessionMutation } from "@gram/client/react-query/revokeOrganizationRemoteSession.js";
import { Button } from "@/components/ui/Button";
import { ActingPrincipal } from "@/components/ui/ActingPrincipal";
import { useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { RevokeAllSessionsDialog } from "../../clientDialogs";
import { formatTimestamp } from "./formatTimestamp";

const SESSION_FILTERS = defineFilters([
  { id: "status", label: "Status", kind: "select", pinned: true },
  { id: "date", label: "Created", kind: "daterange" },
]);
const SORT_OPTIONS = [
  { value: "newest", label: "Newest first" },
  { value: "oldest", label: "Oldest first" },
  { value: "last_used", label: "Last used" },
  { value: "expiry", label: "Authorization expires" },
];

export function SessionsTab({ clientId }: { clientId: string }): JSX.Element {
  const queryClient = useQueryClient();
  const { hasAnyScope } = useRBAC();
  const canManage = hasAnyScope(["org:admin"]);
  const [params, setParams] = useSearchParams();
  const { values, setValue, clearValue, clearAll } =
    useFilterState(SESSION_FILTERS);
  const search = params.get("q") ?? "";
  const rawSort = params.get("sort");
  const sort = (
    SORT_OPTIONS.some((option) => option.value === rawSort) ? rawSort : "newest"
  ) as ListOrganizationRemoteSessionClientSessionsRequest["sort"];
  const status = (
    values.status === "active" || values.status === "expired"
      ? values.status
      : "all"
  ) as ListOrganizationRemoteSessionClientSessionsRequest["status"];
  const setParam = (key: string, value: string) =>
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous);
        if (value) next.set(key, value);
        else next.delete(key);
        return next;
      },
      { replace: true },
    );
  const dates = useMemo(() => {
    if (values.date.customRange) return values.date.customRange;
    if (values.date.preset) return getPresetRange(values.date.preset);
    return { from: undefined, to: undefined };
  }, [values.date]);
  const sessions = useOrganizationRemoteSessionClientSessionsInfinite({
    clientId,
    search: search || undefined,
    sort,
    status,
    createdAfter: dates.from,
    createdBefore: dates.to,
  });
  const sessionItems =
    sessions.data?.pages.flatMap((page) => page.result.items) ?? [];
  const [showRevokeAll, setShowRevokeAll] = useState(false);
  // An agent id off a session is not permission to open that agent, so the
  // roster the management API is willing to return decides which rows link.
  const agents = useReadableAgents(
    sessionItems.some((session) => session.subjectUrn.startsWith("agent:")),
  );
  const agentsByUrn = useMemo(
    () =>
      new Map(
        (agents.isError ? [] : (agents.data ?? [])).map((agent) => [
          `agent:${agent.id}`,
          agent,
        ]),
      ),
    [agents.data, agents.isError],
  );

  const revoke = useRevokeOrganizationRemoteSessionMutation({
    onSuccess: async () => {
      await invalidateAllOrganizationRemoteSessionClientSessions(queryClient, {
        refetchType: "all",
      });
      toast.success("Session revoked");
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to revoke session",
      );
    },
  });

  const refresh = useRefreshOrganizationRemoteSessionMutation({
    onSuccess: async () => {
      await invalidateAllOrganizationRemoteSessionClientSessions(queryClient, {
        refetchType: "all",
      });
      toast.success("Session refreshed");
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to refresh session",
      );
    },
  });

  function sessionActions(session: RemoteSession): Action[] {
    if (!canManage) return [];
    return [
      ...(session.hasRefreshToken
        ? [
            {
              label: "Refresh now",
              disabled: refresh.isPending,
              onClick: () => refresh.mutate({ request: { id: session.id } }),
            },
          ]
        : []),
      {
        label: "Revoke session",
        destructive: true,
        disabled: revoke.isPending,
        onClick: () => revoke.mutate({ request: { id: session.id } }),
      },
    ];
  }

  const columns: Column<RemoteSession>[] = [
    {
      key: "subjectUrn",
      header: "Identity",
      width: "2fr",
      render: (session) => (
        <Text small as="div" className="break-all">
          {session.subjectUrn.startsWith("agent:") ? (
            // No authorizer on a RemoteSession, so the agent
            // stands alone — but it is named and linked now,
            // where it used to fall through to a raw urn. Linked
            // only once the roster returns it: an agent the
            // viewer may not read would offer a page they
            // cannot open.
            <ActingPrincipal
              agent={{
                id: session.subjectUrn.slice("agent:".length),
                name:
                  agentsByUrn.get(session.subjectUrn)?.name ??
                  session.subjectDisplayName ??
                  undefined,
              }}
              linked={agentsByUrn.has(session.subjectUrn)}
            />
          ) : (
            <IdentityLink
              // Session subjects can also be anonymous, which
              // names no subject the identity resolver can
              // address; those stay plain text.
              identifier={
                session.subjectUrn.startsWith("user:") ||
                session.subjectUrn.startsWith("apikey:")
                  ? { urn: session.subjectUrn }
                  : null
              }
            >
              {session.subjectDisplayName ??
                session.subjectEmail ??
                session.subjectUrn}
            </IdentityLink>
          )}
        </Text>
      ),
    },
    {
      key: "createdAt",
      header: "Created",
      render: (session) => (
        <Text small muted>
          {formatTimestamp(session.createdAt)}
        </Text>
      ),
    },
    {
      key: "lastUsedAt",
      header: "Last used",
      render: (session) => (
        <Text small muted>
          {formatTimestamp(session.lastUsedAt)}
        </Text>
      ),
    },
    {
      key: "authorizationExpiresAt",
      header: "Authorization expires",
      render: (session) => (
        <Text small muted>
          {formatTimestamp(session.authorizationExpiresAt)}
        </Text>
      ),
    },
    {
      key: "refreshExpiresAt",
      header: "Refresh expires",
      render: (session) => (
        <Text small muted>
          {formatTimestamp(session.refreshExpiresAt)}
        </Text>
      ),
    },
    {
      key: "accessExpiresAt",
      header: "Access expires",
      render: (session) => (
        <Text small muted>
          {formatTimestamp(session.accessExpiresAt)}
        </Text>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "60px",
      render: (session) => (
        <RequireScope scope="org:admin" level="section">
          <MoreActions
            actions={sessionActions(session)}
            triggerAriaLabel="Session actions"
          />
        </RequireScope>
      ),
    },
  ];

  let body: JSX.Element;
  if (sessions.isLoading) {
    body = <SkeletonTable />;
  } else if (sessions.isError && !sessions.data) {
    body = (
      <InlineEmptyState
        icon="triangle-alert"
        heading="Failed to load sessions"
        action={
          <Button
            onClick={() => void sessions.refetch()}
            disabled={sessions.isFetching}
          >
            Retry
          </Button>
        }
      />
    );
  } else {
    body = (
      <div className="overflow-x-auto">
        <Table
          className="min-w-3xl"
          columns={columns}
          data={sessionItems}
          rowKey={(session) => session.id}
          noResultsMessage="No sessions match these filters."
          renderRow={(session, row) => (
            <TableRowContextMenu actions={sessionActions(session)}>
              {row}
            </TableRowContextMenu>
          )}
        />
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4 pb-24">
      <Page.Toolbar>
        <Page.Toolbar.Search
          value={search}
          onChange={(value) => setParam("q", value)}
          placeholder="Search identities…"
          debounceMs={300}
        />
        <Page.Toolbar.Filters
          schema={SESSION_FILTERS}
          values={values}
          optionsById={{
            status: [
              { value: "active", label: "Active" },
              { value: "expired", label: "Expired" },
            ],
          }}
          onChange={setValue as (id: string, value: FilterValue) => void}
          onClear={clearValue as (id: string) => void}
          onClearAll={clearAll}
        />
        <Page.Toolbar.SortBy
          value={sort ?? "newest"}
          onChange={(value) => setParam("sort", value)}
          options={SORT_OPTIONS}
        />
        <Page.Toolbar.Refresh
          onRefresh={() => void sessions.refetch()}
          isRefreshing={sessions.isFetching}
        />
      </Page.Toolbar>
      <div className="flex justify-end">
        <RequireScope scope="org:admin" level="component">
          <Button
            variant="destructive-primary"
            size="sm"
            onClick={() => setShowRevokeAll(true)}
            disabled={sessionItems.length === 0}
          >
            <Button.Text>Revoke all sessions</Button.Text>
          </Button>
        </RequireScope>
      </div>

      {body}

      {sessions.isError && sessions.data && (
        <div
          role="alert"
          className="flex flex-wrap items-center justify-center gap-3"
        >
          <Text className="text-destructive">
            {sessions.isFetchNextPageError
              ? "Failed to load more sessions."
              : "Failed to refresh sessions."}
          </Text>
          <Button
            variant="secondary"
            size="sm"
            disabled={sessions.isFetching}
            onClick={() => {
              if (sessions.isFetchNextPageError) void sessions.fetchNextPage();
              else void sessions.refetch();
            }}
          >
            <Button.Text>Retry</Button.Text>
          </Button>
        </div>
      )}
      {sessions.hasNextPage && !sessions.isError && (
        <div className="flex justify-center">
          <Button
            variant="tertiary"
            size="sm"
            disabled={sessions.isFetching}
            onClick={() => void sessions.fetchNextPage()}
          >
            <Button.Text>
              {sessions.isFetchingNextPage ? "Loading…" : "Load more"}
            </Button.Text>
          </Button>
        </div>
      )}

      {showRevokeAll && (
        <RevokeAllSessionsDialog
          clientId={clientId}
          onClose={() => setShowRevokeAll(false)}
        />
      )}
    </div>
  );
}
