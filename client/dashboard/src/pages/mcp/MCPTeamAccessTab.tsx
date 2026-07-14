import { Card } from "@/components/ui/card";
import { IdentityCell } from "@/components/ui/identity-cell";
import { InlineEmptyState } from "@/components/ui/inline-empty-state";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Type } from "@/components/ui/type";
import { MethodBadge } from "@/components/tool-list/MethodBadge";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";
import type { Tool } from "@/lib/toolTypes";
import { resourceKindForScope, selectorMatches } from "@/hooks/useRBAC";
import { useOrgRoutes } from "@/routes";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { type Column, Table } from "@/components/ui/table";
import { SkeletonTable } from "@/components/ui/skeleton";
import { useMemo, useState, ReactElement } from "react";

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

function ToolRow({ tool }: { tool: Tool }) {
  const isHttp = tool.type === "http";
  const httpTool = isHttp ? (tool as Tool & { type: "http" }) : null;
  const method = httpTool?.httpMethod;

  const annotations = isHttp ? httpTool?.annotations : undefined;
  const annotationTags: string[] = [];
  if (annotations?.readOnlyHint) annotationTags.push("Read-only");
  if (annotations?.destructiveHint) annotationTags.push("Destructive");
  if (annotations?.idempotentHint) annotationTags.push("Idempotent");
  if (annotations?.openWorldHint) annotationTags.push("Open-world");

  return (
    <Card className="gap-0 p-3">
      <div className="flex items-start gap-2">
        {method && <MethodBadge method={method} />}
        <div className="min-w-0 flex-1">
          <Type variant="body" className="font-mono text-sm font-medium">
            {"name" in tool ? tool.name : "Unknown tool"}
          </Type>
          {httpTool?.path && (
            <Type
              variant="body"
              className="text-muted-foreground mt-0.5 font-mono text-xs"
            >
              {httpTool.path}
            </Type>
          )}
        </div>
      </div>
      {"description" in tool && tool.description && (
        <Type
          variant="body"
          className="text-muted-foreground mt-1.5 line-clamp-2 text-xs"
        >
          {tool.description}
        </Type>
      )}
      {annotationTags.length > 0 && (
        <div className="mt-2 flex gap-1">
          {annotationTags.map((tag) => (
            <Badge key={tag} variant="neutral" background={false} size="sm">
              {tag}
            </Badge>
          ))}
        </div>
      )}
    </Card>
  );
}

function AccessBadge({
  level,
  onClick,
}: {
  level: AccessLevel;
  onClick?: () => void;
}) {
  switch (level) {
    case "full":
      return <Badge>All servers</Badge>;
    case "server":
      return <Badge background={false}>This server</Badge>;
    case "tools":
      return (
        <button type="button" onClick={onClick} className="cursor-pointer">
          <Badge
            background={false}
            className="hover:bg-accent transition-colors"
          >
            Specific tools &ensp;&rsaquo;
          </Badge>
        </button>
      );
    case "none":
      return (
        <Type variant="body" className="text-muted-foreground/50 text-sm">
          No access
        </Type>
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
  const { data: membersData, isLoading: membersLoading } = useMembers();
  const { data: rolesData, isLoading: rolesLoading } = useRoles();

  const [sheetData, setSheetData] = useState<ToolDetailSheet | null>(null);

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
        <IdentityCell
          name={row.member.name}
          subtitle={row.member.email}
          imageUrl={row.member.photoUrl}
        />
      ),
    },
    {
      key: "role",
      header: "Role",
      width: "1fr",
      render: (row) => (
        <div className="flex flex-wrap gap-1">
          {row.roles.map((role) => (
            <Type key={role.id} variant="body" className="text-sm">
              {role.name}
            </Type>
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
        />
      ),
    },
  ];

  return (
    <div>
      <div className="mb-4">
        <Type variant="body" className="text-muted-foreground text-sm">
          {memberAccess.length} team member
          {memberAccess.length !== 1 ? "s" : ""} with access to this server
        </Type>
      </div>
      <Table columns={columns}>
        <Table.Header columns={columns} />
        {memberAccess.length === 0 ? (
          <Table.NoResultsMessage>
            No team members have access to this server.
          </Table.NoResultsMessage>
        ) : (
          <Table.Body
            columns={columns}
            data={memberAccess}
            rowKey={(row) => row.member.id}
          />
        )}
        <Table.Row>
          <div className="border-border bg-muted/20 col-span-full border-t py-5 text-center">
            <Type variant="body" className="text-muted-foreground text-sm">
              Want to grant new members access?
            </Type>
            <div className="mt-2">
              <orgRoutes.access.roles.Link>
                <Button variant="tertiary" size="sm">
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
                      <Card key={name} className="p-3">
                        <Type
                          variant="body"
                          className="font-mono text-sm font-medium"
                        >
                          {name}
                        </Type>
                      </Card>
                    ))
                  ) : (
                    <InlineEmptyState title="Could not resolve tool names from grants." />
                  )}
                </div>
              </div>
            </>
          )}
        </SheetContent>
      </Sheet>
    </div>
  );
}
