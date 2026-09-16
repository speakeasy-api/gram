import { buildObserveHref } from "@/components/observe/observeDeepLink";
import { useSlugs } from "@/contexts/Sdk";
import { cn } from "@/lib/utils";
import { Link, useLocation, useSearchParams } from "react-router";

type ObserveView = "insights" | "logs";

const TABS: Array<{ view: ObserveView; label: string }> = [
  { view: "insights", label: "Insights" },
  { view: "logs", label: "Tool Logs" },
];

/**
 * Switches between the two observe views without losing the filters.
 *
 * The pages answer the same question at different resolutions — the aggregates
 * on one, the rows behind them on the other — so a reader who has narrowed to
 * one server over one week expects to keep that narrowing when they cross
 * over. Carrying the shared params also keeps each tab a real link: it can be
 * middle-clicked or copied, and it lands on the other view already filtered.
 */
export function ObserveViewTabs({
  active,
}: {
  active: ObserveView;
}): JSX.Element {
  const { orgSlug, projectSlug } = useSlugs();
  const [searchParams] = useSearchParams();
  const location = useLocation();

  return (
    <div className="border-border flex h-auto w-full items-end border-b px-8">
      {TABS.map((tab) => {
        const base = `/${orgSlug}/projects/${projectSlug}/${tab.view}`;
        const isActive =
          tab.view === active || location.pathname.startsWith(base + "/");
        return (
          <Link
            key={tab.view}
            to={buildObserveHref(
              base,
              searchParams,
              {},
              {
                // Insights answers from the summary endpoints, which have no
                // status field.
                summaryOnly: tab.view === "insights",
              },
            )}
            aria-current={isActive ? "page" : undefined}
            className={cn(
              "relative flex-none px-4 py-3 text-sm font-medium no-underline transition-colors",
              "after:absolute after:right-0 after:bottom-0 after:left-0 after:h-0.5",
              "inline-flex items-center gap-2",
              isActive
                ? "text-foreground after:bg-primary"
                : "text-muted-foreground hover:text-foreground bg-transparent after:bg-transparent",
            )}
          >
            {tab.label}
          </Link>
        );
      })}
    </div>
  );
}
