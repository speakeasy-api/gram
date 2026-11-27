import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { cn } from "@/lib/utils";
import {
  requestDurationChoices,
  type RequestedDurationKey,
} from "@/pages/access/requestedDuration";
import { useId } from "react";

/**
 * "How long do you need it?" on an access request. The answer is a
 * suggestion: the administrator still picks the window when they approve.
 * End of day follows the requester's own clock and is left out when the day
 * is nearly over.
 */
export function RequestDurationSelect({
  value,
  onChange,
  disabled,
  className,
}: {
  value: RequestedDurationKey;
  onChange: (value: RequestedDurationKey) => void;
  disabled?: boolean;
  className?: string;
}): React.JSX.Element {
  const id = useId();
  const choices = requestDurationChoices(new Date());
  // A choice made earlier in the day can drop out of the list (End of day
  // close to midnight); fall back to no preference rather than show nothing.
  const current = choices.some((choice) => choice.key === value)
    ? value
    : "none";
  return (
    <div className={cn("flex items-center gap-2 text-sm", className)}>
      <label htmlFor={id} className="text-muted-foreground whitespace-nowrap">
        How long do you need it?
      </label>
      <Select
        value={current}
        onValueChange={(next) => onChange(next as RequestedDurationKey)}
        disabled={disabled}
      >
        <SelectTrigger id={id} size="sm" className="min-w-36">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {choices.map((choice) => (
            <SelectItem key={choice.key} value={choice.key}>
              {choice.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
