import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
} from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { useQueryClient } from "@tanstack/react-query";
import {
  invalidateAllRiskListPolicies,
  useRiskPoliciesGet,
  useRiskPoliciesTriggerMutation,
  useRiskPoliciesUpdateMutation,
} from "@gram/client/react-query/index.js";
import {
  invalidateAllRiskPoliciesStatus,
  useRiskPoliciesStatus,
} from "@gram/client/react-query/riskPoliciesStatus.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { Loader2, RefreshCw } from "lucide-react";
import { useState } from "react";
import { useParams } from "react-router";

import {
  ALL_CHECK_SCOPES,
  categoriesToPayload,
  policyToCategories,
  PolicySheetBody,
} from "./PolicyCenter";
import { type CheckScope, type PolicyAction } from "./policy-data";

export default function RiskPolicyDetail() {
  return (
    <RequireScope scope="org:admin" level="page">
      <RiskPolicyDetailContent />
    </RequireScope>
  );
}

function RiskPolicyDetailContent() {
  const { policyId } = useParams<{ policyId: string }>();
  const { data: policy, isLoading } = useRiskPoliciesGet(
    { id: policyId ?? "" },
    undefined,
    { enabled: !!policyId },
  );

  if (!policyId) {
    return <PolicyNotFound />;
  }

  if (isLoading || !policy) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <div className="flex items-center justify-center py-20">
            <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
          </div>
        </Page.Body>
      </Page>
    );
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{ [policy.id]: policy.name, risk: "Risk" }}
          skipSegments={["risk"]}
        />
      </Page.Header>
      <Page.Body>
        <div className="mb-4 flex items-start justify-between gap-4">
          <div>
            <h2 className="text-lg font-semibold">{policy.name}</h2>
            <Type small muted className="mt-1">
              Standard Risk Policy
            </Type>
          </div>
          <Badge variant={policy.enabled ? "secondary" : "outline"}>
            {policy.enabled ? "On" : "Off"}
          </Badge>
        </div>

        <Tabs defaultValue="configure">
          <div className="border-border -mx-8 border-b px-8">
            <TabsList className="h-auto justify-start gap-4 rounded-none bg-transparent p-0 text-sm">
              <PageTabsTrigger value="configure">Configure</PageTabsTrigger>
              <PageTabsTrigger value="activity">Activity</PageTabsTrigger>
            </TabsList>
          </div>

          <TabsContent value="configure" className="mt-6">
            <RiskPolicyConfigureTab policy={policy} />
          </TabsContent>
          <TabsContent value="activity" className="mt-6">
            <RiskPolicyActivityTab policy={policy} />
          </TabsContent>
        </Tabs>
      </Page.Body>
    </Page>
  );
}

function PolicyNotFound() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <div className="text-muted-foreground py-20 text-center text-sm">
          Policy not found.
        </div>
      </Page.Body>
    </Page>
  );
}

function RiskPolicyConfigureTab({ policy }: { policy: RiskPolicy }) {
  const queryClient = useQueryClient();
  const [formName, setFormName] = useState(policy.name);
  const [formEnabled, setFormEnabled] = useState(policy.enabled);
  const [selectedCategories, setSelectedCategories] = useState(
    policyToCategories(policy.sources, policy.presidioEntities),
  );
  const [formAction, setFormAction] = useState<PolicyAction>(
    (policy.action as PolicyAction) ?? "flag",
  );
  const [formAutoName, setFormAutoName] = useState(policy.autoName ?? true);
  const [formUserMessage, setFormUserMessage] = useState(
    policy.userMessage ?? "",
  );
  const [formTargets, setFormTargets] = useState<CheckScope[]>(
    policy.targets?.length
      ? (policy.targets as CheckScope[])
      : ALL_CHECK_SCOPES,
  );

  const updateMutation = useRiskPoliciesUpdateMutation({
    onSuccess: () => {
      invalidateAllRiskListPolicies(queryClient);
      invalidateAllRiskPoliciesStatus(queryClient);
    },
  });

  const onSave = () => {
    const { sources, presidioEntities, promptInjectionRules } =
      categoriesToPayload(selectedCategories);
    const action =
      sources.includes("destructive_tool") && formAction === "block"
        ? "flag"
        : formAction;

    updateMutation.mutate({
      request: {
        updateRiskPolicyRequestBody: {
          id: policy.id,
          name: formName,
          enabled: formEnabled,
          sources,
          presidioEntities,
          promptInjectionRules,
          targets: formTargets,
          action,
          autoName: formAutoName,
          userMessage: formUserMessage,
        },
      },
    });
  };

  return (
    <Card className="space-y-6 p-6">
      <PolicySheetBody
        formName={formName}
        setFormName={setFormName}
        formEnabled={formEnabled}
        setFormEnabled={setFormEnabled}
        selectedCategories={selectedCategories}
        setSelectedCategories={setSelectedCategories}
        formAction={formAction}
        setFormAction={setFormAction}
        formAutoName={formAutoName}
        setFormAutoName={setFormAutoName}
        formUserMessage={formUserMessage}
        setFormUserMessage={setFormUserMessage}
        formTargets={formTargets}
        setFormTargets={setFormTargets}
      />
      <div className="flex justify-end">
        <Button
          onClick={onSave}
          disabled={
            (!formAutoName && !formName.trim()) ||
            formTargets.length === 0 ||
            updateMutation.isPending
          }
        >
          {updateMutation.isPending && (
            <Loader2 className="mr-2 h-4 w-4 animate-spin" />
          )}
          Save
        </Button>
      </div>
    </Card>
  );
}

function RiskPolicyActivityTab({ policy }: { policy: RiskPolicy }) {
  const queryClient = useQueryClient();
  const {
    data: status,
    isLoading,
    refetch,
    isFetching,
  } = useRiskPoliciesStatus({ id: policy.id }, undefined, {
    refetchInterval: 5000,
  });
  const triggerMutation = useRiskPoliciesTriggerMutation({
    onSuccess: () => {
      invalidateAllRiskListPolicies(queryClient);
      invalidateAllRiskPoliciesStatus(queryClient);
    },
  });

  const pct =
    status && status.totalMessages > 0
      ? Math.round((status.analyzedMessages / status.totalMessages) * 100)
      : 0;

  return (
    <Card className="space-y-4 p-6">
      {isLoading ? (
        <div className="flex items-center justify-center py-12">
          <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
        </div>
      ) : status ? (
        <>
          <div className="grid gap-3 md:grid-cols-3">
            <div className="border-border rounded-lg border p-3">
              <Type small muted>
                Status
              </Type>
              <Badge variant="outline" className="mt-1">
                {status.workflowStatus === "not_started"
                  ? "Idle"
                  : status.workflowStatus}
              </Badge>
            </div>
            <div className="border-border rounded-lg border p-3">
              <Type small muted>
                Version
              </Type>
              <p className="mt-1 text-sm font-medium">
                v{status.policyVersion}
              </p>
            </div>
            <div className="border-border rounded-lg border p-3">
              <Type small muted>
                Findings
              </Type>
              <p className="mt-1 text-2xl font-bold tracking-tight">
                {status.findingsCount.toLocaleString()}
              </p>
            </div>
          </div>

          <div className="border-border rounded-lg border p-4">
            <div className="mb-3 flex items-center justify-between">
              <p className="text-sm font-medium">Analysis Progress</p>
              <div className="flex items-center gap-2">
                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={() => refetch()}
                  disabled={isFetching}
                  tooltip="Refresh"
                  className="h-6 w-6"
                >
                  <RefreshCw
                    className={isFetching ? "h-3 w-3 animate-spin" : "h-3 w-3"}
                  />
                </Button>
                <span className="text-muted-foreground text-xs font-medium">
                  {pct}%
                </span>
              </div>
            </div>
            <div className="bg-muted mb-2 h-2 overflow-hidden rounded-full">
              <div
                className="bg-primary h-full rounded-full transition-all duration-500"
                style={{ width: `${pct}%` }}
              />
            </div>
            <p className="text-muted-foreground text-xs">
              {status.analyzedMessages.toLocaleString()} of{" "}
              {status.totalMessages.toLocaleString()} messages analyzed
              {status.pendingMessages > 0 && (
                <span>
                  {" "}
                  &middot; {status.pendingMessages.toLocaleString()} pending
                </span>
              )}
            </p>
          </div>
        </>
      ) : null}

      <div className="flex justify-end">
        <Button
          onClick={() =>
            triggerMutation.mutate({
              request: { triggerRiskAnalysisRequestBody: { id: policy.id } },
            })
          }
          disabled={triggerMutation.isPending}
        >
          {triggerMutation.isPending && (
            <Loader2 className="mr-2 h-4 w-4 animate-spin" />
          )}
          Trigger Analysis
        </Button>
      </div>
    </Card>
  );
}
