import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { type MeterCycleWindow } from "./use-meter-period";
const cycleMonthFormat = new Intl.DateTimeFormat("en-US", {
  month: "long",
  year: "numeric",
  timeZone: "UTC",
});

function cycleKey(cycle: MeterCycleWindow): string {
  return cycle.from.toISOString();
}

/**
 * Billing-cycle shortcut next to the time-range picker: selecting a cycle sets
 * the page's custom date range to that cycle's exact boundaries, so every
 * query on the page scopes to it. Purely a convenience over the range params —
 * the range picker remains the source of truth.
 */
export function BillingCyclePicker({
  cycles,
  selected,
  onSelect,
}: {
  cycles: MeterCycleWindow[];
  selected: MeterCycleWindow | null;
  onSelect: (cycle: MeterCycleWindow) => void;
}): JSX.Element {
  const handleChange = (key: string) => {
    const cycle = cycles.find((c) => cycleKey(c) === key);
    if (cycle) onSelect(cycle);
  };

  return (
    // The empty string is Radix's sanctioned controlled-mode "show the
    // placeholder" value for the ROOT (its no-empty-string rule applies to
    // Item values only); undefined would flip the Select to uncontrolled and
    // freeze the stale cycle label.
    <Select
      value={selected ? cycleKey(selected) : ""}
      onValueChange={handleChange}
    >
      <SelectTrigger className="bg-background h-auto w-auto gap-1.5 py-1.5 text-sm">
        <SelectValue placeholder="Billing cycle" />
      </SelectTrigger>
      <SelectContent>
        {cycles.map((c) => (
          <SelectItem key={cycleKey(c)} value={cycleKey(c)}>
            {cycleMonthFormat.format(c.from)} billing cycle
            {c === cycles[cycles.length - 1] ? " (current)" : ""}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
