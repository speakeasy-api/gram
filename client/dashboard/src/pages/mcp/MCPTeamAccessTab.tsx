import { IdentityLink } from "@/components/identity-link";
import { Page } from "@/components/page-layout";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import { Heading } from "@/components/ui/Heading";
import { Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import type { ToolAnnotation } from "@/components/tool-selection/ToolSelectionPanel";
import type { Tool } from "@/lib/toolTypes";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { ResourceAudienceEntry } from "@gram/client/models/components/resourceaudienceentry.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useResourceAudience } from "@gram/client/react-query/resourceAudience.js";
import { useMemo, type ReactElement } from "react";
import { ManageAccess } from "./access/ManageAccess";
import { RoleLink } from "./access/RoleLink";
import {
  blockingRules,
  effectiveReach,
  LEVEL_VERB,
  type AudienceLevel,
  type EffectiveReach,
} from "./access/serverAudience";

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

interface MemberAccess {
  member: AccessMember;
  reach: EffectiveReach;
}

/** A rule named on a row, linked when it is a role someone can go and edit. */
interface NamedRule {
  principalUrn: string;
  displayName: string;
}

/**
 * Someone two rules disagree about: a role gives them the server and another
 * role they are also in takes it away. A block outranks every grant, so the
 * grant does nothing — which is invisible from either role's own page.
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
}: {
  resourceId: string;
  serverName?: string;
  /** The server's tools, when the backend exposes a catalogue for it. */
  tools?: Tool[];
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
  const { data: membersData, isLoading: membersLoading } = useMembers();

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

  // Who the rules actually reach. The server resolves each rule to the members
  // it currently covers, so a directory attribute like job_title reaches
  // whoever carries that value today. This is a lookup, not a guess.
  const people = useMemo((): MemberAccess[] => {
    const members = membersData?.members ?? [];
    // Every rule that names a person, not just the first: grants add, so what
    // someone can do here is their union, and a rule that looks narrow on the
    // Access list may be doing nothing at all.
    const reaching = new Map<string, ResourceAudienceEntry[]>();
    for (const entry of entries) {
      for (const memberId of entry.memberIds ?? []) {
        reaching.set(memberId, [...(reaching.get(memberId) ?? []), entry]);
      }
    }

    return members
      .map((member) => {
        // effectiveReach returns null when every capability this server
        // offered has been blocked away, which is what "does not reach" is.
        const reach = effectiveReach(
          reaching.get(member.id) ?? [],
          toolCatalog ?? [],
        );
        if (!reach) return null;
        return { member, reach };
      })
      .filter((row): row is MemberAccess => row !== null)
      .sort((a, b) => a.member.name.localeCompare(b.member.name));
  }, [membersData?.members, entries, toolCatalog]);

  // The people two rules disagree about. They are not in the list above —
  // nothing they were given survives — and an absence explains nothing, so
  // they get a section that names both rules and says how to resolve it.
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
        const blocks = blockingRules(rules);
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

  const memberColumns: Column<MemberAccess>[] = [
    {
      key: "member",
      header: "Person",
      width: "300px",
      render: (row) => <PersonCell member={row.member} />,
    },
    {
      key: "platform",
      header: "Platform access",
      width: "230px",
      // What they can do with the server in Gram, as opposed to through it.
      // Connecting belongs to the next column, since how far it reaches is
      // the whole of that answer.
      render: (row) => (
        <Text variant="body" className="text-sm">
          {platformAccess(row.reach.capabilities)}
        </Text>
      ),
    },
    {
      key: "tools",
      header: "MCP access",
      width: "1fr",
      // The rule that reaches someone may cover the whole server or a slice of
      // it, and that is the part a reader cannot infer from the level alone.
      // Tool access is about connect: view and manage are server-level, and
      // both satisfy a connect check, so an unnarrowed rule at any level
      // reaches every tool.
      render: (row) => <MCPAccessCell reach={row.reach} />,
    },
    {
      key: "via",
      header: "Granted by",
      width: "200px",
      // Roles wrap rather than truncate: which role opened a server is the
      // answer someone came to this table for.
      render: (row) => (
        <Text variant="body" className="text-sm break-words">
          {row.reach.grantedBy.join(", ")}
        </Text>
      ),
    },
  ];

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

        <div className="mt-10 mb-4">
          <Heading variant="h4">People this reaches</Heading>
          <Text muted small className="mt-1">
            {audienceFailed
              ? "Unavailable while the access rules cannot be read"
              : membersLoading
                ? "Resolving members"
                : `${people.length} team member${people.length === 1 ? "" : "s"} can reach this server`}
          </Text>
        </div>
        {/* Without the rules, nobody resolves — which is not the same as
            nobody having access, and must not read as it. */}
        {audienceFailed ? null : (
          <Table columns={memberColumns}>
            <Table.Header columns={memberColumns} />
            {people.length === 0 ? (
              <Table.NoResultsMessage>
                <div className="text-center">
                  No team members can reach this server.
                </div>
              </Table.NoResultsMessage>
            ) : (
              <Table.Body
                columns={memberColumns}
                data={people}
                rowKey={(row) => row.member.id}
              />
            )}
          </Table>
        )}

        {/* Two rules disagreeing about the same person. They are missing from
            the list above and nothing there says why, so the pair is named
            here along with the only place either can be changed. */}
        {!audienceFailed && conflicts.length > 0 && (
          <>
            <div className="mt-10 mb-4">
              <Heading variant="h4">Conflicting roles</Heading>
              <Text muted small className="mt-1">
                These people are granted access through one role and blocked by
                another role they are also in. A block outranks every grant, so
                they cannot reach this server. To fix it, remove them from the
                role that blocks access on that role&rsquo;s page.
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

/** How far one person's connect access reaches inside the server. */
function MCPAccessCell({ reach }: { reach: EffectiveReach }): ReactElement {
  return (
    <div className="min-w-0 space-y-0.5">
      {reach.reachableTools.length > 0 ? (
        // The count is the answer; the names are what someone hovers to
        // check, and there is no room for them in the cell.
        <Tooltip delayDuration={0}>
          <TooltipTrigger asChild>
            <Text
              variant="body"
              className="cursor-help text-sm underline decoration-dotted underline-offset-4"
            >
              {reach.toolsLabel}
            </Text>
          </TooltipTrigger>
          <TooltipContent>{reach.reachableTools.join(", ")}</TooltipContent>
        </Tooltip>
      ) : (
        <Text variant="body" className="text-sm">
          {reach.toolsLabel}
        </Text>
      )}
      {reach.scopedLevels.map((scoped) => (
        <Text key={scoped.id} muted small>
          {scoped.label}
        </Text>
      ))}
      {reach.excluded.map((excluded) => (
        <Text key={excluded.id} muted small>
          except {excluded.label}
        </Text>
      ))}
    </div>
  );
}

/**
 * What someone can do with this server inside Gram: see it in the catalogue,
 * and change its settings. Connecting is left out — it is about calling the
 * server's tools, which the MCP access column answers in full.
 */
function platformAccess(capabilities: AudienceLevel[]): string {
  const can = capabilities.filter((capability) => capability !== "use");
  if (can.length === 0) return "None";
  return `Can ${can.map((capability) => LEVEL_VERB[capability]).join(" & ")}`;
}
