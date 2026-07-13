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
import { invalidateAllListChats } from "@gram/client/react-query/listChats.js";
import { useRiskCreateExclusionMutation } from "@gram/client/react-query/riskCreateExclusion.js";
import { invalidateAllRiskListExclusions } from "@gram/client/react-query/riskListExclusions.js";
import { useRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
import { invalidateAllRiskListResults } from "@gram/client/react-query/riskListResults.js";
import { invalidateAllRiskListResultsByChat } from "@gram/client/react-query/riskListResultsByChat.js";
import { invalidateAllRiskListResultsForAgent } from "@gram/client/react-query/riskListResultsForAgent.js";
import { invalidateAllRiskOverview } from "@gram/client/react-query/riskOverview.js";
import { useRiskSuggestCustomRuleMutation } from "@gram/client/react-query/riskSuggestCustomRule.js";
import { useRiskUpdateExclusionMutation } from "@gram/client/react-query/riskUpdateExclusion.js";
import type { RiskExclusion } from "@gram/client/models/components/riskexclusion.js";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2, Sparkles } from "lucide-react";
import type { JSX } from "react";
import { useState } from "react";
import { toast } from "sonner";
import { BUILTIN_RULES_BY_CATEGORY } from "./detection-rules-data";
import {
  type ExclusionFields,
  parseExclusionExpression,
  serializeExclusionExpression,
} from "./exclusion-expression";

export const GLOBAL_SCOPE = "__global__";

// Rule ids the AI suggestion may reference in rule_id clauses. Built-ins only:
// they cover the common asks ("email findings", "AWS keys") without an extra
// fetch; custom rule ids can still be named in the prompt itself.
const BUILTIN_RULE_ID_LIST = Object.values(BUILTIN_RULES_BY_CATEGORY)
  .flat()
  .map((rule) => rule.id);

export type ExclusionSheetState =
  | { mode: "create"; initialExpression?: string; initialScope?: string }
  | { mode: "edit"; exclusion: RiskExclusion };

/**
 * The create/edit exclusion form together with its policy list, mutations and
 * cache invalidation — but no surrounding chrome. Render it inside a Sheet (see
 * `ExclusionSheet`) or inline as a sub-view (the chat detail panel). `onDone`
 * fires after a successful save; the host uses it to close or navigate back.
 */
export function ExclusionEditor({
  state,
  onDone,
}: {
  state: ExclusionSheetState;
  onDone: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const { data: policyData } = useRiskListPolicies();
  // Exclusions aren't supported for prompt-based (LLM-judge) policies yet
  // (AGE-2750), so keep them out of the scope dropdown.
  const policies = (policyData?.policies ?? []).filter(
    (p) => p.policyType !== "prompt_based",
  );

  // Saving an exclusion suppresses/restores findings retroactively, so refresh
  // the exclusion list AND every risk-results surface (chat detail, agent,
  // overview) so stale findings disappear without a manual reload. Note the
  // server applies the exclusion asynchronously (Temporal reconcile), so the
  // refetched results lag; hosts that need instant feedback hide the originating
  // finding optimistically on `onDone`.
  const invalidate = () =>
    Promise.all([
      invalidateAllRiskListExclusions(queryClient),
      invalidateAllRiskListResults(queryClient),
      invalidateAllRiskListResultsByChat(queryClient),
      invalidateAllRiskListResultsForAgent(queryClient),
      invalidateAllRiskOverview(queryClient),
      // The Agent Sessions list shows per-session risk counts, so refresh it too
      // (lags the async reconcile like the other surfaces).
      invalidateAllListChats(queryClient),
    ]);

  const createMutation = useRiskCreateExclusionMutation({
    onSuccess: () => {
      void invalidate();
      toast.success(
        "Exclusion created. Matching findings will update shortly.",
      );
      onDone();
    },
    onError: () => toast.error("Failed to create exclusion."),
  });
  const updateMutation = useRiskUpdateExclusionMutation({
    onSuccess: () => {
      void invalidate();
      toast.success("Exclusion updated. Findings will update shortly.");
      onDone();
    },
    onError: () => toast.error("Failed to update exclusion."),
  });

  const editing = state.mode === "edit" ? state.exclusion : null;
  const submitting = createMutation.isPending || updateMutation.isPending;

  const formKey =
    state.mode === "edit"
      ? `edit-${state.exclusion.id}`
      : `create-${state.initialExpression ?? ""}-${state.initialScope ?? ""}`;

  return (
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
  );
}

/**
 * Reusable create/edit exclusion sheet. Drops the {@link ExclusionEditor} into a
 * Sheet so it can be used from any surface (the Exclusions tab, a trace entry)
 * by passing a `state` and an `onOpenChange` handler.
 */
export function ExclusionSheet({
  state,
  onOpenChange,
}: {
  state: ExclusionSheetState | null;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  const editing = state?.mode === "edit";
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
          <ExclusionEditor state={state} onDone={() => onOpenChange(false)} />
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
  const [askPrompt, setAskPrompt] = useState("");

  // Same endpoint the detection-rule create sheet uses, pointed at the
  // exclusion surface. The structured fields it returns are serialized through
  // the same mapping the form parses on save, so a suggestion the user accepts
  // untouched is guaranteed to round-trip.
  const suggestMutation = useRiskSuggestCustomRuleMutation({
    onSuccess: (data) => {
      if (!data.exclusionMatchType || !data.exclusionMatchValue) {
        toast.error("No suggestion came back. Try rewording your request.");
        return;
      }
      setExpression(
        serializeExclusionExpression({
          matchType: data.exclusionMatchType,
          matchValue: data.exclusionMatchValue,
          ruleIdFilter: data.exclusionRuleIdFilter ?? "",
          sourceFilter: data.exclusionSourceFilter ?? "",
        }),
      );
      setError(null);
    },
    onError: (err) => {
      const message =
        err instanceof Error ? err.message : "Failed to generate suggestion";
      toast.error(message);
    },
  });

  const handleSuggest = () => {
    const prompt = askPrompt.trim();
    if (prompt.length < 3) {
      toast.error("Describe what you want to stop flagging first.");
      return;
    }
    suggestMutation.mutate({
      request: {
        suggestCustomDetectionRuleRequestBody: {
          prompt,
          existingRuleIds: BUILTIN_RULE_ID_LIST,
          target: "exclusion",
        },
      },
    });
  };

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
          <Label>Suggest with AI</Label>
          <TextArea
            rows={2}
            value={askPrompt}
            onChange={setAskPrompt}
            placeholder="e.g. stop flagging our shared test account jane.doe@acme.com in email findings"
          />
          <div className="flex items-center justify-between gap-3">
            <Type className="text-muted-foreground" small>
              Describe what to stop flagging. We'll write the criteria
              expression, you tweak before saving.
            </Type>
            <Button
              variant="secondary"
              size="sm"
              disabled={
                askPrompt.trim().length < 3 || suggestMutation.isPending
              }
              onClick={handleSuggest}
            >
              <Button.LeftIcon>
                {suggestMutation.isPending ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <Sparkles className="h-4 w-4" />
                )}
              </Button.LeftIcon>
              <Button.Text>Suggest with AI</Button.Text>
            </Button>
          </div>
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
    ['source == "prompt_injection"', "suppress a source"],
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
