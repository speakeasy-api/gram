import { Input } from "@/components/ui/Input";
import { Text } from "@/components/ui/Text";
import { TextArea } from "@/components/ui/Textarea";
import { useId } from "react";

import type { GitHubAppDraft } from "../model/githubApp";

export function GitHubAppFields({
  value,
  onChange,
  disabled = false,
}: {
  value: GitHubAppDraft;
  onChange: (value: GitHubAppDraft) => void;
  disabled?: boolean;
}): JSX.Element {
  const id = useId();
  return (
    <div className="space-y-3">
      <Text small muted className="block">
        Install your GitHub App on the repositories it needs, then provide its
        credentials. Speakeasy renews installation tokens automatically. No
        OAuth authorization server is needed. Saving replaces this server’s
        upstream identity; existing connections must reconnect.
      </Text>
      {(
        [
          ["instanceUrl", "GitHub instance URL", "https://github.com"],
          ["appId", "App ID", "123456"],
          ["installationId", "Installation ID", "12345678"],
        ] as const
      ).map(([field, label, placeholder]) => (
        <div key={field} className="space-y-1">
          <label htmlFor={`${id}-${field}`} className="text-sm">
            {label}
          </label>
          <Input
            id={`${id}-${field}`}
            value={value[field]}
            onChange={(next) => onChange({ ...value, [field]: next })}
            placeholder={placeholder}
            disabled={disabled}
            noAutofill
          />
        </div>
      ))}
      <div className="space-y-1">
        <label htmlFor={`${id}-key`} className="text-sm">
          Private key (PEM)
        </label>
        <TextArea
          noAutofill
          id={`${id}-key`}
          value={value.privateKey}
          onChange={(privateKey) => onChange({ ...value, privateKey })}
          disabled={disabled}
          placeholder="Paste the GitHub App private key"
          rows={4}
        />
      </div>
      <Text small muted className="block">
        The key is encrypted and never returned. To rotate it, paste a
        replacement key. Enterprise instances must be reachable over HTTPS with
        a trusted certificate.
      </Text>
    </div>
  );
}
