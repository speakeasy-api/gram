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
import { cn } from "@/lib/utils";
import { Check, Trash2, Users } from "lucide-react";
import { useMemo, useState, type JSX, type ReactNode } from "react";
import {
  ACTOR_ATTRIBUTES,
  validateBudgetCel,
  type ActorAttribute,
} from "./budget-cel";
import { UsageBar } from "./budget-shared";
import {
  BREACH_ACTION_LABELS,
  MODELS,
  PROVIDERS,
  WINDOW_LABELS,
  defaultRuleDraft,
  estimateRuleUsage,
  formatUsd,
  type BreachAction,
  type BudgetWindow,
  type MockActor,
  type RuleDraft,
  type SpendRule,
  type WindowReset,
} from "./budgets-data";

const WINDOWS: BudgetWindow[] = ["daily", "weekly", "monthly"];
const BREACH_ACTIONS: BreachAction[] = [
  "block",
  "route_fallback",
  "alert_only",
];
const BREACH_ACTION_HINTS: Record<BreachAction, string> = {
  block: "Reject further requests from matched actors once the limit is hit.",
  route_fallback:
    "Keep actors productive by downgrading them to a cheaper fallback model.",
  alert_only:
    "Never block — just record the overage and (later) notify admins.",
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
  id: string;
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

let nextConditionId = 1;

function toDraft(rule: SpendRule): RuleDraft {
  const { id: _id, createdAt: _createdAt, ...draft } = rule;
  return draft;
}

function makeConditionId(): string {
  return `cond_${nextConditionId++}`;
}

function makeDefaultCondition(): TargetCondition {
  const attr = ACTOR_ATTRIBUTES[0]!;
  return {
    id: makeConditionId(),
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

function parseTargetConditions(expr: string): TargetCondition[] {
  const parts = expr
    .split(/\s+&&\s+/)
    .map((part) => part.trim())
    .filter(Boolean);
  const parsed = parts
    .map(parseTargetCondition)
    .filter((condition) => condition !== null);
  return parsed.length > 0 ? parsed : [makeDefaultCondition()];
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
    id: makeConditionId(),
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

function targetConditionsToExpr(conditions: TargetCondition[]): string {
  return conditions.map(targetConditionToExpr).join(" && ");
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
      estimateRuleUsage({
        targetExpr: draft.targetExpr,
        limitUsd: draft.limitUsd,
        window: draft.window,
        models: draft.models,
        providers: draft.providers,
      }),
    [
      draft.targetExpr,
      draft.limitUsd,
      draft.window,
      draft.models,
      draft.providers,
    ],
  );

  const targetError = validateBudgetCel(draft.targetExpr);
  const canSubmit =
    draft.name.trim() !== "" &&
    !targetError &&
    draft.limitUsd > 0 &&
    (draft.breachAction !== "route_fallback" || draft.fallbackModel !== "");

  const toggleInList = (list: string[], value: string): string[] =>
    list.includes(value) ? list.filter((v) => v !== value) : [...list, value];

  return (
    <>
      <SheetHeader className="px-6 pt-6">
        <SheetTitle>{rule ? "Edit rule" : "New spend rule"}</SheetTitle>
        <SheetDescription>
          Set a budget for a set of actors, scoped to models and a time window,
          and choose what happens when the budget is spent.
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
            Choose directory-synced attributes to define who this budget covers.
          </p>
          <TargetConditionBuilder
            value={draft.targetExpr}
            onChange={(targetExpr) => patch({ targetExpr })}
          />
          <MatchedActors matched={usage.matched} />
        </div>

        {/* Limit + window */}
        <div className="grid grid-cols-2 gap-4">
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
        </div>

        <Field label="Window reset">
          <RadioGroup
            value={draft.reset}
            onValueChange={(v) => patch({ reset: v as WindowReset })}
            className="flex gap-4"
          >
            <RadioPill
              id="reset-fixed"
              value="fixed"
              label="Fixed"
              hint="Resets on a calendar boundary"
            />
            <RadioPill
              id="reset-rolling"
              value="rolling"
              label="Rolling"
              hint="Trailing window from now"
            />
          </RadioGroup>
        </Field>

        {/* Scope: providers + models */}
        <div className="space-y-3">
          <div>
            <Label className="text-sm font-medium">Scope</Label>
            <p className="text-muted-foreground text-xs">
              Limit only counts spend on the selected models or providers. Leave
              empty to cover all AI spend.
            </p>
          </div>
          <div className="space-y-2">
            <div className="text-muted-foreground text-[11px] font-medium tracking-wide uppercase">
              Providers
            </div>
            <div className="flex flex-wrap gap-1.5">
              {PROVIDERS.map((provider) => (
                <Chip
                  key={provider}
                  label={provider}
                  active={draft.providers.includes(provider)}
                  onClick={() =>
                    patch({
                      providers: toggleInList(draft.providers, provider),
                    })
                  }
                />
              ))}
            </div>
          </div>
          <div className="space-y-2">
            <div className="text-muted-foreground text-[11px] font-medium tracking-wide uppercase">
              Models
            </div>
            <div className="flex flex-wrap gap-1.5">
              {MODELS.map((model) => (
                <Chip
                  key={model.id}
                  label={model.label}
                  active={draft.models.includes(model.id)}
                  onClick={() =>
                    patch({ models: toggleInList(draft.models, model.id) })
                  }
                />
              ))}
            </div>
          </div>
        </div>

        {/* On breach */}
        <div className="space-y-2">
          <Label className="text-sm font-medium">
            When the budget is spent
          </Label>
          <p className="text-muted-foreground text-xs">
            If multiple budgets match a request, the strictest exhausted budget
            decides automatically.
          </p>
          <RadioGroup
            value={draft.breachAction}
            onValueChange={(v) => patch({ breachAction: v as BreachAction })}
            className="gap-2"
          >
            {BREACH_ACTIONS.map((action) => (
              <label
                key={action}
                htmlFor={`breach-${action}`}
                className="hover:bg-muted/40 flex cursor-pointer items-start gap-3 rounded-md border px-3 py-2.5"
              >
                <RadioGroupItem
                  id={`breach-${action}`}
                  value={action}
                  className="mt-0.5"
                />
                <div className="min-w-0">
                  <div className="text-sm">{BREACH_ACTION_LABELS[action]}</div>
                  <div className="text-muted-foreground text-xs">
                    {BREACH_ACTION_HINTS[action]}
                  </div>
                </div>
              </label>
            ))}
          </RadioGroup>
          {draft.breachAction === "route_fallback" && (
            <div className="pt-1">
              <Label className="text-muted-foreground mb-1 block text-xs">
                Fallback model
              </Label>
              <Select
                value={draft.fallbackModel}
                onValueChange={(fallbackModel) => patch({ fallbackModel })}
              >
                <SelectTrigger className="w-full">
                  <SelectValue placeholder="Choose a cheaper model" />
                </SelectTrigger>
                <SelectContent>
                  {MODELS.map((model) => (
                    <SelectItem key={model.id} value={model.id}>
                      {model.label} · {model.provider}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}
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
              <UsageBar usage={usage} limitUsd={draft.limitUsd} />
              <ProjectedBreachText
                overLimit={usage.projectedOverLimit}
                action={draft.breachAction}
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

function TargetConditionBuilder({
  value,
  onChange,
}: {
  value: string;
  onChange: (value: string) => void;
}): JSX.Element {
  const [conditions, setConditions] = useState<TargetCondition[]>(() =>
    parseTargetConditions(value),
  );

  const updateConditions = (next: TargetCondition[]) => {
    setConditions(next);
    onChange(targetConditionsToExpr(next));
  };

  return (
    <div className="space-y-2">
      <div className="space-y-2">
        {conditions.map((condition, index) => (
          <TargetConditionRow
            key={condition.id}
            condition={condition}
            index={index}
            canRemove={conditions.length > 1}
            onChange={(nextCondition) =>
              updateConditions(
                conditions.map((c) =>
                  c.id === condition.id ? nextCondition : c,
                ),
              )
            }
            onRemove={() =>
              updateConditions(conditions.filter((c) => c.id !== condition.id))
            }
          />
        ))}
      </div>
      <div className="flex items-center justify-between">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={() =>
            updateConditions([...conditions, makeDefaultCondition()])
          }
        >
          Add condition
        </Button>
        <p className="text-muted-foreground text-xs">
          Conditions are combined with AND.
        </p>
      </div>
    </div>
  );
}

function TargetConditionRow({
  condition,
  index,
  canRemove,
  onChange,
  onRemove,
}: {
  condition: TargetCondition;
  index: number;
  canRemove: boolean;
  onChange: (condition: TargetCondition) => void;
  onRemove: () => void;
}): JSX.Element {
  const attribute = actorAttribute(condition.attribute);
  const operators = operatorsForAttribute(attribute);
  const samples = attribute.samples;

  return (
    <div className="space-y-2 rounded-md border p-3">
      {index > 0 && (
        <div className="text-muted-foreground text-[11px] font-medium tracking-wide uppercase">
          And
        </div>
      )}
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-[1fr_150px_1fr_auto]">
        <Select
          value={condition.attribute}
          onValueChange={(attributeName) => {
            const nextAttribute = actorAttribute(attributeName);
            onChange({
              ...condition,
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
            onChange({ ...condition, operator: operator as ConditionOperator })
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
            onChange({ ...condition, value: nextValue })
          }
        >
          <SelectTrigger className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {samples.map((sample) => (
              <SelectItem key={sample} value={sample}>
                {sample}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={onRemove}
          disabled={!canRemove}
        >
          Remove
        </Button>
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
  action: BreachAction;
}): JSX.Element | null {
  if (!overLimit) return null;

  return (
    <p className="text-destructive text-xs">
      Projected to exceed the limit. This gate would {breachOutcome(action)}{" "}
      unless a stricter matching budget applies.
    </p>
  );
}

function breachOutcome(action: BreachAction): string {
  switch (action) {
    case "block":
      return "block requests";
    case "route_fallback":
      return "route requests to the fallback model";
    case "alert_only":
      return "flag requests";
  }
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

function RadioPill({
  id,
  value,
  label,
  hint,
}: {
  id: string;
  value: string;
  label: string;
  hint: string;
}): JSX.Element {
  return (
    <label
      htmlFor={id}
      className="hover:bg-muted/40 flex flex-1 cursor-pointer items-start gap-2 rounded-md border px-3 py-2"
    >
      <RadioGroupItem id={id} value={value} className="mt-0.5" />
      <div className="min-w-0">
        <div className="text-sm">{label}</div>
        <div className="text-muted-foreground text-xs">{hint}</div>
      </div>
    </label>
  );
}

function Chip({
  label,
  active,
  onClick,
}: {
  label: string;
  active: boolean;
  onClick: () => void;
}): JSX.Element {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "rounded-full border px-2.5 py-1 text-xs transition-colors",
        active
          ? "border-primary bg-primary/10 text-foreground"
          : "border-border text-muted-foreground hover:bg-muted hover:text-foreground",
      )}
    >
      {label}
    </button>
  );
}
