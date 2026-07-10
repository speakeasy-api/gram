import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Type } from "@/components/ui/type";
import {
  getActionCategory,
  getActionColorConfig,
} from "@/lib/audit-log-colors";
import {
  formatDateHeader,
  type FacetOption,
  type TimestampMode,
} from "@/lib/audit-log-feed";
import { formatAuditAction } from "@/lib/audit-log-format";
import { cn } from "@/lib/utils";
import React from "react";

export function DateGroupHeader({
  date,
  mode,
}: {
  date: Date;
  mode: TimestampMode;
}): React.JSX.Element {
  return (
    <div className="flex items-center gap-3 px-4 py-2">
      <span className="text-muted-foreground shrink-0 text-[11px] font-semibold tracking-wide uppercase">
        {formatDateHeader(date, mode)}
      </span>
      <div className="bg-border h-px flex-1" />
    </div>
  );
}

export function ActionBadge({ action }: { action: string }): React.JSX.Element {
  const category = getActionCategory(action);
  const colors = getActionColorConfig(category);
  return (
    <span
      className={cn(
        "inline-flex items-center rounded px-1.5 py-0.5 font-mono text-[11px] font-medium",
        colors.bg,
        colors.text,
      )}
    >
      {formatAuditAction(action)}
    </span>
  );
}

export function ActionDot({ action }: { action: string }): React.JSX.Element {
  const category = getActionCategory(action);
  const colors = getActionColorConfig(category);
  return (
    <span
      className={cn(
        "mt-[3px] inline-block size-2 shrink-0 rounded-full",
        colors.dot,
      )}
    />
  );
}

export function FacetSelect({
  label,
  value,
  onValueChange,
  placeholder,
  allLabel,
  options,
}: {
  label: string;
  value: string;
  onValueChange: (value: string) => void;
  placeholder: string;
  allLabel: string;
  options: FacetOption[];
}): React.JSX.Element {
  return (
    <div className="flex flex-col gap-1.5">
      <Type small muted>
        {label}
      </Type>
      <Select value={value} onValueChange={onValueChange}>
        <SelectTrigger size="sm" className="bg-background min-w-[220px]">
          <SelectValue placeholder={placeholder} />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">{allLabel}</SelectItem>
          {options.map((option) => (
            <SelectItem
              key={option.value}
              value={option.value}
              description={
                option.count == null
                  ? undefined
                  : `${option.count.toLocaleString()} audit log${option.count === 1 ? "" : "s"}`
              }
            >
              {option.displayName}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
