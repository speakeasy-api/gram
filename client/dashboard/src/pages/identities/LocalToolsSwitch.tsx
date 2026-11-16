import { useId } from "react";
import { Switch } from "@/components/ui/Switch";
import { SimpleTooltip } from "@/components/ui/Tooltip";

/**
 * "Show local tools", for an identity section's header: one switch for the
 * Tool calls chart and the Tool logs below it, so the two always count the
 * same calls.
 */
export function LocalToolsSwitch({
  checked,
  onCheckedChange,
}: {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
}): JSX.Element {
  const labelId = useId();
  return (
    <SimpleTooltip tooltip="Calls to an agent's built-in tools, such as file reads and shell commands. Applies to Tool calls and Tool logs.">
      <label className="flex cursor-pointer items-center gap-2">
        <Switch
          checked={checked}
          onCheckedChange={onCheckedChange}
          aria-labelledby={labelId}
        />
        <span
          id={labelId}
          className="text-muted-foreground text-xs font-medium"
        >
          Show local tools
        </span>
      </label>
    </SimpleTooltip>
  );
}
