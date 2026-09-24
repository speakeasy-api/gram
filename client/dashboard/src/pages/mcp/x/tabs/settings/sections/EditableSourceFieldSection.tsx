import {
  FooterSaveButton,
  SettingsSection,
} from "@/components/detail/settings-section";
import { RequireScope } from "@/components/require-scope";
import { Field, FieldError, FieldLabel } from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import { useEffect, useState, type ReactNode } from "react";
import { toast } from "sonner";

// One settings section editing a single text field of the source behind an
// MCP server. Owns its own draft, error, and pending state so sibling sections
// never reflect each other's activity. Ported from the retired tunneled source
// page and generalized so the remote source's fields use it too.
export function EditableSourceFieldSection({
  id,
  title,
  description,
  label,
  placeholder,
  stored,
  projectId,
  requireValue = false,
  footerHint,
  save,
  toastMessage,
  fallbackError,
}: {
  id: string;
  title: string;
  description: ReactNode;
  label: string;
  placeholder: string;
  stored: string;
  // The source's own project, so the gate matches what saving will target.
  projectId: string;
  requireValue?: boolean;
  footerHint?: string;
  // Persists the trimmed draft and returns the canonical stored value.
  save: (value: string) => Promise<string>;
  toastMessage: (cleared: boolean) => string;
  fallbackError: string;
}): JSX.Element {
  const [draft, setDraft] = useState(stored);
  const [error, setError] = useState<string>();
  const [saving, setSaving] = useState(false);

  // Re-sync when the upstream value changes so a stale draft doesn't survive
  // an edit from another tab or a refetch.
  useEffect(() => {
    setDraft(stored);
  }, [stored]);

  const dirty = draft.trim() !== stored.trim();
  const saveDisabled =
    !dirty || (requireValue && draft.trim() === "") || saving;

  const handleSave = async () => {
    const value = draft.trim();
    setSaving(true);
    setError(undefined);
    try {
      // Adopt the stored form the server normalized to. Refetching alone
      // leaves the draft dirty whenever normalization is a no-op server-side.
      setDraft(await save(value));
      toast.success(toastMessage(value === ""));
    } catch (err) {
      const message = err instanceof Error ? err.message : fallbackError;
      setError(message);
      toast.error(message);
    } finally {
      setSaving(false);
    }
  };

  return (
    <SettingsSection id={id}>
      <SettingsSection.Header>
        <SettingsSection.Title>{title}</SettingsSection.Title>
        <SettingsSection.Description>{description}</SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <Field
            data-invalid={error !== undefined ? true : undefined}
            className="max-w-md"
          >
            <FieldLabel htmlFor={`${id}-input`}>{label}</FieldLabel>
            <Input
              id={`${id}-input`}
              value={draft}
              onChange={(value) => setDraft(value)}
              placeholder={placeholder}
              disabled={saving}
              aria-invalid={error !== undefined}
            />
            {error !== undefined && <FieldError>{error}</FieldError>}
          </Field>
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>{footerHint}</SettingsSection.FooterHint>
          <SettingsSection.FooterActions>
            <RequireScope
              scope="mcp:write"
              resourceId={projectId}
              level="component"
            >
              <FooterSaveButton
                pending={saving}
                disabled={saveDisabled}
                onClick={() => void handleSave()}
              />
            </RequireScope>
          </SettingsSection.FooterActions>
        </SettingsSection.Footer>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
