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
import { Check, Trash2, Users } from "lucide-react";
import { useMemo, useState, type JSX, type ReactNode } from "react";
import {
  ACTOR_ATTRIBUTES,
  validateBudgetCel,
  type ActorAttribute,
} from "./budget-cel";
import { UsageBar } from "./budget-shared";
import {
  WINDOW_LABELS,
  defaultRuleDraft,
  estimateRuleUsage,
  formatUsd,
  type BudgetWindow,
  type MockActor,
  type RuleAction,
  type RuleDraft,
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
    hint: "Reject further requests from matched people until the window resets.",
  },
];

const WINDOW_RESET_HINTS: Record<BudgetWindow, string> = {
  daily: "Fixed window — resets at midnight.",
  weekly: "Fixed window — resets every Monday.",
  monthly: "Fixed window — resets on the 1st of each month.",
};

type ConditionOperator =
  | "equals"
  | "not_equals"
  | "starts_with"
  | "ends_with"
  | "contains"
  | "matches"
  | "includes";

interface TargetCondition {
  attribute: string;
  operator: ConditionOperator;
  value: string;
}

const STRING_OPERATORS: ConditionOperator[] = [
  "equals",
  "not_equals",
  "starts_with",
  "ends_with",
  "contains",
  "matches",
];
const LIST_OPERATORS: ConditionOperator[] = ["includes"];
const OPERATOR_LABELS: Record<ConditionOperator, string> = {
  equals: "is",
  not_equals: "is not",
  starts_with: "starts with",
  ends_with: "ends with",
  contains: "contains",
  matches: "matches pattern",
  includes: "includes",
};

function toDraft(rule: SpendRule): RuleDraft {
  const {
    id: _id,
    createdAt: _createdAt,
    version: _version,
    evaluatedFrom: _evaluatedFrom,
    ...draft
  } = rule;
  return draft;
}

function makeDefaultCondition(): TargetCondition {
  const attr = ACTOR_ATTRIBUTES[0]!;
  return {
    attribute: attr.name,
    operator: operatorsForAttribute(attr)[0]!,
    value: attr.samples[0] ?? "",
  };
}

function actorAttribute(name: string): ActorAttribute {
  return (
    ACTOR_ATTRIBUTES.find((attr) => attr.name === name) ?? ACTOR_ATTRIBUTES[0]!
  );
}

function operatorsForAttribute(attr: ActorAttribute): ConditionOperator[] {
  return attr.type === "list" ? LIST_OPERATORS : STRING_OPERATORS;
}

function attributeLabel(name: string): string {
  return name
    .split("_")
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

/** Parse a stored expression into a single builder condition (v1: rules carry
 *  exactly one condition; legacy AND expressions collapse to their first). */
function parseSingleCondition(expr: string): TargetCondition {
  const first = expr.split(/\s+&&\s+/)[0]?.trim() ?? "";
  return parseTargetCondition(first) ?? makeDefaultCondition();
}

function parseTargetCondition(part: string): TargetCondition | null {
  const comparison = /^([A-Za-z_]\w*)\s*(==|!=)\s*"((?:\\.|[^"])*)"$/.exec(
    part,
  );
  if (comparison) {
    return makeConditionFromParts(
      comparison[1]!,
      comparison[2] === "==" ? "equals" : "not_equals",
      unescapeCelString(comparison[3]!),
    );
  }

  const call =
    /^([A-Za-z_]\w*)\.(startsWith|endsWith|contains|matches)\("((?:\\.|[^"])*)"\)$/.exec(
      part,
    );
  if (call) {
    return makeConditionFromParts(
      call[1]!,
      operatorFromMethod(call[2]!),
      unescapeCelString(call[3]!),
    );
  }

  const listMembership = /^"((?:\\.|[^"])*)"\s+in\s+([A-Za-z_]\w*)$/.exec(part);
  if (listMembership) {
    return makeConditionFromParts(
      listMembership[2]!,
      "includes",
      unescapeCelString(listMembership[1]!),
    );
  }

  return null;
}

function makeConditionFromParts(
  attributeName: string,
  operator: ConditionOperator,
  value: string,
): TargetCondition | null {
  const attr = actorAttribute(attributeName);
  const operators = operatorsForAttribute(attr);
  if (attr.name !== attributeName || !operators.includes(operator)) return null;
  return {
    attribute: attr.name,
    operator,
    value: attr.samples.includes(value) ? value : (attr.samples[0] ?? value),
  };
}

function operatorFromMethod(method: string): ConditionOperator {
  switch (method) {
    case "startsWith":
      return "starts_with";
    case "endsWith":
      return "ends_with";
    case "matches":
      return "matches";
    default:
      return "contains";
  }
}

function targetConditionToExpr(condition: TargetCondition): string {
  const value = `"${escapeCelString(condition.value)}"`;
  switch (condition.operator) {
    case "equals":
      return `${condition.attribute} == ${value}`;
    case "not_equals":
      return `${condition.attribute} != ${value}`;
    case "starts_with":
      return `${condition.attribute}.startsWith(${value})`;
    case "ends_with":
      return `${condition.attribute}.endsWith(${value})`;
    case "contains":
      return `${condition.attribute}.contains(${value})`;
    case "matches":
      return `${condition.attribute}.matches(${value})`;
    case "includes":
      return `${value} in ${condition.attribute}`;
  }
}

function escapeCelString(value: string): string {
  return value.replace(/\\/g, "\\\\").replace(/"/g, '\\"');
}

function unescapeCelString(value: string): string {
  return value.replace(/\\"/g, '"').replace(/\\\\/g, "\\");
}

/** Create or edit a spend-control rule. Purely local (prototype). */
export function RuleSheet({
  open,
  onOpenChange,
  rule,
  onSubmit,
  onDelete,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Editing an existing rule, or undefined when creating. */
  rule?: SpendRule;
  onSubmit: (draft: RuleDraft) => void;
  onDelete?: () => void;
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
        />
      </SheetContent>
    </Sheet>
  );
}

function RuleForm({
  rule,
  onSubmit,
  onDelete,
}: {
  rule?: SpendRule;
  onSubmit: (draft: RuleDraft) => void;
  onDelete?: () => void;
}): JSX.Element {
  const [draft, setDraft] = useState<RuleDraft>(
    rule ? toDraft(rule) : defaultRuleDraft(),
  );

  const patch = (p: Partial<RuleDraft>) => setDraft((d) => ({ ...d, ...p }));

  const usage = useMemo(
    () =>
      // Pass the identity through when editing so the preview shows the same
      // (seeded) current spend as the rules table. Saving a material change
      // bumps the version and restarts evaluation from scratch.
      estimateRuleUsage({
        targetExpr: draft.targetExpr,
        limitUsd: draft.limitUsd,
        window: draft.window,
        ...(rule
          ? {
              id: rule.id,
              version: rule.version,
              evaluatedFrom: rule.evaluatedFrom,
            }
          : {}),
      }),
    [draft.targetExpr, draft.limitUsd, draft.window, rule],
  );

  const targetError = validateBudgetCel(draft.targetExpr);
  const canSubmit =
    draft.name.trim() !== "" && !targetError && draft.limitUsd > 0;

  return (
    <>
      <SheetHeader className="px-6 pt-6">
        <SheetTitle>{rule ? "Edit rule" : "New spend rule"}</SheetTitle>
        <SheetDescription>
          Give a group of people a fixed-window budget and choose what happens
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
            Pick one directory-synced attribute to define who this budget
            covers. Need to combine attributes? Create a second rule — the
            strictest matching rule wins.
          </p>
          <TargetConditionField
            value={draft.targetExpr}
            onChange={(targetExpr) => patch({ targetExpr })}
          />
          <MatchedActors matched={usage.matched} />
        </div>

        {/* Limit + window + warn threshold */}
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3">
          <Field label="Budget limit">
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
              value={draft.window}
              onValueChange={(v) => patch({ window: v as BudgetWindow })}
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
          {WINDOW_RESET_HINTS[draft.window]} Turns Approaching and records a
          warning event at {draft.warnAtPct}% of the budget.
        </p>

        {/* On breach */}
        <div className="space-y-2">
          <Label className="text-sm font-medium">
            When the budget is spent
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
              Estimated usage this {draft.window} window
            </Type>
            <span className="text-muted-foreground text-xs">
              Projected {formatUsd(usage.projectedSpendUsd)}
            </span>
          </div>
          {usage.matched === null ? (
            <p className="text-muted-foreground text-xs">
              Enter a valid target expression to preview usage.
            </p>
          ) : (
            <>
              <UsageBar
                usage={usage}
                limitUsd={draft.limitUsd}
                warnAtPct={draft.warnAtPct}
              />
              <ProjectedBreachText
                overLimit={usage.projectedOverLimit}
                action={draft.action}
              />
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
  matched,
}: {
  matched: MockActor[] | null;
}): JSX.Element {
  if (matched === null) {
    return (
      <p className="text-muted-foreground text-xs">
        Matched actors will appear here once the expression is valid.
      </p>
    );
  }
  return (
    <div className="border-border rounded-lg border">
      <div className="border-border bg-muted/40 flex items-center gap-2 border-b px-3 py-2 text-xs font-medium">
        <Users className="size-3.5" />
        {matched.length} matched actor{matched.length === 1 ? "" : "s"}
      </div>
      {matched.length === 0 ? (
        <p className="text-muted-foreground px-3 py-3 text-xs">
          No actors in the mock directory match this expression.
        </p>
      ) : (
        <ul className="divide-border max-h-40 divide-y overflow-y-auto">
          {matched.map((actor) => (
            <li
              key={actor.id}
              className="flex items-center justify-between gap-3 px-3 py-2 text-xs"
            >
              <div className="min-w-0">
                <div className="truncate">{actor.name}</div>
                <div className="text-muted-foreground truncate">
                  {actor.department_name} · {actor.job_title}
                </div>
              </div>
              <span className="text-muted-foreground shrink-0 font-mono">
                {formatUsd(actor.monthlySpendUsd)}/mo
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

/** Single attribute/operator/value picker backing the rule's target
 *  expression. v1 deliberately allows exactly one condition per rule. */
function TargetConditionField({
  value,
  onChange,
}: {
  value: string;
  onChange: (value: string) => void;
}): JSX.Element {
  const [condition, setCondition] = useState<TargetCondition>(() =>
    parseSingleCondition(value),
  );

  const update = (next: TargetCondition) => {
    setCondition(next);
    onChange(targetConditionToExpr(next));
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
            update({ ...condition, operator: operator as ConditionOperator })
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
        <Select
          value={condition.value}
          onValueChange={(nextValue) =>
            update({ ...condition, value: nextValue })
          }
        >
          <SelectTrigger className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {attribute.samples.map((sample) => (
              <SelectItem key={sample} value={sample}>
                {sample}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <p className="text-muted-foreground text-xs">{attribute.description}</p>
    </div>
  );
}

function ProjectedBreachText({
  overLimit,
  action,
}: {
  overLimit: boolean;
  action: RuleAction;
}): JSX.Element | null {
  if (!overLimit) return null;

  const outcome =
    action === "block"
      ? "block requests from matched people"
      : "flag overspend for review";
  return (
    <p className="text-destructive text-xs">
      Projected to exceed the limit this window. This rule would {outcome}.
    </p>
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
