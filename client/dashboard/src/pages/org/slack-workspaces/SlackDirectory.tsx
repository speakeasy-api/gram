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
import { useRBAC } from "@/hooks/useRBAC";
import { SlackMappingDialog } from "./SlackMappingDialog";
import { MappingStatus, PersonnelAvatar } from "./MappingStatus";
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
  onEdit: (id: string) => void,
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
      width: "1.6fr",
      render: (member) => {
        const mapping = member.mapping;
        return (
          <Button
            variant="tertiary"
            size="sm"
            className="h-auto max-w-full justify-start whitespace-normal text-left"
            disabled={!canEdit || (member.memberType === "bot" && !mapping)}
            aria-label={`Change mapping for ${member.displayName || member.slackUserId} in ${member.workspaceName || member.workspaceId}`}
            onClick={() => onEdit(member.id)}
          >
            {mapping && (
              <Button.LeftIcon>
                <PersonnelAvatar
                  name={mapping.displayName}
                  email={mapping.email}
                  photoUrl={mapping.photoUrl}
                />
              </Button.LeftIcon>
            )}
            <Button.Text>
              {mapping ? mapping.displayName || mapping.email : "Not mapped"}
            </Button.Text>
          </Button>
        );
      },
    },
    {
      key: "mappingStatus",
      header: "Mapping status",
      width: "1.6fr",
      render: (member) => <MappingStatus member={member} />,
    },
  ];
}

export function SlackDirectory({
  connections,
}: {
  connections: SlackDirectoryConnection[];
}): JSX.Element {
  const { values, setValue, clearValue, clearAll } = useFilterState(FILTERS);
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
        key={`${connectionId ?? "all"}:${mappingStatus ?? "all"}:${search}`}
        connectionId={connectionId}
        mappingStatus={mappingStatus ?? undefined}
        search={search}
        syncing={connections.some(syncInProgress)}
      />
    </section>
  );
}

function MemberTable({
  connectionId,
  mappingStatus,
  search,
  syncing,
}: {
  connectionId?: string;
  mappingStatus?: SlackDirectoryMember["mappingStatus"];
  search: string;
  syncing: boolean;
}): JSX.Element {
  const { hasScope } = useRBAC();
  const [editing, setEditing] = useState<string | null>(null);
  const columns = memberColumns(setEditing, hasScope("org:admin"));
  const [cursors, setCursors] = useState<Array<string | undefined>>([
    undefined,
  ]);
  const cursor = cursors[cursors.length - 1];
  const query = useSlackDirectoryMembers(
    { connectionId, mappingStatus, search, cursor, limit: 50 },
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
      {editing && (
        <SlackMappingDialog id={editing} onClose={() => setEditing(null)} />
      )}
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
