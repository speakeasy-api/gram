import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import {
  KIT_REASONING_EFFORT_HELP,
  REASONING_EFFORT_HELP,
  REASONING_EFFORT_LABELS,
  type ReasoningEffort,
} from "./reasoning-effort";

export function ReasoningEffortSelect({
  value,
  onValueChange,
  disabled,
  disabledOff = false,
}: {
  value: ReasoningEffort;
  onValueChange: (value: ReasoningEffort) => void;
  disabled?: boolean;
  disabledOff?: boolean;
}): JSX.Element {
  return (
    <Select
      value={value}
      onValueChange={(value) => {
        if (disabledOff && value === "none") return;
        onValueChange(value as ReasoningEffort);
      }}
      disabled={disabled}
    >
      <SelectTrigger
        aria-label="Reasoning effort"
        title={disabledOff ? KIT_REASONING_EFFORT_HELP : REASONING_EFFORT_HELP}
        className="h-7 w-32 text-xs"
      >
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {Object.entries(REASONING_EFFORT_LABELS).map(([value, label]) => (
          <SelectItem
            key={value}
            value={value}
            disabled={disabledOff && value === "none"}
          >
            {label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
