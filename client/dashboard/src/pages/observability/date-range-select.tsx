import { CalendarIcon, X } from "lucide-react";
import { subDays, subHours, format } from "date-fns";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Button } from "@/components/ui/button";

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
}

export function DateRangeSelect({
  value,
  onValueChange,
  customRange,
  onClearCustomRange,
}: DateRangeSelectProps) {
  if (customRange) {
    return (
      <div className="flex items-center gap-2 px-3 py-2 border rounded-md bg-blue-50 border-blue-200">
        <CalendarIcon className="size-4 text-blue-600" />
        <span className="text-sm text-blue-800">
          {format(customRange.from, "MMM d, HH:mm")} –{" "}
          {format(customRange.to, "MMM d, HH:mm")}
        </span>
        {onClearCustomRange && (
          <Button
            variant="ghost"
            size="sm"
            className="h-5 w-5 p-0 hover:bg-blue-100"
            onClick={onClearCustomRange}
          >
            <X className="size-3 text-blue-600" />
          </Button>
        )}
      </div>
    );
  }

  return (
    <Select value={value} onValueChange={onValueChange}>
      <SelectTrigger className="w-[180px]">
        <CalendarIcon className="size-4 mr-2" />
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {Object.entries(PRESETS).map(([key, preset]) => (
          <SelectItem key={key} value={key}>
            {preset.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

export function getDateRange(preset: DateRangePreset) {
  return PRESETS[preset].getRange();
}
