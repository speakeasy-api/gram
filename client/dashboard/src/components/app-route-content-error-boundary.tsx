import type { ReactNode } from "react";
import { useLocation } from "react-router";
import { ContentErrorBoundary } from "./content-error-boundary";
import { PageHeader } from "./page-header";
import { useInsightsState } from "./insights-context";

type AppRouteContentErrorBoundaryProps = {
  children: ReactNode;
  fallback?: ReactNode;
};

export function AppRouteContentErrorBoundary({
  children,
  fallback,
}: AppRouteContentErrorBoundaryProps): JSX.Element {
  const { key } = useLocation();

  return (
    <ContentErrorBoundary resetKeys={[key]} fallback={fallback}>
      {children}
    </ContentErrorBoundary>
  );
}

/**
 * Suspense fallback for a route while its data loads. Keeps the page header
 * (top bar and breadcrumbs) on screen so the chrome does not vanish behind a
 * full-page "Loading…". Everywhere else keeps the plain fallback.
 */
export function RouteLoadingFallback(): JSX.Element {
  const { pathname } = useLocation();
  // Only project pages own the top bar; standalone routes (e.g. /headless),
  // org pages and chat routes draw their own chrome.
  const { headerChrome } = useInsightsState();
  if (!headerChrome || /\/chat(\/|$)/.test(pathname)) {
    return <div className="p-8 text-sm">Loading…</div>;
  }
  return (
    <>
      <PageHeader>
        <PageHeader.Breadcrumbs />
      </PageHeader>
      <div className="text-muted-foreground p-8 text-sm">Loading…</div>
    </>
  );
}
