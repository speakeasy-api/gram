import { CalendarIcon, X } from "lucide-react";
import { subDays, subHours, format } from "date-fns";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

export type DateRangePreset = "24h" | "7d" | "30d" | "90d";

export type DateRange =
  | { type: "preset"; preset: DateRangePreset }
  | { type: "custom"; from: Date; to: Date };

const PRESETS: Record<
  DateRangePreset,
  { label: string; getRange: () => { from: Date; to: Date } }
> = {
  "24h": {
    label: "Last 24 hours",
    getRange: () => ({ from: subHours(new Date(), 24), to: new Date() }),
  },
  "7d": {
    label: "Last 7 days",
    getRange: () => ({ from: subDays(new Date(), 7), to: new Date() }),
  },
  "30d": {
    label: "Last 30 days",
    getRange: () => ({ from: subDays(new Date(), 30), to: new Date() }),
  },
  "90d": {
    label: "Last 90 days",
    getRange: () => ({ from: subDays(new Date(), 90), to: new Date() }),
  },
};

interface DateRangeSelectProps {
  value: DateRangePreset;
  onValueChange: (value: DateRangePreset) => void;
  customRange?: { from: Date; to: Date } | null;
  onClearCustomRange?: () => void;
  disabled?: boolean;
}

export function DateRangeSelect({
  value,
  onValueChange,
  customRange,
  onClearCustomRange,
  disabled,
}: DateRangeSelectProps) {
  if (customRange) {
    return (
      <div
        className={`inline-flex items-center gap-2.5 px-3.5 py-2 border border-border/60 rounded-lg bg-background/95 backdrop-blur-sm transition-all ${disabled ? "opacity-50 cursor-not-allowed" : "hover:border-border"}`}
      >
        <CalendarIcon className="size-4 text-foreground/60" />
        <span className="text-sm font-medium text-foreground/90 flex items-center gap-2">
          <span>{format(customRange.from, "MMM d, HH:mm")}</span>
          <span className="text-muted-foreground">→</span>
          <span>{format(customRange.to, "MMM d, HH:mm")}</span>
        </span>
        {onClearCustomRange && (
          <button
            className="ml-0.5 p-0.5 rounded hover:bg-muted/50 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            onClick={onClearCustomRange}
            disabled={disabled}
          >
            <X className="size-3.5 text-muted-foreground hover:text-foreground" />
          </button>
        )}
      </div>
    );
  }

  // Use explicit array to guarantee order
  const presetOrder: DateRangePreset[] = ["24h", "7d", "30d", "90d"];

  return (
    <Select value={value} onValueChange={onValueChange} disabled={disabled}>
      <SelectTrigger className="w-[180px]" disabled={disabled}>
        <CalendarIcon className="size-4 mr-2" />
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {presetOrder.map((key) => (
          <SelectItem key={key} value={key}>
            {PRESETS[key].label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

export function getDateRange(preset: DateRangePreset) {
  return PRESETS[preset].getRange();
}
