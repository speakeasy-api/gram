import { Page } from "@/components/page-layout";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { RequireScope } from "@/components/require-scope";
import {
  PageTabsList,
  PageTabsTrigger,
  Tabs,
  TabsContent,
} from "@/components/ui/tabs";
import { useTelemetry } from "@/contexts/Telemetry";
import { BudgetsContent } from "@/pages/budgets/Budgets";
import { useMemo, useState, type JSX } from "react";
import { useLocation } from "react-router";
import { InsightsAgentsContent } from "@/components/observe/InsightsAgents";
import { InsightsAgentsPage } from "../insights/Insights";
import { CostsExplorer } from "./CostsExplorer";
import { displayName, parseDrillPath } from "./taxonomy";

type CostsTab = "costs" | "budgets";

// Map each drill segment (`Dimension~encodedValue`) to its pretty label so the
// breadcrumb bar renders "R&D", "Olivia Novak", … instead of the raw encoding.
// Derived straight from the pathname (not the resolved route href) and keyed
// by the literal path segment, so the substitution is present and matches on
// every render — no slug-resolution race, no raw-segment flash.
function useCostsBreadcrumbSubstitutions(): Record<string, string> {
  const location = useLocation();

  return useMemo(() => {
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
}

// New cost-taxonomy dashboard. Part of the Observe surface, gated on org:admin
// so basic members (synced via directory/SCIM) don't see it by default.
function NewCostsPage(): JSX.Element {
  const breadcrumbSubstitutions = useCostsBreadcrumbSubstitutions();

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs substitutions={breadcrumbSubstitutions} />
      </Page.Header>
      <Page.Body noPadding fullWidth>
        <RequireScope scope="org:admin" level="page">
          <CostsExplorer />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

// The Costs page with a Budgets tab beside the cost explorer. Tab state is
// local (PolicyCenter pattern): the drill URL under /costs/... keeps working
// and always lands on the Costs tab. The tab strip lives inside the content
// column — aligned with each tab's max-width content, like PolicyCenter's
// Policies/Exclusions strip — not up in the page chrome beside the
// breadcrumbs.
function TabbedCostsPage({
  newCostsEnabled,
}: {
  newCostsEnabled: boolean;
}): JSX.Element {
  const [activeTab, setActiveTab] = useState<CostsTab>("costs");
  const breadcrumbSubstitutions = useCostsBreadcrumbSubstitutions();

  // The two cost views scroll differently: the explorer expects its container
  // to scroll (the old Page.Body default), while the legacy agents view owns
  // an internal scroll area and needs a non-scrolling flex column around it.
  const costsTabClass = newCostsEnabled
    ? "min-h-0 overflow-y-auto"
    : "flex min-h-0 flex-col overflow-hidden";

  return (
    <div className="flex h-full flex-col">
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs
            fullWidth
            substitutions={breadcrumbSubstitutions}
          />
        </Page.Header>
        <Page.Body noPadding fullWidth overflowHidden>
          <Tabs
            value={activeTab}
            onValueChange={(value) => setActiveTab(value as CostsTab)}
            className="min-h-0 flex-1 gap-0"
          >
            <div className="mx-auto w-full max-w-7xl px-8 pt-6">
              <div className="border-b">
                <PageTabsList>
                  <PageTabsTrigger value="costs">Costs</PageTabsTrigger>
                  <PageTabsTrigger
                    value="budgets"
                    className="inline-flex items-center gap-2"
                  >
                    Budgets
                    <ReleaseStageBadge stage="preview" noTooltip />
                  </PageTabsTrigger>
                </PageTabsList>
              </div>
            </div>
            <TabsContent value="costs" className={costsTabClass}>
              <RequireScope scope="org:admin" level="page">
                {newCostsEnabled ? (
                  <CostsExplorer />
                ) : (
                  <InsightsAgentsContent />
                )}
              </RequireScope>
            </TabsContent>
            <TabsContent value="budgets" className="min-h-0 overflow-y-auto">
              <div className="mx-auto flex w-full max-w-7xl flex-col gap-4 p-8 pt-6 pb-24">
                <RequireScope scope="org:admin" level="page">
                  <BudgetsContent />
                </RequireScope>
              </div>
            </TabsContent>
          </Tabs>
        </Page.Body>
      </Page>
    </div>
  );
}

// Routed at the project-level `costs` path. Two PostHog flags shape it:
//   • `gram-new-costs-page` toggles the new taxonomy explorer on; off (the
//     default) keeps the existing agents/costs page so we can roll out and
//     roll back per org/user.
//   • `gram-budgets-page` adds the Budgets tab beside the cost view; off (the
//     default) renders the cost view alone with no tab strip, so budgets can
//     be released to select users only.
export default function Costs(): JSX.Element {
  const telemetry = useTelemetry();
  const newCostsEnabled =
    telemetry.isFeatureEnabled("gram-new-costs-page") ?? false;
  const budgetsEnabled =
    telemetry.isFeatureEnabled("gram-budgets-page") ?? false;

  if (budgetsEnabled) {
    return <TabbedCostsPage newCostsEnabled={newCostsEnabled} />;
  }
  if (newCostsEnabled) {
    return <NewCostsPage />;
  }
  return <InsightsAgentsPage />;
}
