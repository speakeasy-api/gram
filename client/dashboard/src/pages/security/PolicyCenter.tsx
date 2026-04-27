import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Switch } from "@/components/ui/switch";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
  SheetFooter,
} from "@/components/ui/sheet";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Type } from "@/components/ui/type";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Icon,
} from "@speakeasy-api/moonshine";
import type { IconName } from "@speakeasy-api/moonshine";
import {
  Plus,
  Shield,
  Ellipsis,
  Loader2,
  ChevronRight,
  RefreshCw,
} from "lucide-react";
import { useState, useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  useRiskListPolicies,
  useRiskCreatePolicyMutation,
  useRiskPoliciesUpdateMutation,
  useRiskPoliciesDeleteMutation,
  useRiskPoliciesTriggerMutation,
  invalidateAllRiskListPolicies,
} from "@gram/client/react-query/index.js";
import {
  useRiskPoliciesStatus,
  invalidateAllRiskPoliciesStatus,
} from "@gram/client/react-query/riskPoliciesStatus.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import {
  RULE_CATEGORY_META,
  DETECTION_RULES,
  type RuleCategory,
  type PolicyAction,
} from "./policy-data";
import { cn } from "@/lib/utils";

/** Presidio-backed categories */
const PRESIDIO_CATEGORIES: RuleCategory[] = [
  "financial",
  "pii",
  "government_ids",
  "healthcare",
];

/** Categories that are currently available */
const AVAILABLE_CATEGORIES: Set<RuleCategory> = new Set([
  "secrets",
  ...PRESIDIO_CATEGORIES,
]);

/** All rule categories in display order */
const ALL_CATEGORIES: RuleCategory[] = [
  "secrets",
  ...PRESIDIO_CATEGORIES,
  "prompt_attacks",
  "prompt_injection",
  "off_policy",
];

/** Derive selected categories from a policy's sources + presidioEntities. */
function policyToCategories(
  sources: string[],
  presidioEntities?: string[],
): Set<RuleCategory> {
  const cats = new Set<RuleCategory>();
  if (sources.includes("gitleaks")) cats.add("secrets");
  for (const cat of PRESIDIO_CATEGORIES) {
    const catEntityIds = DETECTION_RULES[cat].map((r) => r.id);
    if (catEntityIds.some((id) => presidioEntities?.includes(id))) {
      cats.add(cat);
    }
  }
  return cats;
}

/** Derive sources + presidioEntities from selected categories. */
function categoriesToPayload(cats: Set<RuleCategory>) {
  const sources: string[] = [];
  const presidioEntities: string[] = [];
  if (cats.has("secrets")) sources.push("gitleaks");
  for (const cat of PRESIDIO_CATEGORIES) {
    if (cats.has(cat)) {
      for (const rule of DETECTION_RULES[cat]) {
        presidioEntities.push(rule.id);
      }
    }
  }
  if (presidioEntities.length > 0) sources.push("presidio");
  return { sources, presidioEntities };
}

/** Map sources to display categories for the table row badges. */
function sourcesToCategories(
  sources: string[],
  presidioEntities?: string[],
): RuleCategory[] {
  return [...policyToCategories(sources, presidioEntities)];
}

export default function PolicyCenter() {
  return (
    <RequireScope scope="org:admin" level="page">
      <PolicyCenterContent />
    </RequireScope>
  );
}

function PolicyCenterContent() {
  const queryClient = useQueryClient();
  const { data, isLoading } = useRiskListPolicies();
  const policies = data?.policies ?? [];

  const [sheetOpen, setSheetOpen] = useState(false);
  const [editingPolicy, setEditingPolicy] = useState<RiskPolicy | null>(null);
  const [formName, setFormName] = useState("");
  const [formEnabled, setFormEnabled] = useState(true);
  const [selectedCategories, setSelectedCategories] = useState<
    Set<RuleCategory>
  >(new Set<RuleCategory>(["secrets", "pii"]));
  const [formAction, setFormAction] = useState<PolicyAction>("flag");

  const [runPanelPolicy, setRunPanelPolicy] = useState<RiskPolicy | null>(null);

  const invalidate = useCallback(() => {
    invalidateAllRiskListPolicies(queryClient);
    invalidateAllRiskPoliciesStatus(queryClient);
  }, [queryClient]);

  const createMutation = useRiskCreatePolicyMutation({
    onSuccess: () => {
      invalidate();
      setSheetOpen(false);
    },
  });

  const updateMutation = useRiskPoliciesUpdateMutation({
    onSuccess: () => {
      invalidate();
      setSheetOpen(false);
    },
  });

  const deleteMutation = useRiskPoliciesDeleteMutation({
    onSuccess: invalidate,
  });

  const triggerMutation = useRiskPoliciesTriggerMutation({
    onSuccess: invalidate,
  });

  const handleCreate = () => {
    setEditingPolicy(null);
    setFormName("");
    setFormEnabled(true);
    setSelectedCategories(new Set<RuleCategory>(["secrets", "pii"]));
    setFormAction("flag");
    setSheetOpen(true);
  };

  const handleEdit = (policy: RiskPolicy) => {
    setEditingPolicy(policy);
    setFormName(policy.name);
    setFormEnabled(policy.enabled);
    setSelectedCategories(
      policyToCategories(policy.sources, policy.presidioEntities),
    );
    setFormAction((policy.action as PolicyAction) ?? "flag");
    setSheetOpen(true);
  };

  const handleSave = () => {
    const { sources, presidioEntities } =
      categoriesToPayload(selectedCategories);
    if (editingPolicy) {
      updateMutation.mutate({
        request: {
          updateRiskPolicyRequestBody: {
            id: editingPolicy.id,
            name: formName,
            enabled: formEnabled,
            sources,
            presidioEntities,
            action: formAction,
          },
        },
      });
    } else {
      createMutation.mutate({
        request: {
          createRiskPolicyRequestBody: {
            name: formName,
            enabled: formEnabled,
            sources,
            presidioEntities,
            action: formAction,
          },
        },
      });
    }
  };

  const handleDelete = (id: string) => {
    deleteMutation.mutate({ request: { id } });
  };

  const handleTrigger = (id: string) => {
    triggerMutation.mutate({
      request: { triggerRiskAnalysisRequestBody: { id } },
    });
  };

  const handleToggle = (policy: RiskPolicy, enabled: boolean) => {
    updateMutation.mutate({
      request: {
        updateRiskPolicyRequestBody: {
          id: policy.id,
          name: policy.name,
          enabled,
        },
      },
    });
  };

  if (isLoading) {
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

  if (policies.length === 0) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <div className="bg-muted/20 flex flex-col items-center justify-center rounded-xl border border-dashed px-8 py-16">
            <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
              <Shield className="text-muted-foreground h-6 w-6" />
            </div>
            <Type variant="subheading" className="mb-1">
              No Risk Policies
            </Type>
            <Type small muted className="mb-4 max-w-md text-center">
              Risk policies scan your chat messages for secrets and sensitive
              data. Create your first policy to get started.
            </Type>
            <Button
              onClick={() => {
                const { sources, presidioEntities } = categoriesToPayload(
                  new Set<RuleCategory>(["secrets", "pii"]),
                );
                createMutation.mutate({
                  request: {
                    createRiskPolicyRequestBody: {
                      name: "Risk Scanner",
                      enabled: true,
                      sources,
                      presidioEntities,
                    },
                  },
                });
              }}
              disabled={createMutation.isPending}
            >
              {createMutation.isPending ? (
                <>
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                  Creating...
                </>
              ) : (
                "Get Started"
              )}
            </Button>
          </div>
        </Page.Body>
      </Page>
    );
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <div className="flex items-center justify-between">
          <div>
            <h2 className="text-lg font-semibold">Risk Policies</h2>
            <p className="text-muted-foreground text-sm">
              Configure risk analysis rules to detect secrets and sensitive
              information in chat messages.
            </p>
          </div>
          {policies.length === 0 && (
            <Button onClick={handleCreate}>
              <Plus className="mr-2 h-4 w-4" />
              New Policy
            </Button>
          )}
        </div>

        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Action</TableHead>
              <TableHead>Categories</TableHead>
              <TableHead>Progress</TableHead>
              <TableHead>Status</TableHead>
              <TableHead className="w-[60px]" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {policies.map((policy) => {
              const categories = sourcesToCategories(
                policy.sources,
                policy.presidioEntities,
              );
              return (
                <TableRow
                  key={policy.id}
                  className="cursor-pointer"
                  onClick={() => handleEdit(policy)}
                >
                  <TableCell className="font-medium">{policy.name}</TableCell>
                  <TableCell>
                    <ActionBadge
                      action={(policy.action as PolicyAction) ?? "flag"}
                    />
                  </TableCell>
                  <TableCell>
                    <div className="flex gap-1">
                      {categories.map((cat) => (
                        <Badge key={cat} variant="secondary">
                          {RULE_CATEGORY_META[cat].label}
                        </Badge>
                      ))}
                    </div>
                  </TableCell>
                  <TableCell>
                    {policy.pendingMessages > 0 ? (
                      <span className="text-muted-foreground text-xs">
                        {policy.totalMessages - policy.pendingMessages}/
                        {policy.totalMessages} analyzed
                      </span>
                    ) : (
                      <Badge variant="secondary">Complete</Badge>
                    )}
                  </TableCell>
                  <TableCell onClick={(e) => e.stopPropagation()}>
                    <Switch
                      checked={policy.enabled}
                      onCheckedChange={(checked) =>
                        handleToggle(policy, checked)
                      }
                    />
                  </TableCell>
                  <TableCell onClick={(e) => e.stopPropagation()}>
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          onClick={(e) => e.stopPropagation()}
                        >
                          <Ellipsis className="h-4 w-4" />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        <DropdownMenuItem
                          className="cursor-pointer"
                          onSelect={() =>
                            setTimeout(() => setRunPanelPolicy(policy), 0)
                          }
                        >
                          View Progress
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          className="text-destructive focus:text-destructive cursor-pointer"
                          onSelect={() => handleDelete(policy.id)}
                        >
                          Delete
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>

        {/* Edit/Create Sheet */}
        <Sheet open={sheetOpen} onOpenChange={setSheetOpen}>
          <SheetContent className="flex flex-col overflow-y-auto sm:max-w-lg">
            <SheetHeader className="px-6 pt-6">
              <SheetTitle>
                {editingPolicy ? "Edit Policy" : "New Policy"}
              </SheetTitle>
              <SheetDescription>
                {editingPolicy
                  ? "Update the risk analysis policy configuration."
                  : "Create a new risk analysis policy to scan chat messages."}
              </SheetDescription>
            </SheetHeader>
            <div className="flex-1 overflow-y-auto px-6">
              <PolicySheetBody
                formName={formName}
                setFormName={setFormName}
                formEnabled={formEnabled}
                setFormEnabled={setFormEnabled}
                selectedCategories={selectedCategories}
                setSelectedCategories={setSelectedCategories}
                formAction={formAction}
                setFormAction={setFormAction}
              />
            </div>
            <SheetFooter className="px-6 pb-6">
              <Button
                onClick={handleSave}
                disabled={
                  !formName.trim() ||
                  createMutation.isPending ||
                  updateMutation.isPending
                }
              >
                {createMutation.isPending || updateMutation.isPending ? (
                  <>
                    <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                    Saving...
                  </>
                ) : editingPolicy ? (
                  "Update"
                ) : (
                  "Create"
                )}
              </Button>
            </SheetFooter>
          </SheetContent>
        </Sheet>

        {/* View Run Panel */}
        <Sheet
          open={!!runPanelPolicy}
          onOpenChange={(open) => {
            if (!open) setRunPanelPolicy(null);
          }}
        >
          <SheetContent side="right" className="sm:max-w-md">
            {runPanelPolicy && (
              <RunPanel
                policy={runPanelPolicy}
                onTrigger={() => handleTrigger(runPanelPolicy.id)}
                isTriggerPending={triggerMutation.isPending}
              />
            )}
          </SheetContent>
        </Sheet>
      </Page.Body>
    </Page>
  );
}

/* -------------------------------------------------------------------------- */
/*  PolicySheetBody                                                           */
/* -------------------------------------------------------------------------- */

function PolicySheetBody({
  formName,
  setFormName,
  formEnabled,
  setFormEnabled,
  selectedCategories,
  setSelectedCategories,
  formAction,
  setFormAction,
}: {
  formName: string;
  setFormName: (v: string) => void;
  formEnabled: boolean;
  setFormEnabled: (v: boolean) => void;
  selectedCategories: Set<RuleCategory>;
  setSelectedCategories: (v: Set<RuleCategory>) => void;
  formAction: PolicyAction;
  setFormAction: (v: PolicyAction) => void;
}) {
  const [expandedCategory, setExpandedCategory] = useState<RuleCategory | null>(
    null,
  );

  return (
    <div className="space-y-6 py-4">
      {/* Policy Name */}
      <div className="space-y-2">
        <Label className="text-sm font-medium">Policy Name</Label>
        <Input
          value={formName}
          onChange={(value) => setFormName(value)}
          placeholder="e.g. Secret Detection"
        />
      </div>

      {/* Action */}
      <div className="space-y-2">
        <Label className="text-sm font-medium">Action</Label>
        <RadioGroup
          value={formAction}
          onValueChange={(v) => setFormAction(v as PolicyAction)}
          className="space-y-2"
        >
          <label
            htmlFor="action-flag"
            className="hover:bg-muted/50 flex cursor-pointer items-start gap-3 rounded-md border p-3"
          >
            <RadioGroupItem value="flag" id="action-flag" className="mt-0.5" />
            <div>
              <div className="text-sm font-medium">Flag</div>
              <div className="text-muted-foreground text-xs">
                Log findings for review without interrupting the session
              </div>
            </div>
          </label>
          <label
            htmlFor="action-block"
            className="hover:bg-muted/50 flex cursor-pointer items-start gap-3 rounded-md border p-3"
          >
            <RadioGroupItem
              value="block"
              id="action-block"
              className="mt-0.5"
            />
            <div>
              <div className="text-sm font-medium">Block</div>
              <div className="text-muted-foreground text-xs">
                Deny prompts and tool calls that match detection rules
              </div>
            </div>
          </label>
          <label
            htmlFor="action-redact"
            className="hover:bg-muted/50 flex cursor-pointer items-start gap-3 rounded-md border p-3"
          >
            <RadioGroupItem
              value="redact"
              id="action-redact"
              className="mt-0.5"
            />
            <div>
              <div className="text-sm font-medium">Redact</div>
              <div className="text-muted-foreground text-xs">
                Replace sensitive content with [REDACTED] before it reaches the
                model
              </div>
            </div>
          </label>
        </RadioGroup>
      </div>

      {/* Detection Rules */}
      <div className="space-y-3">
        <Label className="text-sm font-medium">Detection Rules</Label>
        <div className="border-border divide-border divide-y rounded-lg border">
          {ALL_CATEGORIES.map((cat) => {
            const meta = RULE_CATEGORY_META[cat];
            const isAvailable = AVAILABLE_CATEGORIES.has(cat);
            const isExpanded = expandedCategory === cat;
            const rules = DETECTION_RULES[cat];

            return (
              <div key={cat}>
                {/* Category header */}
                <div
                  className={cn(
                    "flex items-center gap-3 px-4 py-3",
                    isAvailable && "cursor-pointer",
                  )}
                  onClick={() => {
                    if (isAvailable) {
                      setExpandedCategory(isExpanded ? null : cat);
                    }
                  }}
                >
                  {/* Expand chevron (only for available categories) */}
                  {isAvailable && (
                    <ChevronRight
                      className={cn(
                        "text-muted-foreground h-4 w-4 shrink-0 transition-transform",
                        isExpanded && "rotate-90",
                      )}
                    />
                  )}
                  {!isAvailable && <div className="w-4 shrink-0" />}

                  {/* Category icon */}
                  <Icon
                    name={meta.icon as IconName}
                    className="text-muted-foreground size-4 shrink-0"
                  />

                  {/* Label & description */}
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium">{meta.label}</span>
                      {!isAvailable && (
                        <Badge variant="outline" className="text-[10px]">
                          Coming Soon
                        </Badge>
                      )}
                    </div>
                    <p className="text-muted-foreground text-xs">
                      {meta.description}
                    </p>
                  </div>

                  {/* Category checkbox */}
                  <Checkbox
                    checked={selectedCategories.has(cat)}
                    disabled={!isAvailable}
                    onCheckedChange={(checked) => {
                      const next = new Set(selectedCategories);
                      if (checked) {
                        next.add(cat);
                      } else {
                        next.delete(cat);
                      }
                      setSelectedCategories(next);
                    }}
                    onClick={(e) => e.stopPropagation()}
                  />
                </div>

                {/* Expanded rules list */}
                {isAvailable && isExpanded && rules.length > 0 && (
                  <div className="bg-muted/30 border-border border-t px-4 py-2">
                    <div className="space-y-2 py-1">
                      {rules.map((rule) => (
                        <div
                          key={rule.id}
                          className="flex items-center gap-3 py-1 pl-8"
                        >
                          <Checkbox
                            id={rule.id}
                            checked={selectedCategories.has(cat)}
                            disabled={true}
                          />
                          <label
                            htmlFor={rule.id}
                            className="text-muted-foreground text-xs"
                          >
                            {rule.title}
                          </label>
                        </div>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </div>

      {/* Enabled toggle */}
      <div className="flex items-center justify-between">
        <div>
          <Label className="text-sm font-medium">Enabled</Label>
          <p className="text-muted-foreground text-xs">
            Enable this policy to begin scanning messages.
          </p>
        </div>
        <Switch checked={formEnabled} onCheckedChange={setFormEnabled} />
      </div>
    </div>
  );
}

/* -------------------------------------------------------------------------- */
/*  RunPanel                                                                  */
/* -------------------------------------------------------------------------- */

function RunPanel({
  policy,
  onTrigger,
  isTriggerPending,
}: {
  policy: RiskPolicy;
  onTrigger: () => void;
  isTriggerPending: boolean;
}) {
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
                    variant="ghost"
                    size="icon-sm"
                    onClick={() => refetch()}
                    disabled={isFetching}
                    tooltip="Refresh"
                    className="h-6 w-6"
                  >
                    <RefreshCw
                      className={cn("h-3 w-3", isFetching && "animate-spin")}
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

      <SheetFooter className="border-border border-t px-6 py-4">
        <Button
          onClick={onTrigger}
          disabled={isTriggerPending}
          className="w-full"
        >
          {isTriggerPending && (
            <Loader2 className="mr-2 h-4 w-4 animate-spin" />
          )}
          Trigger Analysis
        </Button>
      </SheetFooter>
    </>
  );
}

/* -------------------------------------------------------------------------- */
/*  ActionBadge                                                               */
/* -------------------------------------------------------------------------- */

const ACTION_BADGE_CONFIG: Record<
  PolicyAction,
  { label: string; variant: "secondary" | "destructive" | "outline" }
> = {
  flag: { label: "Flag", variant: "secondary" },
  block: { label: "Block", variant: "destructive" },
  redact: { label: "Redact", variant: "outline" },
};

function ActionBadge({ action }: { action: PolicyAction }) {
  const config = ACTION_BADGE_CONFIG[action] ?? ACTION_BADGE_CONFIG.flag;
  return <Badge variant={config.variant}>{config.label}</Badge>;
}
