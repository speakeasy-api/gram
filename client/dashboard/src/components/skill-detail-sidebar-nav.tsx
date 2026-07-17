import {
  McpSidebarInfoLabel,
  McpSidebarNavShell,
  type McpSidebarNavItem,
} from "@/components/mcp-sidebar-nav-shell";
import { Type } from "@/components/ui/type";
import { HumanizeDateTime } from "@/lib/dates";
import {
  SKILL_DISTRIBUTIONS_SECTION_ID,
  SKILL_FRONTMATTER_SECTION_ID,
  SKILL_MANIFEST_SECTION_ID,
  SKILL_VERSIONS_SECTION_ID,
} from "@/pages/skills/SkillDetail";
import { useRoutes } from "@/routes";
import { useSkill } from "@gram/client/react-query/skill.js";
import { useSkillDistributionsInfinite } from "@gram/client/react-query/skillDistributions.js";
import { Badge } from "@speakeasy-api/moonshine";
import { Braces, FileText, History, Puzzle } from "lucide-react";
import * as React from "react";
import { useLocation, useParams } from "react-router";

export function SkillDetailSidebarNav(): React.JSX.Element | null {
  const routes = useRoutes();
  const location = useLocation();
  const { skillId } = useParams<{ skillId: string }>();

  const skillQuery = useSkill({ id: skillId ?? "" }, undefined, {
    throwOnError: false,
    enabled: !!skillId,
  });
  const distributionsQuery = useSkillDistributionsInfinite(
    { skillId: skillId ?? "", limit: 50 },
    undefined,
    { throwOnError: false, enabled: !!skillId },
  );

  if (!skillId) return null;

  const skill = skillQuery.data?.skill;
  const latestVersion = skillQuery.data?.latestVersion;
  const distributionCount =
    distributionsQuery.data?.pages.flatMap((page) => page.result.distributions)
      .length ?? 0;
  const hasFrontmatter =
    Object.keys(latestVersion?.frontmatter ?? {}).filter(
      (key) => key !== "name" && key !== "description",
    ).length > 0;

  const detailHref = routes.skills.detail.href(skillId);
  const activeSectionId = location.hash.replace("#", "");
  const sectionItem = (
    sectionId: string,
    title: string,
    Icon: React.ComponentType<{ className?: string }>,
    isDefault = false,
  ): McpSidebarNavItem => ({
    key: sectionId,
    title,
    Icon,
    href: `${detailHref}#${sectionId}`,
    active:
      activeSectionId === sectionId || (isDefault && activeSectionId === ""),
  });

  const items: McpSidebarNavItem[] = [
    sectionItem(SKILL_MANIFEST_SECTION_ID, "SKILL.md", FileText, true),
    ...(hasFrontmatter
      ? [sectionItem(SKILL_FRONTMATTER_SECTION_ID, "Frontmatter", Braces)]
      : []),
    sectionItem(SKILL_DISTRIBUTIONS_SECTION_ID, "Plugin distributions", Puzzle),
    sectionItem(SKILL_VERSIONS_SECTION_ID, "Version history", History),
  ];

  const cardContent = skill && (
    <>
      <div className="flex flex-col gap-0.5">
        <Type className="truncate font-semibold">{skill.displayName}</Type>
        <Type variant="small" muted className="truncate font-mono text-xs">
          {skill.name}
        </Type>
      </div>

      <div className="flex flex-wrap gap-1.5">
        <Badge variant="neutral">{skill.classification}</Badge>
        {latestVersion && !latestVersion.specValid && (
          <Badge variant="destructive">Needs review</Badge>
        )}
      </div>

      <div className="flex flex-col gap-1">
        <McpSidebarInfoLabel>Source</McpSidebarInfoLabel>
        <Type variant="small" muted className="font-mono text-xs">
          {skill.sourceKind}
        </Type>
      </div>

      <div className="flex flex-col gap-1">
        <McpSidebarInfoLabel>Distributions</McpSidebarInfoLabel>
        <Type variant="small" muted className="text-xs">
          {distributionCount === 1
            ? "1 plugin"
            : `${distributionCount} plugins`}
        </Type>
      </div>

      <div className="flex flex-col gap-1">
        <McpSidebarInfoLabel>Versions</McpSidebarInfoLabel>
        <Type variant="small" muted className="text-xs">
          {skill.versionCount} · updated{" "}
          <HumanizeDateTime date={skill.updatedAt} />
        </Type>
      </div>
    </>
  );

  return (
    <McpSidebarNavShell
      backHref={routes.skills.href()}
      backLabel="Back to all skills"
      cardContent={cardContent}
      items={items}
      itemsTitle="Sections"
    />
  );
}
