import { Outlet, Link, Navigate, useParams } from "react-router";

import { Page } from "@/components/page-layout";
import { PageTabsTrigger, Tabs, TabsList } from "@/components/ui/tabs";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRoutes } from "@/routes";
import { useListSkills, useProductFeatures } from "@gram/client/react-query";

export function SkillsIndexRedirect() {
  return <Navigate to="registry" replace />;
}

export function SkillsRoot() {
  const routes = useRoutes();
  const { skillSlug } = useParams();
  const telemetry = useTelemetry();
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

  // Double-gated during rollout: the gram-skills-management PostHog flag hides
  // skills from all users by default; skills_capture is the org capability.
  // isFeatureEnabled returns undefined until PostHog flags load — wait for
  // both sources to resolve before gating, since redirecting while either is
  // in flight would bounce direct navigations to skills URLs.
  const flagEnabled = telemetry.isFeatureEnabled("gram-skills-management");
  if (featuresPending || flagEnabled === undefined) {
    return null;
  }

  if (!flagEnabled || !featuresData?.skillsCaptureEnabled) {
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
