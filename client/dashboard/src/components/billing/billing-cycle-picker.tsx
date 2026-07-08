import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { type BillingCycle, cycleKey, formatCycleName } from "./billing-cycles";

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
  // Available cycles, most recent first.
  cycles: BillingCycle[];
  // The cycle matching the current date range, if any.
  selected: BillingCycle | null;
  onSelect: (cycle: BillingCycle) => void;
}): JSX.Element {
  const handleChange = (key: string) => {
    const cycle = cycles.find((c) => cycleKey(c) === key);
    if (cycle) onSelect(cycle);
  };

  return (
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
            {formatCycleName(c)}
            {c.current ? " (current)" : ""}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
