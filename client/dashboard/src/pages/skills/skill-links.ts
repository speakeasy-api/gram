import type { LinkSkillRequestBodyTargetKind } from "@gram/client/models/components/linkskillrequestbody.js";
import type { SkillLink } from "@gram/client/models/components/skilllink.js";
import type { ListSkillLinksRequest } from "@gram/client/models/operations/listskilllinks.js";

/**
 * The MCP surface a skill link points at. A hosted MCP server is one
 * `mcp_servers` row (the MCP server page uses the toolset's canonical row,
 * whose id is the toolset id), a gateway is an operator-built meta MCP
 * server, and the organization gateway takes no id.
 */
export type SkillLinkTarget =
  | { kind: "mcp_server"; mcpServerId: string }
  | { kind: "meta_mcp_server"; metaMcpServerId: string }
  | { kind: "organization_gateway" };

/** The target fields of a `skills.link` request body. */
export function skillLinkTargetFields(target: SkillLinkTarget): {
  targetKind: LinkSkillRequestBodyTargetKind;
  mcpServerId?: string;
  metaMcpServerId?: string;
} {
  switch (target.kind) {
    case "mcp_server":
      return { targetKind: "mcp_server", mcpServerId: target.mcpServerId };
    case "meta_mcp_server":
      return {
        targetKind: "meta_mcp_server",
        metaMcpServerId: target.metaMcpServerId,
      };
    case "organization_gateway":
      return { targetKind: "organization_gateway" };
  }
}

/** The `skills.listLinks` filter that returns exactly this target's links. */
export function skillLinksFilter(
  target: SkillLinkTarget,
): Pick<
  ListSkillLinksRequest,
  "mcpServerId" | "metaMcpServerId" | "targetKind"
> {
  switch (target.kind) {
    case "mcp_server":
      return { targetKind: "mcp_server", mcpServerId: target.mcpServerId };
    case "meta_mcp_server":
      return {
        targetKind: "meta_mcp_server",
        metaMcpServerId: target.metaMcpServerId,
      };
    case "organization_gateway":
      return { targetKind: "organization_gateway" };
  }
}

/** A stable key for a target, for query state and React keys. */
export function skillLinkTargetKey(target: SkillLinkTarget): string {
  switch (target.kind) {
    case "mcp_server":
      return `mcp_server:${target.mcpServerId}`;
    case "meta_mcp_server":
      return `meta_mcp_server:${target.metaMcpServerId}`;
    case "organization_gateway":
      return "organization_gateway";
  }
}

/** What a link's target is called in the UI. */
export function skillLinkTargetName(
  link: Pick<SkillLink, "targetKind" | "targetName">,
): string {
  switch (link.targetKind) {
    case "organization_gateway":
      return "Organization gateway";
    case "meta_mcp_server":
      return link.targetName || "Unnamed gateway";
    case "mcp_server":
      return link.targetName || "Unnamed MCP server";
  }
}

/** What kind of surface a link's target is, in the user's words. */
export function skillLinkTargetKindLabel(
  kind: SkillLink["targetKind"],
): string {
  switch (kind) {
    case "organization_gateway":
    case "meta_mcp_server":
      return "Gateway";
    case "mcp_server":
      return "MCP server";
  }
}

/** Links ordered for display: by skill name, then by target. */
export function sortSkillLinks<
  T extends Pick<SkillLink, "skillDisplayName" | "targetKind" | "targetName">,
>(links: readonly T[]): T[] {
  return [...links].sort(
    (a, b) =>
      a.skillDisplayName.localeCompare(b.skillDisplayName) ||
      skillLinkTargetName(a).localeCompare(skillLinkTargetName(b)),
  );
}
