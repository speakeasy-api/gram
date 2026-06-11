import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { TextArea } from "@/components/ui/textarea";
import { Type } from "@/components/ui/type";
import { Button } from "@speakeasy-api/moonshine";
import {
  invalidateAllRiskListExclusions,
  invalidateAllRiskListResults,
  invalidateAllRiskListResultsByChat,
  invalidateAllRiskListResultsForAgent,
  invalidateAllRiskOverview,
  useRiskCreateExclusionMutation,
  useRiskListPolicies,
  useRiskUpdateExclusionMutation,
} from "@gram/client/react-query/index.js";
import type { RiskExclusion } from "@gram/client/models/components/riskexclusion.js";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import type { JSX } from "react";
import { useState } from "react";
import { toast } from "sonner";
import {
  type ExclusionFields,
  parseExclusionExpression,
  serializeExclusionExpression,
} from "./exclusion-expression";

export const GLOBAL_SCOPE = "__global__";

export type ExclusionSheetState =
  | { mode: "create"; initialExpression?: string; initialScope?: string }
  | { mode: "edit"; exclusion: RiskExclusion };

/**
 * Reusable create/edit exclusion sheet. Owns its own policy list, mutations,
 * and cache invalidation so it can be dropped into any surface (the Exclusions
 * tab or a trace/session entry's "Create exclusion" action) by passing a
 * `state` and an `onOpenChange` handler.
 */
export function ExclusionSheet({
  state,
  onOpenChange,
}: {
  state: ExclusionSheetState | null;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const { data: policyData } = useRiskListPolicies();
  const policies = policyData?.policies ?? [];

  // Saving an exclusion suppresses/restores findings retroactively, so refresh
  // the exclusion list AND every risk-results surface (chat detail, agent,
  // overview) so stale findings disappear without a manual reload.
  const invalidate = () =>
    Promise.all([
      invalidateAllRiskListExclusions(queryClient),
      invalidateAllRiskListResults(queryClient),
      invalidateAllRiskListResultsByChat(queryClient),
      invalidateAllRiskListResultsForAgent(queryClient),
      invalidateAllRiskOverview(queryClient),
    ]);

  const createMutation = useRiskCreateExclusionMutation({
    onSuccess: () => {
      void invalidate();
      onOpenChange(false);
      toast.success(
        "Exclusion created. Matching findings will update shortly.",
      );
    },
    onError: () => toast.error("Failed to create exclusion."),
  });
  const updateMutation = useRiskUpdateExclusionMutation({
    onSuccess: () => {
      void invalidate();
      onOpenChange(false);
      toast.success("Exclusion updated. Findings will update shortly.");
    },
    onError: () => toast.error("Failed to update exclusion."),
  });

  const editing = state?.mode === "edit" ? state.exclusion : null;
  const submitting = createMutation.isPending || updateMutation.isPending;

  const formKey = (() => {
    if (!state) return "closed";
    if (state.mode === "edit") return `edit-${state.exclusion.id}`;
    return `create-${state.initialExpression ?? ""}-${state.initialScope ?? ""}`;
  })();

  return (
    <Sheet open={state !== null} onOpenChange={onOpenChange}>
      <SheetContent className="flex flex-col overflow-y-auto sm:max-w-lg">
        <SheetHeader className="px-6 pt-6">
          <SheetTitle>
            {editing ? "Edit exclusion" : "Create exclusion"}
          </SheetTitle>
          <SheetDescription>
            {editing
              ? "Changes re-apply retroactively across existing findings."
              : "Suppress matching findings retroactively and going forward. Does not re-run analysis."}
          </SheetDescription>
        </SheetHeader>
        {state && (
          <ExclusionForm
            key={formKey}
            policies={policies}
            state={state}
            submitting={submitting}
            onSubmit={({ fields, scope, enabled }) => {
              const riskPolicyId = scope === GLOBAL_SCOPE ? undefined : scope;
              if (editing) {
                updateMutation.mutate({
                  request: {
                    updateRiskExclusionRequestBody: {
                      id: editing.id,
                      matchType: fields.matchType,
                      matchValue: fields.matchValue,
                      ruleIdFilter: fields.ruleIdFilter,
                      sourceFilter: fields.sourceFilter,
                      riskPolicyId,
                      enabled,
                    },
                  },
                });
              } else {
                createMutation.mutate({
                  request: {
                    createRiskExclusionRequestBody: {
                      matchType: fields.matchType,
                      matchValue: fields.matchValue,
                      ruleIdFilter: fields.ruleIdFilter,
                      sourceFilter: fields.sourceFilter,
                      riskPolicyId,
                      enabled,
                    },
                  },
                });
              }
            }}
          />
        )}
      </SheetContent>
    </Sheet>
  );
}

interface RiskPolicyOption {
  id: string;
  name: string;
}

interface ExclusionFormProps {
  policies: RiskPolicyOption[];
  state: ExclusionSheetState;
  submitting: boolean;
  onSubmit: (payload: {
    fields: ExclusionFields;
    scope: string;
    enabled: boolean;
  }) => void;
}

function initialExpressionFor(state: ExclusionSheetState): string {
  if (state.mode === "edit") {
    return serializeExclusionExpression(state.exclusion);
  }
  return state.initialExpression ?? "";
}

function initialScopeFor(state: ExclusionSheetState): string {
  if (state.mode === "edit") {
    return state.exclusion.riskPolicyId ?? GLOBAL_SCOPE;
  }
  return state.initialScope ?? GLOBAL_SCOPE;
}

function ExclusionForm({
  policies,
  state,
  submitting,
  onSubmit,
}: ExclusionFormProps) {
  const editing = state.mode === "edit" ? state.exclusion : null;
  const [scope, setScope] = useState<string>(initialScopeFor(state));
  const [expression, setExpression] = useState<string>(
    initialExpressionFor(state),
  );
  const [enabled, setEnabled] = useState<boolean>(editing?.enabled ?? true);
  const [error, setError] = useState<string | null>(null);

  const handleSave = () => {
    const parsed = parseExclusionExpression(expression);
    if (!parsed.ok) {
      setError(parsed.error);
      return;
    }
    setError(null);
    onSubmit({ fields: parsed.value, scope, enabled });
  };

  return (
    <>
      <div className="flex-1 space-y-5 overflow-y-auto px-6">
        <div className="space-y-2">
          <Label>Scope</Label>
          <Select value={scope} onValueChange={setScope}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={GLOBAL_SCOPE}>
                Global — all policies in this project
              </SelectItem>
              {policies.map((policy) => (
                <SelectItem key={policy.id} value={policy.id}>
                  {policy.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="space-y-2">
          <Label>Exclusion criteria</Label>
          <TextArea
            rows={4}
            value={expression}
            onChange={setExpression}
            placeholder={'e.g. match == "jane.doe@acme.com"'}
            className="font-mono text-sm"
          />
          {error && <Type className="text-destructive text-sm">{error}</Type>}
          <ExclusionExamples />
        </div>

        {editing && (
          <div className="flex items-center justify-between">
            <Label>Status</Label>
            <Switch checked={enabled} onCheckedChange={setEnabled} />
          </div>
        )}
      </div>

      <SheetFooter className="px-6 pb-6">
        <Button onClick={handleSave} disabled={submitting}>
          {submitting && (
            <Button.LeftIcon>
              <Loader2 className="h-4 w-4 animate-spin" />
            </Button.LeftIcon>
          )}
          <Button.Text>
            {submitting ? "Saving…" : editing ? "Update" : "Create"}
          </Button.Text>
        </Button>
      </SheetFooter>
    </>
  );
}

function ExclusionExamples() {
  const examples: [string, string][] = [
    ['match == "value"', "exact literal match"],
    ['match ~= "regex"', "regex (RE2 syntax, ≤ 512 chars)"],
    ['rule_id == "pii.email_address"', "suppress a specific rule"],
    ['source == "presidio"', "suppress a source"],
    ['entity_type == "EMAIL_ADDRESS"', "suppress by entity type"],
  ];
  return (
    <div className="bg-muted/40 text-muted-foreground space-y-1 rounded-md p-3 text-xs">
      <Type className="font-medium" small>
        Examples
      </Type>
      <ul className="space-y-1">
        {examples.map(([code, desc]) => (
          <li key={code}>
            <code className="font-mono">{code}</code> — {desc}
          </li>
        ))}
      </ul>
      <Type className="text-muted-foreground" small>
        Combine with <code className="font-mono">&amp;&amp;</code> to scope by
        rule or source.
      </Type>
    </div>
  );
}
