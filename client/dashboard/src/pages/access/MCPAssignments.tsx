import { defineFilters, useFilterState } from "@/components/filters";
import { Page } from "@/components/page-layout";
import { ProjectIcon } from "@/components/project-icon";
import { Link } from "react-router";
import { useRoutes } from "@/routes";
import { AssignmentServerIcon } from "./AssignmentServerIcon";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
  TooltipProvider,
} from "@/components/ui/Tooltip";
import { Info, Pencil, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/Button";
import { useTable } from "@/components/ui/Table/context/context";
import { RoleLink } from "@/components/role-link";
import { useExplainResourceAccess } from "@gram/client/react-query/explainResourceAccess.js";
import { directorySourceLabel } from "../mcp/access/explainAccess";
import { Table, type Column } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { allMemberRoleIds } from "@/lib/member-roles";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { AccessibleMCPServer } from "@gram/client/models/components/accessiblemcpserver.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { RequireScope } from "@/components/require-scope";
import { ChangeRoleDialog } from "./ChangeRoleDialog";
import { RemoveAssignmentDialog } from "./RemoveAssignmentDialog";

const FILTERS = defineFilters([
  { id: "server", label: "MCP server", kind: "select", pinned: true },
  { id: "role", label: "Role", kind: "select", pinned: true },
  { id: "department", label: "Department", kind: "select", pinned: true },
]);

type AssignmentRow = { member: AccessMember; servers: AccessibleMCPServer[] };

/** Shared organization-wide review, independent of the selected project. */
export function MCPAssignments({
  onReady,
}: {
  onReady?: (ready: boolean) => void;
}): JSX.Element {
  const organization = useOrganization();
  const sdk = useSdkClient();
  const members = useMembers(undefined, undefined, { throwOnError: false });
  const roles = useRoles(undefined, undefined, { throwOnError: false });
  const [editingMember, setEditingMember] = useState<AccessMember | null>(null);
  const [search, setSearch] = useState("");
  const [sortBy, setSortBy] = useState("servers");
  const [direction, setDirection] = useState<"asc" | "desc">("desc");
  const { values, setValue, clearValue, clearAll } = useFilterState(FILTERS);
  // A refetch timestamp is not a membership change. Access editors also read
  // members, so keying by that timestamp would unmount them as soon as opened.
  const assignments = useQuery<AssignmentRow[]>({
    queryKey: [
      "mcpAssignments",
      organization.id,
      "connect",
      members.data?.members
        .map((member) => ({
          id: member.id,
          name: member.name,
          email: member.email,
          roles: allMemberRoleIds(member).sort(),
          directRoles: member.roleIds?.toSorted(),
          directoryRoles: member.directoryRoleIds?.toSorted(),
          department: member.department,
          groups: member.groups?.toSorted(),
        }))
        .sort((a, b) => a.id.localeCompare(b.id)),
    ],
    placeholderData: (previous, previousQuery) =>
      previousQuery?.queryKey[1] === organization.id ? previous : undefined,
    enabled: members.isSuccess,
    throwOnError: false,
    queryFn: async ({ signal }): Promise<AssignmentRow[]> => {
      const people = members.data?.members ?? [];
      const result: AssignmentRow[] = [];
      // Bound request concurrency even for large directories. An incomplete
      // result must never look like a completed access review.
      for (let i = 0; i < people.length; i += 6) {
        signal.throwIfAborted();
        const batch = await Promise.all(
          people.slice(i, i + 6).map(async (member) => {
            const access = await sdk.access.listIdentityAccess(
              { userId: member.id },
              undefined,
              { signal },
            );
            const servers: AccessibleMCPServer[] = [];
            // Identity access includes view/manage-only servers; connect has
            // its own exclusions and must be resolved independently.
            for (const server of access.servers) {
              const explanation = await sdk.access.explainResourceAccess(
                {
                  resourceKind: "mcp",
                  resourceId: server.id,
                  userId: member.id,
                },
                undefined,
                { signal },
              );
              if (
                explanation.levels.some(
                  (level) => level.level === "use" && level.allowed,
                )
              )
                servers.push(server);
            }
            return { member, servers };
          }),
        );
        result.push(...batch);
      }
      return result;
    },
  });
  const ready =
    members.isSuccess &&
    roles.isSuccess &&
    assignments.isSuccess &&
    !members.isFetching &&
    !roles.isFetching &&
    !assignments.isFetching &&
    !assignments.isPlaceholderData;
  useEffect(() => {
    onReady?.(ready);
    return () => onReady?.(false);
  }, [onReady, ready]);

  const serverOptions = useMemo(() => {
    const servers = new Map(
      (assignments.data ?? [])
        .flatMap((row) => row.servers)
        .map((server) => [server.id, server]),
    );
    return [...servers.values()]
      .map((server) => ({
        value: server.id,
        label: `${server.name || "Unnamed server"} (${server.projectSlug === "default" ? "Default project" : `Project: ${server.projectSlug}`})`,
      }))
      .sort((a, b) => a.label.localeCompare(b.label));
  }, [assignments.data]);
  const rows = useMemo(
    () =>
      (assignments.data ?? [])
        .filter(({ member, servers }) => {
          const query = search.trim().toLowerCase();
          return (
            (!query ||
              `${member.name} ${member.email}`.toLowerCase().includes(query)) &&
            (!values.server ||
              servers.some((server) => server.id === values.server)) &&
            (!values.role || allMemberRoleIds(member).includes(values.role)) &&
            (!values.department || member.department === values.department)
          );
        })
        .sort(
          (a, b) =>
            (sortBy === "name"
              ? direction === "desc"
                ? b.member.name.localeCompare(a.member.name)
                : a.member.name.localeCompare(b.member.name)
              : direction === "desc"
                ? b.servers.length - a.servers.length
                : a.servers.length - b.servers.length) ||
            a.member.name.localeCompare(b.member.name),
        ),
    [assignments.data, search, values, direction, sortBy],
  );

  const columns: Column<AssignmentRow>[] = [
    {
      key: "person",
      header: "Person",
      width: "260px",
      render: ({ member }) => (
        <div className="min-w-0">
          <Text className="truncate font-medium">
            {member.name || member.email}
          </Text>
          <Text small muted className="truncate">
            {member.email}
          </Text>
        </div>
      ),
    },
    {
      key: "servers",
      header: "MCP servers",
      width: "2fr",
      render: ({ servers }) => (
        <div className="flex items-center gap-3">
          <span className="tabular-nums">{servers.length}</span>
          <div className="flex -space-x-2">
            {servers.slice(0, 8).map((server) => (
              <Tooltip
                key={server.id}
                delayDuration={0}
                disableHoverableContent
              >
                <TooltipTrigger asChild>
                  <span
                    tabIndex={0}
                    aria-label={server.name || "Unnamed server"}
                    className="relative rounded-full focus:z-10 hover:z-10"
                  >
                    <AssignmentServerIcon
                      server={server}
                      className="border-background border-2"
                    />
                  </span>
                </TooltipTrigger>
                <TooltipContent>
                  <span className="flex items-center gap-2">
                    <ProjectIcon
                      project={{
                        id: server.projectId,
                        name:
                          organization.projects?.find(
                            (project) => project.id === server.projectId,
                          )?.name ||
                          (server.projectSlug === "default"
                            ? "Default"
                            : server.projectSlug),
                      }}
                      className="size-5 text-[10px]"
                    />
                    {server.name || "Unnamed server"}
                  </span>
                </TooltipContent>
              </Tooltip>
            ))}
            {servers.length > 8 && (
              <span className="bg-muted flex size-8 items-center justify-center rounded-full text-xs">
                +{servers.length - 8}
              </span>
            )}
          </div>
        </div>
      ),
    },
    {
      key: "roles",
      header: "Roles",
      width: "2fr",
      render: ({ member }) => (
        <div className="flex flex-wrap gap-1.5 text-sm">
          {allMemberRoleIds(member).map((id) => (
            <span key={id} className="bg-muted px-2 py-1">
              {roles.data?.roles.find((role) => role.id === id)?.name ||
                "Role unavailable"}
            </span>
          ))}
        </div>
      ),
    },
    {
      key: "editRoles",
      header: <span className="sr-only">Edit roles</span>,
      width: "56px",
      render: ({ member }) => (
        <ExpandedRowOnly memberId={member.id}>
          <RequireScope scope="org:admin" level="component">
            <Tooltip delayDuration={0}>
              <TooltipTrigger asChild>
                <Button
                  variant="tertiary"
                  size="sm"
                  aria-label={`Edit roles for ${member.name || member.email}`}
                  disabled={organization.scimEnabled}
                  onClick={(event) => {
                    event.stopPropagation();
                    setEditingMember(member);
                  }}
                >
                  <Button.LeftIcon>
                    <Pencil className="size-4" />
                  </Button.LeftIcon>
                </Button>
              </TooltipTrigger>
              <TooltipContent>
                {organization.scimEnabled
                  ? "Roles are managed by your identity provider"
                  : "Edit roles"}
              </TooltipContent>
            </Tooltip>
          </RequireScope>
        </ExpandedRowOnly>
      ),
    },
  ];
  const failed = members.isError || assignments.isError || roles.isError;
  return (
    <TooltipProvider>
      <div className="space-y-5">
        <Text muted small>
          See which MCP servers each person can connect to across all projects
          (mcp:connect). View-only and manage-only access is excluded. Expand a
          person to see where their connection access comes from or remove it.
        </Text>
        <Page.Toolbar>
          <Page.Toolbar.Search
            value={search}
            onChange={setSearch}
            placeholder="Search people by name or email"
          />
          <Page.Toolbar.Filters
            schema={FILTERS}
            values={values}
            onChange={(id, value) => {
              if (
                (id === "server" || id === "role" || id === "department") &&
                (typeof value === "string" || value === null)
              )
                setValue(id, value);
            }}
            onClear={(id) => {
              if (id === "server" || id === "role" || id === "department")
                clearValue(id);
            }}
            onClearAll={clearAll}
            optionsById={{
              server: serverOptions,
              role: (roles.data?.roles ?? []).map((role) => ({
                value: role.id,
                label: role.name,
              })),
              department: [
                ...new Set(
                  (members.data?.members ?? []).flatMap((member) =>
                    member.department ? [member.department] : [],
                  ),
                ),
              ]
                .sort()
                .map((value) => ({ value, label: value })),
            }}
          />
          <Page.Toolbar.SortBy
            value={sortBy}
            onChange={setSortBy}
            options={[
              { value: "servers", label: "Servers assigned" },
              { value: "name", label: "Name" },
            ]}
            direction={direction}
            onDirectionChange={setDirection}
          />
          <Page.Toolbar.Actions>
            <Text small muted>
              {assignments.isSuccess
                ? `${rows.length} people`
                : assignments.isError
                  ? "Review unavailable"
                  : "Loading people…"}
            </Text>
          </Page.Toolbar.Actions>
          <Page.Toolbar.Refresh
            onRefresh={() =>
              void Promise.all([
                members.refetch(),
                roles.refetch(),
                assignments.refetch(),
              ])
            }
            isRefreshing={members.isFetching || assignments.isFetching}
          />
        </Page.Toolbar>
        {failed ? (
          <div role="alert" className="space-y-3">
            <Text>
              Couldn’t load the complete assignment review. This review requires
              permission to view every assigned MCP server. Check your access
              and retry before enabling the gateway.
            </Text>
            <Button
              variant="secondary"
              onClick={() =>
                void Promise.all([
                  members.refetch(),
                  roles.refetch(),
                  assignments.refetch(),
                ])
              }
            >
              Retry
            </Button>
          </div>
        ) : !assignments.data ? (
          <SkeletonTable />
        ) : (
          <Table
            columns={columns}
            data={rows}
            rowKey={(row) => row.member.id}
            noResultsMessage="No people match these filters. Clear the filters to review everyone."
            renderExpandedContent={(row) => <ExpandedPerson row={row} />}
          />
        )}
        {editingMember && (
          <ChangeRoleDialog
            member={editingMember}
            onOpenChange={(open) => {
              if (!open) setEditingMember(null);
            }}
          />
        )}
      </div>
    </TooltipProvider>
  );
}

function ExpandedPerson({ row }: { row: AssignmentRow }): JSX.Element | null {
  const { expandedRowKeys } = useTable();
  return expandedRowKeys.has(row.member.id) ? (
    <PersonAssignments row={row} />
  ) : null;
}

function PersonAssignments({ row }: { row: AssignmentRow }): JSX.Element {
  return (
    <div className="border-border bg-muted/40 space-y-3 border-b px-5 py-4">
      {row.servers.length === 0 && (
        <Text small>This person has no MCP server assignments.</Text>
      )}
      {row.servers.map((server) => (
        <ServerAssignment key={server.id} server={server} member={row.member} />
      ))}
    </div>
  );
}

function ServerAssignment({
  server,
  member,
}: {
  server: AccessibleMCPServer;
  member: AccessMember;
}): JSX.Element {
  const [open, setOpen] = useState(false);
  const organization = useOrganization();
  const project = organization.projects?.find(
    (project) => project.id === server.projectId,
  );
  const projectName =
    project?.name ||
    (server.projectSlug === "default" ? "Default" : server.projectSlug);
  const projectLabel =
    server.projectSlug === "default"
      ? "Default project"
      : `Project: ${projectName}`;
  const routes = useRoutes({ projectSlug: server.projectSlug });
  const serverHref = routes.mcp.x.overview.href(server.slug || server.id);
  const explanation = useExplainResourceAccess(
    { resourceKind: "mcp", resourceId: server.id, userId: member.id },
    undefined,
    { throwOnError: false },
  );
  const sources = [
    ...new Map(
      (
        explanation.data?.levels.find((level) => level.level === "use")
          ?.rules ?? []
      )
        .filter(
          (rule) => rule.effect === "allows" || rule.effect === "overrides",
        )
        .map((rule) => [rule.principalUrn, rule]),
    ).values(),
  ];
  return (
    <div className="border-border border-b last:border-b-0">
      <div className="flex items-center gap-3 py-2">
        <div className="relative mr-2 shrink-0">
          <AssignmentServerIcon server={server} />
          <Tooltip delayDuration={0}>
            <TooltipTrigger asChild>
              <span
                tabIndex={0}
                aria-label={projectLabel}
                className="absolute -right-2 -bottom-2 inline-flex border-2 border-background"
              >
                <ProjectIcon
                  project={{ id: server.projectId, name: projectName }}
                  className="size-4 text-[9px]"
                />
              </span>
            </TooltipTrigger>
            <TooltipContent>{projectLabel}</TooltipContent>
          </Tooltip>
        </div>
        <div className="flex min-w-0 flex-1 items-center gap-2">
          <Link
            to={serverHref}
            className="truncate text-sm font-medium hover:underline"
            aria-label={`Open ${server.name || "MCP server"}`}
          >
            {server.name || "Unnamed server"}
          </Link>
        </div>
        <div className="ml-auto max-w-[55%] text-right text-sm">
          <Text small muted className="text-xs">
            Assigned through
          </Text>
          {explanation.isError ? (
            <button
              className="underline"
              onClick={() => void explanation.refetch()}
            >
              Retry loading sources
            </button>
          ) : explanation.isLoading ? (
            <Text small muted>
              Loading…
            </Text>
          ) : (
            <div className="font-medium">
              {sources.length
                ? sources.map((rule, index) => (
                    <span key={rule.principalUrn}>
                      {index > 0 && ", "}
                      {rule.kind === "role" ? (
                        <RoleLink principalUrn={rule.principalUrn}>
                          {rule.displayName}
                        </RoleLink>
                      ) : rule.kind === "user" ? (
                        "Direct assignment"
                      ) : (
                        rule.displayName
                      )}
                      {directorySourceLabel(rule) && (
                        <Tooltip delayDuration={0}>
                          <TooltipTrigger asChild>
                            <span
                              tabIndex={0}
                              aria-label={`Directory mapping for ${rule.displayName}`}
                              className="text-muted-foreground ml-1 inline-flex align-middle"
                            >
                              <Info className="size-3" />
                            </span>
                          </TooltipTrigger>
                          <TooltipContent>
                            {directorySourceLabel(rule)}
                          </TooltipContent>
                        </Tooltip>
                      )}
                    </span>
                  ))
                : explanation.data?.visibility === "public"
                  ? "Public server"
                  : "View or manage permission"}
            </div>
          )}
        </div>
        <RequireScope scope="org:admin" level="component">
          <Tooltip delayDuration={0}>
            <TooltipTrigger asChild>
              <Button
                variant="tertiary"
                size="sm"
                className="text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                aria-label={`Remove access to ${server.name} for ${member.name}`}
                onClick={() => setOpen(true)}
              >
                <Button.LeftIcon>
                  <Trash2 className="size-4" />
                </Button.LeftIcon>
              </Button>
            </TooltipTrigger>
            <TooltipContent>Remove access</TooltipContent>
          </Tooltip>
        </RequireScope>
      </div>
      {open && (
        <RemoveAssignmentDialog
          server={server}
          member={member}
          onClose={() => setOpen(false)}
        />
      )}
    </div>
  );
}

function ExpandedRowOnly({
  memberId,
  children,
}: {
  memberId: string;
  children: ReactNode;
}): JSX.Element | null {
  const { expandedRowKeys } = useTable();
  return expandedRowKeys.has(memberId) ? <>{children}</> : null;
}
