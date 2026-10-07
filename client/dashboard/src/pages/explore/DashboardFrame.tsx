import { Page } from "@/components/page-layout";
import { Icon } from "@/components/ui/Icon";
import type { JSX, ReactNode } from "react";
import { Link } from "react-router";

/**
 * The chrome a dashboard page draws around its cards, the same for a
 * project dashboard and a Speakeasy-built one: the way back to the list,
 * the name and what the dashboard is for, who it is by, its actions, the
 * filter bar's row, and then whatever the page puts under it.
 */
export function DashboardFrame({
  backHref,
  backState,
  heading = "section",
  name,
  description,
  badge,
  byline,
  actions,
  toolbar,
  children,
}: {
  /** Where the list of dashboards is; a page with no list above it has none. */
  backHref?: string | undefined;
  backState?: unknown;
  /**
   * How the name is drawn: as a section under a page's tabs, or as the
   * page's own title when the dashboard is the page.
   */
  heading?: "section" | "page";
  name: string;
  description?: string | undefined;
  /** Drawn beside the name: how the dashboard is marked, when it is. */
  badge?: ReactNode;
  /** Under the name: who made the dashboard and when it changed. */
  byline: ReactNode;
  /** The buttons and menu at the top right. */
  actions: ReactNode;
  /** The filter bar's row, when the page has cards to filter. */
  toolbar?: ReactNode;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-3">
        {backHref === undefined ? null : (
          <DashboardBackLink href={backHref} state={backState} />
        )}
        <div className="flex items-start justify-between gap-4">
          <div className="flex min-w-0 flex-col gap-1">
            <div className="flex min-w-0 items-center gap-2">
              {heading === "page" ? (
                <h1 className="text-display-sm truncate font-thin" title={name}>
                  {name}
                </h1>
              ) : (
                <h2 className="text-heading-lg truncate" title={name}>
                  {name}
                </h2>
              )}
              {badge}
            </div>
            {description ? (
              <p className="text-muted-foreground text-sm">{description}</p>
            ) : null}
            <p className="text-muted-foreground text-xs">{byline}</p>
          </div>
          <div className="flex shrink-0 items-center gap-2">{actions}</div>
        </div>
      </div>
      {toolbar ? (
        <Page.Toolbar>
          <Page.Toolbar.Row>{toolbar}</Page.Toolbar.Row>
        </Page.Toolbar>
      ) : null}
      {children}
    </div>
  );
}

/** The way back to the list of dashboards. */
export function DashboardBackLink({
  href,
  state,
}: {
  href: string;
  state: unknown;
}): JSX.Element {
  return (
    <Link
      to={href}
      state={state}
      className="text-muted-foreground hover:text-foreground inline-flex w-max items-center gap-1 text-xs no-underline hover:underline"
    >
      <Icon name="arrow-left" className="size-3" aria-hidden />
      All dashboards
    </Link>
  );
}
