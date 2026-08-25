import { useProjectGuideStarted } from "@/components/project-guide/projectGuideStores";
import { useSlugs } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { ArrowRight, Sparkles } from "lucide-react";
import { Link } from "react-router";

export function ProjectGuideSidebarCta(): JSX.Element | null {
  const { orgSlug, projectSlug } = useSlugs();
  const routes = useRoutes();

  if (!useProjectGuideStarted(orgSlug, projectSlug)) return null;

  return (
    <div className="group border-border/60 bg-card relative overflow-hidden border p-3 group-data-[collapsible=icon]:border-0 group-data-[collapsible=icon]:bg-transparent group-data-[collapsible=icon]:p-0">
      <span
        aria-hidden="true"
        className="bg-gradient-primary pointer-events-none absolute inset-x-0 top-0 z-10 h-px group-data-[collapsible=icon]:hidden"
      />
      <Link
        to={routes.guide.href()}
        aria-label="Project guide"
        className="text-foreground block focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset group-data-[collapsible=icon]:hidden"
      >
        <span className="relative z-10 flex gap-2.5">
          <span className="bg-primary/10 text-primary flex size-8 shrink-0 items-center justify-center">
            <Sparkles className="size-4" />
          </span>
          <span className="min-w-0">
            <span className="text-foreground block text-sm font-medium">
              Project guide
            </span>
            <span className="text-muted-foreground mt-0.5 block text-xs leading-relaxed">
              Continue getting data into your project.
            </span>
          </span>
        </span>
        <span className="text-foreground relative z-10 mt-3 flex items-center gap-1 text-sm font-medium">
          Continue
          <ArrowRight className="size-3.5 transition-transform group-hover:translate-x-0.5" />
        </span>
      </Link>
      <Link
        to={routes.guide.href()}
        aria-label="Project guide"
        title="Project guide"
        className="hidden text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset group-data-[collapsible=icon]:flex group-data-[collapsible=icon]:size-8 group-data-[collapsible=icon]:items-center group-data-[collapsible=icon]:justify-center"
      >
        <Sparkles className="size-4" />
      </Link>
    </div>
  );
}
