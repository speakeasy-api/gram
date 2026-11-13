import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import type { ReasoningEffort as CreateReasoningEffort } from "@gram/client/models/components/createassistantform.js";

export type ReasoningEffort = CreateReasoningEffort;

export function reasoningEffortOrDefault(
  value: string | undefined,
): ReasoningEffort {
  return value === "none" || value === "low" || value === "high"
    ? value
    : "medium";
}

export const REASONING_EFFORT_LABELS = {
  none: "Off",
  low: "Low",
  medium: "Medium",
  high: "High",
} satisfies Record<ReasoningEffort, string>;

export const REASONING_EFFORT_HELP =
  "Controls how much reasoning the model uses. Off is only supported by models that permit disabling reasoning.";

export const KIT_REASONING_EFFORT_HELP =
  "Kit runtime supports Low, Medium, and High reasoning effort only. Switch to the Default runtime to turn reasoning Off.";

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
