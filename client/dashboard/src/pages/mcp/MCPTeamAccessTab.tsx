import { IdentityLink } from "@/components/identity-link";
import { Page } from "@/components/page-layout";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Text } from "@/components/ui/Text";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";
import type { Tool } from "@/lib/toolTypes";
import { resourceKindForScope, selectorMatches } from "@/hooks/useRBAC";
import { useOrgRoutes } from "@/routes";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Column, Table } from "@/components/ui/Table";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { useMemo, useState, ReactElement } from "react";
import { useNavigate } from "react-router";

function getInitials(name: string) {
  return name
    .split(" ")
    .map((n) => n[0])
    .join("")
    .toUpperCase()
    .slice(0, 2);
}

type AccessLevel = "full" | "server" | "tools" | "none";

interface MemberAccess {
  member: AccessMember;
  roles: Role[];
  scopes: {
    read: AccessLevel;
    write: AccessLevel;
    connect: AccessLevel;
  };
}

function getAccessLevel(
  role: Role,
  scope: string,
  resourceId: string,
): AccessLevel {
  const grant = role.grants.find((g) => g.scope === scope);
  if (!grant) return "none";
  // selectors undefined/null = unrestricted
  if (grant.selectors === undefined || grant.selectors === null) return "full";
  if (grant.selectors.length === 0) return "none";

  const check: Record<string, string> = {
    resourceKind: resourceKindForScope(scope),
    resourceId,
  };

  // Check if any selector matches this server (without tool constraint)
  const hasServer = grant.selectors.some(
    (s) => selectorMatches(s, check) && !s.tool,
  );
  // Check if any selector matches with a specific tool on this server
  const hasTools = grant.selectors.some(
    (s) => selectorMatches(s, check) && !!s.tool,
  );
  if (hasServer) return "server";
  if (hasTools) return "tools";
  return "none";
}

/** Extract tool names from selectors for this server */
function getToolIdsForScope(
  role: Role,
  scope: string,
  resourceId: string,
): string[] {
  const grant = role.grants.find((g) => g.scope === scope);
  if (!grant?.selectors) return [];
  const check: Record<string, string> = {
    resourceKind: resourceKindForScope(scope),
    resourceId,
  };
  return grant.selectors
    .filter((s) => selectorMatches(s, check) && s.tool)
    .map((s) => s.tool!);
}

/** Match tool identifiers against toolset tools (by id or name) */
function resolveTools(toolIds: string[], tools: Tool[]): Tool[] {
  const idSet = new Set(toolIds);
  return tools.filter(
    (t) => ("id" in t && idSet.has(t.id)) || ("name" in t && idSet.has(t.name)),
  );
}

const ACCESS_LEVEL_PRIORITY: Record<AccessLevel, number> = {
  full: 3,
  server: 2,
  tools: 1,
  none: 0,
};

function bestAccessLevel(levels: AccessLevel[]): AccessLevel {
  let best: AccessLevel = "none";
  for (const level of levels) {
    if (ACCESS_LEVEL_PRIORITY[level] > ACCESS_LEVEL_PRIORITY[best]) {
      best = level;
    }
  }
  return best;
}

const METHOD_COLORS: Record<string, string> = {
  GET: "text-blue-600 bg-blue-50",
  POST: "text-green-600 bg-green-50",
  PUT: "text-amber-600 bg-amber-50",
  PATCH: "text-orange-600 bg-orange-50",
  DELETE: "text-red-600 bg-red-50",
};

function ToolRow({ tool }: { tool: Tool }) {
  const isHttp = tool.type === "http";
  const httpTool = isHttp ? (tool as Tool & { type: "http" }) : null;
  const method = httpTool?.httpMethod?.toUpperCase();
  const methodColors = method
    ? (METHOD_COLORS[method] ?? "text-muted-foreground bg-muted")
    : null;

  const annotations = isHttp ? httpTool?.annotations : undefined;
  const annotationTags: string[] = [];
  if (annotations?.readOnlyHint) annotationTags.push("Read-only");
  if (annotations?.destructiveHint) annotationTags.push("Destructive");
  if (annotations?.idempotentHint) annotationTags.push("Idempotent");
  if (annotations?.openWorldHint) annotationTags.push("Open-world");

  return (
    <div className="border-border border p-3">
      <div className="flex items-start gap-2">
        {method && methodColors && (
          <span
            className={`mt-0.5 inline-flex shrink-0 items-center px-1.5 py-0.5 font-mono text-[10px] font-bold ${methodColors}`}
          >
            {method}
          </span>
        )}
        <div className="min-w-0 flex-1">
          <Text variant="body" className="font-mono text-sm font-medium">
            {"name" in tool ? tool.name : "Unknown tool"}
          </Text>
          {httpTool?.path && (
            <Text
              variant="body"
              className="text-muted-foreground mt-0.5 font-mono text-xs"
            >
              {httpTool.path}
            </Text>
          )}
        </div>
      </div>
      {"description" in tool && tool.description && (
        <Text
          variant="body"
          className="text-muted-foreground mt-1.5 line-clamp-2 text-xs"
        >
          {tool.description}
        </Text>
      )}
      {annotationTags.length > 0 && (
        <div className="mt-2 flex gap-1">
          {annotationTags.map((tag) => (
            <span
              key={tag}
              className="bg-muted text-muted-foreground inline-flex items-center rounded-full px-2 py-0.5 text-[10px]"
            >
              {tag}
            </span>
          ))}
        </div>
      )}
    </div>
  );
}

function AccessBadge({
  level,
  onClick,
  onGrant,
}: {
  level: AccessLevel;
  onClick?: () => void;
  /** Deep-link to grant this scope; renders "No access" as a click target. */
  onGrant?: () => void;
}) {
  switch (level) {
    case "full":
      return (
        <Badge variant="neutral">
          <Badge.Text>All servers</Badge.Text>
        </Badge>
      );
    case "server":
      return (
        <Badge variant="neutral">
          <Badge.Text>This server</Badge.Text>
        </Badge>
      );
    case "tools":
      return (
        <button
          type="button"
          // The row underneath navigates to the grant flow; this button only
          // opens the tool drill-down sheet.
          onClick={(e) => {
            e.stopPropagation();
            onClick?.();
          }}
          className="cursor-pointer"
        >
          <Badge
            variant="neutral"
            className="hover:bg-accent transition-colors"
          >
            <Badge.Text>Specific tools &ensp;&rsaquo;</Badge.Text>
          </Badge>
        </button>
      );
    case "none":
      if (!onGrant) {
        return (
          <span className="text-muted-foreground/50 text-sm">No access</span>
        );
      }
      return (
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            onGrant();
          }}
          className="text-muted-foreground/50 hover:text-foreground cursor-pointer text-sm underline-offset-2 hover:underline"
        >
          No access
        </button>
      );
  }
}

interface ToolDetailSheet {
  member: AccessMember;
  roles: Role[];
  scope: string;
  scopeLabel: string;
  // Rich Tool objects resolved against a toolset catalog. Empty for
  // mcp_servers-backed servers (no Gram-side tool catalog), in which case
  // toolNames carries the per-grant tool identifiers verbatim.
  tools: Tool[];
  toolNames: string[];
}

// MCPTeamAccessTab renders the per-server team access matrix for any MCP
// server identified by its resource id. Both toolset-backed and
// mcp_servers-backed (Remote MCP) servers grant under the same `mcp:*` scope
// family and the same `"mcp"` resource kind today, so the same component
// serves both — the caller just supplies the resource id and, when
// available, the toolset's tool catalog for rich per-tool drilldowns.
//
// TODO(AGE-1902): once toolset-backed MCP data moves to mcp_servers, the
// resourceId on every callsite should already be an mcp_servers id and the
// `tools` prop will be sourced from whatever tool-catalog primitive replaces
// `toolset.tools` for both backing kinds.
export function MCPTeamAccessTab({
  resourceId,
  tools,
}: {
  resourceId: string;
  tools?: Tool[];
}): ReactElement | null {
  const orgRoutes = useOrgRoutes();
  const navigate = useNavigate();
  const { data: membersData, isLoading: membersLoading } = useMembers();
  const { data: rolesData, isLoading: rolesLoading } = useRoles();

  const [sheetData, setSheetData] = useState<ToolDetailSheet | null>(null);

  // Deep-links into the Access page's pre-filled grant dialog (the same one
  // access-request emails open), scoped to this member and this server.
  const goToGrantAccess = (row: MemberAccess, scope: string) => {
    const params = new URLSearchParams({
      grant_user: row.member.id,
      scope,
      resource_id: resourceId,
    });
    void navigate(`${orgRoutes.access.roles.href()}?${params.toString()}`);
  };

  const memberAccess = useMemo((): MemberAccess[] => {
    const members = membersData?.members ?? [];
    const roles = rolesData?.roles ?? [];
    const roleMap = new Map(roles.map((r) => [r.id, r]));
    return members
      .map((member) => {
        const roles = member.roleIds
          .map((id) => roleMap.get(id))
          .filter((r): r is Role => r !== undefined);
        if (roles.length === 0) return null;
        const scopes = {
          read: bestAccessLevel(
            roles.map((r) => getAccessLevel(r, "mcp:read", resourceId)),
          ),
          write: bestAccessLevel(
            roles.map((r) => getAccessLevel(r, "mcp:write", resourceId)),
          ),
          connect: bestAccessLevel(
            roles.map((r) => getAccessLevel(r, "mcp:connect", resourceId)),
          ),
        };
        return { member, roles, scopes };
      })
      .filter((m): m is MemberAccess => m !== null)
      .filter(
        (m) =>
          m.scopes.read !== "none" ||
          m.scopes.write !== "none" ||
          m.scopes.connect !== "none",
      )
      .sort((a, b) => a.member.name.localeCompare(b.member.name));
  }, [membersData?.members, rolesData?.roles, resourceId]);

  const openToolSheet = (
    row: MemberAccess,
    scope: string,
    scopeLabel: string,
  ) => {
    const toolNames = [
      ...new Set(
        row.roles.flatMap((r) => getToolIdsForScope(r, scope, resourceId)),
      ),
    ];
    const matched = tools ? resolveTools(toolNames, tools) : [];
    setSheetData({
      member: row.member,
      roles: row.roles,
      scope,
      scopeLabel,
      tools: matched,
      toolNames,
    });
  };

  if (membersLoading || rolesLoading) {
    return <SkeletonTable />;
  }

  const columns: Column<MemberAccess>[] = [
    {
      key: "member",
      header: "Member",
      width: "280px",
      render: (row) => (
        <div className="flex items-center gap-3">
          <Avatar className="h-8 w-8">
            {row.member.photoUrl && (
              <AvatarImage src={row.member.photoUrl} alt={row.member.name} />
            )}
            <AvatarFallback className="text-xs">
              {getInitials(row.member.name)}
            </AvatarFallback>
          </Avatar>
          <div className="min-w-0">
            <IdentityLink identifier={{ userId: row.member.id }}>
              <Text variant="body" className="truncate font-medium">
                {row.member.name}
              </Text>
            </IdentityLink>
            <Text
              variant="body"
              className="text-muted-foreground truncate text-xs"
            >
              {row.member.email}
            </Text>
          </div>
        </div>
      ),
    },
    {
      key: "role",
      header: "Role",
      width: "1fr",
      render: (row) => (
        <div className="flex flex-wrap gap-1">
          {row.roles.map((role) => (
            <Text key={role.id} variant="body" className="text-sm">
              {role.name}
            </Text>
          ))}
        </div>
      ),
    },
    {
      key: "read",
      header: "Read",
      width: "120px",
      render: (row) => (
        <AccessBadge
          level={row.scopes.read}
          onClick={
            row.scopes.read === "tools"
              ? () => openToolSheet(row, "mcp:read", "Read")
              : undefined
          }
          onGrant={() => goToGrantAccess(row, "mcp:read")}
        />
      ),
    },
    {
      key: "write",
      header: "Write",
      width: "120px",
      render: (row) => (
        <AccessBadge
          level={row.scopes.write}
          onClick={
            row.scopes.write === "tools"
              ? () => openToolSheet(row, "mcp:write", "Write")
              : undefined
          }
          onGrant={() => goToGrantAccess(row, "mcp:write")}
        />
      ),
    },
    {
      key: "connect",
      header: "Connect",
      width: "160px",
      render: (row) => (
        <AccessBadge
          level={row.scopes.connect}
          onClick={
            row.scopes.connect === "tools"
              ? () => openToolSheet(row, "mcp:connect", "Connect")
              : undefined
          }
          onGrant={() => goToGrantAccess(row, "mcp:connect")}
        />
      ),
    },
  ];

  return (
    <Page.Section>
      <Page.Section.Title>Team access</Page.Section.Title>
      <Page.Section.Body>
        <div className="mb-4">
          <Text variant="body" className="text-muted-foreground text-sm">
            {memberAccess.length} team member
            {memberAccess.length !== 1 ? "s" : ""} with access to this server
          </Text>
        </div>
        <Table columns={columns}>
          <Table.Header columns={columns} />
          {memberAccess.length === 0 ? (
            <Table.NoResultsMessage>
              <div className="px-4 py-6 text-center">
                No team members have access to this server.
              </div>
            </Table.NoResultsMessage>
          ) : (
            <Table.Body
              columns={columns}
              data={memberAccess}
              rowKey={(row) => row.member.id}
              // Row click lands on the Access page's grant dialog for this
              // member and server; connect is the "can use this server" scope.
              onRowClick={(row) => goToGrantAccess(row, "mcp:connect")}
            />
          )}
          <Table.Row>
            <div className="border-border bg-muted/20 col-span-full border-t py-5 text-center">
              <Text variant="body" className="text-muted-foreground text-sm">
                Want to grant new members access?
              </Text>
              <div className="mt-2">
                <orgRoutes.access.roles.Link>
                  <Button variant="primary" size="sm">
                    <Button.Text>Configure Roles</Button.Text>
                  </Button>
                </orgRoutes.access.roles.Link>
              </div>
            </div>
          </Table.Row>
        </Table>

        <Sheet open={!!sheetData} onOpenChange={() => setSheetData(null)}>
          <SheetContent className="sm:max-w-md">
            {sheetData && (
              <>
                <SheetHeader>
                  <SheetTitle>{sheetData.scopeLabel} access</SheetTitle>
                  <SheetDescription>
                    <span className="text-foreground font-medium">
                      {sheetData.member.name}
                    </span>{" "}
                    can {sheetData.scopeLabel.toLowerCase()}{" "}
                    {sheetData.toolNames.length} tool
                    {sheetData.toolNames.length !== 1 ? "s" : ""} on this server
                    via the{" "}
                    <span className="text-foreground font-medium">
                      {sheetData.roles.map((r) => r.name).join(", ")}
                    </span>{" "}
                    {sheetData.roles.length === 1 ? "role" : "roles"}.
                  </SheetDescription>
                </SheetHeader>
                <div className="flex-1 overflow-y-auto px-4 pb-4">
                  <div className="space-y-2">
                    {sheetData.tools.length > 0 ? (
                      sheetData.tools.map((tool, i) => (
                        <ToolRow key={i} tool={tool} />
                      ))
                    ) : sheetData.toolNames.length > 0 ? (
                      // No catalog available (mcp_servers-backed servers don't
                      // expose tools through Gram). Surface the raw tool
                      // identifiers from the grant selectors so the user can
                      // at least see what they have access to.
                      sheetData.toolNames.map((name) => (
                        <div key={name} className="border-border border p-3">
                          <Text
                            variant="body"
                            className="font-mono text-sm font-medium"
                          >
                            {name}
                          </Text>
                        </div>
                      ))
                    ) : (
                      <div className="text-muted-foreground py-8 text-center text-sm">
                        Could not resolve tool names from grants.
                      </div>
                    )}
                  </div>
                </div>
              </>
            )}
          </SheetContent>
        </Sheet>
      </Page.Section.Body>
    </Page.Section>
  );
}
