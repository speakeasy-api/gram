import {
  DetailSidebarInfoLabel,
  DetailSidebarNav,
  type DetailSidebarNavItem,
} from "@/components/detail/detail-sidebar-nav";
import { Text } from "@/components/ui/Text";
import { useDrainInfiniteQuery } from "@/hooks/useDrainInfiniteQuery";
import { HumanizeDateTime } from "@/lib/dates";
import {
  SkillClassificationBadge,
  SkillSourceBadge,
} from "@/pages/skills/skill-badges";
import { SkillSharingCardBlocks } from "@/pages/skills/SkillSharingControl";
import { useRoutes } from "@/routes";
import { useSkill } from "@gram/client/react-query/skill.js";
import { useSkillDistributionsInfinite } from "@gram/client/react-query/skillDistributions.js";
import { Badge } from "@/components/ui/Badge";
import {
  Activity,
  FileText,
  History,
  LayoutDashboard,
  ListChecks,
  MessageSquareText,
  Settings,
} from "lucide-react";
import * as React from "react";
import { useParams } from "react-router";

export function SkillDetailSidebarNav(): React.JSX.Element | null {
  const routes = useRoutes();
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
  // Drained so the count reflects every distribution, not the first page.
  useDrainInfiniteQuery(distributionsQuery, !!skillId);

  if (!skillId) return null;

  const skill = skillQuery.data?.skill;
  const latestVersion = skillQuery.data?.latestVersion;
  const distributionCount =
    distributionsQuery.data?.pages.flatMap((page) => page.result.distributions)
      .length ?? 0;

  const items: DetailSidebarNavItem[] = [
    {
      key: "overview",
      title: "Overview",
      Icon: LayoutDashboard,
      href: routes.skills.detail.overview.href(skillId),
      active: routes.skills.detail.overview.active,
    },
    {
      key: "content",
      title: "Skill Content",
      Icon: FileText,
      href: routes.skills.detail.content.href(skillId),
      active: routes.skills.detail.content.active,
    },
    {
      key: "usage",
      title: "Usage",
      Icon: Activity,
      href: routes.skills.detail.usage.href(skillId),
      active: routes.skills.detail.usage.active,
    },
    {
      key: "scored-sessions",
      title: "Scored Sessions",
      Icon: ListChecks,
      href: routes.skills.detail.scoredSessions.href(skillId),
      active: routes.skills.detail.scoredSessions.active,
    },
    {
      key: "feedback",
      title: "Agent Feedback",
      Icon: MessageSquareText,
      href: routes.skills.detail.feedback.href(skillId),
      active: routes.skills.detail.feedback.active,
    },
    {
      key: "versions",
      title: "Version History",
      Icon: History,
      href: routes.skills.detail.versions.href(skillId),
      active: routes.skills.detail.versions.active,
    },
    {
      key: "settings",
      title: "Settings",
      Icon: Settings,
      href: routes.skills.detail.settings.href(skillId),
      active: routes.skills.detail.settings.active,
    },
  ];

  const cardContent = skill && (
    <>
      <div className="flex flex-col gap-0.5">
        <Text className="truncate font-semibold">{skill.displayName}</Text>
        <Text variant="small" muted className="truncate font-mono text-xs">
          {skill.name}
        </Text>
      </div>

      <div className="flex flex-wrap gap-1.5">
        <SkillSourceBadge value={skill.sourceKind} />
        <SkillClassificationBadge value={skill.classification} />
        {latestVersion && !latestVersion.specValid && (
          <Badge variant="destructive">Needs review</Badge>
        )}
      </div>

      <SkillSharingCardBlocks skill={skill} />

      <div className="flex flex-col gap-1">
        <DetailSidebarInfoLabel>Distributions</DetailSidebarInfoLabel>
        <Text variant="small" muted className="text-xs">
          {distributionCount === 1
            ? "1 plugin"
            : `${distributionCount}${
                distributionsQuery.hasNextPage ? "+" : ""
              } plugins`}
        </Text>
      </div>

      <div className="flex flex-col gap-1">
        <DetailSidebarInfoLabel>Versions</DetailSidebarInfoLabel>
        <Text variant="small" muted className="text-xs">
          {skill.versionCount} · updated{" "}
          <HumanizeDateTime date={skill.updatedAt} />
        </Text>
      </div>

      <div className="flex flex-col gap-1">
        <DetailSidebarInfoLabel>Activations</DetailSidebarInfoLabel>
        <Text variant="small" muted className="text-xs">
          {skill.seenCount}
          {skill.lastSeenAt && (
            <>
              {" "}
              · last <HumanizeDateTime date={skill.lastSeenAt} />
            </>
          )}
        </Text>
      </div>
    </>
  );

  return (
    <DetailSidebarNav
      backHref={routes.skills.href()}
      backLabel="Back to all skills"
      cardContent={cardContent}
      items={items}
      itemsTitle="Configuration"
    />
  );
}
