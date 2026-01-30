import { CalendarIcon } from "lucide-react";
import { subDays, subHours } from "date-fns";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

export type DateRangePreset = "24h" | "7d" | "30d";

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
};

interface DateRangeSelectProps {
  value: DateRangePreset;
  onValueChange: (value: DateRangePreset) => void;
}

export function DateRangeSelect({
  value,
  onValueChange,
}: DateRangeSelectProps) {
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
