import { IdentityLink } from "@/components/identity-link";
import { Page } from "@/components/page-layout";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";
import { Badge } from "@/components/ui/Badge";
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
import {
  effectiveReach,
  LEVEL_MENU_LABEL,
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
    // Only a block covering the whole server removes someone. A block
    // narrowed to an annotation takes those tools away and leaves the rest,
    // so the person still reaches this server — with an exception.
    const blockedIds = new Set(
      entries
        .filter(
          (entry) =>
            entry.level === "blocked" &&
            (entry.tools ?? []).length === 0 &&
            (entry.dispositions ?? []).length === 0,
        )
        .flatMap((entry) => entry.memberIds ?? []),
    );
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
        if (blockedIds.has(member.id)) return null;
        const reach = effectiveReach(reaching.get(member.id) ?? []);
        if (!reach) return null;
        return { member, reach };
      })
      .filter((row): row is MemberAccess => row !== null)
      .sort((a, b) => a.member.name.localeCompare(b.member.name));
  }, [membersData?.members, entries]);

  const memberColumns: Column<MemberAccess>[] = [
    {
      key: "member",
      header: "Person",
      width: "300px",
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
      key: "level",
      header: "Access",
      width: "230px",
      // Every capability the level carries, not just its name: "View" alone
      // read as though Hana could not call the server's tools, when a read
      // grant satisfies a connect check.
      render: (row) => (
        <div className="flex flex-wrap gap-1">
          {row.reach.capabilities.map((capability) => (
            <Badge key={capability} variant="neutral">
              <Badge.Text>{LEVEL_MENU_LABEL[capability]}</Badge.Text>
            </Badge>
          ))}
        </div>
      ),
    },
    {
      key: "tools",
      header: "Tool access",
      width: "1fr",
      // The rule that reaches someone may cover the whole server or a slice of
      // it, and that is the part a reader cannot infer from the level alone.
      // Tool access is about connect: view and manage are server-level, and
      // both satisfy a connect check, so an unnarrowed rule at any level
      // reaches every tool.
      render: (row) => (
        <div className="min-w-0 space-y-0.5">
          <Text variant="body" className="text-sm">
            {row.reach.toolsLabel}
            {row.reach.ineffective && (
              <Text muted small as="span">
                {" "}
                — the {row.reach.ineffective.narrowing} rule has no effect,{" "}
                {row.reach.ineffective.because} already grants all tools
              </Text>
            )}
          </Text>
          {row.reach.scopedLevels.map((scoped) => (
            <Text key={scoped} muted small>
              {scoped}
            </Text>
          ))}
          {row.reach.excluded.map((excluded) => (
            <Text key={excluded} muted small>
              except {excluded}
            </Text>
          ))}
        </div>
      ),
    },
    {
      key: "via",
      header: "Granted by",
      width: "200px",
      render: (row) => {
        const names = row.reach.grantedBy.join(", ");
        // Roles wrap rather than truncate: which role opened a server is the
        // answer someone came to this table for.
        return (
          <Text variant="body" className="text-sm break-words">
            {names}
          </Text>
        );
      },
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
      </Page.Section.Body>
    </Page.Section>
  );
}
