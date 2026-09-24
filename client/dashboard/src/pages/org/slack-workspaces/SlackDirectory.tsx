import { syncInProgress } from "./syncView";
import { useEffect, useState } from "react";
import { keepPreviousData } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router";
import { Page } from "@/components/page-layout";
import { defineFilters, useFilterState } from "@/components/filters";
import { ApiErrorAlert } from "@/components/api-error-alert";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Heading } from "@/components/ui/Heading";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Switch } from "@/components/ui/Switch";
import { Table, type Column } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { useRBAC } from "@/hooks/useRBAC";
import { SlackPersonnelPicker } from "./SlackPersonnelPicker";
import type { OrganizationUser } from "@gram/client/models/components/organizationuser.js";
import { useListOrganizationUsers } from "@gram/client/react-query/listOrganizationUsers.js";
import { stateLabels, typeLabels } from "./memberLabels";
import type { SlackDirectoryConnection } from "@gram/client/models/components/slackdirectoryconnection.js";
import type { SlackDirectoryMember } from "@gram/client/models/components/slackdirectorymember.js";
import { useSlackDirectoryMembers } from "@gram/client/react-query/slackDirectoryMembers.js";
import { SESSION_SECURITY } from "../identity-provider/identityProviderQueries";
import { SlackSyncButton, SlackSyncStatus } from "./SlackSyncStatus";

const FILTERS = defineFilters([
  { id: "slack_workspace", label: "Workspace", kind: "select", pinned: true },
  {
    id: "slack_mapping",
    label: "Mapping status",
    kind: "select",
    pinned: true,
  },
]);
function memberColumns(
  people: OrganizationUser[] | undefined,
  canEdit: boolean,
): Column<SlackDirectoryMember>[] {
  return [
    {
      key: "displayName",
      header: "Slack identity",
      width: "2fr",
      render: (member) => (
        <div className="min-w-0 space-y-1">
          <Text className="break-words font-medium">
            {member.displayName || member.slackUserId}
          </Text>
          <Text muted small className="break-all">
            {member.email || "Not provided"}
          </Text>
        </div>
      ),
    },
    {
      key: "workspaceName",
      header: "Workspace",
      width: "1.3fr",
      render: (member) => (
        <Text small className="break-words">
          {member.workspaceName || member.workspaceId}
        </Text>
      ),
    },
    {
      key: "status",
      header: "Directory state",
      width: "1.4fr",
      render: (member) => (
        <div className="space-y-1">
          <Badge variant={member.status === "active" ? "success" : "neutral"}>
            {stateLabels[member.status]}
          </Badge>
          <Text muted small>
            {typeLabels[member.memberType]}
          </Text>
          {!member.observedInLastSync && (
            <Text muted small>
              Not seen in last sync
            </Text>
          )}
        </div>
      ),
    },
    {
      key: "mapping",
      header: "Personnel",
      width: "2fr",
      render: (member) => (
        <SlackPersonnelPicker
          member={member}
          people={people}
          canEdit={canEdit}
        />
      ),
    },
  ];
}

export function SlackDirectory({
  connections,
}: {
  connections: SlackDirectoryConnection[];
}): JSX.Element {
  const [params, setParams] = useSearchParams();
  const { values, setValue, clearValue, clearAll } = useFilterState(FILTERS);
  const includeGuests = params.get("slack_guests") === "true";
  const includeDeactivated = params.get("slack_deactivated") === "true";
  const includeBots = params.get("slack_bots") === "true";
  const setToggle = (key: string, on: boolean) => {
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous);
        if (on) next.set(key, "true");
        else next.delete(key);
        return next;
      },
      { replace: true },
    );
  };
  const [search, setSearch] = useState("");
  const connectionId = values.slack_workspace ?? undefined;
  const mappingStatus = values.slack_mapping as
    | SlackDirectoryMember["mappingStatus"]
    | null;
  // Remount pagination when the directory scope changes, including browser navigation.
  return (
    <section className="space-y-5" aria-label="Slack member directory">
      <Button variant="tertiary" size="sm" asChild>
        <Link to="?tab=slack-workspaces">Back to workspaces</Link>
      </Button>
      <div className="space-y-2">
        <Heading variant="h3">Slack members</Heading>
        <Text muted small>
          Map Slack accounts to existing personnel. Email addresses do not
          confirm a person’s identity. Mappings grant no new permissions.
        </Text>
      </div>
      <Page.Toolbar>
        <Page.Toolbar.Search
          className="w-64 max-w-full"
          value={search}
          onChange={setSearch}
          placeholder="Search name, email or Slack ID…"
          debounceMs={300}
        />
        <Page.Toolbar.Filters
          schema={FILTERS}
          values={values}
          optionsById={{
            slack_mapping: [
              { value: "unmapped", label: "Not mapped" },
              { value: "mapped", label: "Mapped" },
              { value: "needs_review", label: "Needs review" },
            ],
            slack_workspace: connections.map((c) => ({
              label: c.workspaceName || c.workspaceId,
              value: c.id,
            })),
          }}
          onChange={(id, value) => {
            if (
              (id === "slack_workspace" || id === "slack_mapping") &&
              (typeof value === "string" || value === null)
            )
              setValue(id, value);
          }}
          onClear={(id) => {
            if (id === "slack_workspace" || id === "slack_mapping")
              clearValue(id);
          }}
          onClearAll={clearAll}
        />
        <Page.Toolbar.Actions>
          <label className="flex cursor-pointer items-center gap-2 text-sm">
            <Switch
              checked={includeDeactivated}
              onCheckedChange={(on) => setToggle("slack_deactivated", on)}
            />
            Show deactivated members
          </label>
          <label className="flex cursor-pointer items-center gap-2 text-sm">
            <Switch
              checked={includeBots}
              onCheckedChange={(on) => setToggle("slack_bots", on)}
            />
            Show bots and apps
          </label>
          <label className="flex cursor-pointer items-center gap-2 text-sm">
            <Switch
              checked={includeGuests}
              onCheckedChange={(on) => setToggle("slack_guests", on)}
            />
            Show guests
          </label>
        </Page.Toolbar.Actions>
      </Page.Toolbar>
      <div className="divide-border divide-y border">
        {connections
          .filter((c) => !connectionId || c.id === connectionId)
          .map((c) => (
            <div
              key={c.id}
              className="flex flex-wrap items-center justify-between gap-3 p-4"
            >
              <div className="space-y-1">
                <h3 className="font-medium">
                  {c.workspaceName || c.workspaceId}
                </h3>
                <SlackSyncStatus connection={c} />
              </div>
              <SlackSyncButton connection={c} />
            </div>
          ))}
      </div>
      <MemberTable
        key={`${connectionId ?? "all"}:${mappingStatus ?? "all"}:${search}:${includeDeactivated}:${includeBots}:${includeGuests}`}
        connectionId={connectionId}
        mappingStatus={mappingStatus ?? undefined}
        search={search}
        includeDeactivated={includeDeactivated}
        includeBots={includeBots}
        includeGuests={includeGuests}
        syncing={connections.some(syncInProgress)}
      />
    </section>
  );
}

function MemberTable({
  connectionId,
  mappingStatus,
  search,
  includeDeactivated,
  includeBots,
  includeGuests,
  syncing,
}: {
  connectionId?: string;
  mappingStatus?: SlackDirectoryMember["mappingStatus"];
  search: string;
  includeDeactivated: boolean;
  includeBots: boolean;
  includeGuests: boolean;
  syncing: boolean;
}): JSX.Element {
  const { hasScope } = useRBAC();
  const canEdit = hasScope("org:admin");
  const people = useListOrganizationUsers(undefined, SESSION_SECURITY, {
    enabled: canEdit,
    retry: false,
    throwOnError: false,
  });
  const columns = memberColumns(people.data?.users, canEdit);
  // Pin the sort to the server's time for the first page so mapping someone does not move rows until a refresh.
  const [sortAsOf, setSortAsOf] = useState<Date | undefined>(undefined);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(50);
  const query = useSlackDirectoryMembers(
    {
      connectionId,
      mappingStatus,
      search,
      includeDeactivated,
      includeBots,
      includeGuests,
      sortAsOf,
      page,
      limit: pageSize,
    },
    SESSION_SECURITY,
    {
      retry: false,
      throwOnError: false,
      refetchInterval: syncing ? 3000 : 60000,
      // Keep rows on screen while the pinned sort time replaces the first request.
      placeholderData: keepPreviousData,
    },
  );
  const loadedSortAsOf = query.data?.sortAsOf;
  useEffect(() => {
    if (!sortAsOf && loadedSortAsOf) setSortAsOf(loadedSortAsOf);
  }, [sortAsOf, loadedSortAsOf]);
  const rows = query.data?.members ?? [];
  const total = query.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  // An edit under a mapping filter can shrink the result set below the current page.
  const outOfRange = Boolean(query.data) && page > totalPages;
  useEffect(() => {
    if (outOfRange) setPage(totalPages);
  }, [outOfRange, totalPages]);
  return (
    <div className="space-y-4" aria-busy={query.isFetching}>
      <ApiErrorAlert error={query.error} />
      {people.isError && (
        <div className="space-y-2">
          <ApiErrorAlert error={people.error} />
          <Button
            variant="secondary"
            size="sm"
            onClick={() => {
              void people.refetch();
            }}
          >
            Try loading people again
          </Button>
        </div>
      )}
      {query.isError && (
        <Button
          variant="secondary"
          onClick={() => {
            void query.refetch();
          }}
        >
          Try loading members again
        </Button>
      )}
      {query.isPending && <SkeletonTable />}
      {query.data && !query.isError && !outOfRange && rows.length === 0 && (
        <InlineEmptyState
          icon="users"
          heading={search ? "No matching members" : "No members to show"}
          description={
            search
              ? "Try another name, email or Slack ID."
              : "Sync a connected workspace to read its member directory."
          }
        />
      )}
      {rows.length > 0 && (
        <>
          <div className="overflow-x-auto">
            <Table
              columns={columns}
              data={rows}
              rowKey={(member) => member.id}
            />
          </div>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <Text muted small>
              {total.toLocaleString()} {total === 1 ? "member" : "members"} ·
              Page {page} of {totalPages}
            </Text>
            <div className="flex flex-wrap items-center gap-2">
              <Text muted small>
                Per page
              </Text>
              <Select
                value={String(pageSize)}
                onValueChange={(value) => {
                  setPageSize(Number(value));
                  setPage(1);
                }}
              >
                <SelectTrigger className="w-20" aria-label="Members per page">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {[25, 50, 100].map((size) => (
                    <SelectItem key={size} value={String(size)}>
                      {size}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button
                variant="secondary"
                size="sm"
                disabled={page <= 1 || query.isFetching}
                onClick={() => setPage((current) => current - 1)}
              >
                Previous
              </Button>
              <Button
                variant="secondary"
                size="sm"
                disabled={page >= totalPages || query.isFetching}
                onClick={() => setPage((current) => current + 1)}
              >
                Next
              </Button>
            </div>
          </div>
        </>
      )}
    </div>
  );
}
