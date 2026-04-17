import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Icon,
} from "@speakeasy-api/moonshine";
import type { IconName } from "@speakeasy-api/moonshine";
import { Plus, Shield, Ellipsis, Loader2, ChevronRight } from "lucide-react";
import { useState, useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  useRiskListPolicies,
  useRiskCreatePolicyMutation,
  useRiskUpdatePolicyMutation,
  useRiskDeletePolicyMutation,
  useRiskTriggerAnalysisMutation,
  invalidateAllRiskListPolicies,
} from "@gram/client/react-query/index.js";
import {
  useRiskGetPolicyStatus,
  invalidateAllRiskGetPolicyStatus,
} from "@gram/client/react-query/riskGetPolicyStatus.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import {
  RULE_CATEGORY_META,
  DETECTION_RULES,
  type RuleCategory,
} from "./policy-data";
import { cn } from "@/lib/utils";

/** Map API source names to rule categories */
function sourcesToCategories(sources: string[]): RuleCategory[] {
  const mapping: Record<string, RuleCategory> = {
    gitleaks: "secrets",
    presidio: "pii",
  };
  return sources
    .map((s) => mapping[s] ?? ("secrets" as RuleCategory))
    .filter((v, i, a) => a.indexOf(v) === i);
}

/** All rule categories in display order */
const ALL_CATEGORIES: RuleCategory[] = [
  "secrets",
  "financial",
  "pii",
  "government_ids",
  "healthcare",
  "prompt_attacks",
  "prompt_injection",
  "off_policy",
];

/** Categories that are currently available (not "Coming Soon") */
const AVAILABLE_CATEGORIES: Set<RuleCategory> = new Set(["secrets"]);

export default function PolicyCenter() {
  const queryClient = useQueryClient();
  const { data, isLoading } = useRiskListPolicies();
  const policies = data?.policies ?? [];

  const [sheetOpen, setSheetOpen] = useState(false);
  const [editingPolicy, setEditingPolicy] = useState<RiskPolicy | null>(null);
  const [formName, setFormName] = useState("");
  const [formEnabled, setFormEnabled] = useState(true);

  const [runPanelPolicy, setRunPanelPolicy] = useState<RiskPolicy | null>(null);

  const invalidate = useCallback(() => {
    invalidateAllRiskListPolicies(queryClient);
    invalidateAllRiskGetPolicyStatus(queryClient);
  }, [queryClient]);

  const createMutation = useRiskCreatePolicyMutation({
    onSuccess: () => {
      invalidate();
      setSheetOpen(false);
    },
  });

  const updateMutation = useRiskUpdatePolicyMutation({
    onSuccess: () => {
      invalidate();
      setSheetOpen(false);
    },
  });

  const deleteMutation = useRiskDeletePolicyMutation({
    onSuccess: invalidate,
  });

  const triggerMutation = useRiskTriggerAnalysisMutation({
    onSuccess: invalidate,
  });

  const handleCreate = () => {
    setEditingPolicy(null);
    setFormName("");
    setFormEnabled(true);
    setSheetOpen(true);
  };

  const handleEdit = (policy: RiskPolicy) => {
    setEditingPolicy(policy);
    setFormName(policy.name);
    setFormEnabled(policy.enabled);
    setSheetOpen(true);
  };

  const handleSave = () => {
    if (editingPolicy) {
      updateMutation.mutate({
        request: {
          updateRiskPolicyRequestBody: {
            id: editingPolicy.id,
            name: formName,
            enabled: formEnabled,
          },
        },
      });
    } else {
      createMutation.mutate({
        request: {
          createRiskPolicyRequestBody: {
            name: formName,
            enabled: formEnabled,
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
          <div className="flex flex-col items-center justify-center gap-4 py-20">
            <div className="bg-muted flex h-14 w-14 items-center justify-center rounded-full">
              <Shield className="text-muted-foreground h-7 w-7" />
            </div>
            <h2 className="text-lg font-semibold">No Risk Policies</h2>
            <p className="text-muted-foreground max-w-md text-center text-sm">
              Risk policies scan your chat messages for secrets and sensitive
              data. Create your first policy to get started.
            </p>
            <Button
              onClick={() =>
                createMutation.mutate({
                  request: {
                    createRiskPolicyRequestBody: {
                      name: "Secret Scanner",
                      enabled: true,
                    },
                  },
                })
              }
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
          <Button
            onClick={handleCreate}
            disabled={policies.length > 0}
            tooltip={
              policies.length > 0
                ? "Only one policy per project is supported"
                : undefined
            }
          >
            <Plus className="mr-2 h-4 w-4" />
            New Policy
          </Button>
        </div>

        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Categories</TableHead>
              <TableHead>Progress</TableHead>
              <TableHead>Status</TableHead>
              <TableHead className="w-[60px]" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {policies.map((policy) => {
              const categories = sourcesToCategories(policy.sources);
              return (
                <TableRow
                  key={policy.id}
                  className="cursor-pointer"
                  onClick={() => handleEdit(policy)}
                >
                  <TableCell className="font-medium">{policy.name}</TableCell>
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
}: {
  formName: string;
  setFormName: (v: string) => void;
  formEnabled: boolean;
  setFormEnabled: (v: boolean) => void;
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
          onChange={(e) => setFormName(e.target.value)}
          placeholder="e.g. Secret Detection"
        />
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
                    checked={isAvailable && cat === "secrets"}
                    disabled={!isAvailable}
                    onCheckedChange={() => {
                      // Currently only secrets is togglable; future: per-category toggle
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
                            checked={true}
                            disabled={true}
                          />
                          <label
                            htmlFor={rule.id}
                            className="text-muted-foreground text-xs"
                          >
                            {rule.description}
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
  const { data: status, isLoading } = useRiskGetPolicyStatus(
    { id: policy.id },
    undefined,
    { refetchInterval: 5000 },
  );

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
                <span className="text-muted-foreground text-xs font-medium">
                  {pct}%
                </span>
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
