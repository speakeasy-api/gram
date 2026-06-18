import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { useTelemetry } from "@/contexts/Telemetry";
import { InsightsAgentsPage } from "../insights/Insights";
import { CostsExplorer } from "./CostsExplorer";

// New cost-taxonomy dashboard. Org-scoped data, so gate to org readers/admins.
function NewCostsPage(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
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
