import { Outlet, Link, Navigate, useParams } from "react-router";

import { Page } from "@/components/page-layout";
import { PageTabsTrigger, Tabs, TabsList } from "@/components/ui/tabs";
import { useRoutes } from "@/routes";
import { useListSkills, useProductFeatures } from "@gram/client/react-query";

export function SkillsIndexRedirect(): React.JSX.Element {
  return <Navigate to="registry" replace />;
}

export function SkillsRoot(): React.JSX.Element | null {
  const routes = useRoutes();
  const { skillSlug } = useParams();
  const { data: featuresData, isPending: featuresPending } = useProductFeatures(
    undefined,
    undefined,
    {
      throwOnError: false,
    },
  );
  const { data } = useListSkills(undefined, undefined, {
    enabled: Boolean(skillSlug),
  });

  // Skills is gated on the skills_capture product feature, toggled by org
  // admins in Admin Settings. Wait for the query to resolve before gating —
  // redirecting while it's in flight would bounce direct navigations.
  if (featuresPending) {
    return null;
  }

  if (!featuresData?.skillsCaptureEnabled) {
    return <Navigate to=".." replace />;
  }

  const selectedSkill =
    data?.skills.find((skill) => skill.slug === skillSlug) ?? null;

  const activeTab = routes.skills.review.active
    ? "review"
    : routes.skills.settings.active
      ? "settings"
      : "registry";

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          fullWidth
          substitutions={
            skillSlug && selectedSkill
              ? { [skillSlug]: selectedSkill.name }
              : {}
          }
        />
      </Page.Header>
      <Page.Body fullWidth noPadding>
        <Tabs value={activeTab} className="flex h-full flex-col">
          <div className="border-b">
            <div className="px-8">
              <TabsList className="h-auto items-stretch gap-6 rounded-none bg-transparent p-0">
                <PageTabsTrigger value="registry" asChild>
                  <Link to={routes.skills.registry.href()}>Registry</Link>
                </PageTabsTrigger>
                <PageTabsTrigger value="review" asChild>
                  <Link to={routes.skills.review.href()}>Review</Link>
                </PageTabsTrigger>
                <PageTabsTrigger value="settings" asChild>
                  <Link to={routes.skills.settings.href()}>Settings</Link>
                </PageTabsTrigger>
              </TabsList>
            </div>
          </div>
          <div className="min-h-0 flex-1">
            <Outlet />
          </div>
        </Tabs>
      </Page.Body>
    </Page>
  );
}
