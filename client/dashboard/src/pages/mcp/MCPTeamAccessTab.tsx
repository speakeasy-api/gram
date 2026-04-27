import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Type } from "@/components/ui/type";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";
import type { Tool, Toolset } from "@/lib/toolTypes";
import { resourceKindForScope, selectorMatches } from "@/hooks/useRBAC";
import { useOrgRoutes } from "@/routes";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { Column, Table } from "@speakeasy-api/moonshine";
import { SkeletonTable } from "@/components/ui/skeleton";
import { useMemo, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@speakeasy-api/moonshine";

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
  role: Role;
  scopes: {
    read: AccessLevel;
    write: AccessLevel;
    connect: AccessLevel;
  };
}

function getAccessLevel(
  role: Role,
  scope: string,
  toolsetSlug: string,
): AccessLevel {
  const grant = role.grants.find((g) => g.scope === scope);
  if (!grant) return "none";
  // selectors undefined/null = unrestricted
  if (grant.selectors === undefined || grant.selectors === null) return "full";
  if (grant.selectors.length === 0) return "none";

  const check: Record<string, string> = {
    resource_kind: resourceKindForScope(scope),
    resource_id: toolsetSlug,
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
  toolsetSlug: string,
): string[] {
  const grant = role.grants.find((g) => g.scope === scope);
  if (!grant?.selectors) return [];
  const check: Record<string, string> = {
    resource_kind: resourceKindForScope(scope),
    resource_id: toolsetSlug,
  };
  return grant.selectors
    .filter((s) => selectorMatches(s, check) && s.tool)
    .map((s) => s.tool);
}

/** Match tool identifiers against toolset tools (by id or name) */
function resolveTools(toolIds: string[], tools: Tool[]): Tool[] {
  const idSet = new Set(toolIds);
  return tools.filter(
    (t) => ("id" in t && idSet.has(t.id)) || ("name" in t && idSet.has(t.name)),
  );
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
    <div className="border-border rounded-lg border p-3">
      <div className="flex items-start gap-2">
        {method && methodColors && (
          <span
            className={`mt-0.5 inline-flex shrink-0 items-center rounded px-1.5 py-0.5 font-mono text-[10px] font-bold ${methodColors}`}
          >
            {method}
          </span>
        )}
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
}: {
  level: AccessLevel;
  onClick?: () => void;
}) {
  switch (level) {
    case "full":
      return <Badge variant="default">All servers</Badge>;
    case "server":
      return <Badge variant="secondary">This server</Badge>;
    case "tools":
      return (
        <button type="button" onClick={onClick} className="cursor-pointer">
          <Badge
            variant="outline"
            className="hover:bg-accent transition-colors"
          >
            Specific tools &ensp;&rsaquo;
          </Badge>
        </button>
      );
    case "none":
      return (
        <span className="text-muted-foreground/50 text-sm">No access</span>
      );
  }
}

interface ToolDetailSheet {
  member: AccessMember;
  role: Role;
  scope: string;
  scopeLabel: string;
  tools: Tool[];
}

export function MCPTeamAccessTab({ toolset }: { toolset: Toolset }) {
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
        const role = roleMap.get(member.roleId);
        if (!role) return null;
        const scopes = {
          read: getAccessLevel(role, "mcp:read", toolset.slug),
          write: getAccessLevel(role, "mcp:write", toolset.slug),
          connect: getAccessLevel(role, "mcp:connect", toolset.slug),
        };
        return { member, role, scopes };
      })
      .filter((m): m is MemberAccess => m !== null)
      .filter(
        (m) =>
          m.scopes.read !== "none" ||
          m.scopes.write !== "none" ||
          m.scopes.connect !== "none",
      )
      .sort((a, b) => a.member.name.localeCompare(b.member.name));
  }, [membersData?.members, rolesData?.roles, toolset.slug]);

  const openToolSheet = (
    row: MemberAccess,
    scope: string,
    scopeLabel: string,
  ) => {
    const toolIds = getToolIdsForScope(row.role, scope, toolset.slug);
    const matched = resolveTools(toolIds, toolset.tools);
    setSheetData({
      member: row.member,
      role: row.role,
      scope,
      scopeLabel,
      tools: matched.length > 0 ? matched : [],
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
            <Type variant="body" className="truncate font-medium">
              {row.member.name}
            </Type>
            <Type
              variant="body"
              className="text-muted-foreground truncate text-xs"
            >
              {row.member.email}
            </Type>
          </div>
        </div>
      ),
    },
    {
      key: "role",
      header: "Role",
      width: "1fr",
      render: (row) => (
        <Type variant="body" className="text-sm">
          {row.role.name}
        </Type>
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
                  {sheetData.tools.length} tool
                  {sheetData.tools.length !== 1 ? "s" : ""} on this server via
                  the{" "}
                  <span className="text-foreground font-medium">
                    {sheetData.role.name}
                  </span>{" "}
                  role.
                </SheetDescription>
              </SheetHeader>
              <div className="flex-1 overflow-y-auto px-4 pb-4">
                <div className="space-y-2">
                  {sheetData.tools.map((tool, i) => (
                    <ToolRow key={i} tool={tool} />
                  ))}
                  {sheetData.tools.length === 0 && (
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
    </div>
  );
}
