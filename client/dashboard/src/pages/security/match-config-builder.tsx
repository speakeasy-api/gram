import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
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
import { Plus, Trash2 } from "lucide-react";
import type { JSX } from "react";
import {
  MATCH_OPS,
  MATCH_TARGETS,
  OP_LABELS,
  TARGET_LABELS,
  defaultCondition,
  validateCondition,
} from "./detection-rules-data";

type MatchTarget = RiskMatchCondition["target"];
type MatchOp = RiskMatchCondition["op"];

/** A query-builder for a custom rule's match_config conditions. Conditions are
 *  ANDed; each row picks a message target, an operator, and the operand. */
export function MatchConfigBuilder({
  conditions,
  onChange,
}: {
  conditions: RiskMatchCondition[];
  onChange: (next: RiskMatchCondition[]) => void;
}): JSX.Element {
  const updateAt = (index: number, patch: Partial<RiskMatchCondition>) =>
    onChange(
      conditions.map((c, i) =>
        i === index ? normalizeCondition({ ...c, ...patch }) : c,
      ),
    );
  const removeAt = (index: number) =>
    onChange(conditions.filter((_, i) => i !== index));
  const add = () => onChange([...conditions, defaultCondition()]);

  return (
    <div className="space-y-2">
      {conditions.map((condition, index) => (
        <ConditionRow
          key={index}
          condition={condition}
          showAnd={index > 0}
          onChange={(patch) => updateAt(index, patch)}
          onRemove={conditions.length > 1 ? () => removeAt(index) : undefined}
        />
      ))}
      <Button variant="ghost" size="sm" onClick={add}>
        <Plus className="mr-2 h-4 w-4" />
        Add condition
      </Button>
    </div>
  );
}

/** Drop fields that don't apply to the current target/op so a condition never
 *  carries a stale value (e.g. a path after switching away from tool_args). */
function normalizeCondition(c: RiskMatchCondition): RiskMatchCondition {
  const next: RiskMatchCondition = { target: c.target, op: c.op };
  if (c.target === "tool_args") next.path = c.path ?? "";
  if (c.op === "keyword") next.values = c.values ?? [];
  else if (c.op !== "exists") next.value = c.value ?? "";
  if (c.caseInsensitive && supportsCaseInsensitive(c.op)) {
    next.caseInsensitive = true;
  }
  return next;
}

function supportsCaseInsensitive(op: MatchOp): boolean {
  return op === "equals" || op === "not_equals" || op === "keyword";
}

function ConditionRow({
  condition,
  showAnd,
  onChange,
  onRemove,
}: {
  condition: RiskMatchCondition;
  showAnd: boolean;
  onChange: (patch: Partial<RiskMatchCondition>) => void;
  onRemove?: () => void;
}) {
  const error = validateCondition(condition);
  return (
    <div className="border-border space-y-2 rounded-md border p-3">
      <div className="flex items-center gap-2">
        <span
          className={cn(
            "text-muted-foreground w-8 shrink-0 text-xs font-medium",
            !showAnd && "invisible",
          )}
        >
          AND
        </span>
        <Select
          value={condition.target}
          onValueChange={(v) => onChange({ target: v as MatchTarget })}
        >
          <SelectTrigger className="h-8 w-[170px]">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {MATCH_TARGETS.map((t) => (
              <SelectItem key={t} value={t}>
                {TARGET_LABELS[t]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select
          value={condition.op}
          onValueChange={(v) => onChange({ op: v as MatchOp })}
        >
          <SelectTrigger className="h-8 w-[160px]">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {MATCH_OPS.map((o) => (
              <SelectItem key={o} value={o}>
                {OP_LABELS[o]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {onRemove && (
          <Button
            variant="ghost"
            size="icon"
            className="text-muted-foreground hover:text-destructive ml-auto h-8 w-8"
            onClick={onRemove}
          >
            <Trash2 className="h-4 w-4" />
          </Button>
        )}
      </div>

      {condition.target === "tool_args" && (
        <Input
          value={condition.path ?? ""}
          onChange={(v) => onChange({ path: v })}
          placeholder="$.path.to.field"
          className="font-mono text-xs"
        />
      )}

      <ConditionOperand condition={condition} onChange={onChange} />

      {supportsCaseInsensitive(condition.op) && (
        <label className="text-muted-foreground flex items-center gap-2 text-xs">
          <Checkbox
            checked={condition.caseInsensitive ?? false}
            onCheckedChange={(checked) =>
              onChange({ caseInsensitive: checked === true })
            }
          />
          Case-insensitive
        </label>
      )}

      {error && <p className="text-destructive text-xs">{error}</p>}
    </div>
  );
}

function ConditionOperand({
  condition,
  onChange,
}: {
  condition: RiskMatchCondition;
  onChange: (patch: Partial<RiskMatchCondition>) => void;
}) {
  if (condition.op === "exists") return null;

  if (condition.op === "keyword") {
    return (
      <Input
        value={(condition.values ?? []).join(",")}
        onChange={(v) => onChange({ values: v.split(",") })}
        placeholder="comma,separated,keywords"
      />
    );
  }

  const mono = condition.op === "regex" || condition.op === "glob";
  return (
    <Input
      value={condition.value ?? ""}
      onChange={(v) => onChange({ value: v })}
      placeholder={operandPlaceholder(condition.op, condition.target)}
      className={cn(mono && "font-mono text-xs")}
    />
  );
}

function operandPlaceholder(op: MatchOp, target: MatchTarget): string {
  switch (op) {
    case "regex":
      return target === "tool_args" ? "regex" : "e.g. acme_[a-z0-9]{32}";
    case "glob":
      return "e.g. *secret*";
    case "equals":
    case "not_equals":
    case "keyword":
    case "exists":
      return target === "tool_server"
        ? "MCP server name, or empty for native tools"
        : "value";
  }
}
