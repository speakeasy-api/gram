import { Label } from "@/components/ui/Label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Switch } from "@/components/ui/Switch";
import { TextArea } from "@/components/ui/Textarea";
import { Text } from "@/components/ui/Text";
import { Button } from "@/components/ui/Button";
import { invalidateAllListChats } from "@gram/client/react-query/listChats.js";
import { useRiskCreateExclusionMutation } from "@gram/client/react-query/riskCreateExclusion.js";
import { invalidateAllRiskListExclusions } from "@gram/client/react-query/riskListExclusions.js";
import { useRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
import { invalidateAllRiskListResults } from "@gram/client/react-query/riskListResults.js";
import { invalidateAllRiskListResultsByChat } from "@gram/client/react-query/riskListResultsByChat.js";
import { invalidateAllRiskListResultsForAgent } from "@gram/client/react-query/riskListResultsForAgent.js";
import { invalidateAllRiskOverview } from "@gram/client/react-query/riskOverview.js";
import { useRiskSuggestExclusionMutation } from "@gram/client/react-query/riskSuggestExclusion.js";
import { useRiskUpdateExclusionMutation } from "@gram/client/react-query/riskUpdateExclusion.js";
import type { RiskExclusion } from "@gram/client/models/components/riskexclusion.js";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { RadioGroup, RadioGroupItem } from "@/components/ui/RadioGroup";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2, Sparkles } from "lucide-react";
import type { JSX } from "react";
import { useState } from "react";
import { toast } from "sonner";
import { BUILTIN_RULE_ID_LIST } from "./detection-rules-data";
import {
  type ExclusionFields,
  parseExclusionExpression,
  serializeExclusionExpression,
} from "./exclusion-expression";
import {
  exactCandidate,
  exclusionOptions,
  type ExclusionOption,
} from "./exclusion-options";
import { hasRevealableEvent, REVEAL_SCOPE, useUnmaskedMatch } from "./unmask";
import { useRBAC } from "@/hooks/useRBAC";

const GLOBAL_SCOPE = "__global__";

export type ExclusionSheetState =
  | {
      mode: "create";
      /** Findings the create was started from. Drives the ready-made rule
       * picker; absent for the Exclusions tab / Policy Center buttons, which
       * have no finding and open straight onto the criteria box. */
      results?: RiskResult[];
    }
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
      : `create-${(state.results ?? []).map((r) => r.id).join(",")}`;

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
            {editing ? "Edit exclusion rule" : "Set up exclusion rule"}
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

function ExclusionForm({
  policies,
  state,
  submitting,
  onSubmit,
}: ExclusionFormProps) {
  const editing = state.mode === "edit" ? state.exclusion : null;
  const results = state.mode === "create" ? (state.results ?? []) : [];
  const single = results.length === 1 ? results[0] : undefined;

  // Risk Events and Risk Overview null the raw match at the API boundary, so
  // an exact rule there needs the same audited, chat:read-gated reveal the row
  // itself offers. Fired on selecting the option rather than on open: an
  // operator who picks "any finding from this rule" should not leave an audit
  // entry for a value they never looked at.
  const { hasScope } = useRBAC();
  const reveals = useUnmaskedMatch(single?.id ?? "");
  const exact = exactCandidate(
    single,
    reveals.value,
    hasScope(REVEAL_SCOPE, single?.chatId) &&
      hasRevealableEvent(single?.matchRedacted),
  );

  // Ready-made rules for the selection. Always at least ["custom"], so an
  // edit or a no-finding create renders no picker and opens on the DSL box.
  const options = exclusionOptions(results, exact);
  const [choice, setChoice] = useState<ExclusionOption["value"]>(
    // A pending exact option is not savable yet, so never open on it —
    // selecting it is the gesture that fires the reveal.
    () =>
      options.find((o) => o.value !== "exact" || o.fields)?.value ?? "custom",
  );
  const picked = options.find((o) => o.value === choice);
  // A finding-originated create is always global: "stop flagging this" means
  // everywhere, and the Scope select is there to narrow it.
  const [scope, setScope] = useState<string>(
    editing?.riskPolicyId ?? GLOBAL_SCOPE,
  );
  const [expression, setExpression] = useState<string>(
    editing ? serializeExclusionExpression(editing) : "",
  );
  const [enabled, setEnabled] = useState<boolean>(editing?.enabled ?? true);
  const [error, setError] = useState<string | null>(null);
  const [askPrompt, setAskPrompt] = useState("");

  // Dedicated exclusion-suggestion endpoint. The structured fields it returns
  // are serialized through the same mapping the form parses on save, so a
  // suggestion the user accepts untouched is guaranteed to round-trip.
  const suggestMutation = useRiskSuggestExclusionMutation({
    onSuccess: (data) => {
      if (!data.matchType || !data.matchValue) {
        toast.error("No suggestion came back. Try rewording your request.");
        return;
      }
      setExpression(
        serializeExclusionExpression({
          matchType: data.matchType,
          matchValue: data.matchValue,
          ruleIdFilter: data.ruleIdFilter ?? "",
          sourceFilter: data.sourceFilter ?? "",
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

  // A batch the picker can't cover (no shared rule or source) is exactly what
  // the server generalizes well, so hand it the selection alongside the
  // prompt. Single findings gain nothing — their local options are complete —
  // and the endpoint caps the list at 50 ids.
  const findingIds =
    results.length > 1 ? results.slice(0, 50).map((r) => r.id) : [];

  const handleSuggest = () => {
    const prompt = askPrompt.trim();
    if (prompt.length < 3) {
      toast.error("Describe what you want to stop flagging first.");
      return;
    }
    suggestMutation.mutate({
      request: {
        suggestExclusionRequestBody: {
          prompt,
          findingIds: findingIds.length > 0 ? findingIds : undefined,
          knownRuleIds: BUILTIN_RULE_ID_LIST,
        },
      },
    });
  };

  const handleSave = () => {
    // A picked option is already a complete rule, so it skips the
    // serialize -> parse round trip; only "custom" carries no fields.
    if (picked?.fields) {
      setError(null);
      onSubmit({ fields: picked.fields, scope, enabled });
      return;
    }

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
      <div className="flex-1 space-y-5 overflow-y-auto px-6 py-2">
        {options.length > 1 && (
          <div className="space-y-2">
            <Label>What should we stop flagging?</Label>
            <RadioGroup
              value={choice}
              onValueChange={(v) => {
                setChoice(v as ExclusionOption["value"]);
                if (v === "exact" && !single?.match) reveals.reveal();
              }}
              className="gap-2"
            >
              {options.map((option) => (
                <label
                  key={option.value}
                  htmlFor={`exclusion-${option.value}`}
                  className="hover:bg-muted/40 flex cursor-pointer items-start gap-3 border px-3 py-2.5"
                >
                  <RadioGroupItem
                    id={`exclusion-${option.value}`}
                    value={option.value}
                    className="mt-0.5"
                  />
                  <div className="min-w-0">
                    <div className="text-sm break-all">{option.title}</div>
                    <div className="text-muted-foreground text-xs">
                      {option.hint}
                    </div>
                  </div>
                </label>
              ))}
            </RadioGroup>
          </div>
        )}

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

        {choice === "custom" && (
          <>
            <div className="space-y-2">
              <Label>Suggest with AI</Label>
              <TextArea
                rows={2}
                value={askPrompt}
                onChange={setAskPrompt}
                placeholder="e.g. stop flagging our shared test account jane.doe@acme.com in email findings"
              />
              <div className="flex items-center justify-between gap-3">
                <Text className="text-muted-foreground" small>
                  {"Describe what to stop flagging. We'll write the criteria expression, you tweak before saving." +
                    (findingIds.length > 0
                      ? ` Your ${findingIds.length} selected findings go along as context.`
                      : "")}
                </Text>
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
              {error && (
                <Text className="text-destructive text-sm">{error}</Text>
              )}
              <ExclusionExamples />
            </div>
          </>
        )}

        {editing && (
          <div className="flex items-center justify-between">
            <Label>Status</Label>
            <Switch checked={enabled} onCheckedChange={setEnabled} />
          </div>
        )}
      </div>

      <SheetFooter className="px-6 pb-6">
        {/* An exact rule stays unsavable until the reveal resolves — there is
            no client-side stand-in for the plaintext to write it against. */}
        <Button
          onClick={handleSave}
          disabled={submitting || (choice === "exact" && !picked?.fields)}
        >
          {(submitting || reveals.isLoading) && (
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
    <div className="bg-muted/40 text-muted-foreground space-y-1 p-3 text-xs">
      <Text className="font-medium" small>
        Examples
      </Text>
      <ul className="space-y-1">
        {examples.map(([code, desc]) => (
          <li key={code}>
            <code className="font-mono">{code}</code> — {desc}
          </li>
        ))}
      </ul>
      <Text className="text-muted-foreground" small>
        Combine with <code className="font-mono">&amp;&amp;</code> to scope by
        rule or source.
      </Text>
    </div>
  );
}
