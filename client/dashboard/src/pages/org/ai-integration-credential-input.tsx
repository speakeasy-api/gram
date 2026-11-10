import { Input } from "@/components/ui/Input";
import type { AIIntegrationProvider } from "./ai-integration-providers";

export function AIIntegrationCredentialInput({
  id,
  provider,
  value,
  onChange,
  hasSavedKey,
  disabled,
}: {
  id: string;
  provider: AIIntegrationProvider;
  value: string;
  onChange: (value: string) => void;
  hasSavedKey: boolean;
  disabled: boolean;
}): JSX.Element {
  const placeholder = hasSavedKey
    ? "•••••• (saved)"
    : provider.apiKeyPlaceholder;
  if (!provider.apiKeyMultiline) {
    return (
      <Input
        id={id}
        placeholder={placeholder}
        value={value}
        onChange={onChange}
        type="password"
        disabled={disabled}
      />
    );
  }
  return (
    <textarea
      id={id}
      placeholder={placeholder}
      value={value}
      onChange={(event) => onChange(event.target.value)}
      rows={8}
      autoComplete="off"
      autoCorrect="off"
      spellCheck={false}
      data-1p-ignore
      data-lpignore="true"
      className="border-input bg-background w-full rounded-md border px-3 py-2 font-mono text-sm [-webkit-text-security:disc]"
      disabled={disabled}
    />
  );
}
