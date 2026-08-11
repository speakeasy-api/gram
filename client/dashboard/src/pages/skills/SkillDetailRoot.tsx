import { Page } from "@/components/page-layout";
import {
  RouteNotFoundState,
  SecondaryRouteAction,
} from "@/components/route-not-found-state";
import { Skeleton } from "@/components/ui/Skeleton";
import { isNotFoundError } from "@/lib/route-errors";
import { useRoutes } from "@/routes";
import { useSkill } from "@gram/client/react-query/skill.js";
import { Navigate, Outlet, useLocation, useParams } from "react-router";
import { pageFromLegacySkillHash, pagePath } from "./SkillDetailRouting";
import { useSkillVersionLabels } from "./use-skill-version-labels";

export default function SkillDetailRoot(): JSX.Element {
  const { skillId } = useParams<{ skillId: string }>();
  const routes = useRoutes();
  const location = useLocation();
  const skillQuery = useSkill({ id: skillId ?? "" }, undefined, {
    throwOnError: false,
    enabled: !!skillId,
  });
  const versionState = useSkillVersionLabels(
    skillId ?? "",
    skillQuery.data?.skill.versionCount ?? 0,
  );

  if (skillId) {
    const legacyPage = pageFromLegacySkillHash(location.hash);
    const isBaseRoute =
      location.pathname === routes.skills.detail.href(skillId);
    if (isBaseRoute || legacyPage) {
      const page = legacyPage ?? "overview";
      return (
        <Navigate
          to={`${routes.skills.detail.href(skillId)}/${pagePath(page)}`}
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

  if (skillQuery.isPending || !skillQuery.data) return <SkillDetailLoading />;

  const { skill } = skillQuery.data;
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{ [skillId]: skill.displayName }}
        />
      </Page.Header>
      <Page.Body fullWidth className="gap-0">
        <div className="mx-auto w-full max-w-[1270px] flex-1 space-y-10 px-8 py-8">
          <Outlet
            context={{
              skillQueryData: skillQuery.data,
              ...versionState,
            }}
          />
        </div>
      </Page.Body>
    </Page>
  );
}

function SkillDetailLoading(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body fullWidth className="gap-0">
        <div
          aria-label="Loading skill"
          className="mx-auto w-full max-w-[1270px] flex-1 space-y-10 px-8 py-8"
        >
          <Skeleton className="h-36 w-full" />
          <Skeleton className="h-80 w-full" />
          <Skeleton className="h-48 w-full" />
        </div>
      </Page.Body>
    </Page>
  );
}
