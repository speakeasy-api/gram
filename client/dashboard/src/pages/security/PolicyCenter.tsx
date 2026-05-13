import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Switch } from "@/components/ui/switch";
import { TextArea } from "@/components/ui/textarea";
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
  "shadow_mcp",
  "destructive_tool",
  "prompt_injection",
]);

/** All rule categories in display order */
const ALL_CATEGORIES: RuleCategory[] = [
  "secrets",
  ...PRESIDIO_CATEGORIES,
  "shadow_mcp",
  "destructive_tool",
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
  if (sources.includes("shadow_mcp")) cats.add("shadow_mcp");
  if (sources.includes("destructive_tool")) cats.add("destructive_tool");
  if (sources.includes("prompt_injection")) cats.add("prompt_injection");
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
  if (cats.has("shadow_mcp")) sources.push("shadow_mcp");
  if (cats.has("destructive_tool")) sources.push("destructive_tool");
  if (cats.has("prompt_injection")) sources.push("prompt_injection");
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
  const [formAutoName, setFormAutoName] = useState(true);
  const [formUserMessage, setFormUserMessage] = useState("");

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
    setFormAutoName(true);
    setFormUserMessage("");
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
    setFormAutoName(policy.autoName ?? true);
    setFormUserMessage(policy.userMessage ?? "");
    setSheetOpen(true);
  };

  const handleSave = () => {
    const { sources, presidioEntities } =
      categoriesToPayload(selectedCategories);
    const action =
      sources.includes("destructive_tool") && formAction === "block"
        ? "flag"
        : formAction;
    if (editingPolicy) {
      updateMutation.mutate({
        request: {
          updateRiskPolicyRequestBody: {
            id: editingPolicy.id,
            name: formName,
            enabled: formEnabled,
            sources,
            presidioEntities,
            action,
            autoName: formAutoName,
            userMessage: formUserMessage,
          },
        },
      });
    } else {
      createMutation.mutate({
        request: {
          createRiskPolicyRequestBody: {
            ...(formAutoName ? {} : { name: formName }),
            enabled: formEnabled,
            sources,
            presidioEntities,
            action,
            autoName: formAutoName,
            ...(formUserMessage.trim() ? { userMessage: formUserMessage } : {}),
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
          <Button onClick={handleCreate}>
            <Plus className="mr-2 h-4 w-4" />
            New Policy
          </Button>
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
                formAutoName={formAutoName}
                setFormAutoName={setFormAutoName}
                formUserMessage={formUserMessage}
                setFormUserMessage={setFormUserMessage}
              />
            </div>
            <SheetFooter className="px-6 pb-6">
              <Button
                onClick={handleSave}
                disabled={
                  (!formAutoName && !formName.trim()) ||
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
  formAutoName,
  setFormAutoName,
  formUserMessage,
  setFormUserMessage,
}: {
  formName: string;
  setFormName: (v: string) => void;
  formEnabled: boolean;
  setFormEnabled: (v: boolean) => void;
  selectedCategories: Set<RuleCategory>;
  setSelectedCategories: (v: Set<RuleCategory>) => void;
  formAction: PolicyAction;
  setFormAction: (v: PolicyAction) => void;
  formAutoName: boolean;
  setFormAutoName: (v: boolean) => void;
  formUserMessage: string;
  setFormUserMessage: (v: string) => void;
}) {
  const [expandedCategory, setExpandedCategory] = useState<RuleCategory | null>(
    null,
  );
  const destructiveToolsSelected = selectedCategories.has("destructive_tool");
  const actionValue =
    destructiveToolsSelected && formAction === "block" ? "flag" : formAction;

  return (
    <div className="space-y-6 py-4">
      {/* Policy Name */}
      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <Label className="text-sm font-medium">Policy Name</Label>
          <div className="flex items-center gap-2">
            <span className="text-muted-foreground text-xs">Auto</span>
            <Switch checked={formAutoName} onCheckedChange={setFormAutoName} />
          </div>
        </div>
        {formAutoName ? (
          <p className="text-muted-foreground text-xs">
            Name will be generated automatically based on detection rules and
            action.
          </p>
        ) : (
          <Input
            value={formName}
            onChange={(value) => setFormName(value)}
            placeholder="e.g. Secret Detection"
          />
        )}
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
            const isExpandable = isAvailable && rules.length > 0;

            return (
              <div key={cat}>
                {/* Category header */}
                <div
                  className={cn(
                    "flex items-center gap-3 px-4 py-3",
                    isExpandable && "cursor-pointer",
                  )}
                  onClick={() => {
                    if (isExpandable) {
                      setExpandedCategory(isExpanded ? null : cat);
                    }
                  }}
                >
                  {/* Expand chevron (only for categories with rules to expand) */}
                  {isExpandable ? (
                    <ChevronRight
                      className={cn(
                        "text-muted-foreground h-4 w-4 shrink-0 transition-transform",
                        isExpanded && "rotate-90",
                      )}
                    />
                  ) : (
                    <div className="w-4 shrink-0" />
                  )}

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
                      if (
                        checked &&
                        cat === "destructive_tool" &&
                        formAction === "block"
                      ) {
                        setFormAction("flag");
                      }
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

      {/* Action */}
      <div className="space-y-2">
        <Label className="text-sm font-medium">Action</Label>
        <RadioGroup
          value={actionValue}
          onValueChange={(v) => {
            if (destructiveToolsSelected && v === "block") {
              return;
            }
            setFormAction(v as PolicyAction);
          }}
        >
          <div className="border-border divide-border divide-y rounded-lg border">
            {ACTION_OPTIONS.map((opt) => {
              const disabled =
                destructiveToolsSelected && opt.value === "block";

              return (
                <label
                  key={opt.value}
                  htmlFor={`action-${opt.value}`}
                  className={cn(
                    "flex items-start gap-3 p-3",
                    disabled
                      ? "cursor-not-allowed opacity-60"
                      : "hover:bg-muted/50 cursor-pointer",
                  )}
                >
                  <RadioGroupItem
                    value={opt.value}
                    id={`action-${opt.value}`}
                    className="mt-0.5"
                    disabled={disabled}
                  />
                  <div className="flex-1">
                    <div className="flex items-center gap-2">
                      <ActionBadge action={opt.value} />
                    </div>
                    <div className="text-muted-foreground mt-1 text-xs">
                      {opt.description}
                    </div>
                    {disabled && (
                      <div className="text-destructive mt-1 text-xs font-medium">
                        Destructive Tools supports flagging only.
                      </div>
                    )}
                  </div>
                </label>
              );
            })}
          </div>
        </RadioGroup>
      </div>

      {/* Custom message */}
      <div className="space-y-2">
        <Label className="text-sm font-medium">Custom Message</Label>
        <p className="text-muted-foreground text-xs">
          {formAction === "block"
            ? "Shown to the user when this policy blocks a tool call or prompt. Leave blank to use the default message."
            : "Shown alongside flagged findings in the dashboard. Leave blank to use the default message."}
        </p>
        <TextArea
          value={formUserMessage}
          onChange={setFormUserMessage}
          placeholder="e.g. This action was blocked by your organization's security policy. Contact your admin for help."
          rows={3}
        />
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
  { label: string; variant: "secondary" | "destructive" }
> = {
  flag: { label: "Flag", variant: "secondary" },
  block: { label: "Block", variant: "destructive" },
};

const ACTION_OPTIONS: { value: PolicyAction; description: string }[] = [
  {
    value: "flag",
    description: "Log findings for review without interrupting the session",
  },
  {
    value: "block",
    description: "Deny prompts and tool calls that match detection rules",
  },
];

function ActionBadge({ action }: { action: PolicyAction }) {
  const config = ACTION_BADGE_CONFIG[action] ?? ACTION_BADGE_CONFIG.flag;
  return <Badge variant={config.variant}>{config.label}</Badge>;
}
