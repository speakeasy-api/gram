import { useProjectGuideStarted } from "@/components/project-guide/projectGuideStores";
import { useSlugs } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { RouteIcon, ArrowRight } from "lucide-react";
import { SidebarFooterAction } from "./sidebar-footer-action";

export function ProjectGuideSidebarCta(): JSX.Element | null {
  const { orgSlug, projectSlug } = useSlugs();
  const routes = useRoutes();

  if (!useProjectGuideStarted(orgSlug, projectSlug) || routes.guide.active) {
    return null;
  }

  return (
    <SidebarFooterAction
      to={routes.guide.href()}
      icon={RouteIcon}
      label="Resume the Project Guide"
      trailing={<ArrowRight className="size-4" />}
    />
  );
}
