import { Switch } from "@/components/ui/Switch";

export function RawValuesToggle({
  checked,
  onCheckedChange,
}: {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
}): JSX.Element {
  return (
    <label className="text-muted-foreground flex cursor-pointer items-center gap-2 text-xs">
      <Switch checked={checked} onCheckedChange={onCheckedChange} />
      Show raw values
    </label>
  );
}
