import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { useTelemetry } from "@/contexts/Telemetry";
import { useMemo } from "react";
import { useLocation } from "react-router";
import { InsightsAgentsPage } from "../insights/Insights";
import { CostsExplorer } from "./CostsExplorer";
import { displayName, parseDrillPath } from "./taxonomy";

// New cost-taxonomy dashboard. Part of the Observe surface, so gate on
// observe:read (basic members do not hold it; admins and custom roles do).
function NewCostsPage(): JSX.Element {
  const location = useLocation();

  // Map each drill segment (`Dimension~encodedValue`) to its pretty label so the
  // breadcrumb bar renders "R&D", "Olivia Novak", … instead of the raw encoding.
  // Derived straight from the pathname (not the resolved route href) and keyed
  // by the literal path segment, so the substitution is present and matches on
  // every render — no slug-resolution race, no raw-segment flash.
  const breadcrumbSubstitutions = useMemo(() => {
    const segments = location.pathname.split("/").filter(Boolean);
    const costsIndex = segments.indexOf("costs");
    const map: Record<string, string> = {};
    if (costsIndex >= 0) {
      for (const rawSegment of segments.slice(costsIndex + 1)) {
        const [crumb] = parseDrillPath(rawSegment);
        if (crumb) map[rawSegment] = displayName(crumb.dim, crumb.value);
      }
    }
    return map;
  }, [location.pathname]);

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs substitutions={breadcrumbSubstitutions} />
      </Page.Header>
      <Page.Body noPadding fullWidth>
        <RequireScope scope="observe:read" level="page">
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
