// Policy Detail View — AGE-2704
//
// Shell for a single risk policy's detail view. Hosts a tab bar:
//   - "Overview" (placeholder)
//   - "Evals"    (session-replay eval runs — AGE-2704)
//
// Extension point: a future "Derived rules" tab (AGE-2706) plugs into the
// `POLICY_DETAIL_TABS` list + the switch in the body — see the markers below.

import { Page } from "@/components/page-layout";
import { DetailHero } from "@/components/detail-hero";
import { Heading } from "@/components/ui/heading";
import { RequireScope } from "@/components/require-scope";
import { Type } from "@/components/ui/type";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
} from "@/components/ui/tabs";
import { useRoutes } from "@/routes";
import { useRiskPoliciesGet } from "@gram/client/react-query/index.js";
import { Badge, Stack } from "@speakeasy-api/moonshine";
import { Loader2, Shield } from "lucide-react";
import { Navigate, useParams } from "react-router";
import { useQueryState } from "nuqs";
import { EvalsTab } from "./policy-evals/EvalsTab";

// The tab value lives in `?tab=` so a tab is deep-linkable and survives reload
// without a route-per-tab. To add the "Derived rules" tab (AGE-2706):
//   1. add its value here,
//   2. add a <PageTabsTrigger> in the tab bar below,
//   3. add a <TabsContent> branch in the body.
const POLICY_DETAIL_TABS = ["overview", "evals"] as const;
type PolicyDetailTab = (typeof POLICY_DETAIL_TABS)[number];
const DEFAULT_TAB: PolicyDetailTab = "overview";

function isPolicyDetailTab(v: string | null): v is PolicyDetailTab {
  return v != null && (POLICY_DETAIL_TABS as readonly string[]).includes(v);
}

export default function PolicyDetail(): JSX.Element {
  return (
    <RequireScope scope="org:admin" level="page">
      <PolicyDetailContent />
    </RequireScope>
  );
}

function PolicyDetailContent(): JSX.Element {
  const { policyId } = useParams<{ policyId: string }>();
  const routes = useRoutes();
  const [tabParam, setTabParam] = useQueryState("tab");
  const activeTab: PolicyDetailTab = isPolicyDetailTab(tabParam)
    ? tabParam
    : DEFAULT_TAB;

  const id = policyId ?? "";
  const {
    data: policy,
    isLoading,
    isError,
  } = useRiskPoliciesGet({ id }, undefined, { enabled: id !== "" });

  if (!id || isError) {
    return <Navigate to={routes.policyCenter.href()} replace />;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{
            [id]: policy?.name || "Policy",
          }}
        />
      </Page.Header>

      <Page.Body fullWidth noPadding className="gap-0">
        <PolicyHero name={policy?.name} enabled={policy?.enabled} />

        <Tabs
          value={activeTab}
          onValueChange={(v) => void setTabParam(v)}
          className="flex w-full flex-1 flex-col"
        >
          <div className="shrink-0 border-b">
            <div className="mx-auto max-w-[1270px] px-8">
              <TabsList className="h-auto gap-6 rounded-none bg-transparent p-0">
                <PageTabsTrigger value="overview">Overview</PageTabsTrigger>
                <PageTabsTrigger value="evals">Evals</PageTabsTrigger>
                {/* AGE-2706 extension point: add a "Derived rules" trigger here. */}
              </TabsList>
            </div>
          </div>

          <div className="mx-auto w-full max-w-[1270px] px-8 py-8">
            <TabsContent
              value="overview"
              className="mt-0 w-full data-[state=inactive]:hidden"
            >
              <OverviewPlaceholder />
            </TabsContent>

            <TabsContent
              value="evals"
              className="mt-0 w-full data-[state=inactive]:hidden"
            >
              {isLoading ? (
                <div className="flex items-center justify-center py-20">
                  <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
                </div>
              ) : (
                <EvalsTab
                  riskPolicyId={id}
                  policyEnabled={policy?.enabled ?? false}
                />
              )}
            </TabsContent>

            {/* AGE-2706 extension point: add a "Derived rules" TabsContent here. */}
          </div>
        </Tabs>
      </Page.Body>
    </Page>
  );
}

function PolicyHero({
  name,
  enabled,
}: {
  name: string | undefined;
  enabled: boolean | undefined;
}) {
  return (
    <DetailHero>
      <Stack gap={2}>
        <Stack direction="horizontal" gap={3} align="center">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-emerald-500/10 dark:bg-emerald-500/20">
            <Shield className="h-5 w-5 text-emerald-600 dark:text-emerald-400" />
          </div>
          <Heading variant="h1" className="break-all normal-case">
            {name || "Policy"}
          </Heading>
          {enabled != null && (
            <Badge variant={enabled ? "success" : "neutral"}>
              <Badge.Text>{enabled ? "Enabled" : "Disabled"}</Badge.Text>
            </Badge>
          )}
        </Stack>
      </Stack>
    </DetailHero>
  );
}

function OverviewPlaceholder() {
  return (
    <div className="bg-muted/20 rounded-xl border border-dashed px-8 py-16 text-center">
      <Type variant="subheading" className="mb-1">
        Policy overview
      </Type>
      <Type small muted className="mx-auto max-w-md">
        {/* TODO(AGE-2704): render policy summary (detectors, scope, action,
            audience) here, reusing PolicyCenter's view helpers. */}
        Policy configuration summary will live here.
      </Type>
    </div>
  );
}
