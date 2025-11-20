import { Input } from "@/components/ui/input";
import { useState } from "react";

export interface EnvironmentEntryInputProps {
  varName: string;
  isSensitive: boolean;
  inputValue: string;
  entryValue: string | null;
  hasExistingValue: boolean;
  isDirty: boolean;
  isSaving: boolean;
  onValueChange: (varName: string, value: string) => void;
  onKeyDown: (e: React.KeyboardEvent<HTMLInputElement>) => void;
}

const PASSWORD_MASK = "••••••••";

export function EnvironmentEntryInput({
  varName,
  isSensitive,
  inputValue,
  entryValue,
  hasExistingValue,
  isDirty,
  isSaving,
  onValueChange,
  onKeyDown,
}: EnvironmentEntryInputProps) {
  const [isFocused, setIsFocused] = useState(false);

  // Compute display value
  let displayValue = "";
  if (isDirty) {
    displayValue = inputValue;
  } else if (!isFocused && hasExistingValue && entryValue) {
    displayValue = isSensitive ? PASSWORD_MASK : entryValue;
  }

  return (
    <div className="grid grid-cols-2 gap-4 items-center">
      <label
        htmlFor={`env-${varName}`}
        className="text-sm font-medium text-foreground"
      >
        {varName}
      </label>
      <Input
        id={`env-${varName}`}
        value={displayValue}
        onChange={(value) => onValueChange(varName, value)}
        onFocus={() => setIsFocused(true)}
        onBlur={() => setIsFocused(false)}
        onKeyDown={onKeyDown}
        placeholder={
          hasExistingValue ? "Replace existing value" : "Enter value"
        }
        type={isSensitive ? "password" : "text"}
        className="font-mono text-sm"
        disabled={isSaving}
        autoComplete={isSensitive ? "new-password" : "off"}
      />
    </div>
  );
}
