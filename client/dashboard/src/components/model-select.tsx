import { Badge } from "@/components/ui/Badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { AVAILABLE_MODELS, type AvailableModel } from "@/lib/models";

/**
 * The app-wide model picker: AVAILABLE_MODELS with an "Expensive" badge on
 * premium-priced entries. A current value outside the list is kept selectable
 * so the trigger never renders blank for legacy or hand-configured models.
 */
export function ModelSelect({
  value,
  onValueChange,
  disabled,
  triggerClassName,
}: {
  value: string;
  onValueChange: (model: string) => void;
  disabled?: boolean;
  triggerClassName?: string;
}): JSX.Element {
  const options: AvailableModel[] = AVAILABLE_MODELS.some(
    (m) => m.value === value,
  )
    ? AVAILABLE_MODELS
    : [{ value, label: value }, ...AVAILABLE_MODELS];

  return (
    <Select value={value} onValueChange={onValueChange} disabled={disabled}>
      <SelectTrigger size="sm" className={triggerClassName}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {options.map((m) => (
          <SelectItem key={m.value} value={m.value}>
            <span className="flex items-center gap-2">
              {m.label}
              {m.expensive && (
                <Badge size="sm" variant="warning" background>
                  <Badge.Text>Expensive</Badge.Text>
                </Badge>
              )}
            </span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
