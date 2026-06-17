import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { cn } from "@/lib/utils";
import type { RiskMatchCondition } from "@gram/client/models/components";
import { Button } from "@speakeasy-api/moonshine";
import { Plus, X } from "lucide-react";
import type { JSX, ReactNode } from "react";
import {
  defaultCondition,
  OP_LABELS,
  TARGET_DESCRIPTIONS,
  TARGET_LABELS,
  validateCondition,
  type MatchCombine,
} from "./detection-rules-data";

type MatchTarget = RiskMatchCondition["target"];
type MatchOp = RiskMatchCondition["op"];

// Field menu order: message/prompt text first, then tool facets. Each item
// carries a one-line description so the close-but-distinct tool fields
// (name vs server vs function vs args) are unambiguous.
const BUILDER_TARGETS: MatchTarget[] = [
  "content",
  "user_prompt",
  "assistant_text",
  "tool_result",
  "tool_name",
  "tool_server",
  "tool_function",
  "tool_args",
];

// Operators the builder authors, in menu order. The legacy glob/keyword/in ops
// are intentionally omitted — old rules using them still parse and render, they
// just aren't offered as new choices.
const BUILDER_OPS: MatchOp[] = [
  "equals",
  "not_equals",
  "contains",
  "not_contains",
  "starts_with",
  "ends_with",
  "regex",
  "exists",
];

// Operators that match on presence alone — the value input is hidden for these.
const VALUELESS_OPS = new Set<MatchOp>(["exists"]);

// Fixed-width leading column that holds the row connector ("Where" / "and" /
// "or"), so the field selects line up regardless of the connector content. Wide
// enough that the and/or dropdown isn't cramped.
const CONNECTOR = "w-[72px] shrink-0";

/** A structured, no-DSL editor for a single match_config: a list of
 *  `target <op> value` rows joined by a sentence-style connector. The AND/OR is
 *  the editable connector on the second row (it sets the whole group); there is
 *  no separate combinator header. Fully controlled. */
export function ConditionBuilder({
  conditions,
  combine,
  onChange,
}: {
  conditions: RiskMatchCondition[];
  combine: MatchCombine;
  onChange: (conditions: RiskMatchCondition[], combine: MatchCombine) => void;
}): JSX.Element {
  // Always render at least one row. When `conditions` is empty we show a blank
  // "ghost" row (not part of the data) so scope and exemptions both present a
  // ready-to-fill row; editing it commits the first real condition, while an
  // untouched ghost stays out of the data — so an unused group never blocks save
  // or persists a spurious rule.
  const rows = conditions.length > 0 ? conditions : [defaultCondition()];
  const removable = conditions.length > 0;

  const setTarget = (i: number, target: MatchTarget) =>
    onChange(
      rows.map((c, j) => {
        if (j !== i) return c;
        const next = { ...c, target };
        // The JSON path only applies to tool_args; drop it when leaving.
        if (target !== "tool_args") delete next.path;
        return next;
      }),
      combine,
    );

  const setOp = (i: number, op: MatchOp) =>
    onChange(
      rows.map((c, j) => {
        if (j !== i) return c;
        const next = { ...c, op };
        if (VALUELESS_OPS.has(op)) next.value = "";
        return next;
      }),
      combine,
    );

  const patch = (i: number, fields: Partial<RiskMatchCondition>) =>
    onChange(
      rows.map((c, j) => (j === i ? { ...c, ...fields } : c)),
      combine,
    );

  const remove = (i: number) =>
    onChange(
      rows.filter((_, j) => j !== i),
      combine,
    );

  const add = () => onChange([...conditions, defaultCondition()], combine);

  // The connector for each row: "Where" leads; the first join is an editable
  // and/or that flips the whole group; later joins mirror it as static text.
  const connector = (i: number): ReactNode => {
    if (i === 0) return <span className="text-muted-foreground">Where</span>;
    if (i === 1)
      return (
        <Select
          value={combine}
          onValueChange={(v) => onChange(conditions, v as MatchCombine)}
        >
          <SelectTrigger className="h-8 w-full px-2 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="and">and</SelectItem>
            <SelectItem value="or">or</SelectItem>
          </SelectContent>
        </Select>
      );
    return (
      <span className="text-muted-foreground">
        {combine === "or" ? "or" : "and"}
      </span>
    );
  };

  return (
    <div className="space-y-2">
      {rows.map((condition, i) => (
        <ConditionRow
          // Rows are positional and fully controlled by `condition`.
          // eslint-disable-next-line react/no-array-index-key
          key={i}
          condition={condition}
          connector={connector(i)}
          removable={removable}
          onTarget={(t) => setTarget(i, t)}
          onOp={(op) => setOp(i, op)}
          onPatch={(fields) => patch(i, fields)}
          onRemove={() => remove(i)}
        />
      ))}

      <div className="flex gap-2">
        <div className={CONNECTOR} />
        <Button variant="secondary" onClick={add}>
          <Button.LeftIcon>
            <Plus className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text>Add condition</Button.Text>
        </Button>
      </div>
    </div>
  );
}

function ConditionRow({
  condition,
  connector,
  removable,
  onTarget,
  onOp,
  onPatch,
  onRemove,
}: {
  condition: RiskMatchCondition;
  connector: ReactNode;
  removable: boolean;
  onTarget: (t: MatchTarget) => void;
  onOp: (op: MatchOp) => void;
  onPatch: (fields: Partial<RiskMatchCondition>) => void;
  onRemove: () => void;
}): JSX.Element {
  const showValue = !VALUELESS_OPS.has(condition.op);
  const isRegex = condition.op === "regex";
  const isToolArgs = condition.target === "tool_args";
  // Only surface a row error for content that's present but invalid (e.g. a
  // malformed regex). "Missing value" is left to the parent's save gate so a
  // freshly-added row doesn't shout before it's been filled in.
  const valueError =
    (condition.value ?? "").trim() !== "" ? validateCondition(condition) : null;

  return (
    <div className="space-y-1">
      {/* Wraps so the value field drops to its own full-width line when the row
          is too tight to keep everything inline (e.g. the narrower rule sheet),
          instead of squeezing the value to a sliver. */}
      <div className="flex flex-wrap items-start gap-2">
        <div className={cn("flex h-9 items-center text-sm", CONNECTOR)}>
          {connector}
        </div>

        <Select
          value={condition.target}
          onValueChange={(v) => onTarget(v as MatchTarget)}
        >
          <SelectTrigger className="h-9 w-[190px] shrink-0">
            <SelectValue />
          </SelectTrigger>
          <SelectContent className="w-[340px]">
            {BUILDER_TARGETS.map((t) => (
              <SelectItem
                key={t}
                value={t}
                description={TARGET_DESCRIPTIONS[t]}
              >
                {TARGET_LABELS[t]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Select value={condition.op} onValueChange={(v) => onOp(v as MatchOp)}>
          <SelectTrigger className="h-9 w-[176px] shrink-0">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {BUILDER_OPS.map((op) => (
              <SelectItem key={op} value={op}>
                {OP_LABELS[op]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <div className="flex min-w-[220px] flex-1 items-start gap-2">
          {showValue ? (
            <Input
              value={condition.value ?? ""}
              onChange={(v) => onPatch({ value: v })}
              placeholder={isRegex ? "regular expression" : "value"}
              className={cn(
                "h-9 min-w-0 flex-1",
                isRegex && "font-mono text-xs",
              )}
            />
          ) : (
            <span className="text-muted-foreground flex h-9 flex-1 items-center text-xs">
              (any value)
            </span>
          )}

          {removable ? (
            <button
              type="button"
              onClick={onRemove}
              aria-label="Remove condition"
              className="text-muted-foreground hover:text-foreground mt-2.5 shrink-0"
            >
              <X className="h-4 w-4" />
            </button>
          ) : (
            // Keep the value field's width stable when the lone ghost row has no
            // remove affordance.
            <div className="mt-2.5 h-4 w-4 shrink-0" />
          )}
        </div>
      </div>

      {isToolArgs && (
        <div className="flex gap-2">
          <div className={CONNECTOR} />
          <div className="flex items-center gap-2">
            <span className="text-muted-foreground shrink-0 text-xs">
              argument path
            </span>
            <Input
              value={condition.path ?? ""}
              onChange={(v) => onPatch({ path: v })}
              placeholder="$.command (optional — omit to match the whole payload)"
              className="h-8 w-[360px] max-w-full font-mono text-xs"
            />
          </div>
        </div>
      )}

      {valueError && (
        <div className="flex gap-2">
          <div className={CONNECTOR} />
          <p className="text-destructive text-xs">{valueError}</p>
        </div>
      )}
    </div>
  );
}
