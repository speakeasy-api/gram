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
import { useRiskCreateExclusionMutation } from "@gram/client/react-query/riskCreateExclusion.js";
import { useRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
import { useRiskSuggestExclusionMutation } from "@gram/client/react-query/riskSuggestExclusion.js";
import { useRiskUpdateExclusionMutation } from "@gram/client/react-query/riskUpdateExclusion.js";
import type { RiskExclusion } from "@gram/client/models/components/riskexclusion.js";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import type { RiskSignal } from "@gram/client/models/components/risksignal.js";
import { RadioGroup, RadioGroupItem } from "@/components/ui/RadioGroup";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2, Sparkles } from "lucide-react";
import type { JSX, ReactNode } from "react";
import { useState } from "react";
import { cn } from "@/lib/utils";
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
import { EventMatchDialog, MaskedMatch } from "./risk-ui";
import {
  getCategoryCodeForFinding,
  getRuleTitleFallback,
  isJudgeSource,
} from "./risk-utils";
import { useRBAC } from "@/hooks/useRBAC";
import { invalidateExclusionSurfaces } from "./exclusion-invalidation";
import { useCelEngine } from "./use-cel-engine";

const GLOBAL_SCOPE = "__global__";

export type ExclusionSheetState =
  | {
      mode: "create";
      /** Findings the create was started from. Drives the ready-made rule
       * picker; absent for the Exclusions tab / Policy Center buttons, which
       * have no finding and open straight onto the criteria box. */
      results?: RiskResult[];
      /** The Watchdog signal the create was started from. Signals cluster on
       * one rule, so this pre-fills the "Any <rule> finding" option — and
       * makes it the default — even before any evidence rows have loaded. */
      signal?: RiskSignal;
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
  secondaryAction,
  embedded,
}: {
  state: ExclusionSheetState;
  onDone: () => void;
  /** Optional extra control rendered beside the save button — see
   * {@link ExclusionFormProps.secondaryAction}. */
  secondaryAction?: ReactNode;
  /** See {@link ExclusionFormProps.embedded}. */
  embedded?: boolean;
}): JSX.Element {
  const queryClient = useQueryClient();
  const { data: policyData } = useRiskListPolicies();
  // Exclusions aren't supported for prompt-based (LLM-judge) policies yet
  // (AGE-2750), so keep them out of the scope dropdown.
  const policies = (policyData?.policies ?? []).filter(
    (p) => p.policyType !== "prompt_based",
  );

  const invalidate = () => invalidateExclusionSurfaces(queryClient);

  // Surface the API's message (e.g. "invalid regex pattern: …") rather than a
  // generic failure: server-side validation is the backstop for anything the
  // form couldn't check locally, so its reason must reach the operator.
  const createMutation = useRiskCreateExclusionMutation({
    onSuccess: () => {
      void invalidate();
      toast.success(
        "Exclusion created. Matching findings will update shortly.",
      );
      onDone();
    },
    onError: (err) =>
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : "Failed to create exclusion.",
      ),
  });
  const updateMutation = useRiskUpdateExclusionMutation({
    onSuccess: () => {
      void invalidate();
      toast.success("Exclusion updated. Findings will update shortly.");
      onDone();
    },
    onError: (err) =>
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : "Failed to update exclusion.",
      ),
  });

  const editing = state.mode === "edit" ? state.exclusion : null;
  const submitting = createMutation.isPending || updateMutation.isPending;

  const formKey =
    state.mode === "edit"
      ? `edit-${state.exclusion.id}`
      : // The signal key participates so a signal-originated create remounts
        // when the drawer switches signals even while both have no evidence
        // rows loaded yet.
        `create-${state.signal?.key ?? ""}-${(state.results ?? []).map((r) => r.id).join(",")}`;

  return (
    <ExclusionForm
      key={formKey}
      policies={policies}
      state={state}
      submitting={submitting}
      secondaryAction={secondaryAction}
      embedded={embedded}
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
  /** Optional extra control rendered beside the save button (e.g. the
   * Watchdog drawer's Back button when the editor is embedded in place). */
  secondaryAction?: ReactNode;
  /** Embedded hosts (the Watchdog drawer) control their own horizontal
   * inset, so the form drops the standalone sheet's px-6 padding. */
  embedded?: boolean;
}

function ExclusionForm({
  policies,
  state,
  submitting,
  onSubmit,
  secondaryAction,
  embedded = false,
}: ExclusionFormProps) {
  const editing = state.mode === "edit" ? state.exclusion : null;
  const results = state.mode === "create" ? (state.results ?? []) : [];
  const signal = state.mode === "create" ? state.signal : undefined;
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
  // A signal supplies its rule directly, so the rule option is on offer even
  // before evidence rows load.
  const options = exclusionOptions(results, exact, signal?.ruleId);
  const [choice, setChoice] = useState<ExclusionOption["value"]>(() => {
    // A signal-originated create defaults to the rule option: the flow began
    // as "stop flagging this signal", which is the whole rule cluster, not
    // any one matched value.
    if (signal && options.some((o) => o.value === "rule")) return "rule";
    // A pending exact option is not savable yet, so never open on it —
    // selecting it is the gesture that fires the reveal.
    return (
      options.find((o) => o.value !== "exact" || o.fields)?.value ?? "custom"
    );
  });
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

  // Regex patterns are RE2 (compiled by Go on the server and in the
  // analyzers), so save-time validation must use the wasm engine's RE2
  // compiler — JS RegExp is a different dialect and rejects valid RE2 like
  // "(?i)". Loaded only once the criteria box is visible (the wasm asset is
  // large); if it isn't ready by save time the API validates instead.
  const engineState = useCelEngine(choice === "custom");

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
    if (parsed.value.matchType === "regex" && engineState.status === "ready") {
      const compiled = engineState.engine.compileRegex(parsed.value.matchValue);
      if (!compiled.ok) {
        setError(`Invalid regex pattern: ${compiled.error}`);
        return;
      }
    }
    setError(null);
    onSubmit({ fields: parsed.value, scope, enabled });
  };

  return (
    <>
      <div
        className={cn(
          "flex-1 space-y-5 overflow-y-auto py-2",
          !embedded && "px-6",
        )}
      >
        {options.length > 1 && (
          <div className="space-y-2">
            <Label>What should we stop flagging?</Label>
            <RadioGroup
              value={choice}
              onValueChange={(v) => {
                const next = v as ExclusionOption["value"];
                // Moving to the DSL box keeps the ready-made rule the user was
                // on as a starting point instead of an empty textarea — but
                // never clobbers criteria they already typed.
                if (
                  next === "custom" &&
                  expression.trim() === "" &&
                  picked?.fields
                ) {
                  setExpression(serializeExclusionExpression(picked.fields));
                }
                setChoice(next);
                if (next === "exact" && !single?.match) reveals.reveal();
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
            {results.length > 0 && (
              <SelectedFindingsContext results={results} />
            )}

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

      <SheetFooter
        className={cn(
          "pb-6",
          !embedded && "px-6",
          embedded && "px-0",
          secondaryAction && "flex-row items-center justify-between",
        )}
      >
        {secondaryAction}
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

// How many originating findings the custom branch lists before collapsing the
// rest into a "+N more" line — the block is writing context, not a findings
// table.
const FINDINGS_CONTEXT_CAP = 5;

// Customer-safe title for a context row: the detection rule's name, falling
// back to the category code when the finding carries no rule id. Never the raw
// scanner source.
function findingContextTitle(result: RiskResult): string {
  if (result.ruleId) return getRuleTitleFallback(result.ruleId);
  return getCategoryCodeForFinding(result.source, result.ruleId);
}

// The findings the create was started from, kept visible in the "Write it
// myself" branch so the criteria can be written against the real false
// positives. Values stay behind the audited reveal flow (MaskedMatch, or the
// event dialog for judge findings) — rendering this block never exposes a raw
// match by itself.
function SelectedFindingsContext({
  results,
}: {
  results: RiskResult[];
}): JSX.Element {
  const shown = results.slice(0, FINDINGS_CONTEXT_CAP);
  const hiddenCount = results.length - shown.length;
  return (
    <div className="space-y-2">
      <Label>Selected findings</Label>
      <div className="border">
        {shown.map((result) => (
          <SelectedFindingRow key={result.id} result={result} />
        ))}
        {hiddenCount > 0 && (
          <div className="border-t px-3 py-2">
            <Text className="text-muted-foreground" small>
              +{hiddenCount} more in this selection
            </Text>
          </div>
        )}
      </div>
    </div>
  );
}

function SelectedFindingRow({ result }: { result: RiskResult }): JSX.Element {
  return (
    <div className="space-y-1 border-t px-3 py-2 first:border-t-0">
      <div className="flex items-baseline justify-between gap-3">
        <Text className="min-w-0 truncate" small>
          {findingContextTitle(result)}
        </Text>
        {result.confidence !== undefined && (
          <Text className="text-muted-foreground shrink-0 font-mono" small>
            conf {result.confidence.toFixed(2)}
          </Text>
        )}
      </div>
      <div className="min-w-0">
        <SelectedFindingMatch result={result} />
      </div>
    </div>
  );
}

// The evidence cell for a context row, mirroring the findings tables: judge
// findings show their rationale and open the flagged event in a dialog, other
// findings go through the audited click-to-reveal. Chat surfaces carry the
// plaintext on the finding itself, so there is nothing left to reveal there.
function SelectedFindingMatch({ result }: { result: RiskResult }): JSX.Element {
  if (isJudgeSource(result.source)) {
    return (
      <EventMatchDialog
        resultId={result.id}
        matchRedacted={result.matchRedacted}
        rationale={result.description}
      />
    );
  }
  if (result.match) {
    return (
      <span
        className="block min-w-0 overflow-x-auto font-mono text-xs whitespace-nowrap"
        title={result.match}
      >
        {result.match}
      </span>
    );
  }
  return (
    <MaskedMatch resultId={result.id} matchRedacted={result.matchRedacted} />
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
