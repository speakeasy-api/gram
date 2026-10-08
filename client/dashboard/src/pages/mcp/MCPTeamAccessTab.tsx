import { IdentityLink } from "@/components/identity-link";
import { Page } from "@/components/page-layout";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";
import { Heading } from "@/components/ui/Heading";
import { Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import type { ToolAnnotation } from "@/components/tool-selection/ToolSelectionPanel";
import type { Tool } from "@/lib/toolTypes";
import { TriangleAlert } from "lucide-react";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { ResourceAudienceEntry } from "@gram/client/models/components/resourceaudienceentry.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useResourceAudience } from "@gram/client/react-query/resourceAudience.js";
import { useMemo, type ReactElement } from "react";
import { CheckAccess } from "./access/CheckAccess";
import { ManageAccess } from "./access/ManageAccess";
import { RoleLink } from "@/components/role-link";
import { blockingRules } from "./access/serverAudience";

/** The annotations a tool carries, in the vocabulary selectors store. */
function toolAnnotations(tool: Tool): ToolAnnotation[] {
  const annotations = "annotations" in tool ? tool.annotations : undefined;
  if (!annotations) return [];
  const carried: ToolAnnotation[] = [];
  if (annotations.readOnlyHint) carried.push("read_only");
  if (annotations.destructiveHint) carried.push("destructive");
  if (annotations.idempotentHint) carried.push("idempotent");
  if (annotations.openWorldHint) carried.push("open_world");
  return carried;
}

function getInitials(name: string) {
  return name
    .split(" ")
    .map((n) => n[0])
    .join("")
    .toUpperCase()
    .slice(0, 2);
}

/** A rule named on a row, linked when it is a role someone can go and edit. */
interface NamedRule {
  principalUrn: string;
  displayName: string;
}

/**
 * Someone two rules disagree about: a role gives them the server and another
 * role they are also in takes it away. A role's block outranks every grant
 * except one made to the person by name for this server, so the role's grant
 * does nothing — which is invisible from either role's own page.
 */
interface MemberConflict {
  member: AccessMember;
  grantedBy: NamedRule[];
  blockedBy: NamedRule[];
}

// MCPTeamAccessTab renders who can use one MCP server, for any server
// identified by its resource id. Toolset-backed and mcp_servers-backed (Remote
// MCP) servers grant under the same `mcp:*` scope family and the same `"mcp"`
// resource kind today, so the same component serves both.
export function MCPTeamAccessTab({
  resourceId,
  serverName,
  tools,
  checkAccess = true,
}: {
  resourceId: string;
  serverName?: string;
  /** The server's tools, when the backend exposes a catalogue for it. */
  tools?: Tool[];
  /**
   * Whether to offer Check access. A gateway turns it off: nothing checks
   * access on a gateway's own id, each server it fronts is checked instead.
   */
  checkAccess?: boolean;
}): ReactElement | null {
  const {
    data: audienceData,
    isLoading: audienceLoading,
    isError: audienceFailed,
  } = useResourceAudience(
    { resourceKind: "mcp", resourceId },
    undefined,
    // Recover inline: the page's other sections still work, and a save must
    // not treat an unread audience as an empty one.
    { throwOnError: false },
  );
  const { data: membersData } = useMembers();

  const entries = useMemo(
    () => audienceData?.entries ?? [],
    [audienceData?.entries],
  );

  // Remote and gateway servers resolve their tools per caller, so the
  // catalogue is optional: without it the picker still offers annotations.
  const toolCatalog = useMemo(
    () =>
      tools?.map((tool) => ({
        name: "name" in tool ? tool.name : "",
        annotations: toolAnnotations(tool),
      })),
    [tools],
  );

  // The people two rules disagree about. Nothing they were given survives,
  // and nothing on the rules list says why, so they get a section that names
  // both rules and says how to resolve it.
  const conflicts = useMemo((): MemberConflict[] => {
    const members = membersData?.members ?? [];
    const reaching = new Map<string, ResourceAudienceEntry[]>();
    for (const entry of entries) {
      for (const memberId of entry.memberIds ?? []) {
        reaching.set(memberId, [...(reaching.get(memberId) ?? []), entry]);
      }
    }

    const named = (rules: ResourceAudienceEntry[]): NamedRule[] => {
      const byUrn = new Map<string, NamedRule>();
      for (const rule of rules) {
        byUrn.set(rule.principalUrn, {
          principalUrn: rule.principalUrn,
          displayName: rule.displayName,
        });
      }
      return [...byUrn.values()];
    };

    return members
      .map((member) => {
        const rules = reaching.get(member.id) ?? [];
        const blocks = blockingRules(rules, `user:${member.id}`);
        // A grant they never had is not a conflict, it is just no access.
        if (blocks.length === 0) return null;
        const blockedBy = named(blocks);
        const blocking = new Set(blockedBy.map((rule) => rule.principalUrn));
        // Only the roles the conflict is with. A role that both grants the
        // server organization-wide and blocks it here is arguing with itself,
        // not with another role, and naming it on both sides of the row would
        // read as a contradiction rather than a thing to go and fix.
        const grantedBy = named(
          rules.filter(
            (rule) =>
              !rule.level.startsWith("blocked") &&
              !blocking.has(rule.principalUrn),
          ),
        );
        if (grantedBy.length === 0) return null;
        return { member, grantedBy, blockedBy };
      })
      .filter((row): row is MemberConflict => row !== null)
      .sort((a, b) => a.member.name.localeCompare(b.member.name));
  }, [membersData?.members, entries]);

  const conflictColumns: Column<MemberConflict>[] = [
    {
      key: "member",
      header: "Person",
      width: "300px",
      render: (row) => <PersonCell member={row.member} />,
    },
    {
      key: "granted",
      header: "Granted by",
      width: "1fr",
      render: (row) => <RuleNames rules={row.grantedBy} />,
    },
    {
      key: "blocked",
      header: "Blocked by",
      width: "1fr",
      render: (row) => <RuleNames rules={row.blockedBy} />,
    },
  ];

  return (
    <Page.Section>
      <Page.Section.Title>Access</Page.Section.Title>
      <Page.Section.Description>
        Who can use {serverName ?? "this server"}. Access given here applies to
        this server only.
      </Page.Section.Description>
      <Page.Section.Body>
        {audienceFailed ? (
          <Text muted small>
            Access rules could not be loaded, so they cannot be changed here
            yet. Reload the page to try again.
          </Text>
        ) : (
          <ManageAccess
            resourceId={resourceId}
            resourceName={serverName}
            entries={entries}
            version={audienceData?.version ?? ""}
            toolCatalog={toolCatalog}
            isLoading={audienceLoading}
          />
        )}

        {/* Below the rules it explains: the list above is what gets changed,
            this is where one person's outcome is read back. */}
        {checkAccess && (
          <>
            <div className="mt-10 mb-4">
              <Heading variant="h4">Check a person&rsquo;s access</Heading>
              <Text muted small className="mt-1">
                Pick one person to see what they can do on{" "}
                {serverName ?? "this server"}, and which rules decide it.
              </Text>
            </div>
            <CheckAccess
              resourceId={resourceId}
              serverName={serverName ?? "this server"}
            />
          </>
        )}

        {/* Two rules disagreeing about the same person. Nothing on the rules
            list says why they lost access, so the pair is named here along
            with the only place either can be changed. */}
        {!audienceFailed && conflicts.length > 0 && (
          <>
            <div className="mt-10 mb-4">
              {/* The one section on this page reporting something wrong
                  rather than something configured, so it is marked as such
                  before the heading is read. */}
              <div className="flex items-center gap-2">
                <TriangleAlert
                  className="h-4 w-4 shrink-0 text-amber-500"
                  aria-hidden
                />
                <Heading variant="h4">Conflicting roles</Heading>
              </div>
              <Text muted small className="mt-1">
                These people are granted access through one rule and blocked by
                another, so they cannot reach this server. To fix it, remove the
                block: on the blocking role&rsquo;s page, or from the list above
                when it names the person directly. When the block comes from a
                role or everyone, giving them access to this server by name also
                works, since that outranks it; a block on the person themselves
                still applies.
              </Text>
            </div>
            <Table columns={conflictColumns}>
              <Table.Header columns={conflictColumns} />
              <Table.Body
                columns={conflictColumns}
                data={conflicts}
                rowKey={(row) => row.member.id}
              />
            </Table>
          </>
        )}
      </Page.Section.Body>
    </Page.Section>
  );
}

/** The person a row is about: avatar, name, and the address that identifies them. */
function PersonCell({ member }: { member: AccessMember }): ReactElement {
  return (
    <div className="flex items-center gap-3">
      <Avatar className="h-8 w-8">
        {member.photoUrl && (
          <AvatarImage src={member.photoUrl} alt={member.name} />
        )}
        <AvatarFallback className="text-xs">
          {getInitials(member.name)}
        </AvatarFallback>
      </Avatar>
      <div className="min-w-0">
        <IdentityLink identifier={{ userId: member.id }}>
          <Text variant="body" className="truncate font-medium">
            {member.name}
          </Text>
        </IdentityLink>
        <Text variant="body" className="text-muted-foreground truncate text-xs">
          {member.email}
        </Text>
      </div>
    </div>
  );
}

/** Rules named on a conflict row, linked when the rule lives on a role. */
function RuleNames({ rules }: { rules: NamedRule[] }): ReactElement {
  return (
    <Text variant="body" className="text-sm break-words">
      {rules.map((rule, index) => (
        <span key={rule.principalUrn}>
          {index > 0 && ", "}
          {rule.principalUrn.startsWith("role:") ? (
            <RoleLink principalUrn={rule.principalUrn}>
              {rule.displayName}
            </RoleLink>
          ) : (
            rule.displayName
          )}
        </span>
      ))}
    </Text>
  );
}
