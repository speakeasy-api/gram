import { mcpServerRouteParam } from "@/lib/sources";
import { skillLinkTargetName } from "@/pages/skills/skill-links";
import type { MetaMcpMember } from "@gram/client/models/components/metamcpmember.js";
import type { SkillLink } from "@gram/client/models/components/skilllink.js";

/** A member server a skill is inherited from. */
export type InheritedSkillServer = {
  name: string;
  /** The server's route segment: its slug, or its id when it has none. */
  routeParam: string;
};

/** One skill a gateway inherits, with the member servers it comes from. */
export type InheritedSkill = {
  skillId: string;
  skillName: string;
  skillDisplayName: string;
  /** Member servers, sorted by name. */
  servers: InheritedSkillServer[];
};

/**
 * The skills linked to any of a gateway's member servers, one entry per
 * skill, sorted by display name. Links to servers outside the gateway are
 * ignored.
 */
export function inheritedSkills(
  links: readonly SkillLink[],
  members: readonly Pick<
    MetaMcpMember,
    "mcpServerId" | "mcpServerName" | "mcpServerSlug"
  >[],
): InheritedSkill[] {
  const membersById = new Map(
    members.map((member) => [member.mcpServerId, member]),
  );
  const bySkill = new Map<string, InheritedSkill>();
  for (const link of links) {
    if (link.targetKind !== "mcp_server" || !link.mcpServerId) continue;
    const member = membersById.get(link.mcpServerId);
    if (!member) continue;
    const server: InheritedSkillServer = {
      name: member.mcpServerName || skillLinkTargetName(link),
      routeParam: mcpServerRouteParam({
        id: member.mcpServerId,
        slug: member.mcpServerSlug,
      }),
    };
    const entry: InheritedSkill = bySkill.get(link.skillId) ?? {
      skillId: link.skillId,
      skillName: link.skillName,
      skillDisplayName: link.skillDisplayName,
      servers: [],
    };
    if (!entry.servers.some((known) => known.routeParam === server.routeParam))
      entry.servers.push(server);
    bySkill.set(link.skillId, entry);
  }
  return [...bySkill.values()]
    .map((entry) => ({
      ...entry,
      servers: [...entry.servers].sort((a, b) => a.name.localeCompare(b.name)),
    }))
    .sort((a, b) => a.skillDisplayName.localeCompare(b.skillDisplayName));
}
