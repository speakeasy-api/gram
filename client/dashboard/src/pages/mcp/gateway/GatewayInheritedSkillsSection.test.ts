import type { SkillLink } from "@gram/client/models/components/skilllink.js";
import { describe, expect, it } from "vitest";
import { inheritedSkills } from "./GatewayInheritedSkillsSection";

function serverLink(
  skillId: string,
  displayName: string,
  mcpServerId: string,
  targetName?: string,
): SkillLink {
  return {
    id: `${skillId}:${mcpServerId}`,
    createdAt: new Date(0),
    skillId,
    skillName: displayName.toLowerCase(),
    skillDisplayName: displayName,
    targetKind: "mcp_server",
    mcpServerId,
    targetName,
  };
}

describe("inheritedSkills", () => {
  const members = [
    { mcpServerId: "notion", mcpServerName: "Notion" },
    { mcpServerId: "linear", mcpServerName: "Linear" },
  ];

  it("groups member links by skill and names their servers", () => {
    const skills = inheritedSkills(
      [
        serverLink("s2", "Triage", "notion"),
        serverLink("s1", "Release", "linear"),
        serverLink("s2", "Triage", "linear"),
      ],
      members,
    );
    expect(skills).toEqual([
      {
        skillId: "s1",
        skillName: "release",
        skillDisplayName: "Release",
        servers: ["Linear"],
      },
      {
        skillId: "s2",
        skillName: "triage",
        skillDisplayName: "Triage",
        servers: ["Linear", "Notion"],
      },
    ]);
  });

  it("ignores links to servers outside the gateway and to gateways", () => {
    const skills = inheritedSkills(
      [
        serverLink("s1", "Outside", "github"),
        {
          ...serverLink("s2", "Direct", "notion"),
          targetKind: "meta_mcp_server",
          mcpServerId: undefined,
          metaMcpServerId: "gateway",
        },
      ],
      members,
    );
    expect(skills).toEqual([]);
  });

  it("falls back to the link's target name for an unnamed member", () => {
    const skills = inheritedSkills(
      [serverLink("s1", "Release", "unnamed", "From link")],
      [{ mcpServerId: "unnamed", mcpServerName: undefined }],
    );
    expect(skills[0]?.servers).toEqual(["From link"]);
  });
});
