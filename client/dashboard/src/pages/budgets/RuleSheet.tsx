import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/RadioGroup";
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
import { TextArea } from "@/components/ui/Textarea";
import { Text } from "@/components/ui/Text";
import { Archive, Check, Loader2, Search, Users } from "lucide-react";
import { useEffect, useMemo, useState, type JSX, type ReactNode } from "react";
import {
  WINDOW_LABELS,
  defaultRuleDraft,
  formatUsd,
  toDraft,
  type ActorAttribute,
  type BudgetWindow,
  type PreviewSpendRuleResult,
  type RuleAction,
  type RuleDraft,
  type RuleTargetCondition,
  type RuleTargetOperator,
  type SpendRule,
} from "./budgets-data";
import { useActorAttributes, usePreviewBudgetRule } from "./budgets-queries";

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

function actorAttribute(
  attributes: ActorAttribute[],
  name: string,
): ActorAttribute | undefined {
  return attributes.find((attr) => attr.name === name);
}

/** Operators offered for an attribute. Unknown/not-yet-loaded attributes fall
 *  back to the string operators — the common case, and never a list-only op. */
function operatorsForAttribute(
  attr: ActorAttribute | undefined,
): RuleTargetOperator[] {
  return attr?.type === "list" ? LIST_OPERATORS : STRING_OPERATORS;
}

function attributeLabel(name: string): string {
  return name
    .split("_")
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

/** Create or edit a budget rule. Saving an edit archives the current version
 *  row server-side and creates a successor; archiving ends the rule outright. */
export function RuleSheet({
  open,
  onOpenChange,
  rule,
  onSubmit,
  onArchive,
  submitting = false,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Editing an existing rule, or undefined when creating. */
  rule?: SpendRule;
  onSubmit: (draft: RuleDraft) => void;
  onArchive?: () => void;
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
          onArchive={onArchive}
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
): { preview: PreviewSpendRuleResult | null; loading: boolean } {
  const { preview: runPreviewMutation, isPending } = usePreviewBudgetRule();
  const [preview, setPreview] = useState<PreviewSpendRuleResult | null>(null);

  useEffect(() => {
    if (draft.limitUsd <= 0 || draft.target.value.trim() === "") {
      // No preview request will be issued for an invalid draft — drop any
      // previous result so a stale usage/breach preview isn't shown for it.
      setPreview(null);
      return;
    }
    const timer = setTimeout(() => {
      runPreviewMutation(
        {
          target: draft.target,
          limitUsd: draft.limitUsd,
          warnAtPct: draft.warnAtPct,
          windowKind: draft.windowKind,
        },
        { onSuccess: (data) => setPreview(data) },
      );
    }, 350);
    return () => clearTimeout(timer);
  }, [
    draft.target,
    draft.limitUsd,
    draft.warnAtPct,
    draft.windowKind,
    runPreviewMutation,
  ]);

  return { preview, loading: isPending };
}

function RuleForm({
  rule,
  onSubmit,
  onArchive,
  submitting,
}: {
  rule?: SpendRule;
  onSubmit: (draft: RuleDraft) => void;
  onArchive?: () => void;
  submitting: boolean;
}): JSX.Element {
  const [draft, setDraft] = useState<RuleDraft>(
    rule ? toDraft(rule) : defaultRuleDraft(),
  );
  const [confirmOpen, setConfirmOpen] = useState(false);

  const patch = (p: Partial<RuleDraft>) => setDraft((d) => ({ ...d, ...p }));

  const { attributes } = useActorAttributes();
  const { preview, loading: previewLoading } = useRulePreview(draft);

  const canSubmit =
    draft.name.trim() !== "" &&
    draft.target.value.trim() !== "" &&
    draft.limitUsd > 0 &&
    !submitting;

  const overLimitCount = useMemo(() => {
    if (!preview) return 0;
    return preview.actors.filter((a) => a.breached).length;
  }, [preview]);

  const requiresConfirmation =
    rule !== undefined &&
    isMaterialRuleEdit(rule, draft) &&
    draft.action === "block";

  const confirmationDescription =
    blockingEditConfirmationDescription(overLimitCount);

  const handleSubmit = () => {
    if (requiresConfirmation) {
      setConfirmOpen(true);
      return;
    }
    onSubmit(draft);
  };

  const handleConfirmSubmit = () => {
    setConfirmOpen(false);
    onSubmit(draft);
  };

  return (
    <>
      <SheetHeader className="px-6 pt-6">
        <SheetTitle>{rule ? "Edit rule" : "New budget rule"}</SheetTitle>
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
            attributes={attributes}
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
              <span className="border-input bg-muted text-muted-foreground inline-flex h-9 items-center border border-r-0 px-3 text-sm">
                $
              </span>
              <input
                type="number"
                min={1}
                value={draft.limitUsd}
                onChange={(e) =>
                  patch({ limitUsd: Math.max(0, Number(e.target.value) || 0) })
                }
                className="border-input dark:bg-input/30 h-9 w-full min-w-0 border bg-transparent px-3 py-1 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px]"
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
                step={1}
                value={draft.warnAtPct}
                onChange={(e) =>
                  patch({
                    // warn_at_pct is an integer in the API contract; round so a
                    // fractional entry can't leave the form submittable but
                    // rejected by the generated client.
                    warnAtPct: Math.min(
                      99,
                      Math.max(1, Math.round(Number(e.target.value) || 0)),
                    ),
                  })
                }
                className="border-input dark:bg-input/30 h-9 w-full min-w-0 border bg-transparent px-3 py-1 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px]"
              />
              <span className="border-input bg-muted text-muted-foreground inline-flex h-9 items-center border border-l-0 px-3 text-sm">
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
                className="hover:bg-muted/40 flex cursor-pointer items-start gap-3 border px-3 py-2.5"
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
        <div className="bg-muted/30 space-y-2 border p-4">
          <div className="flex items-center justify-between">
            <Text variant="small" className="font-medium">
              Current usage this {draft.windowKind} window
            </Text>
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
        {onArchive ? (
          <Button
            variant="tertiary"
            size="sm"
            onClick={onArchive}
            disabled={submitting}
            className="text-muted-foreground"
          >
            <Archive className="mr-2 h-4 w-4" />
            Archive
          </Button>
        ) : (
          <span />
        )}
        <Button disabled={!canSubmit} onClick={handleSubmit}>
          <Check className="mr-2 h-4 w-4" />
          {rule ? "Save changes" : "Create rule"}
        </Button>
      </SheetFooter>

      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Apply blocking rule changes?</Dialog.Title>
            <Dialog.Description>{confirmationDescription}</Dialog.Description>
          </Dialog.Header>
          <Dialog.Footer>
            <Button
              variant="secondary"
              onClick={() => setConfirmOpen(false)}
              disabled={submitting}
            >
              Cancel
            </Button>
            <Button
              variant="destructive-primary"
              onClick={handleConfirmSubmit}
              disabled={submitting}
            >
              Save and apply
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
}

function isMaterialRuleEdit(rule: SpendRule, draft: RuleDraft): boolean {
  const current = toDraft(rule);
  return (
    !sameTargetCondition(current.target, draft.target) ||
    rule.limitUsd !== draft.limitUsd ||
    rule.windowKind !== draft.windowKind ||
    rule.warnAtPct !== draft.warnAtPct ||
    rule.action !== draft.action
  );
}

function sameTargetCondition(
  left: RuleTargetCondition,
  right: RuleTargetCondition,
): boolean {
  return (
    left.attribute === right.attribute &&
    left.operator === right.operator &&
    left.value === right.value
  );
}

function blockingEditConfirmationDescription(overLimitCount: number): string {
  if (overLimitCount === 1) {
    return "One matched person is already over the proposed limit for the current fixed window, so their requests may be blocked as soon as this change is evaluated.";
  }
  if (overLimitCount > 1) {
    return `${overLimitCount} matched people are already over the proposed limit for the current fixed window, so their requests may be blocked as soon as this change is evaluated.`;
  }
  return "If any matched people are already over the proposed limit for the current fixed window, their requests may be blocked as soon as this change is evaluated.";
}

/** Rows shown without a search query. The preview payload itself is capped at
 *  50 actors server-side; searching covers that full payload. */
const MATCHED_ACTOR_ROWS = 5;

function matchingActors(
  actors: PreviewSpendRuleResult["actors"],
  query: string,
): PreviewSpendRuleResult["actors"] {
  const needle = query.trim().toLowerCase();
  if (needle === "") return actors;
  return actors.filter(
    (actor) =>
      actor.email.toLowerCase().includes(needle) ||
      (actor.displayName ?? "").toLowerCase().includes(needle),
  );
}

function MatchedActors({
  preview,
  loading,
}: {
  preview: PreviewSpendRuleResult | null;
  loading: boolean;
}): JSX.Element {
  const [query, setQuery] = useState("");

  if (!preview) {
    return (
      <p className="text-muted-foreground text-xs">
        {loading
          ? "Matching members…"
          : "Matched people will appear here once the condition is valid."}
      </p>
    );
  }

  const actors = preview.actors;
  const searchable = actors.length > MATCHED_ACTOR_ROWS;
  const filtered = matchingActors(actors, query);
  const hasQuery = query.trim() !== "";
  // A query means the admin is checking whether someone is covered, so show
  // every hit; the default view stays a short top-of-list sample.
  const visible = hasQuery ? filtered : filtered.slice(0, MATCHED_ACTOR_ROWS);
  // The server caps the actor payload, so search is only authoritative over
  // the people it returned.
  const payloadCapped = preview.matchedCount > actors.length;
  const cappedNote = payloadCapped
    ? ` Search covers the first ${actors.length} of ${preview.matchedCount} matched people.`
    : "";

  return (
    <div className="border-border border">
      <div className="border-border bg-muted/40 flex items-center gap-2 border-b px-3 py-2 text-xs font-medium">
        <Users className="size-3.5" />
        {preview.matchedCount} matched{" "}
        {preview.matchedCount === 1 ? "person" : "people"}
      </div>
      {searchable && (
        <div className="border-border flex items-center gap-2 border-b px-3 py-1.5">
          <Search className="text-muted-foreground size-3.5 shrink-0" />
          <input
            type="text"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Search matched people"
            aria-label="Search matched people"
            className="placeholder:text-muted-foreground w-full bg-transparent text-xs outline-none"
          />
        </div>
      )}
      <MatchedActorRows
        actors={actors}
        visible={visible}
        query={query}
        cappedNote={cappedNote}
      />
      {!hasQuery && filtered.length > MATCHED_ACTOR_ROWS && (
        <p className="text-muted-foreground border-border border-t px-3 py-2 text-xs">
          +{filtered.length - MATCHED_ACTOR_ROWS} more — search to check whether
          someone is covered.{cappedNote}
        </p>
      )}
    </div>
  );
}

function MatchedActorRows({
  actors,
  visible,
  query,
  cappedNote,
}: {
  actors: PreviewSpendRuleResult["actors"];
  visible: PreviewSpendRuleResult["actors"];
  query: string;
  cappedNote: string;
}): JSX.Element {
  if (actors.length === 0) {
    return (
      <p className="text-muted-foreground px-3 py-3 text-xs">
        No members match this condition.
      </p>
    );
  }
  if (visible.length === 0) {
    return (
      <p className="text-muted-foreground px-3 py-3 text-xs">
        No matched person found for “{query.trim()}”.{cappedNote}
      </p>
    );
  }
  return (
    <ul className="divide-border max-h-40 divide-y overflow-y-auto">
      {visible.map((actor) => (
        <li
          key={actor.email}
          className="flex items-center justify-between gap-3 px-3 py-2 text-xs"
        >
          <div className="min-w-0">
            <div className="truncate">{actor.displayName || actor.email}</div>
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
  );
}

/** Single attribute/operator/value picker backing the rule's target
 *  condition. v1 deliberately allows exactly one condition per rule. The
 *  attribute catalog is fetched from the server (see useActorAttributes). */
function TargetConditionField({
  value,
  onChange,
  attributes,
}: {
  value: RuleTargetCondition;
  onChange: (value: RuleTargetCondition) => void;
  attributes: ActorAttribute[];
}): JSX.Element {
  const [condition, setCondition] = useState<RuleTargetCondition>(value);

  const update = (next: RuleTargetCondition) => {
    setCondition(next);
    onChange(next);
  };

  const attribute = actorAttribute(attributes, condition.attribute);
  const operators = operatorsForAttribute(attribute);

  return (
    <div className="space-y-2 border p-3">
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-[1fr_150px_1fr]">
        <Select
          value={condition.attribute}
          onValueChange={(attributeName) => {
            const nextAttribute = actorAttribute(attributes, attributeName);
            update({
              attribute: attributeName,
              operator: operatorsForAttribute(nextAttribute)[0]!,
              value: "",
            });
          }}
        >
          <SelectTrigger className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {attributes.map((attr) => (
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
        <Input
          value={condition.value}
          onChange={(nextValue) => update({ ...condition, value: nextValue })}
          placeholder="Value"
          aria-label="Condition value"
        />
      </div>
    </div>
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
