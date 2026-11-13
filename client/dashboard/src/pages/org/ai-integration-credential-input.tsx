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
        noAutofill
        disabled={disabled}
      />
    );
  }
  // Pasted JSON needs a textarea, which has no password mode. Input's
  // className styles its wrapper, so the mask targets the textarea inside it.
  return (
    <Input
      id={id}
      placeholder={placeholder}
      value={value}
      onChange={onChange}
      lines={8}
      noAutofill
      className="font-mono [&_textarea]:[-webkit-text-security:disc]"
      disabled={disabled}
    />
  );
}
