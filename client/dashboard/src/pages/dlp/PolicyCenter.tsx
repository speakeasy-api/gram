import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
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
} from "@speakeasy-api/moonshine";
import { Plus, Shield, Ellipsis, Loader2 } from "lucide-react";
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
    triggerMutation.mutate({ request: { id } });
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
            <p className="text-muted-foreground text-sm">Loading policies...</p>
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
            <Shield className="text-muted-foreground h-12 w-12" />
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
              {createMutation.isPending ? "Creating..." : "Get Started"}
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
              <TableHead>Sources</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Progress</TableHead>
              <TableHead>Version</TableHead>
              <TableHead className="w-[60px]" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {policies.map((policy) => (
              <TableRow
                key={policy.id}
                className="cursor-pointer"
                onClick={() => handleEdit(policy)}
              >
                <TableCell className="font-medium">{policy.name}</TableCell>
                <TableCell>
                  <div className="flex gap-1">
                    {policy.sources.map((s) => (
                      <Badge key={s} variant="outline">
                        {s}
                      </Badge>
                    ))}
                  </div>
                </TableCell>
                <TableCell>
                  <Switch
                    checked={policy.enabled}
                    onCheckedChange={(checked) => handleToggle(policy, checked)}
                    onClick={(e) => e.stopPropagation()}
                  />
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
                <TableCell>v{policy.version}</TableCell>
                <TableCell>
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
                        View Run
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        className="cursor-pointer"
                        onSelect={() => handleTrigger(policy.id)}
                      >
                        Trigger Analysis
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
            ))}
          </TableBody>
        </Table>

        {/* Edit/Create Sheet */}
        <Sheet open={sheetOpen} onOpenChange={setSheetOpen}>
          <SheetContent>
            <SheetHeader>
              <SheetTitle>
                {editingPolicy ? "Edit Policy" : "New Policy"}
              </SheetTitle>
              <SheetDescription>
                {editingPolicy
                  ? "Update the risk analysis policy configuration."
                  : "Create a new risk analysis policy to scan chat messages."}
              </SheetDescription>
            </SheetHeader>
            <div className="space-y-4 py-4">
              <div className="space-y-2">
                <label className="text-sm font-medium">Policy Name</label>
                <Input
                  value={formName}
                  onChange={(e) => setFormName(e.target.value)}
                  placeholder="e.g. Secret Detection"
                />
              </div>
              <div className="flex items-center justify-between">
                <label className="text-sm font-medium">Enabled</label>
                <Switch
                  checked={formEnabled}
                  onCheckedChange={setFormEnabled}
                />
              </div>
            </div>
            <SheetFooter>
              <Button
                onClick={handleSave}
                disabled={
                  !formName.trim() ||
                  createMutation.isPending ||
                  updateMutation.isPending
                }
              >
                {editingPolicy ? "Update" : "Create"}
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

  return (
    <>
      <SheetHeader>
        <SheetTitle>{policy.name}</SheetTitle>
        <SheetDescription>
          Analysis run details for version {policy.version}
        </SheetDescription>
      </SheetHeader>

      <div className="space-y-6 py-6">
        {isLoading ? (
          <div className="flex items-center justify-center py-8">
            <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
          </div>
        ) : status ? (
          <>
            {/* Workflow Status */}
            <div className="space-y-2">
              <label className="text-muted-foreground text-xs font-medium tracking-wider uppercase">
                Workflow
              </label>
              <div className="flex items-center gap-2">
                <span
                  className={`inline-block h-2 w-2 rounded-full ${
                    status.workflowStatus === "running"
                      ? "bg-green-500"
                      : status.workflowStatus === "sleeping"
                        ? "bg-yellow-500"
                        : "bg-muted-foreground"
                  }`}
                />
                <span className="text-sm capitalize">
                  {status.workflowStatus.replace("_", " ")}
                </span>
              </div>
            </div>

            {/* Progress */}
            <div className="space-y-2">
              <label className="text-muted-foreground text-xs font-medium tracking-wider uppercase">
                Analysis Progress
              </label>
              <div className="space-y-1">
                <div className="flex justify-between text-sm">
                  <span>
                    {status.analyzedMessages.toLocaleString()} /{" "}
                    {status.totalMessages.toLocaleString()} messages
                  </span>
                  <span className="text-muted-foreground">
                    {status.totalMessages > 0
                      ? Math.round(
                          (status.analyzedMessages / status.totalMessages) *
                            100,
                        )
                      : 0}
                    %
                  </span>
                </div>
                <div className="bg-muted h-2 overflow-hidden rounded-full">
                  <div
                    className="bg-primary h-full rounded-full transition-all"
                    style={{
                      width: `${status.totalMessages > 0 ? (status.analyzedMessages / status.totalMessages) * 100 : 0}%`,
                    }}
                  />
                </div>
              </div>
              {status.pendingMessages > 0 && (
                <p className="text-muted-foreground text-xs">
                  {status.pendingMessages.toLocaleString()} messages pending
                </p>
              )}
            </div>

            {/* Findings */}
            <div className="space-y-2">
              <label className="text-muted-foreground text-xs font-medium tracking-wider uppercase">
                Findings
              </label>
              <p className="text-2xl font-bold">
                {status.findingsCount.toLocaleString()}
              </p>
              <p className="text-muted-foreground text-xs">
                secrets and sensitive data detected
              </p>
            </div>

            {/* Policy Version */}
            <div className="space-y-2">
              <label className="text-muted-foreground text-xs font-medium tracking-wider uppercase">
                Policy Version
              </label>
              <p className="text-sm">v{status.policyVersion}</p>
            </div>
          </>
        ) : null}
      </div>

      <SheetFooter className="border-border border-t pt-4">
        <Button onClick={onTrigger} disabled={isTriggerPending}>
          {isTriggerPending && (
            <Loader2 className="mr-2 h-4 w-4 animate-spin" />
          )}
          Trigger Analysis
        </Button>
      </SheetFooter>
    </>
  );
}
