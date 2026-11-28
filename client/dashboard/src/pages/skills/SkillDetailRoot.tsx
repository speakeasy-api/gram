import {
  DetailFrame,
  DetailHeaderControls,
  DetailHeaderLayout,
  DetailHeaderTitle,
  DetailHeaderTitleSkeleton,
  type DetailFrameTab,
} from "@/components/detail/detail-frame";
import { Page } from "@/components/page-layout";
import {
  RouteNotFoundState,
  SecondaryRouteAction,
} from "@/components/route-not-found-state";
import { useRecentLabelOverride } from "@/components/command-palette/recentlyVisited";
import { Badge } from "@/components/ui/Badge";
import { isNotFoundError } from "@/lib/route-errors";
import { useRoutes } from "@/routes";
import type { GetSkillResult } from "@gram/client/models/components/getskillresult.js";
import { useSkill } from "@gram/client/react-query/skill.js";
import { Navigate, Outlet, useLocation, useParams } from "react-router";
import { SkillClassificationBadge, SkillSourceBadge } from "./skill-badges";
import { SkillPublicLink, SkillVisibilityControl } from "./SkillSharingControl";

export default function SkillDetailRoot(): JSX.Element {
  const { skillId } = useParams<{ skillId: string }>();
  const routes = useRoutes();
  const location = useLocation();
  const skillQuery = useSkill({ id: skillId ?? "" }, undefined, {
    throwOnError: false,
    enabled: !!skillId,
  });
  useRecentLabelOverride(location.pathname, skillQuery.data?.skill.displayName);
  if (skillId) {
    const isBaseRoute =
      location.pathname === routes.skills.detail.href(skillId);
    if (isBaseRoute) {
      return (
        <Navigate
          to={`${routes.skills.detail.href(skillId)}/overview`}
          replace
        />
      );
    }
  }

  if (
    skillQuery.error &&
    !skillQuery.data &&
    !isNotFoundError(skillQuery.error)
  ) {
    throw skillQuery.error;
  }

  if (!skillId || (skillQuery.error && !skillQuery.data)) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <RouteNotFoundState
            title="Skill not found"
            description="This skill may have been archived or removed from this project."
            action={
              <routes.skills.Link>
                <SecondaryRouteAction>Back to skills</SecondaryRouteAction>
              </routes.skills.Link>
            }
          />
        </Page.Body>
      </Page>
    );
  }

  const data = skillQuery.data;
  return (
    <SkillDetailFrame skillId={skillId} data={data}>
      {data ? <Outlet context={{ skillQueryData: data }} /> : null}
    </SkillDetailFrame>
  );
}

/**
 * The skill's detail frame: the shared detail header (name, badges, public
 * link, visibility) over the skill's tabs. The tabs show while the skill
 * loads, so nothing shifts when it arrives.
 */
function SkillDetailFrame({
  skillId,
  data,
  children,
}: {
  skillId: string;
  /** Undefined while the skill loads. */
  data: GetSkillResult | undefined;
  children: React.ReactNode;
}): JSX.Element {
  const routes = useRoutes();
  const detail = routes.skills.detail;
  const tabs: (DetailFrameTab & { active: boolean })[] = [
    { key: "overview", title: "Overview", route: detail.overview },
    { key: "content", title: "Content", route: detail.content },
    { key: "usage", title: "Usage", route: detail.usage },
    {
      key: "scored-sessions",
      title: "Scored sessions",
      route: detail.scoredSessions,
    },
    { key: "feedback", title: "Agent feedback", route: detail.feedback },
    { key: "versions", title: "Versions", route: detail.versions },
    { key: "settings", title: "Settings", route: detail.settings },
  ].map(({ route, ...tab }) => ({
    ...tab,
    href: route.href(skillId),
    active: route.active,
  }));
  const skill = data?.skill;

  return (
    <DetailFrame
      breadcrumbs={{
        substitutions: { [skillId]: skill?.displayName },
      }}
      tabsLabel="Skill sections"
      tabs={tabs}
      activeTab={tabs.find((tab) => tab.active)?.key}
      header={
        <DetailHeaderLayout
          title={
            skill ? (
              <DetailHeaderTitle
                eyebrow="Distribute · Skill"
                name={skill.displayName}
                below={
                  <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm">
                    <span
                      className="text-muted-foreground max-w-[320px] truncate font-mono text-xs leading-6"
                      title={skill.name}
                    >
                      {skill.name}
                    </span>
                    <div className="flex flex-wrap gap-1.5">
                      <SkillSourceBadge value={skill.sourceKind} />
                      <SkillClassificationBadge value={skill.classification} />
                      {data.latestVersion && !data.latestVersion.specValid && (
                        <Badge variant="destructive">Needs review</Badge>
                      )}
                    </div>
                    <SkillPublicLink skill={skill} />
                  </div>
                }
              />
            ) : (
              <DetailHeaderTitleSkeleton icon={false} />
            )
          }
          controls={
            skill ? (
              <DetailHeaderControls
                rows={[
                  {
                    key: "visibility",
                    label: "Visibility",
                    control: <SkillVisibilityControl skill={skill} />,
                  },
                ]}
              />
            ) : null
          }
        />
      }
    >
      <div className="space-y-10">{children}</div>
    </DetailFrame>
  );
}
