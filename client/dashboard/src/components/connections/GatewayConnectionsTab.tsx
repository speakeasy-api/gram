import { useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { invalidateAllUserSessionClients } from "@gram/client/react-query/userSessionClients.js";
import { invalidateAllUserSessions } from "@gram/client/react-query/userSessions.js";
import {
  useGatewaySessionsInfinite,
  invalidateAllGatewaySessions,
} from "@gram/client/react-query/gatewaySessions.js";
import { useGatewaySessionFacets } from "@gram/client/react-query/gatewaySessionFacets.js";
import type { UserSession } from "@gram/client/models/components/usersession.js";
import {
  Sort,
  Status,
} from "@gram/client/models/operations/listgatewaysessions.js";
import {
  defineFilters,
  useFilterState,
  type FilterValue,
} from "@/components/filters";
import { getPresetRange } from "@/elements";
import { Page } from "@/components/page-layout";
import { IdentityLink } from "@/components/identity-link";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { Table, type Column } from "@/components/ui/Table";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { Button } from "@/components/ui/Button";
import { MoreActions, type Action } from "@/components/ui/MoreActions";
import { useRBAC } from "@/hooks/useRBAC";
import { RevokeSessionDialog } from "@/components/sessions/RevokeSessionDialog";
import { GatewayRegisteredClients } from "./GatewayRegisteredClients";
import { subjectLabel } from "@/lib/user-session-status";
import { providerLabel } from "@/lib/provider-label";

import { useSessionAgents } from "@/hooks/useSessionAgents";
import type { ResolvedSession } from "@/lib/session-agents";
import { ClientDetailSheet } from "@/components/sessions/ClientDetailSheet";
import {
  canonicalUserId,
  killswitchCreateHref,
  useKillswitchUserBadges,
} from "@/components/killswitch/KillswitchUserStatus";
import { KillswitchUserStatusIcon } from "@/components/killswitch/KillswitchUserStatusIcon";
import { useIdentityHrefBuilder } from "@/lib/useIdentityHref";

const FILTERS = defineFilters([
  {
    id: "server",
    label: "Server association",
    kind: "select",
    pinned: true,
    description:
      "An upstream account configured for this member server. This does not indicate historical usage.",
  },
  { id: "provider", label: "Provider", kind: "select" },
  {
    id: "status",
    label: "Status",
    kind: "select",
    pinned: true,
    allLabel: "Active",
  },
  { id: "date", label: "Created", kind: "daterange" },
]);
const SORT_OPTIONS = [
  { value: "newest", label: "Newest first" },
  { value: "oldest", label: "Oldest first" },
  { value: "last_used", label: "Last used" },
  { value: "expiry", label: "Authorization expires" },
];
const STATUS_OPTIONS = [
  { value: "all", label: "All statuses" },
  { value: "active", label: "Active" },
  { value: "expired", label: "Expired" },
  { value: "revoked", label: "Revoked" },
];
function timestamp(date: Date | null | undefined): string {
  return date ? date.toLocaleString() : "Unknown";
}

/** Both gateway surfaces read the same API; organization reads ignore the working project. */
export function GatewayConnectionsTab({
  gatewayId,
  issuerId,
  projectId,
}: {
  gatewayId?: string;
  issuerId?: string;
  projectId?: string;
}): JSX.Element {
  const [params, setParams] = useSearchParams();
  const { values, setValue, clearValue, clearAll } = useFilterState(FILTERS);
  const search = params.get("q") ?? "";
  const rawSort = params.get("sort");
  const sort =
    Object.values(Sort).find((value) => value === rawSort) ?? Sort.Newest;
  const status =
    Object.values(Status).find((value) => value === values.status) ??
    Status.Active;
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
  const sessions = useGatewaySessionsInfinite({
    gatewayId,
    search: search || undefined,
    status,
    sort,
    serverId: values.server ?? undefined,
    providerId: values.provider ?? undefined,
    createdAfter: dates.from,
    createdBefore: dates.to,
    limit: 50,
  });
  const facets = useGatewaySessionFacets({ gatewayId });
  const items = useSessionAgents(
    sessions.data?.pages.flatMap((page) => page.result.items) ?? [],
  );
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const onRevoked = () => {
    void invalidateAllGatewaySessions(queryClient, { refetchType: "all" });
    void invalidateAllUserSessionClients(queryClient, { refetchType: "all" });
    void invalidateAllUserSessions(queryClient, { refetchType: "all" });
  };
  const [registrationId, setRegistrationId] = useState<string | null>(null);
  const badges = useKillswitchUserBadges(
    items.flatMap((session) => {
      const userId = canonicalUserId(session.subjectUrn);
      return userId ? [userId] : [];
    }),
  );
  const identityAccessHref = useIdentityHrefBuilder("access");
  const killswitchHref = (session: UserSession) => {
    const userId = canonicalUserId(session.subjectUrn);
    return userId && badges.canAccess ? identityAccessHref({ userId }) : null;
  };
  const { hasScope } = useRBAC();
  // The project revoke API cascades within one project. Organization-wide
  // reads must never route a mutation through the selected project.
  const canRevoke = Boolean(
    gatewayId && projectId && hasScope("project:write", projectId),
  );
  const [revoking, setRevoking] = useState<UserSession | null>(null);
  const columns: Column<ResolvedSession>[] = [
    {
      key: "subjectUrn",
      header: "Identity",
      width: "2fr",
      render: (session) => (
        <div className="flex items-center gap-2">
          <IdentityLink
            identifier={
              session.subjectType === "user" ||
              session.subjectType === "apikey" ||
              session.subjectAgentId
                ? { urn: session.subjectUrn }
                : null
            }
          >
            {subjectLabel(session)}
          </IdentityLink>
          {killswitchHref(session) && (
            <KillswitchUserStatusIcon
              badge={badges.badges.get(canonicalUserId(session.subjectUrn)!)}
              unavailable={badges.unavailableUserIds.has(
                canonicalUserId(session.subjectUrn)!,
              )}
              href={killswitchHref(session)}
            />
          )}
        </div>
      ),
    },
    {
      key: "clientName",
      header: "Client",
      render: (session) => session.clientName ?? "Unknown",
    },
    {
      key: "upstreams",
      header: "Upstream accounts",
      width: "2fr",
      render: (session) =>
        session.upstreams.length ? (
          <div className="space-y-2">
            {session.upstreams.map((upstream) => (
              <div key={upstream.remoteSessionId}>
                <Text small>{providerLabel(upstream.issuerSlug)}</Text>
                <Text small muted>
                  Authorization expires:{" "}
                  {upstream.authorizationExpiresAt
                    ? timestamp(upstream.authorizationExpiresAt)
                    : "Not specified"}
                </Text>
              </div>
            ))}
          </div>
        ) : (
          <Text small muted>
            None
          </Text>
        ),
    },
    {
      key: "createdAt",
      header: "Created",
      render: (session) => timestamp(session.createdAt),
    },
    {
      key: "lastUsedAt",
      header: "Last used",
      render: (session) => timestamp(session.lastUsedAt),
    },
    {
      key: "refreshExpiresAt",
      header: "Authorization expires",
      render: (session) => timestamp(session.refreshExpiresAt),
    },
    {
      key: "status",
      header: "Status",
      render: (session) =>
        session.revokedAt
          ? "Revoked"
          : session.refreshExpiresAt.getTime() > Date.now()
            ? "Active"
            : "Expired",
    },
    {
      key: "actions",
      header: "",
      width: "48px",
      render: (session) => {
        const accessHref = killswitchHref(session);
        const actions: Action[] = [];
        if (gatewayId && projectId && session.userSessionClientId) {
          actions.push({
            label: "View registration",
            onClick: () => setRegistrationId(session.userSessionClientId!),
          });
        }
        if (accessHref) {
          actions.push(
            {
              label: "View killswitches",
              onClick: () => void navigate(accessHref),
            },
            {
              label: "New killswitch…",
              onClick: () =>
                void navigate(
                  killswitchCreateHref(accessHref, {
                    capabilityKey: "mcp_tool_calls",
                  }),
                ),
            },
          );
        }
        if (canRevoke && !session.revokedAt) {
          actions.push({
            label: "Revoke session",
            destructive: true,
            onClick: () => setRevoking(session),
          });
        }
        return actions.length ? <MoreActions actions={actions} /> : null;
      },
    },
  ];
  return (
    <div className="space-y-6 pb-24">
      {badges.loader}
      <Text small muted>
        Sessions authorized for this gateway and their upstream accounts.
        Issuer-scoped sessions can authorize multiple endpoints. Server
        associations describe configuration, not usage.
      </Text>
      <Page.Toolbar>
        <Page.Toolbar.Search
          value={search}
          onChange={(value) => setParam("q", value)}
          debounceMs={300}
          placeholder="Search identities…"
        />
        <Page.Toolbar.Filters
          schema={FILTERS}
          values={values}
          onChange={setValue as (id: string, value: FilterValue) => void}
          onClear={clearValue as (id: string) => void}
          onClearAll={clearAll}
          optionsById={{
            server: facets.data?.servers ?? [],
            provider: facets.data?.providers ?? [],
            status: STATUS_OPTIONS,
          }}
        />
        <Page.Toolbar.SortBy
          value={sort}
          onChange={(value) => setParam("sort", value)}
          options={SORT_OPTIONS}
        />
        <Page.Toolbar.Refresh
          onRefresh={() => void sessions.refetch()}
          isRefreshing={sessions.isFetching}
        />
      </Page.Toolbar>
      {facets.isError && (
        <div className="flex items-center gap-3">
          <Text small>Couldn’t load filter choices.</Text>
          <Button
            variant="tertiary"
            size="sm"
            onClick={() => void facets.refetch()}
          >
            Retry filters
          </Button>
        </div>
      )}
      {sessions.isPending ? (
        <SkeletonTable />
      ) : sessions.isError && !sessions.data ? (
        <InlineEmptyState
          icon="unplug"
          heading="Couldn’t load connections"
          description="Retry to load gateway sessions."
          action={
            <Button variant="secondary" onClick={() => void sessions.refetch()}>
              Retry
            </Button>
          }
        />
      ) : (
        <>
          <div className="overflow-x-auto">
            <Table
              className="min-w-6xl"
              columns={columns}
              data={items}
              rowKey={(session) => session.id}
              noResultsMessage={<Text>No sessions match these filters.</Text>}
            />
          </div>
          <Text small muted>
            {items.length} sessions loaded
            {sessions.hasNextPage ? " · More available" : ""}
          </Text>
          {sessions.isError && (
            <Text small>
              Couldn’t load sessions. Your loaded results are still shown.
            </Text>
          )}
          {(sessions.hasNextPage || sessions.isError) && (
            <div className="flex justify-center">
              <Button
                variant="tertiary"
                size="sm"
                disabled={sessions.isFetching}
                onClick={() =>
                  void (sessions.isError && !sessions.isFetchNextPageError
                    ? sessions.refetch()
                    : sessions.fetchNextPage())
                }
              >
                {sessions.isFetching
                  ? "Loading…"
                  : sessions.isError
                    ? "Retry"
                    : "Load more"}
              </Button>
            </div>
          )}
        </>
      )}
      {gatewayId && issuerId && projectId && (
        <GatewayRegisteredClients
          issuerId={issuerId}
          projectId={projectId}
          onRevoked={onRevoked}
        />
      )}
      {registrationId && (
        <ClientDetailSheet
          clientId={registrationId}
          open
          onOpenChange={(open) => {
            if (!open) setRegistrationId(null);
          }}
        />
      )}
      {revoking && (
        <RevokeSessionDialog
          session={revoking}
          open
          onOpenChange={(open) => {
            if (!open) setRevoking(null);
          }}
          onRevoked={onRevoked}
        />
      )}
    </div>
  );
}
