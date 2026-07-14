import { useEffect, useState } from "react";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Operator } from "@gram/client/models/components/logfilter";
import { OP_LABELS, type ActiveLogFilter } from "@/pages/logs/log-filter-types";

const OP_OPTIONS: { value: Operator; label: string }[] = [
  { value: Operator.Eq, label: "equals" },
  { value: Operator.NotEq, label: "not equals" },
  { value: Operator.Contains, label: "contains" },
  { value: Operator.In, label: "in" },
];

/**
 * A solid grey filter pill: `value ✕`. The body opens the editor (the filter
 * sheet); the × clears the filter. Matches the Untitled UI reference where every
 * applied filter reads as a uniform pill.
 *
 * `onRemove` is optional: a pinned dimension sitting at its default value (e.g.
 * "All servers" or a daterange on its default preset) has nothing to clear, so
 * the caller omits it and the × is hidden rather than rendered as a no-op.
 */
export function FilterChip({
  label,
  onClick,
  onRemove,
}: {
  label: string;
  onClick?: () => void;
  onRemove?: () => void;
}): JSX.Element {
  return (
    <span
      className={`border-border bg-card hover:bg-muted/50 inline-flex h-10 shrink-0 items-center gap-2 border pl-3 text-sm font-medium transition-colors ${
        onRemove ? "pr-2" : "pr-3"
      }`}
    >
      <button
        type="button"
        onClick={onClick}
        className="hover:text-foreground transition-colors"
      >
        {label}
      </button>
      {onRemove && (
        <button
          type="button"
          onClick={onRemove}
          aria-label={`Remove ${label} filter`}
          className="text-muted-foreground hover:text-foreground transition-colors"
        >
          <X className="size-4" />
        </button>
      )}
    </span>
  );
}

/**
 * Editable chip for an arbitrary attribute filter (the `af` param). Click the
 * body to change the operator/value; the × removes it. Mirrors the editing UX
 * of the MCP Logs attribute filter so custom filters behave identically on
 * every page.
 */
export function CustomFilterChip({
  filter,
  onEdit,
  onRemove,
}: {
  filter: ActiveLogFilter;
  onEdit: (id: string, op: Operator, value?: string) => void;
  onRemove: (id: string) => void;
}): JSX.Element {
  const [open, setOpen] = useState(false);
  const [op, setOp] = useState<Operator>(filter.op);
  const [value, setValue] = useState(filter.value ?? "");

  useEffect(() => {
    if (open) {
      setOp(filter.op);
      setValue(filter.value ?? "");
    }
  }, [open, filter.op, filter.value]);

  const save = () => {
    const trimmed = value.trim();
    if (!trimmed) return;
    onEdit(filter.id, op, trimmed);
    setOpen(false);
  };

  const valuePlaceholder = op === Operator.In ? "value1, value2, ..." : "value";

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <span className="border-border bg-card hover:bg-muted/50 inline-flex h-10 shrink-0 items-center gap-2 border pr-2 pl-3 font-mono text-xs transition-colors">
        <PopoverTrigger asChild>
          <button
            type="button"
            aria-label={`Edit filter ${filter.path}`}
            className="hover:text-foreground cursor-pointer transition-colors"
          >
            {filter.path} {OP_LABELS[filter.op]}
            {filter.value !== undefined ? ` ${filter.value}` : ""}
          </button>
        </PopoverTrigger>
        <button
          type="button"
          aria-label={`Remove filter ${filter.path}`}
          onClick={(e) => {
            e.stopPropagation();
            onRemove(filter.id);
          }}
          className="text-muted-foreground hover:text-foreground transition-colors"
        >
          <X className="size-4" />
        </button>
      </span>
      <PopoverContent
        align="start"
        className="w-[320px] p-3"
        onOpenAutoFocus={(e) => e.preventDefault()}
      >
        <div
          className="text-muted-foreground mb-2 font-mono text-xs break-all"
          title={filter.path}
        >
          {filter.path}
        </div>
        <div className="mb-3 flex gap-2">
          <Select value={op} onValueChange={(v) => setOp(v as Operator)}>
            <SelectTrigger className="!h-8 w-[120px]">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {OP_OPTIONS.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  <span className="font-mono text-xs">
                    {OP_LABELS[option.value]}
                  </span>
                  <span className="text-muted-foreground ml-2 text-xs">
                    {option.label}
                  </span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <input
            type="text"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                save();
              } else if (e.key === "Escape") {
                e.preventDefault();
                setOpen(false);
              }
            }}
            placeholder={valuePlaceholder}
            className="border-border focus-visible:border-ring focus-visible:ring-ring/50 h-8 min-w-0 flex-1 border bg-transparent px-2 font-mono text-xs outline-none focus-visible:ring-[3px]"
            autoFocus
          />
        </div>
        <div className="flex items-center justify-between">
          <Button
            type="button"
            variant="destructive-secondary"
            size="sm"
            onClick={() => {
              onRemove(filter.id);
              setOpen(false);
            }}
          >
            Remove
          </Button>
          <div className="flex gap-2">
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={() => setOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="button"
              size="sm"
              onClick={save}
              disabled={!value.trim()}
            >
              Save
            </Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}
