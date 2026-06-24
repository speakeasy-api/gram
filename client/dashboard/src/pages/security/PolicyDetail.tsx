// Policy Detail View — the single shell for creating, editing, and viewing a
// risk policy (AGE-2704).
//
// Routes:
//   - /risk-policies/new          -> create mode (no :policyId)
//   - /risk-policies/:policyId    -> edit/view a saved policy
//
// Tabs (in `?tab=`):
//   - "configuration" (alias: legacy "overview") — the editable, sectioned
//      policy form (identical for create and edit), topped by an eval-signal
//      banner.
//   - "evals" — session-replay eval runs. Works in create/draft mode by
//      evaluating the on-screen `candidate` config.
//
// `usePolicyForm` is lifted to PolicyDetailContent so Configuration and Evals
// share one form instance (dirty edits drive candidate evals).

import { Page } from "@/components/page-layout";
import { DetailHero } from "@/components/detail-hero";
import { Heading } from "@/components/ui/heading";
import { RequireScope } from "@/components/require-scope";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
} from "@/components/ui/tabs";
import { useRoutes } from "@/routes";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRiskPoliciesGet } from "@gram/client/react-query/index.js";
import { Badge, Stack } from "@speakeasy-api/moonshine";
import { Loader2, Shield } from "lucide-react";
import { Navigate, useParams } from "react-router";
import { useQueryState } from "nuqs";
import { EvalsTab } from "./policy-evals/EvalsTab";
import { EvalSignalBanner } from "./policy-evals/EvalSignalBanner";
import { PolicyConfigurationTab } from "./policy-form/PolicyConfigurationTab";
import { usePolicyForm } from "./policy-form/use-policy-form";

// Tabs live in `?tab=`. "overview" is a back-compat alias for "configuration".
const POLICY_DETAIL_TABS = ["configuration", "evals"] as const;
type PolicyDetailTab = (typeof POLICY_DETAIL_TABS)[number];
const DEFAULT_TAB: PolicyDetailTab = "configuration";

function resolveTab(v: string | null): PolicyDetailTab {
  if (v === "overview") return "configuration";
  if (v != null && (POLICY_DETAIL_TABS as readonly string[]).includes(v)) {
    return v as PolicyDetailTab;
  }
  return DEFAULT_TAB;
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
  const telemetry = useTelemetry();
  const nlEnabled = telemetry.isFeatureEnabled("gram-prompt-policies") ?? false;

  const isCreate = policyId == null;
  const mode = isCreate ? "create" : "edit";

  const [tabParam, setTabParam] = useQueryState("tab");
  const activeTab = resolveTab(tabParam);
  // The selected eval run lives in `?run=` so the Configuration banner can land
  // the user on a just-created run in the Evals tab.
  const [, setRunParam] = useQueryState("run");

  const id = policyId ?? "";
  const {
    data: policy,
    isLoading,
    isError,
  } = useRiskPoliciesGet({ id }, undefined, { enabled: id !== "" });

  const form = usePolicyForm({
    mode,
    initialPolicy: policy ?? null,
    nlEnabled,
  });

  // The policy kind being evaluated: the saved type once persisted, else the
  // on-screen draft kind.
  const policyType: "standard" | "prompt_based" =
    policy?.policyType ??
    (form.state.formPolicyKind === "prompt" ? "prompt_based" : "standard");

  if (!isCreate && isError) {
    return <Navigate to={routes.policyCenter.href()} replace />;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={id ? { [id]: policy?.name || "Policy" } : {}}
        />
      </Page.Header>

      <Page.Body fullWidth noPadding className="gap-0">
        <PolicyHero
          name={isCreate ? "New Policy" : policy?.name}
          enabled={isCreate ? undefined : policy?.enabled}
        />

        <Tabs
          value={activeTab}
          onValueChange={(v) => void setTabParam(v)}
          className="flex w-full flex-1 flex-col"
        >
          <div className="shrink-0 border-b">
            <div className="mx-auto max-w-[1440px] px-8">
              <TabsList className="h-auto gap-6 rounded-none bg-transparent p-0">
                <PageTabsTrigger value="configuration">
                  Configuration
                </PageTabsTrigger>
                <PageTabsTrigger value="evals">Evals</PageTabsTrigger>
              </TabsList>
            </div>
          </div>

          <div className="mx-auto w-full max-w-[1440px] px-8 py-8">
            <TabsContent
              value="configuration"
              className="mt-0 w-full data-[state=inactive]:hidden"
            >
              {!isCreate && isLoading ? (
                <div className="flex items-center justify-center py-20">
                  <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
                </div>
              ) : (
                <>
                  {/* The eval banner only makes sense once there's something to
                      replay — hide it during the kind picker and before any
                      detection is configured. */}
                  {form.derived.hasDetection && (
                    <EvalSignalBanner
                      evalSource={form.evalSource}
                      policyId={isCreate ? undefined : id}
                      currentVersion={policy?.version}
                      canRun={form.derived.hasDetection}
                      onViewResults={(runId) => {
                        void setTabParam("evals");
                        void setRunParam(runId ?? null);
                      }}
                      onCreated={(run) => {
                        void setTabParam("evals");
                        void setRunParam(run.id);
                      }}
                    />
                  )}
                  <PolicyConfigurationTab
                    form={form}
                    mode={mode}
                    nlEnabled={nlEnabled}
                  />
                </>
              )}
            </TabsContent>

            <TabsContent
              value="evals"
              className="mt-0 w-full data-[state=inactive]:hidden"
            >
              {!isCreate && isLoading ? (
                <div className="flex items-center justify-center py-20">
                  <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
                </div>
              ) : (
                <EvalsTab
                  evalSource={form.evalSource}
                  policyId={isCreate ? undefined : id}
                  policyEnabled={policy?.enabled ?? false}
                  currentVersion={policy?.version}
                  policyType={policyType}
                  isDirty={form.derived.isDirty}
                  canRun={form.derived.hasDetection}
                />
              )}
            </TabsContent>
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
