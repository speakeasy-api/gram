import { Fragment, type JSX } from "react";
import { useQueries, type QueryKey } from "@tanstack/react-query";
import {
  useLinkProps,
  useMatches,
  type LinkProps,
} from "@tanstack/react-router";

import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import { Separator } from "@/components/ui/separator";
import { SidebarTrigger } from "@/components/ui/sidebar";

// Given as the query that holds the record, not as a name: the bar renders
// before a cold record's fetch lands, so it needs something to watch.
type CrumbRecord = { queryKey: QueryKey };

// The cache hands back `unknown` for a key whose data tag the crumb type drops.
function recordName(data: unknown): string | undefined {
  if (typeof data !== "object" || data === null) return undefined;
  const { name } = data as { name?: unknown };
  return typeof name === "string" ? name : undefined;
}

/**
 * What a route contributes to the bar: the words for a view, or the record the
 * route is about.
 */
export type Crumb =
  | string
  | ((params: Record<string, string | undefined>) => CrumbRecord | undefined);

declare module "@tanstack/react-router" {
  interface StaticDataRouteOption {
    crumb?: Crumb;
  }
}

// Position is the whole answer: a crumb that is not the last is not the current
// page, whatever the address says. `Link` insists on deciding that for itself
// and writes its answer last, over a handed-in `aria-current` and over
// `activeProps` alike, so the guess is taken back off rather than argued with.
// record-nav.tsx:46 keeps the same rule for the nav's items.
function CrumbLink({
  to,
  children,
}: {
  to: LinkProps["to"];
  children: string;
}): JSX.Element {
  // `exact` still earns its line, for the half of the guess this cannot take
  // back: every crumb's target is an ancestor of the address, so without it
  // every crumb would carry `data-status="active"` and the `active` class.
  const linkProps = useLinkProps({ to, activeOptions: { exact: true } });

  return (
    <BreadcrumbLink asChild>
      <a {...linkProps} aria-current={undefined}>
        {children}
      </a>
    </BreadcrumbLink>
  );
}

export function SiteHeader(): JSX.Element {
  const matches = useMatches();

  // Each source keeps the index of the match that asked for it, so a resolved
  // name goes back to the right crumb.
  const sources = matches.flatMap((match, index) => {
    const { crumb } = match.staticData;
    if (typeof crumb !== "function") return [];
    const query = crumb(match.params);
    return query ? [{ index, query }] : [];
  });

  // Watched, never fetched: the view under the bar owns the request for the
  // record it is about, so the bar costs no second call.
  const results = useQueries({
    queries: sources.map(({ query }) => ({
      queryKey: query.queryKey,
      enabled: false,
    })),
  });

  const names = new Map(
    sources.map(({ index }, position) => [
      index,
      recordName(results[position]?.data),
    ]),
  );

  const crumbs = matches.flatMap((match, index) => {
    const { crumb } = match.staticData;
    const label = typeof crumb === "function" ? names.get(index) : crumb;
    // One crumb short for a frame beats `Organizations / Loading... / Members`.
    if (!label) return [];
    // The cast holds because the bar only links to a route the operator is
    // already standing in; `useMatches` types the pathname as a plain string.
    return [{ id: match.id, to: match.pathname as LinkProps["to"], label }];
  });

  return (
    <header className="flex h-(--header-height) shrink-0 items-center gap-2 border-b transition-[width,height] ease-linear group-has-data-[collapsible=icon]/sidebar-wrapper:h-(--header-height)">
      <div className="flex w-full items-center gap-1 px-4 lg:gap-2 lg:px-6">
        <SidebarTrigger className="-ml-1" />
        <Separator
          orientation="vertical"
          className="mx-2 data-[orientation=vertical]:h-4"
        />
        <Breadcrumb>
          <BreadcrumbList className="text-base">
            {crumbs.map(({ id, to, label }, index) => (
              <Fragment key={id}>
                {index > 0 && <BreadcrumbSeparator />}
                <BreadcrumbItem>
                  {index === crumbs.length - 1 ? (
                    <BreadcrumbPage className="font-medium">
                      {label}
                    </BreadcrumbPage>
                  ) : (
                    <CrumbLink to={to}>{label}</CrumbLink>
                  )}
                </BreadcrumbItem>
              </Fragment>
            ))}
          </BreadcrumbList>
        </Breadcrumb>
      </div>
    </header>
  );
}
