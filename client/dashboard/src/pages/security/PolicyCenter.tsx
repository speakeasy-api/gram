// Risk Policies list (Policy Center) — AGE-2704.
//
// The list surface for risk policies + the Exclusions tab. Creating, editing,
// and viewing a single policy lives in the routed PolicyDetail shell
// (/risk-policies/new and /risk-policies/:policyId); this page only lists,
// toggles, deletes, and shows analysis progress ("View Progress").

import { InsightsConfig } from "@/components/insights-dock";
import { INSIGHTS_SUGGESTIONS } from "@/lib/insights-suggestions";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Switch } from "@/components/ui/switch";
import { SimpleTooltip } from "@/components/ui/tooltip";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@/components/ui/sheet";
import { Type } from "@/components/ui/type";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
} from "@/components/ui/tabs";
import { ExclusionsTab, type ExclusionSheetState } from "./ExclusionsTab";
import {
  Button,
  type Column,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Table,
} from "@speakeasy-api/moonshine";
import {
  Plus,
  Shield,
  Ellipsis,
  Loader2,
  RefreshCw,
  Sparkles,
} from "lucide-react";
import { Outlet } from "react-router";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQueryState } from "nuqs";
import { useQueryClient } from "@tanstack/react-query";
import {
  invalidateAllRiskListPolicies,
  useRiskListPolicies,
  useRiskPoliciesDeleteMutation,
  useRiskPoliciesUpdateMutation,
} from "@gram/client/react-query/index.js";
import {
  useRiskPoliciesStatus,
  invalidateAllRiskPoliciesStatus,
} from "@gram/client/react-query/riskPoliciesStatus.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import {
  RULE_CATEGORY_META,
  POLICY_MESSAGE_TYPE_META,
  type PolicyAction,
} from "./policy-data";
import { cn } from "@/lib/utils";
import { ActionBadge } from "./policy-summary";
import {
  ALL_POLICY_MESSAGE_TYPES,
  hasOnlyToolCallMessageTypes,
  messageTypesSummary,
  policyAudienceSummary,
  policyMessageTypesForDisplay,
  sourcesToCategories,
  truncatePrompt,
} from "./policy-display";
import { isPromptPolicy } from "./policy-form/payload";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRoutes } from "@/routes";
import { useNavigate } from "react-router";

type PolicyKind = "risk" | "prompt";
type PolicyRow = { kind: PolicyKind; policy: RiskPolicy };

/** Outlet host so the create route (/risk-policies/new) can render the routed
 *  PolicyDetail shell inside this section. Mirrors `CollectionsRoot`. */
export function PolicyCenterRoot(): JSX.Element {
  return <Outlet />;
}

export default function PolicyCenter(): JSX.Element {
  return (
    <RequireScope scope="org:admin" level="page">
      <PolicyCenterContent />
    </RequireScope>
  );
}

function PolicyCenterContent() {
  const queryClient = useQueryClient();
  const telemetry = useTelemetry();
  const routes = useRoutes();
  const navigate = useNavigate();
  const { data, isLoading } = useRiskListPolicies();
  const nlEnabled = telemetry.isFeatureEnabled("gram-prompt-policies") ?? false;

  const policyRows = useMemo(
    (): PolicyRow[] =>
      (data?.policies ?? [])
        .filter((policy) => nlEnabled || !isPromptPolicy(policy))
        .map((policy) => {
          const kind: PolicyKind = isPromptPolicy(policy) ? "prompt" : "risk";
          return { kind, policy };
        })
        .sort(
          (a, b) => b.policy.createdAt.getTime() - a.policy.createdAt.getTime(),
        ),
    [data?.policies, nlEnabled],
  );

  const [runPanelPolicy, setRunPanelPolicy] = useState<RiskPolicy | null>(null);

  const [activeTab, setActiveTab] = useState<"policies" | "exclusions">(
    "policies",
  );
  const [exclusionSheet, setExclusionSheet] =
    useState<ExclusionSheetState | null>(null);

  // Back-compat: the legacy `?policy=<id>` deep link (command palette, old
  // bookmarks) opened an edit sheet here. The policy now has its own route, so
  // redirect to the Policy Detail view. Guarded by a ref so it fires once.
  const [policyParam, setPolicyParam] = useQueryState("policy");
  const redirectedPolicyRef = useRef<string | null>(null);
  useEffect(() => {
    if (!policyParam) return;
    if (redirectedPolicyRef.current === policyParam) return;
    redirectedPolicyRef.current = policyParam;
    const target = routes.policyDetail.href(policyParam);
    void setPolicyParam(null);
    void navigate(target, { replace: true });
  }, [policyParam, routes, navigate, setPolicyParam]);

  const invalidate = useCallback(() => {
    void invalidateAllRiskListPolicies(queryClient);
    void invalidateAllRiskPoliciesStatus(queryClient);
  }, [queryClient]);

  const updateMutation = useRiskPoliciesUpdateMutation({
    onSuccess: invalidate,
  });

  const deleteMutation = useRiskPoliciesDeleteMutation({
    onSuccess: invalidate,
  });

  const handleDelete = (row: PolicyRow) => {
    deleteMutation.mutate({ request: { id: row.policy.id } });
  };

  const handleToggle = (policy: RiskPolicy, enabled: boolean) => {
    updateMutation.mutate({
      request: {
        updateRiskPolicyRequestBody: {
          id: policy.id,
          name: policy.name,
          enabled,
          messageTypes: policy.messageTypes ?? [],
        },
      },
    });
  };

  // Empty state for the Policies tab only. It must NOT short-circuit the whole
  // page, otherwise the Exclusions tab (and global exclusions) would be
  // unreachable for projects that have no policies yet.
  const policiesEmptyState = (
    <div className="bg-muted/20 flex flex-col items-center justify-center rounded-xl border border-dashed px-8 py-16">
      <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
        <Shield className="text-muted-foreground h-6 w-6" />
      </div>
      <Type variant="subheading" className="mb-1">
        No Risk Policies
      </Type>
      <Type small muted className="mb-4 max-w-md text-center">
        Risk policies scan your chat messages for secrets and sensitive data.
        Create your first policy to get started.
      </Type>
      <Button onClick={() => void navigate(routes.policyCenter.new.href())}>
        <Button.Text>Get Started</Button.Text>
      </Button>
    </div>
  );

  const enabledPolicies = policyRows.filter((r) => r.policy.enabled);
  const insightsContext = [
    "Page: Policy Center.",
    `Total policies: ${policyRows.length}, enabled: ${enabledPolicies.length}.`,
    `Policy actions: ${policyRows.map((r) => `${r.policy.name} (${r.policy.action})`).join(", ") || "none"}.`,
    "Available risk tools: listRiskPolicies, getRiskPolicy, getRiskPolicyStatus, listRiskResultsForAgent (finding-level with match redaction), listRiskResultsByChat, listShadowMCPApprovals.",
    "Never echo match_redacted values verbatim. Refer to findings by rule_id and source.",
  ].join(" ");

  const dimIfDisabled = (row: PolicyRow) =>
    row.policy.enabled ? "" : "opacity-50";

  const policyColumns: Column<PolicyRow>[] = [
    {
      key: "name",
      header: "Name",
      width: "1fr",
      render: (row) => (
        <span
          className={cn(
            "flex min-w-0 items-center gap-1.5 font-medium",
            dimIfDisabled(row),
          )}
        >
          <span className="truncate">{row.policy.name}</span>
          {row.kind === "prompt" && (
            <SimpleTooltip tooltip="Prompt-based policy">
              <Sparkles
                aria-label="Prompt-based policy"
                className="text-muted-foreground h-3.5 w-3.5 shrink-0"
              />
            </SimpleTooltip>
          )}
        </span>
      ),
    },
    {
      key: "action",
      header: "Action",
      width: "0.5fr",
      render: (row) => (
        <span className={cn("inline-flex", dimIfDisabled(row))}>
          <ActionBadge action={(row.policy.action as PolicyAction) ?? "flag"} />
        </span>
      ),
    },
    {
      key: "sources",
      header: nlEnabled ? "Categories / Prompt" : "Categories",
      width: "2fr",
      render: (row) => {
        if (row.kind === "prompt") {
          const prompt = row.policy.prompt ?? "";
          return (
            <SimpleTooltip tooltip={prompt}>
              <span
                className={cn(
                  "text-muted-foreground block max-w-full truncate text-sm italic",
                  dimIfDisabled(row),
                )}
              >
                {truncatePrompt(prompt)}
              </span>
            </SimpleTooltip>
          );
        }

        const riskPolicy = row.policy;
        const categories = sourcesToCategories(
          riskPolicy.sources,
          riskPolicy.presidioEntities,
        );
        if (riskPolicy.customRuleIds?.length) {
          categories.push("custom");
        }

        if (categories.length === 0) {
          return <span className="text-muted-foreground text-sm">—</span>;
        }

        return (
          <span
            className={cn("text-muted-foreground text-sm", dimIfDisabled(row))}
          >
            {categories.map((cat) => RULE_CATEGORY_META[cat].label).join(", ")}
          </span>
        );
      },
    },
    {
      key: "messageTypes",
      header: "Applies To",
      width: "2.1fr",
      render: (row) => {
        const types = policyMessageTypesForDisplay(row.policy.messageTypes);
        const typeSet = new Set(types);
        const tooltip = types
          .map((type) => POLICY_MESSAGE_TYPE_META[type].label)
          .join(", ");

        if (
          typeSet.size === ALL_POLICY_MESSAGE_TYPES.length ||
          hasOnlyToolCallMessageTypes(typeSet)
        ) {
          return (
            <SimpleTooltip tooltip={tooltip}>
              <span
                className={cn(
                  "text-muted-foreground text-sm",
                  dimIfDisabled(row),
                )}
              >
                {messageTypesSummary(typeSet)}
              </span>
            </SimpleTooltip>
          );
        }

        return (
          <span
            className={cn("text-muted-foreground text-sm", dimIfDisabled(row))}
          >
            {types
              .map((type) => POLICY_MESSAGE_TYPE_META[type].label)
              .join(", ")}
          </span>
        );
      },
    },
    {
      key: "audience",
      header: "Audience",
      width: "1fr",
      render: (row) => (
        <span
          className={cn("text-muted-foreground text-sm", dimIfDisabled(row))}
        >
          {policyAudienceSummary(row.policy)}
        </span>
      ),
    },
    {
      key: "enabled",
      header: "Enabled",
      width: "0.5fr",
      render: (row) => (
        <div onClick={(e) => e.stopPropagation()}>
          <Switch
            checked={row.policy.enabled}
            onCheckedChange={(checked) => handleToggle(row.policy, checked)}
          />
        </div>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "0.3fr",
      render: (row) => (
        <div onClick={(e) => e.stopPropagation()}>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="tertiary"
                size="sm"
                onClick={(e) => e.stopPropagation()}
              >
                <Button.Icon>
                  <Ellipsis className="h-4 w-4" />
                </Button.Icon>
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem
                className="cursor-pointer"
                onSelect={() => {
                  // Open the Policy Detail view (edit/view mode).
                  const href = routes.policyDetail.href(row.policy.id);
                  setTimeout(() => void navigate(href), 0);
                }}
              >
                Edit
              </DropdownMenuItem>
              <DropdownMenuItem
                className="cursor-pointer"
                onSelect={() => {
                  // Opens the Policy Detail View on the Evals tab (AGE-2704).
                  const href = `${routes.policyDetail.href(row.policy.id)}?tab=evals`;
                  setTimeout(() => void navigate(href), 0);
                }}
              >
                View Evals
              </DropdownMenuItem>
              {row.kind === "risk" && (
                <DropdownMenuItem
                  className="cursor-pointer"
                  onSelect={() => {
                    setTimeout(() => setRunPanelPolicy(row.policy), 0);
                  }}
                >
                  View Progress
                </DropdownMenuItem>
              )}
              <DropdownMenuItem
                className="text-destructive focus:text-destructive cursor-pointer"
                onSelect={() => handleDelete(row)}
              >
                Delete
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      ),
    },
  ];

  const headerAction =
    activeTab === "policies"
      ? {
          label: "New Policy",
          onClick: () => void navigate(routes.policyCenter.new.href()),
        }
      : {
          label: "Create Exclusion",
          onClick: () => setExclusionSheet({ mode: "create" }),
        };

  let policiesBody = (
    <Table
      columns={policyColumns}
      data={policyRows}
      rowKey={(row) => row.policy.id}
      onRowClick={(row) =>
        void navigate(routes.policyDetail.href(row.policy.id))
      }
    />
  );
  if (isLoading) {
    policiesBody = (
      <div className="flex items-center justify-center py-20">
        <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
      </div>
    );
  } else if (policyRows.length === 0) {
    policiesBody = policiesEmptyState;
  }

  const cta = isLoading ? null : (
    <Page.Section.CTA>
      <Button onClick={headerAction.onClick}>
        <Button.LeftIcon>
          <Plus className="mr-2 h-4 w-4" />
        </Button.LeftIcon>
        <Button.Text>{headerAction.label}</Button.Text>
      </Button>
    </Page.Section.CTA>
  );

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <InsightsConfig
          contextInfo={insightsContext}
          suggestions={INSIGHTS_SUGGESTIONS["risk-policies"]}
          title="Policy insights"
          subtitle="Ask about policy status, coverage, and detector capabilities. Match content is redacted before it reaches the assistant."
        />
        <Page.Section>
          <Page.Section.Title stage="beta">Policies</Page.Section.Title>
          <Page.Section.Description>
            Configure policies to detect secrets, sensitive information, and
            prompt-defined risks in agent session interactions.
          </Page.Section.Description>
          {cta}
          <Page.Section.Body>
            <Tabs
              value={activeTab}
              onValueChange={(value) =>
                setActiveTab(value as "policies" | "exclusions")
              }
            >
              <div className="border-b">
                <TabsList className="h-auto justify-start gap-4 rounded-none bg-transparent p-0 text-sm">
                  <PageTabsTrigger value="policies">Policies</PageTabsTrigger>
                  <PageTabsTrigger value="exclusions">
                    Exclusions
                  </PageTabsTrigger>
                </TabsList>
              </div>
              <TabsContent value="policies" className="mt-6">
                {policiesBody}
              </TabsContent>
              <TabsContent value="exclusions" className="mt-6">
                <ExclusionsTab
                  policies={data?.policies ?? []}
                  sheet={exclusionSheet}
                  onSheetChange={setExclusionSheet}
                />
              </TabsContent>
            </Tabs>
          </Page.Section.Body>
        </Page.Section>

        {/* View Run Panel */}
        <Sheet
          open={!!runPanelPolicy}
          onOpenChange={(open) => {
            if (!open) setRunPanelPolicy(null);
          }}
        >
          <SheetContent side="right" className="sm:max-w-md">
            {runPanelPolicy && <RunPanel policy={runPanelPolicy} />}
          </SheetContent>
        </Sheet>
      </Page.Body>
    </Page>
  );
}

/* -------------------------------------------------------------------------- */
/*  RunPanel                                                                  */
/* -------------------------------------------------------------------------- */

function RunPanel({ policy }: { policy: RiskPolicy }) {
  const {
    data: status,
    isLoading,
    refetch,
    isFetching,
  } = useRiskPoliciesStatus({ id: policy.id }, undefined, {
    refetchInterval: 5000,
  });

  const pct =
    status && status.totalMessages > 0
      ? Math.round((status.analyzedMessages / status.totalMessages) * 100)
      : 0;

  return (
    <>
      <SheetHeader className="px-6 pt-6">
        <SheetTitle>{policy.name}</SheetTitle>
        <SheetDescription>
          Analysis progress and workflow status
        </SheetDescription>
      </SheetHeader>

      <div className="flex-1 space-y-4 overflow-y-auto px-6 py-4">
        {isLoading ? (
          <div className="flex items-center justify-center py-12">
            <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
          </div>
        ) : status ? (
          <>
            {/* Status + Version row */}
            <div className="grid grid-cols-2 gap-3">
              <div className="border-border rounded-lg border p-3">
                <p className="text-muted-foreground mb-1 text-xs font-medium">
                  Status
                </p>
                <div className="flex items-center gap-2">
                  <span
                    className={cn(
                      "inline-block h-2.5 w-2.5 rounded-full",
                      status.workflowStatus === "running" &&
                        "animate-pulse bg-green-500",
                      status.workflowStatus === "sleeping" && "bg-yellow-500",
                      status.workflowStatus === "not_started" &&
                        "bg-muted-foreground",
                    )}
                  />
                  <span className="text-sm font-medium capitalize">
                    {status.workflowStatus === "not_started"
                      ? "Idle"
                      : status.workflowStatus}
                  </span>
                </div>
              </div>
              <div className="border-border rounded-lg border p-3">
                <p className="text-muted-foreground mb-1 text-xs font-medium">
                  Version
                </p>
                <p className="text-sm font-medium">v{status.policyVersion}</p>
              </div>
            </div>

            {/* Progress */}
            <div className="border-border rounded-lg border p-4">
              <div className="mb-3 flex items-center justify-between">
                <p className="text-sm font-medium">Analysis Progress</p>
                <div className="flex items-center gap-2">
                  <Button
                    variant="tertiary"
                    onClick={() => {
                      void refetch();
                    }}
                    disabled={isFetching}
                    className="h-6 w-6"
                  >
                    <Button.Icon>
                      <RefreshCw
                        className={cn("h-3 w-3", isFetching && "animate-spin")}
                      />
                    </Button.Icon>
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

            {/* Findings */}
            <div className="border-border rounded-lg border p-4">
              <p className="text-muted-foreground mb-1 text-xs font-medium">
                Findings
              </p>
              <p className="text-3xl font-bold tracking-tight">
                {status.findingsCount.toLocaleString()}
              </p>
              <p className="text-muted-foreground mt-1 text-xs">
                secrets detected across all messages
              </p>
            </div>
          </>
        ) : null}
      </div>
    </>
  );
}
