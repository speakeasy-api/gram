import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRoutes } from "@/routes";
import { useMemo } from "react";
import { useLocation } from "react-router";
import { InsightsAgentsPage } from "../insights/Insights";
import { CostsExplorer } from "./CostsExplorer";
import { displayName, encodeCrumb, parseDrillPath } from "./taxonomy";

// New cost-taxonomy dashboard. Org-scoped data, so gate to org readers/admins.
function NewCostsPage(): JSX.Element {
  const routes = useRoutes();
  const location = useLocation();

  // Map each drill segment (`Dimension~encodedValue`) to its pretty label so the
  // breadcrumb bar renders "R&D", "Olivia Novak", … instead of the raw encoding.
  const breadcrumbSubstitutions = useMemo(() => {
    const base = routes.costs.href();
    const tail = location.pathname.startsWith(base)
      ? location.pathname.slice(base.length)
      : "";
    const map: Record<string, string> = {};
    for (const crumb of parseDrillPath(tail)) {
      map[encodeCrumb(crumb)] = displayName(crumb.dim, crumb.value);
    }
    return map;
  }, [routes, location.pathname]);

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs substitutions={breadcrumbSubstitutions} />
      </Page.Header>
      <Page.Body noPadding fullWidth>
        <RequireScope scope={["org:read", "org:admin"]} level="page">
          <CostsExplorer />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

// Routed at the project-level `costs` path. The `gram-new-costs-page` PostHog
// flag toggles the new taxonomy explorer on; off (the default) keeps the
// existing agents/costs page so we can roll out and roll back per org/user.
export default function Costs(): JSX.Element {
  const telemetry = useTelemetry();
  const newCostsEnabled =
    telemetry.isFeatureEnabled("gram-new-costs-page") ?? false;

  if (newCostsEnabled) {
    return <NewCostsPage />;
  }
  return <InsightsAgentsPage />;
}
