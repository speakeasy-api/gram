import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Trash2 } from "lucide-react";

export function WriteOnlyHeaderRow({
  name,
  value,
  hasStoredValue,
  nameInputName,
  valueInputName,
  nameInputLabel,
  valueInputLabel,
  disabled,
  onNameChange,
  onNameBlur,
  onValueChange,
  onValueBlur,
  onRemove,
}: {
  name: string;
  value: string;
  hasStoredValue: boolean;
  nameInputName: string;
  valueInputName: string;
  nameInputLabel: string;
  valueInputLabel: string;
  disabled: boolean;
  onNameChange: (value: string) => void;
  onNameBlur: () => void;
  onValueChange: (value: string) => void;
  onValueBlur: () => void;
  onRemove: () => void;
}): JSX.Element {
  return (
    <div className="flex items-center gap-2">
      <Input
        name={nameInputName}
        aria-label={nameInputLabel}
        placeholder="Header name"
        value={name}
        onChange={onNameChange}
        onBlur={onNameBlur}
        disabled={disabled}
        className="flex-1"
      />
      <Input
        name={valueInputName}
        aria-label={valueInputLabel}
        placeholder={hasStoredValue ? "••••" : "Header value"}
        value={value}
        onChange={onValueChange}
        onBlur={onValueBlur}
        type="password"
        reveal={!hasStoredValue || value !== ""}
        disabled={disabled}
        className="flex-1"
      />
      <Button
        type="button"
        variant="tertiary"
        size="sm"
        onClick={onRemove}
        disabled={disabled}
        aria-label={`Remove header ${name || "row"}`}
      >
        <Button.LeftIcon>
          <Trash2 className="size-3.5" />
        </Button.LeftIcon>
      </Button>
    </div>
  );
}
