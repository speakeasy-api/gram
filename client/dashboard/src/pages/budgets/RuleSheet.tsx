import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
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
import { TextArea } from "@/components/ui/textarea";
import { Type } from "@/components/ui/type";
import { useSpendRulesPreviewRuleMutation } from "@gram/client/react-query/index.js";
import { Check, Loader2, Trash2, Users } from "lucide-react";
import { useEffect, useMemo, useState, type JSX, type ReactNode } from "react";
import { ACTOR_ATTRIBUTES, type ActorAttribute } from "./budget-cel";
import {
  WINDOW_LABELS,
  defaultRuleDraft,
  formatUsd,
  toDraft,
  type BudgetWindow,
  type PreviewSpendRuleResult,
  type RuleAction,
  type RuleDraft,
  type RuleTargetCondition,
  type RuleTargetOperator,
  type SpendRule,
} from "./budgets-data";

const WINDOWS: BudgetWindow[] = ["daily", "weekly", "monthly"];

const ACTION_OPTIONS: {
  value: RuleAction;
  title: string;
  hint: string;
}[] = [
  {
    value: "flag",
    title: "Flag",
    hint: "Keep requests flowing and record budget events for admins to review.",
  },
  {
    value: "block",
    title: "Block",
    hint: "Reject further requests from people over their budget until the window resets.",
  },
];

const WINDOW_RESET_HINTS: Record<BudgetWindow, string> = {
  daily: "Fixed window — resets at midnight UTC.",
  weekly: "Fixed window — resets every Monday (UTC).",
  monthly: "Fixed window — resets on the 1st of each month (UTC).",
};

const STRING_OPERATORS: RuleTargetOperator[] = [
  "equals",
  "not_equals",
  "starts_with",
  "ends_with",
  "contains",
  "matches",
];
const LIST_OPERATORS: RuleTargetOperator[] = ["includes"];
const OPERATOR_LABELS: Record<RuleTargetOperator, string> = {
  equals: "is",
  not_equals: "is not",
  starts_with: "starts with",
  ends_with: "ends with",
  contains: "contains",
  matches: "matches pattern",
  includes: "includes",
};

function actorAttribute(name: string): ActorAttribute {
  return (
    ACTOR_ATTRIBUTES.find((attr) => attr.name === name) ?? ACTOR_ATTRIBUTES[0]!
  );
}

function operatorsForAttribute(attr: ActorAttribute): RuleTargetOperator[] {
  return attr.type === "list" ? LIST_OPERATORS : STRING_OPERATORS;
}

function attributeLabel(name: string): string {
  return name
    .split("_")
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

/** Create or edit a spend-control rule. */
export function RuleSheet({
  open,
  onOpenChange,
  rule,
  onSubmit,
  onDelete,
  submitting = false,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Editing an existing rule, or undefined when creating. */
  rule?: SpendRule;
  onSubmit: (draft: RuleDraft) => void;
  onDelete?: () => void;
  submitting?: boolean;
}): JSX.Element {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="flex flex-col overflow-y-auto sm:max-w-xl">
        {/* key forces a fresh form when switching between create/edit targets */}
        <RuleForm
          key={rule?.id ?? "new"}
          rule={rule}
          onSubmit={onSubmit}
          onDelete={onDelete}
          submitting={submitting}
        />
      </SheetContent>
    </Sheet>
  );
}

/** Debounced server-side preview: matched members plus their
 *  current-window spend against the proposed per-person limit. */
function useRulePreview(
  draft: Pick<RuleDraft, "target" | "limitUsd" | "warnAtPct" | "windowKind">,
  evaluatedFrom: Date | undefined,
): { preview: PreviewSpendRuleResult | null; loading: boolean } {
  const previewMutation = useSpendRulesPreviewRuleMutation();
  const [preview, setPreview] = useState<PreviewSpendRuleResult | null>(null);
  const { mutate } = previewMutation;

  useEffect(() => {
    if (draft.limitUsd <= 0 || draft.target.value.trim() === "") return;
    const timer = setTimeout(() => {
      mutate(
        {
          request: {
            previewSpendRuleRequestBody: {
              target: draft.target,
              limitUsd: draft.limitUsd,
              warnAtPct: draft.warnAtPct,
              windowKind: draft.windowKind,
              evaluatedFrom,
            },
          },
        },
        {
          onSuccess: (data) => setPreview(data),
        },
      );
    }, 350);
    return () => clearTimeout(timer);
  }, [
    draft.target,
    draft.limitUsd,
    draft.warnAtPct,
    draft.windowKind,
    evaluatedFrom,
    mutate,
  ]);

  return { preview, loading: previewMutation.isPending };
}

function RuleForm({
  rule,
  onSubmit,
  onDelete,
  submitting,
}: {
  rule?: SpendRule;
  onSubmit: (draft: RuleDraft) => void;
  onDelete?: () => void;
  submitting: boolean;
}): JSX.Element {
  const [draft, setDraft] = useState<RuleDraft>(
    rule ? toDraft(rule) : defaultRuleDraft(),
  );

  const patch = (p: Partial<RuleDraft>) => setDraft((d) => ({ ...d, ...p }));

  // Pass the rule's evaluated_from through when editing so the preview shows
  // the same numbers the evaluator sees. Material edits reset it server-side.
  const { preview, loading: previewLoading } = useRulePreview(
    draft,
    rule?.evaluatedFrom,
  );

  const canSubmit =
    draft.name.trim() !== "" &&
    draft.target.value.trim() !== "" &&
    draft.limitUsd > 0 &&
    !submitting;

  const overLimitCount = useMemo(() => {
    if (!preview) return 0;
    return preview.actors.filter((a) => a.breached).length;
  }, [preview]);

  return (
    <>
      <SheetHeader className="px-6 pt-6">
        <SheetTitle>{rule ? "Edit rule" : "New spend rule"}</SheetTitle>
        <SheetDescription>
          Give each matched person a fixed-window budget and choose what happens
          when it is spent.
        </SheetDescription>
      </SheetHeader>

      <div className="flex-1 space-y-6 px-6 py-4">
        <Field label="Name">
          <Input
            value={draft.name}
            onChange={(name) => patch({ name })}
            placeholder="e.g. Engineering frontier cap"
          />
        </Field>

        <Field label="Description">
          <TextArea
            value={draft.description}
            onChange={(description) => patch({ description })}
            rows={2}
            placeholder="What this budget is for and who it covers"
          />
        </Field>

        {/* Applies to (actor targeting) */}
        <div className="space-y-2">
          <Label className="text-sm font-medium">Applies to</Label>
          <p className="text-muted-foreground text-xs">
            Pick one member attribute to define who this budget covers. Need to
            combine attributes? Create a second rule — the strictest matching
            rule wins.
          </p>
          <TargetConditionField
            value={draft.target}
            onChange={(target) => patch({ target })}
          />
          <MatchedActors
            preview={draft.target.value.trim() === "" ? null : preview}
            loading={previewLoading}
          />
        </div>

        {/* Limit + window + warn threshold */}
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3">
          <Field label="Budget per person">
            <div className="flex items-center">
              <span className="border-input bg-muted text-muted-foreground inline-flex h-9 items-center rounded-l-md border border-r-0 px-3 text-sm">
                $
              </span>
              <input
                type="number"
                min={1}
                value={draft.limitUsd}
                onChange={(e) =>
                  patch({ limitUsd: Math.max(0, Number(e.target.value) || 0) })
                }
                className="border-input dark:bg-input/30 h-9 w-full min-w-0 rounded-r-md border bg-transparent px-3 py-1 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px]"
              />
            </div>
          </Field>

          <Field label="Window">
            <Select
              value={draft.windowKind}
              onValueChange={(v) => patch({ windowKind: v as BudgetWindow })}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {WINDOWS.map((w) => (
                  <SelectItem key={w} value={w}>
                    {WINDOW_LABELS[w]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>

          <Field label="Warn at">
            <div className="flex items-center">
              <input
                type="number"
                min={1}
                max={99}
                value={draft.warnAtPct}
                onChange={(e) =>
                  patch({
                    warnAtPct: Math.min(
                      99,
                      Math.max(1, Number(e.target.value) || 0),
                    ),
                  })
                }
                className="border-input dark:bg-input/30 h-9 w-full min-w-0 rounded-l-md border bg-transparent px-3 py-1 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px]"
              />
              <span className="border-input bg-muted text-muted-foreground inline-flex h-9 items-center rounded-r-md border border-l-0 px-3 text-sm">
                %
              </span>
            </div>
          </Field>
        </div>
        <p className="text-muted-foreground -mt-4 text-xs">
          {WINDOW_RESET_HINTS[draft.windowKind]} Each matched person gets{" "}
          {formatUsd(draft.limitUsd)} per window; a warning event fires at{" "}
          {draft.warnAtPct}% of it.
        </p>

        {/* On breach */}
        <div className="space-y-2">
          <Label className="text-sm font-medium">
            When a person's budget is spent
          </Label>
          <RadioGroup
            value={draft.action}
            onValueChange={(v) => patch({ action: v as RuleAction })}
            className="gap-2"
          >
            {ACTION_OPTIONS.map((option) => (
              <label
                key={option.value}
                htmlFor={`action-${option.value}`}
                className="hover:bg-muted/40 flex cursor-pointer items-start gap-3 rounded-md border px-3 py-2.5"
              >
                <RadioGroupItem
                  id={`action-${option.value}`}
                  value={option.value}
                  className="mt-0.5"
                />
                <div className="min-w-0">
                  <div className="text-sm">{option.title}</div>
                  <div className="text-muted-foreground text-xs">
                    {option.hint}
                  </div>
                </div>
              </label>
            ))}
          </RadioGroup>
        </div>

        {/* Usage preview */}
        <div className="bg-muted/30 space-y-2 rounded-lg border p-4">
          <div className="flex items-center justify-between">
            <Type variant="small" className="font-medium">
              Current usage this {draft.windowKind} window
            </Type>
            {previewLoading && (
              <Loader2 className="text-muted-foreground size-3.5 animate-spin" />
            )}
          </div>
          {!preview ? (
            <p className="text-muted-foreground text-xs">
              Choose a target condition to preview usage.
            </p>
          ) : (
            <>
              <p className="text-muted-foreground text-xs">
                {preview.matchedCount} matched{" "}
                {preview.matchedCount === 1 ? "person" : "people"}, each with a{" "}
                {formatUsd(draft.limitUsd)} budget.
              </p>
              {overLimitCount > 0 && (
                <p className="text-destructive text-xs">
                  {overLimitCount}{" "}
                  {overLimitCount === 1 ? "person is" : "people are"} already
                  over this limit in the current window.
                  {draft.action === "block" &&
                    " This rule would block their requests."}
                </p>
              )}
            </>
          )}
        </div>
      </div>

      <SheetFooter className="border-border flex-row items-center justify-between border-t px-6 py-4">
        {onDelete ? (
          <Button
            variant="ghost"
            size="sm"
            onClick={onDelete}
            disabled={submitting}
            className="text-destructive hover:text-destructive"
          >
            <Trash2 className="mr-2 h-4 w-4" />
            Delete
          </Button>
        ) : (
          <span />
        )}
        <Button disabled={!canSubmit} onClick={() => onSubmit(draft)}>
          <Check className="mr-2 h-4 w-4" />
          {rule ? "Save changes" : "Create rule"}
        </Button>
      </SheetFooter>
    </>
  );
}

function MatchedActors({
  preview,
  loading,
}: {
  preview: PreviewSpendRuleResult | null;
  loading: boolean;
}): JSX.Element {
  if (!preview) {
    return (
      <p className="text-muted-foreground text-xs">
        {loading
          ? "Matching members…"
          : "Matched people will appear here once the condition is valid."}
      </p>
    );
  }
  return (
    <div className="border-border rounded-lg border">
      <div className="border-border bg-muted/40 flex items-center gap-2 border-b px-3 py-2 text-xs font-medium">
        <Users className="size-3.5" />
        {preview.matchedCount} matched{" "}
        {preview.matchedCount === 1 ? "person" : "people"}
      </div>
      {preview.actors.length === 0 ? (
        <p className="text-muted-foreground px-3 py-3 text-xs">
          No members match this condition.
        </p>
      ) : (
        <ul className="divide-border max-h-40 divide-y overflow-y-auto">
          {preview.actors.map((actor) => (
            <li
              key={actor.email}
              className="flex items-center justify-between gap-3 px-3 py-2 text-xs"
            >
              <div className="min-w-0">
                <div className="truncate">
                  {actor.displayName || actor.email}
                </div>
                {actor.displayName && (
                  <div className="text-muted-foreground truncate">
                    {actor.email}
                  </div>
                )}
              </div>
              <span className="text-muted-foreground shrink-0 font-mono">
                {formatUsd(actor.spendUsd)} this window
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

/** Single attribute/operator/value picker backing the rule's target
 *  condition. v1 deliberately allows exactly one condition per rule. */
function TargetConditionField({
  value,
  onChange,
}: {
  value: RuleTargetCondition;
  onChange: (value: RuleTargetCondition) => void;
}): JSX.Element {
  const [condition, setCondition] = useState<RuleTargetCondition>(value);

  const update = (next: RuleTargetCondition) => {
    setCondition(next);
    onChange(next);
  };

  const attribute = actorAttribute(condition.attribute);
  const operators = operatorsForAttribute(attribute);

  return (
    <div className="space-y-2 rounded-md border p-3">
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-[1fr_150px_1fr]">
        <Select
          value={condition.attribute}
          onValueChange={(attributeName) => {
            const nextAttribute = actorAttribute(attributeName);
            update({
              attribute: attributeName,
              operator: operatorsForAttribute(nextAttribute)[0]!,
              value: nextAttribute.samples[0] ?? "",
            });
          }}
        >
          <SelectTrigger className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {ACTOR_ATTRIBUTES.map((attr) => (
              <SelectItem key={attr.name} value={attr.name}>
                {attributeLabel(attr.name)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select
          value={condition.operator}
          onValueChange={(operator) =>
            update({ ...condition, operator: operator as RuleTargetOperator })
          }
        >
          <SelectTrigger className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {operators.map((operator) => (
              <SelectItem key={operator} value={operator}>
                {OPERATOR_LABELS[operator]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <ConditionValueInput
          attribute={attribute}
          value={condition.value}
          onChange={(nextValue) => update({ ...condition, value: nextValue })}
        />
      </div>
      <p className="text-muted-foreground text-xs">{attribute.description}</p>
    </div>
  );
}

/** Free-text value with sample suggestions. Real directories carry values the
 *  samples can't enumerate, so the field accepts anything. */
function ConditionValueInput({
  attribute,
  value,
  onChange,
}: {
  attribute: ActorAttribute;
  value: string;
  onChange: (value: string) => void;
}): JSX.Element {
  const listId = `budget-samples-${attribute.name}`;
  return (
    <>
      <Input
        value={value}
        onChange={onChange}
        placeholder={attribute.samples[0] ?? "Value"}
        list={listId}
      />
      <datalist id={listId}>
        {attribute.samples.map((sample) => (
          <option key={sample} value={sample} />
        ))}
      </datalist>
    </>
  );
}

function Field({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="space-y-2">
      <Label className="text-sm font-medium">{label}</Label>
      {children}
    </div>
  );
}
