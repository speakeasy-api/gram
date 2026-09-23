import { syncInProgress } from "./syncView";
import { useState } from "react";
import { Link } from "react-router";
import { Page } from "@/components/page-layout";
import { defineFilters, useFilterState } from "@/components/filters";
import { ApiErrorAlert } from "@/components/api-error-alert";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Heading } from "@/components/ui/Heading";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Table, type Column } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { HumanizeDateTime } from "@/lib/dates";
import type { SlackDirectoryConnection } from "@gram/client/models/components/slackdirectoryconnection.js";
import type { SlackDirectoryMember } from "@gram/client/models/components/slackdirectorymember.js";
import { useSlackDirectoryMembers } from "@gram/client/react-query/slackDirectoryMembers.js";
import { SESSION_SECURITY } from "../identity-provider/identityProviderQueries";
import { SlackSyncButton, SlackSyncStatus } from "./SlackSyncStatus";

const FILTERS = defineFilters([
  { id: "slack_workspace", label: "Workspace", kind: "select", pinned: true },
]);
const typeLabels: Record<SlackDirectoryMember["memberType"], string> = {
  person: "Member",
  guest: "Guest",
  single_channel_guest: "Single-channel guest",
  bot: "Bot / app",
  unknown: "Unknown",
};
const stateLabels: Record<SlackDirectoryMember["status"], string> = {
  active: "Active",
  deactivated: "Deactivated",
  invited: "Invited",
  unknown: "Unknown",
};
const columns: Column<SlackDirectoryMember>[] = [
  {
    key: "displayName",
    header: "Slack member",
    width: "2fr",
    render: (member) => (
      <div className="min-w-0">
        <Text className="break-words font-medium">
          {member.displayName || member.slackUserId}
        </Text>
        <Text muted small className="font-mono">
          {member.slackUserId}
        </Text>
      </div>
    ),
  },
  {
    key: "email",
    header: "Email",
    width: "2fr",
    render: (member) => (
      <Text small className="break-all">
        {member.email || "Not provided"}
      </Text>
    ),
  },
  {
    key: "workspaceName",
    header: "Workspace",
    width: "1.5fr",
    render: (member) => (
      <Text small className="break-words">
        {member.workspaceName || member.workspaceId}
      </Text>
    ),
  },
  {
    key: "memberType",
    header: "Type",
    width: "1fr",
    render: (member) => <Text small>{typeLabels[member.memberType]}</Text>,
  },
  {
    key: "status",
    header: "Status",
    width: "1.5fr",
    render: (member) => (
      <div className="space-y-1">
        <Badge variant={member.status === "active" ? "success" : "neutral"}>
          {stateLabels[member.status]}
        </Badge>
        {!member.observedInLastSync && (
          <Text muted small>
            Not seen in last sync
          </Text>
        )}
      </div>
    ),
  },
  {
    key: "lastSeenAt",
    header: "Last seen",
    width: "1fr",
    render: (member) => (
      <Text muted small>
        <HumanizeDateTime date={member.lastSeenAt} />
      </Text>
    ),
  },
];

export function SlackDirectory({
  connections,
}: {
  connections: SlackDirectoryConnection[];
}): JSX.Element {
  const { values, setValue, clearValue, clearAll } = useFilterState(FILTERS);
  const [search, setSearch] = useState("");
  const connectionId = values.slack_workspace ?? undefined;
  // Remount pagination when the directory scope changes, including browser navigation.
  return (
    <section className="space-y-5" aria-label="Slack member directory">
      <Button variant="tertiary" size="sm" asChild>
        <Link to="?tab=slack-workspaces">Back to workspaces</Link>
      </Button>
      <div className="space-y-2">
        <Heading variant="h3">Slack members</Heading>
        <Text muted small>
          Workspace profiles only. Email addresses do not confirm a person’s
          identity. External Slack Connect users are excluded.
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
            slack_workspace: connections.map((c) => ({
              label: c.workspaceName || c.workspaceId,
              value: c.id,
            })),
          }}
          onChange={(id, value) => {
            if (
              id === "slack_workspace" &&
              (typeof value === "string" || value === null)
            )
              setValue("slack_workspace", value);
          }}
          onClear={() => clearValue("slack_workspace")}
          onClearAll={clearAll}
        />
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
        key={`${connectionId ?? "all"}:${search}:${connections.map((c) => c.lastFullSyncSucceededAt ?? "").join(",")}`}
        connectionId={connectionId}
        search={search}
        syncing={connections.some(syncInProgress)}
      />
    </section>
  );
}

function MemberTable({
  connectionId,
  search,
  syncing,
}: {
  connectionId?: string;
  search: string;
  syncing: boolean;
}): JSX.Element {
  const [cursors, setCursors] = useState<Array<string | undefined>>([
    undefined,
  ]);
  const cursor = cursors[cursors.length - 1];
  const query = useSlackDirectoryMembers(
    { connectionId, search, cursor, limit: 50 },
    SESSION_SECURITY,
    {
      retry: false,
      throwOnError: false,
      refetchInterval: syncing ? 3000 : 60000,
    },
  );
  const rows = query.data?.members ?? [];
  return (
    <div className="space-y-4" aria-busy={query.isFetching}>
      <ApiErrorAlert error={query.error} />
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
      {query.data && !query.isError && rows.length === 0 && (
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
              {query.data?.total.toLocaleString()} matching members · page{" "}
              {cursors.length}
            </Text>
            <div className="flex gap-2">
              <Button
                variant="secondary"
                size="sm"
                disabled={cursors.length <= 1 || query.isFetching}
                onClick={() => setCursors((previous) => previous.slice(0, -1))}
              >
                Previous
              </Button>
              <Button
                variant="secondary"
                size="sm"
                disabled={!query.data?.nextCursor || query.isFetching}
                onClick={() =>
                  setCursors((previous) => [
                    ...previous,
                    query.data?.nextCursor,
                  ])
                }
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
