import { IdentityAvatar } from "@/components/identity-avatar";
import { Ban } from "lucide-react";
import { cn } from "@/lib/utils";
import { useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Page } from "@/components/page-layout";
import { defineFilters, useFilterState } from "@/components/filters";
import { Button } from "@/components/ui/Button";
import { SelectFilterChip } from "@/components/filters/FilterChip";
import { getFilterAccents } from "@/components/gradient-colors";
import { MoreActions } from "@/components/ui/MoreActions";
import { RoleLink } from "@/components/role-link";
import { invalidateAllRoles } from "@gram/client/react-query/roles.js";
import { Text } from "@/components/ui/Text";
import { Table, type Column } from "@/components/ui/Table";
import { useTable } from "@/components/ui/Table/context/context";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import { AssignmentServerIcon } from "./AssignmentServerIcon";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { MemberFacepile } from "@/components/member-facepile";
import { RequireScope } from "@/components/require-scope";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { allMemberRoleIds } from "@/lib/member-roles";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { useSetResourceAudienceMutation } from "@gram/client/react-query/setResourceAudience.js";
import { invalidateAllResourceAudience } from "@gram/client/react-query/resourceAudience.js";
import { invalidateAllIdentityAccess } from "@gram/client/react-query/identityAccess.js";
import { invalidateAllExplainResourceAccess } from "@gram/client/react-query/explainResourceAccess.js";
import type { Role } from "@gram/client/models/components/role.js";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { AccessibleMCPServer } from "@gram/client/models/components/accessiblemcpserver.js";
import type { ResourceAudienceEntry } from "@gram/client/models/components/resourceaudienceentry.js";
import {
  buildAccessRows,
  scopeState,
  type AccessRow,
} from "../mcp/access/accessRows";
import { ownRules, narrowingLabel } from "../mcp/access/serverAudience";
import {
  assignmentExclusions,
  assignmentWriteEntry,
} from "./assignmentExclusions";
import {
  ServerAssignment,
  type PersonServerAccess,
} from "./PersonServerAssignment";
import { useRoutes, useOrgRoutes } from "@/routes";
import { Link, useNavigate } from "react-router";

const FILTERS = defineFilters([
  { id: "server", label: "MCP server", kind: "select", pinned: true },
]);
type ServerAccess = {
  server: AccessibleMCPServer;
  entries: ResourceAudienceEntry[];
  rows: AccessRow[];
};
type RoleServer = ServerAccess & { row: AccessRow };
type AccessAction = "remove" | "block" | "unblock";

function isServerBlocked(row: AccessRow): boolean {
  return !scopeState(row, "use").granted && row.cells.use.blocks.length > 0;
}
function serverAccessLabel(row: AccessRow): string {
  return isServerBlocked(row) ? "Blocked" : scopeState(row, "use").value;
}
function RoleServerIcon({
  server,
  row,
  className,
  compact = false,
}: {
  server: AccessibleMCPServer;
  row: AccessRow;
  className?: string;
  compact?: boolean;
}): JSX.Element {
  return (
    <span className="relative inline-flex shrink-0">
      <AssignmentServerIcon
        server={server}
        className={cn(
          className,
          isServerBlocked(row) &&
            "bg-(--color-feedback-red-200) text-(--color-feedback-red-900) [&_[data-slot=avatar-fallback]]:bg-(--color-feedback-red-200) [&_[data-slot=avatar-fallback]]:text-(--color-feedback-red-900)",
        )}
      />
      {isServerBlocked(row) && (
        <Ban
          aria-hidden="true"
          className={cn(
            "bg-card text-destructive pointer-events-none absolute z-20 rounded-full",
            compact
              ? "right-0.5 bottom-0.5 size-3"
              : "-right-0.5 -bottom-0.5 size-3.5",
          )}
        />
      )}
    </span>
  );
}
type RoleAssignment = {
  role: Role;
  servers: RoleServer[];
  members: AccessMember[];
};

/** Fetch each server once, independent of the number of roles or members. */
export function MCPAssignments({
  onReady,
}: {
  onReady?: (ready: boolean) => void;
}): JSX.Element {
  const organization = useOrganization();
  const orgRoutes = useOrgRoutes();
  const navigate = useNavigate();
  const sdk = useSdkClient();
  const roles = useRoles(undefined, undefined, { throwOnError: false });
  const members = useMembers(undefined, undefined, { throwOnError: false });
  const [search, setSearch] = useState("");
  const [checkingUser, setCheckingUser] = useState(false);
  const [selectedMember, setSelectedMember] = useState<AccessMember>();
  const { values, setValue } = useFilterState(FILTERS);
  const assignments = useQuery({
    queryKey: ["mcpAssignments", organization.id, "roles"],
    queryFn: async ({ signal }): Promise<ServerAccess[]> => {
      const inventory = await sdk.mcpServers.listForOrg({}, undefined, {
        signal,
      });
      const servers = inventory.mcpServers.filter(
        (server) => server.visibility !== "disabled",
      );
      const result: ServerAccess[] = [];
      for (let i = 0; i < servers.length; i += 6) {
        signal.throwIfAborted();
        result.push(
          ...(await Promise.all(
            servers.slice(i, i + 6).map(async (server) => {
              const audience = await sdk.access.listResourceAudience(
                { resourceKind: "mcp", resourceId: server.id },
                undefined,
                { signal },
              );
              return {
                server: {
                  id: server.id,
                  name: server.name || "Unnamed server",
                  slug: server.slug || server.id,
                  projectId: server.projectId,
                  projectSlug:
                    organization.projects?.find(
                      (project) => project.id === server.projectId,
                    )?.slug ?? "default",
                },
                entries: audience.entries,
                rows: buildAccessRows(audience.entries),
              };
            }),
          )),
        );
      }
      return result.sort((a, b) => a.server.name.localeCompare(b.server.name));
    },
    throwOnError: false,
  });
  const ready =
    roles.isSuccess &&
    members.isSuccess &&
    assignments.isSuccess &&
    !roles.isFetching &&
    !members.isFetching &&
    !assignments.isFetching;
  useEffect(() => {
    onReady?.(ready);
    return () => onReady?.(false);
  }, [onReady, ready]);
  const roleRows = useMemo(
    () =>
      (roles.data?.roles ?? [])
        .map((role) => ({
          role,
          servers: (assignments.data ?? [])
            .flatMap((access): RoleServer[] => {
              const row = access.rows.find(
                (row) => row.principalUrn === role.principalUrn,
              );
              return row &&
                (scopeState(row, "use").granted || isServerBlocked(row))
                ? [{ ...access, row }]
                : [];
            })
            .sort(
              (a, b) =>
                Number(isServerBlocked(a.row)) - Number(isServerBlocked(b.row)),
            ),
          members: (members.data?.members ?? []).filter((member) =>
            allMemberRoleIds(member).includes(role.id),
          ),
        }))
        .filter(
          ({ role, servers }) =>
            role.name.toLowerCase().includes(search.trim().toLowerCase()) &&
            (!values.server ||
              servers.some(({ server }) => server.id === values.server)),
        )
        .sort((a, b) => a.role.name.localeCompare(b.role.name)),
    [roles.data, members.data, assignments.data, search, values.server],
  );
  const failed = roles.isError || members.isError || assignments.isError;
  const refresh = () =>
    void Promise.all([
      roles.refetch(),
      members.refetch(),
      assignments.refetch(),
    ]);
  return (
    <div className="space-y-5">
      <Text small muted>
        Review MCP access and blocks for each role. Check a person’s access to
        see how their roles, direct assignments, and restrictions combine.
      </Text>
      <Page.Toolbar>
        <Page.Toolbar.Search
          value={search}
          onChange={setSearch}
          placeholder="Search roles…"
        />
        <Page.Toolbar.Leading>
          <SelectFilterChip
            dim={FILTERS[0]}
            value={values.server ?? null}
            options={(assignments.data ?? []).map(({ server }) => ({
              value: server.id,
              label:
                server.projectSlug === "default"
                  ? server.name
                  : `${server.name} · ${server.projectSlug}`,
            }))}
            color={getFilterAccents(["server"]).server}
            onChange={(value) => setValue("server", value)}
          />
        </Page.Toolbar.Leading>

        <Page.Toolbar.Actions>
          <Text small muted>
            {roleRows.length} roles
          </Text>
          <Button
            variant="secondary"
            onClick={() => {
              setSelectedMember(undefined);
              setCheckingUser(true);
            }}
          >
            <Button.Text>Check user access</Button.Text>
          </Button>
        </Page.Toolbar.Actions>
        <Page.Toolbar.Refresh
          onRefresh={refresh}
          isRefreshing={
            roles.isFetching || members.isFetching || assignments.isFetching
          }
        />
      </Page.Toolbar>
      {failed ? (
        <div role="alert" className="space-y-3">
          <Text>
            Couldn’t load the complete assignment review. Check your access and
            retry.
          </Text>
          <Button variant="secondary" onClick={refresh}>
            <Button.Text>Retry</Button.Text>
          </Button>
        </div>
      ) : !assignments.data || !members.data || !roles.data ? (
        <SkeletonTable />
      ) : (
        <Table
          columns={roleColumns(
            (member) => {
              setSelectedMember(member);
              setCheckingUser(true);
            },
            (role) => {
              void navigate(
                `${orgRoutes.access.roles.href()}/${encodeURIComponent(role.id)}/edit`,
              );
            },
          )}
          data={roleRows}
          rowKey={({ role }) => role.id}
          noResultsMessage="No roles match these filters."
          renderExpandedContent={(row) => (
            <ExpandedRole
              assignment={row}
              selectedServer={values.server ?? undefined}
            />
          )}
        />
      )}
      {checkingUser && (
        <UserAccess
          members={members.data?.members ?? []}
          initialMember={selectedMember}
          serverAccess={assignments.data}
          onClose={() => setCheckingUser(false)}
        />
      )}
    </div>
  );
}

function roleColumns(
  onSelectMember: (member: AccessMember) => void,
  onEditRole: (role: Role) => void,
): Column<RoleAssignment>[] {
  return [
    {
      key: "role",
      header: "Role",
      width: "1fr",
      render: ({ role }) => (
        <Text className="truncate font-medium">{role.name}</Text>
      ),
    },
    {
      key: "servers",
      header: "Connect",
      width: "2fr",
      render: ({ servers }) => <RoleServerIcons servers={servers} />,
    },
    {
      key: "members",
      header: "Members",
      width: "1fr",
      render: ({ role, members }) => (
        <MemberFacepile
          members={members}
          maxFaces={3}
          memberActionLabel="View assignments"
          onEditRole={() => onEditRole(role)}
          onMemberSelect={(picked) => {
            const member = members.find((member) => member.id === picked.id);
            if (member) onSelectMember(member);
          }}
        />
      ),
    },
  ];
}

function RoleServerIcons({ servers }: { servers: RoleServer[] }): JSX.Element {
  return (
    <div className="flex items-center gap-3">
      <div className="flex -space-x-2">
        {servers.slice(0, 8).map(({ server, row }) => (
          <Tooltip key={server.id}>
            <TooltipTrigger asChild>
              <span
                tabIndex={0}
                aria-label={`${server.name}: ${serverAccessLabel(row)}`}
                className="relative focus:z-10 hover:z-10"
              >
                <RoleServerIcon
                  server={server}
                  row={row}
                  className="border-card border-2"
                  compact
                />
              </span>
            </TooltipTrigger>
            <TooltipContent>
              {server.name}
              {server.projectSlug !== "default" &&
                ` · ${server.projectSlug}`} ·{" "}
              {serverAccessLabel(row)}
            </TooltipContent>
          </Tooltip>
        ))}
        {servers.length > 8 && (
          <span className="bg-muted relative flex h-8 shrink-0 items-center justify-center px-2 text-xs whitespace-nowrap">
            +{servers.length - 8} more
          </span>
        )}
      </div>
    </div>
  );
}

function ExpandedRole({
  assignment,
  selectedServer,
}: {
  assignment: RoleAssignment;
  selectedServer?: string;
}): JSX.Element | null {
  const { expandedRowKeys } = useTable();
  return expandedRowKeys.has(assignment.role.id) ? (
    <RoleAssignments
      role={assignment.role}
      servers={assignment.servers}
      selectedServer={selectedServer}
    />
  ) : null;
}

function RoleAssignments({
  role,
  servers,
  selectedServer,
}: {
  role: Role;
  servers: RoleServer[];
  selectedServer?: string;
}): JSX.Element {
  const [limit, setLimit] = useState(10);
  const [search, setSearch] = useState("");
  const filtered = servers.filter(
    ({ server }) =>
      (!selectedServer || server.id === selectedServer) &&
      `${server.name} ${server.projectSlug}`
        .toLowerCase()
        .includes(search.toLowerCase()),
  );
  return (
    <div className="border-border bg-muted/40 border-b">
      {servers.length >= 10 && !selectedServer && (
        <div className="border-border border-b">
          <Page.Toolbar className="border-0 p-0">
            <Page.Toolbar.Search
              className="w-full"
              value={search}
              onChange={(value) => {
                setSearch(value);
                setLimit(10);
              }}
              placeholder="Search servers…"
            />
          </Page.Toolbar>
        </div>
      )}
      {!role.isSystem &&
        servers.some(({ row }) =>
          row.cells.use.grants.some(
            (grant) => grant.appliesTo === "all_resources",
          ),
        ) && (
          <Text small muted className="px-5 py-2">
            A role-wide rule grants access across servers.{" "}
            <RoleLink roleId={role.id}>Edit role</RoleLink> to change that rule.
          </Text>
        )}
      {filtered.slice(0, limit).map((access) => (
        <RoleServerAssignment
          key={access.server.id}
          access={access}
          role={role}
        />
      ))}
      {!filtered.length && (
        <Text small muted className="px-5 py-2">
          {servers.length
            ? "No MCP servers match."
            : "This role does not grant connection access to any MCP servers."}
        </Text>
      )}
      {filtered.length > limit && (
        <Button variant="tertiary" onClick={() => setLimit(limit + 10)}>
          Show 10 more
        </Button>
      )}
    </div>
  );
}

function RoleServerAssignment({
  access,
  role,
}: {
  access: RoleServer;
  role: Role;
}): JSX.Element {
  const { server, row } = access;
  const routes = useRoutes({ projectSlug: server.projectSlug });
  const [action, setAction] = useState<AccessAction>();
  const blocked = isServerBlocked(row);
  const unblockingAllowed =
    blocked &&
    row.cells.use.blocks.every(
      (entry) =>
        entry.appliesTo === "resource" &&
        entry.principalUrn === role.principalUrn,
    );
  const removable =
    !role.isSystem &&
    row.cells.use.grants.length > 0 &&
    row.cells.use.grants.every(
      (grant) =>
        grant.appliesTo === "resource" &&
        grant.principalUrn === role.principalUrn,
    );
  const restrictions = [
    ...new Set([
      ...row.cells.use.grants
        .filter((entry) => entry.tools?.length || entry.dispositions?.length)
        .map((entry) => `Allows ${narrowingLabel(entry)}`),
      ...row.cells.use.blocks.map((entry) => `Blocks ${narrowingLabel(entry)}`),
    ]),
  ];
  return (
    <div
      className={cn(
        "border-border flex flex-wrap items-center gap-3 border-b px-5 py-2 last:border-b-0",
        blocked && "bg-destructive/5 text-destructive",
      )}
    >
      <RoleServerIcon server={server} row={row} />
      <div className="flex min-w-0 flex-1 flex-wrap items-baseline gap-x-3">
        <Link
          className="break-words text-sm font-medium hover:underline"
          to={routes.mcp.x.overview.href(server.slug)}
        >
          {server.name || "Unnamed server"}
        </Link>
        {server.projectSlug !== "default" && (
          <Text small muted>
            {server.projectSlug}
          </Text>
        )}
      </div>
      {restrictions.length > 0 ? (
        <Tooltip>
          <TooltipTrigger asChild>
            <span
              tabIndex={0}
              className="text-sm underline decoration-dotted underline-offset-4"
            >
              {serverAccessLabel(row)}
            </span>
          </TooltipTrigger>
          <TooltipContent>
            {restrictions.map((label) => (
              <div key={label}>{label}</div>
            ))}
          </TooltipContent>
        </Tooltip>
      ) : (
        <Text small>{serverAccessLabel(row)}</Text>
      )}
      <RequireScope scope="org:admin" level="component">
        <MoreActions
          triggerAriaLabel={`Access actions for ${role.name} on ${server.name}`}
          actions={[
            {
              label: "Remove access",
              disabled: !removable,
              description: removable
                ? "Remove this role’s grant. Access from other roles still works."
                : role.isSystem
                  ? "Built-in role permissions cannot be edited."
                  : "Granted by a rule covering multiple servers. Edit the role to change that rule.",
              onClick: () => setAction("remove"),
            },
            {
              label: blocked ? "Unblock access" : "Block access",
              disabled: blocked && !unblockingAllowed,
              destructive: !blocked,
              description: blocked
                ? unblockingAllowed
                  ? "Remove this block. Existing grants decide access again."
                  : "A role-wide rule blocks access. Edit the role to change that rule."
                : "Deny access for this role, even when another role grants it.",
              onClick: () => setAction(blocked ? "unblock" : "block"),
            },
          ]}
        />
      </RequireScope>
      {action && (
        <ChangeRoleServerAccess
          server={server}
          role={role}
          action={action}
          onClose={() => setAction(undefined)}
        />
      )}
    </div>
  );
}

// Platform MCP inspection already covers roles and people. Its role mutations
// accept allows only; exclusions keep their explicit confirmation here.
function ChangeRoleServerAccess({
  server,
  role,
  action,
  onClose,
}: {
  server: AccessibleMCPServer;
  role: Role;
  action: AccessAction;
  onClose: () => void;
}): JSX.Element {
  const sdk = useSdkClient();
  const queryClient = useQueryClient();
  const organization = useOrganization();
  const audience = useQuery({
    queryKey: ["mcpAssignments", organization.id, "block", server.id],
    queryFn: () =>
      sdk.access.listResourceAudience({
        resourceKind: "mcp",
        resourceId: server.id,
      }),
    staleTime: 0,
    throwOnError: false,
  });
  const mutation = useSetResourceAudienceMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllRoles(queryClient),
        invalidateAllResourceAudience(queryClient),
        invalidateAllIdentityAccess(queryClient),
        invalidateAllExplainResourceAccess(queryClient),
        queryClient.invalidateQueries({ queryKey: ["mcpAssignments"] }),
      ]);
      onClose();
    },
  });
  const currentRow = buildAccessRows(audience.data?.entries ?? []).find(
    (row) => row.principalUrn === role.principalUrn,
  );
  const canRemove =
    !role.isSystem &&
    !!currentRow?.cells.use.grants.length &&
    currentRow.cells.use.grants.every(
      (grant) =>
        grant.appliesTo === "resource" &&
        grant.principalUrn === role.principalUrn,
    );
  const removing = action === "remove";
  const unblocking = action === "unblock";
  const canUnblock =
    !!currentRow?.cells.use.blocks.length &&
    currentRow.cells.use.blocks.every(
      (entry) =>
        entry.appliesTo === "resource" &&
        entry.principalUrn === role.principalUrn,
    );
  const confirmLabel = {
    remove: "Remove access",
    block: "Block access",
    unblock: "Unblock access",
  }[action];
  const title = {
    remove: `Remove ${role.name} access to ${server.name}?`,
    block: `Block ${role.name} from ${server.name}?`,
    unblock: `Unblock ${role.name} on ${server.name}?`,
  }[action];
  const description = {
    remove:
      "Remove this role’s permissions for this server. Access granted through other roles or directly to a person still works. Role membership stays unchanged.",
    block:
      "Block connection access for people and agents holding this role, even when another role grants it. A person’s direct assignment to this server can override this block. Role membership stays unchanged.",
    unblock:
      "Remove this role’s connection block for this server. Access will be determined by existing roles and direct grants; this does not add a new grant.",
  }[action];
  return (
    <ConfirmDialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      title={title}
      description={description}
      confirmLabel={confirmLabel}
      confirmVariant={unblocking ? "primary" : "destructive-primary"}
      isPending={mutation.isPending}
      confirmDisabled={
        !audience.data ||
        audience.isFetching ||
        audience.isError ||
        (removing && !canRemove) ||
        (unblocking && !canUnblock)
      }
      error={
        audience.isError ? (
          <Button variant="secondary" onClick={() => void audience.refetch()}>
            <Button.Text>Retry loading access</Button.Text>
          </Button>
        ) : mutation.error ? (
          <div className="space-y-2">
            <Text>Couldn’t save the change. Reload access and try again.</Text>
            <Button
              variant="secondary"
              onClick={() => {
                mutation.reset();
                void audience.refetch();
              }}
            >
              <Button.Text>Reload access</Button.Text>
            </Button>
          </div>
        ) : undefined
      }
      onConfirm={() => {
        if (
          audience.data &&
          !audience.isFetching &&
          (!removing || canRemove) &&
          (!unblocking || canUnblock)
        )
          mutation.mutate({
            request: {
              setResourceAudienceForm: {
                resourceKind: "mcp",
                resourceId: server.id,
                expectedVersion: audience.data.version,
                entries:
                  removing || unblocking
                    ? ownRules(audience.data.entries)
                        .filter(
                          (entry) =>
                            entry.principalUrn !== role.principalUrn ||
                            !(unblocking
                              ? entry.level === "blocked"
                              : ["use", "view", "manage"].includes(
                                  entry.level,
                                )),
                        )
                        .map(assignmentWriteEntry)
                    : assignmentExclusions(audience.data.entries, [
                        role.principalUrn,
                      ]),
              },
            },
          });
      }}
    />
  );
}

function UserAccess({
  members,
  initialMember,
  serverAccess,
  onClose,
}: {
  members: AccessMember[];
  initialMember?: AccessMember;
  serverAccess?: ServerAccess[];
  onClose: () => void;
}): JSX.Element {
  const sdk = useSdkClient();
  const organization = useOrganization();
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<AccessMember | undefined>(
    initialMember,
  );
  const [serverSearch, setServerSearch] = useState("");
  const [serverLimit, setServerLimit] = useState(10);
  const roles = useRoles(undefined, undefined, { throwOnError: false });
  const matchingMembers = members.filter((member) =>
    `${member.name} ${member.email}`
      .toLowerCase()
      .includes(search.toLowerCase()),
  );
  const access = useQuery({
    queryKey: [
      "mcpAssignments",
      organization.id,
      "user",
      "connection-decisions",
      selected?.id,
      serverAccess?.map(({ server }) => server.id),
    ],
    enabled: !!selected && !!serverAccess,
    queryFn: async ({ signal }) => {
      if (!selected) return [];
      const result = await sdk.access.listIdentityAccess(
        { userId: selected.id },
        undefined,
        { signal },
      );
      const candidates = [
        ...new Map(
          [
            ...result.servers,
            ...(serverAccess ?? []).map(({ server }) => server),
          ].map((server) => [server.id, server]),
        ).values(),
      ];
      const servers: PersonServerAccess[] = [];
      for (let i = 0; i < candidates.length; i += 6) {
        const batch = await Promise.all(
          candidates
            .slice(i, i + 6)
            .map(async (server): Promise<PersonServerAccess | null> => {
              const explanation = await sdk.access.explainResourceAccess(
                {
                  resourceKind: "mcp",
                  resourceId: server.id,
                  userId: selected.id,
                },
                undefined,
                { signal },
              );
              return explanation.levels.some(
                (level) =>
                  level.level === "use" &&
                  (level.allowed ||
                    level.rules.some((rule) => rule.effect === "blocks")),
              )
                ? { server, explanation }
                : null;
            }),
        );
        servers.push(
          ...batch.filter(
            (server): server is PersonServerAccess => server !== null,
          ),
        );
      }
      return servers.sort(
        (a, b) =>
          Number(
            !a.explanation.levels.find((level) => level.level === "use")
              ?.allowed,
          ) -
            Number(
              !b.explanation.levels.find((level) => level.level === "use")
                ?.allowed,
            ) || a.server.name.localeCompare(b.server.name),
      );
    },
    throwOnError: false,
  });
  const matchingServers = (access.data ?? []).filter(({ server }) =>
    `${server.name} ${server.projectSlug}`
      .toLowerCase()
      .includes(serverSearch.toLowerCase()),
  );
  return (
    <Sheet
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <SheetContent className="w-full overflow-y-auto sm:max-w-2xl">
        <SheetHeader>
          <SheetTitle>Check user access</SheetTitle>
        </SheetHeader>
        <div className="space-y-4 p-5">
          <Page.Toolbar.Search
            className="w-full"
            value={search}
            onChange={setSearch}
            placeholder="Find a person by name or email…"
          />
          {search.trim() && matchingMembers.length > 0 && (
            <div className="divide-border border-border divide-y border">
              {matchingMembers.slice(0, 20).map((member) => (
                <Button
                  key={member.id}
                  aria-label={`${member.name || member.email} · ${member.email}`}
                  variant="tertiary"
                  className="h-auto w-full justify-start px-3 py-3 [&>span]:justify-start"
                  onClick={() => {
                    setSelected(member);
                    setSearch("");
                    setServerSearch("");
                    setServerLimit(10);
                  }}
                >
                  <Button.LeftIcon>
                    <MemberIdentityAvatar member={member} />
                  </Button.LeftIcon>
                  <Button.Text className="min-w-0 text-left normal-case">
                    <span className="block truncate font-medium">
                      {member.name || member.email}
                    </span>
                    <span className="text-muted-foreground block truncate text-xs">
                      {member.email}
                    </span>
                  </Button.Text>
                </Button>
              ))}
            </div>
          )}
          {search.trim() && matchingMembers.length === 0 && (
            <Text muted>No people match this search.</Text>
          )}
          {search.trim() && matchingMembers.length > 20 && (
            <Text small muted>
              Showing the first 20 matches. Refine your search to find a person.
            </Text>
          )}
          {selected ? (
            <>
              <div className="flex items-center gap-3 border-b border-border pb-4">
                <MemberIdentityAvatar member={selected} />
                <div className="min-w-0">
                  <Text className="truncate font-medium">
                    {selected.name || selected.email}
                  </Text>
                  <Text small muted className="truncate">
                    {selected.email}
                  </Text>
                </div>
              </div>
              <Text small muted>
                {(roles.data?.roles ?? [])
                  .filter((role) =>
                    allMemberRoleIds(selected).includes(role.id),
                  )
                  .map((role) => role.name)
                  .join(", ")}
              </Text>
              <Page.Toolbar.Search
                className="w-full"
                value={serverSearch}
                onChange={(value) => {
                  setServerSearch(value);
                  setServerLimit(10);
                }}
                placeholder="Search this person’s MCPs…"
              />
              <Text small muted>
                Connection access after combining all roles, direct assignments,
                and blocks.
              </Text>
              {access.isError ? (
                <div role="alert">
                  <Text>Couldn’t resolve this person’s access.</Text>
                  <Button
                    variant="secondary"
                    onClick={() => void access.refetch()}
                  >
                    <Button.Text>Retry</Button.Text>
                  </Button>
                </div>
              ) : access.isPending ? (
                <SkeletonTable />
              ) : access.data?.length ? (
                matchingServers
                  .slice(0, serverLimit)
                  .map((assignment) => (
                    <ServerAssignment
                      key={assignment.server.id}
                      {...assignment}
                    />
                  ))
              ) : (
                <Text>No MCP connection access.</Text>
              )}
              {access.isSuccess &&
                access.data.length > 0 &&
                matchingServers.length === 0 && (
                  <Text muted>No MCP servers match this search.</Text>
                )}
              {matchingServers.length > serverLimit && (
                <Button
                  variant="tertiary"
                  onClick={() => setServerLimit(serverLimit + 10)}
                >
                  <Button.Text>Show 10 more MCPs</Button.Text>
                </Button>
              )}
            </>
          ) : (
            <Text muted>
              Find a person to inspect their effective MCP access.
            </Text>
          )}
        </div>
      </SheetContent>
    </Sheet>
  );
}

function MemberIdentityAvatar({
  member,
}: {
  member: AccessMember;
}): JSX.Element {
  return (
    <IdentityAvatar
      label={member.name || member.email}
      src={member.photoUrl}
      className="size-9"
    />
  );
}
