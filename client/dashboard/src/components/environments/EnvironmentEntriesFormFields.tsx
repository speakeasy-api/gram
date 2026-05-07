import { Input } from "@/components/ui/input";
import { useState } from "react";
import type { EnvironmentEntryFormInput } from "./useEnvironmentForm";

const PASSWORD_MASK = "••••••••";

interface EnvironmentEntriesFormFieldsProps {
  entries: EnvironmentEntryFormInput[];
  relevantEnvVars: string[];
  disabled: boolean;
  onKeyDown: (e: React.KeyboardEvent<HTMLInputElement>) => void;
}

function EnvironmentEntryInput({
  entry,
  disabled,
  onKeyDown,
}: {
  entry: EnvironmentEntryFormInput;
  disabled: boolean;
  onKeyDown: (e: React.KeyboardEvent<HTMLInputElement>) => void;
}) {
  const [isFocused, setIsFocused] = useState(false);

  const hasExistingValue = entry.initialValue !== null;
  const entryIsDirty = entry.inputValue !== "";

  // Compute display value
  let displayValue = "";
  if (entryIsDirty) {
    displayValue = entry.inputValue;
  } else if (!isFocused && hasExistingValue && entry.initialValue) {
    displayValue = entry.isSensitive ? PASSWORD_MASK : entry.initialValue;
  }

  return (
    <div className="grid grid-cols-2 items-center gap-4">
      <label
        htmlFor={`env-${entry.varName}`}
        className="text-foreground text-sm font-medium"
      >
        {entry.varName}
      </label>
      <Input
        id={`env-${entry.varName}`}
        value={displayValue}
        onChange={entry.onValueChange}
        onFocus={() => setIsFocused(true)}
        onBlur={() => setIsFocused(false)}
        onKeyDown={onKeyDown}
        placeholder={
          hasExistingValue ? "Replace existing value" : "Enter value"
        }
        type={entry.isSensitive ? "password" : "text"}
        className="font-mono text-sm"
        disabled={disabled}
      />
    </div>
  );
}

export function EnvironmentEntriesFormFields({
  entries,
  relevantEnvVars,
  disabled,
  onKeyDown,
}: EnvironmentEntriesFormFieldsProps) {
  if (relevantEnvVars.length === 0) {
    return (
      <div className="py-8 text-center">
        <p className="text-muted-foreground text-sm">
          No authentication required for this MCP server
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {entries.map((entry) => (
        <EnvironmentEntryInput
          key={entry.varName}
          entry={entry}
          disabled={disabled}
          onKeyDown={onKeyDown}
        />
      ))}
    </div>
  );
}
