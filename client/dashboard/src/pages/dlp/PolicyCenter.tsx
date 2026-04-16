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
import { Plus, Trash2, Shield, Play } from "lucide-react";
import { useState, useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  useRiskListPolicies,
  useRiskCreatePolicyMutation,
  useRiskUpdatePolicyMutation,
  useRiskDeletePolicyMutation,
  useRiskTriggerAnalysisMutation,
  queryKeyRiskListPolicies,
} from "@gram/client/react-query/index.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";

export default function PolicyCenter() {
  const queryClient = useQueryClient();
  const { data, isLoading } = useRiskListPolicies();
  const policies = data?.listRiskPoliciesResult?.policies ?? [];

  const [sheetOpen, setSheetOpen] = useState(false);
  const [editingPolicy, setEditingPolicy] = useState<RiskPolicy | null>(null);
  const [formName, setFormName] = useState("");
  const [formEnabled, setFormEnabled] = useState(true);

  const invalidate = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: queryKeyRiskListPolicies() });
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
    deleteMutation.mutate({
      request: { id },
    });
  };

  const handleTrigger = (id: string) => {
    triggerMutation.mutate({
      request: { id },
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
            <Button onClick={handleCreate}>
              <Plus className="mr-2 h-4 w-4" />
              Get Started
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
              <TableHead className="text-right">Actions</TableHead>
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
                <TableCell className="text-right">
                  <div className="flex items-center justify-end gap-1">
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={(e) => {
                        e.stopPropagation();
                        handleTrigger(policy.id);
                      }}
                      title="Trigger analysis"
                    >
                      <Play className="h-4 w-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={(e) => {
                        e.stopPropagation();
                        handleDelete(policy.id);
                      }}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>

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
      </Page.Body>
    </Page>
  );
}
