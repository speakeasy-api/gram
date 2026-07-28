import { Switch } from "@/components/ui/switch";
import { useId } from "react";

export function AnnotationToggle({
  label,
  description,
  checked,
  onCheckedChange,
}: {
  label: string;
  description: string;
  checked: boolean;
  onCheckedChange: (value: boolean) => void;
}): JSX.Element {
  const descriptionId = useId();

  return (
    <div className="flex items-center justify-between">
      <div>
        <p className="text-sm">{label}</p>
        <p id={descriptionId} className="text-muted-foreground text-xs">
          {description}
        </p>
      </div>
      <Switch
        checked={checked}
        onCheckedChange={onCheckedChange}
        aria-label={`${label} hint`}
        aria-describedby={descriptionId}
      />
    </div>
  );
}
