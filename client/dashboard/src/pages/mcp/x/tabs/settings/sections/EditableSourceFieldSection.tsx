import {
  FooterSaveButton,
  SettingsSection,
} from "@/components/detail/settings-section";
import { RequireScope } from "@/components/require-scope";
import { Field, FieldError, FieldLabel } from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import { useEffect, useState, type ReactNode } from "react";
import { toast } from "sonner";

export type SaveConfirmationProps = {
  value: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
  isPending: boolean;
  errorMessage: string | undefined;
};

export const STORED_VALUE_CHANGED_MESSAGE =
  "This setting changed since you opened the confirmation, so nothing was saved. Review the current value and save again.";

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
  confirmSave,
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
  /**
   * Renders a confirmation step between Save and the write, for fields whose
   * change reaches beyond this server. It receives the value frozen at the
   * moment Save was pressed.
   */
  confirmSave?: (props: SaveConfirmationProps) => ReactNode;
}): JSX.Element {
  const [draft, setDraft] = useState(stored);
  const [error, setError] = useState<string>();
  const [saving, setSaving] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  // The value awaiting confirmation and the stored value it was requested
  // against. Kept after the confirmation closes so it does not change while
  // the dialog animates out.
  const [pending, setPending] = useState({ value: "", base: "" });

  // Re-sync when the upstream value changes so a stale draft doesn't survive
  // an edit from another tab or a refetch.
  useEffect(() => {
    setDraft(stored);
  }, [stored]);

  const dirty = draft.trim() !== stored.trim();
  const saveDisabled =
    !dirty || (requireValue && draft.trim() === "") || saving;

  const handleSave = async (value: string): Promise<boolean> => {
    setSaving(true);
    setError(undefined);
    try {
      // Adopt the stored form the server normalized to. Refetching alone
      // leaves the draft dirty whenever normalization is a no-op server-side.
      setDraft(await save(value));
      toast.success(toastMessage(value === ""));
      return true;
    } catch (err) {
      const message = err instanceof Error ? err.message : fallbackError;
      setError(message);
      toast.error(message);
      return false;
    } finally {
      setSaving(false);
    }
  };

  const requestSave = () => {
    const value = draft.trim();
    if (confirmSave) {
      setError(undefined);
      setPending({ value, base: stored });
      setConfirmOpen(true);
      return;
    }
    void handleSave(value);
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
              projectId={projectId}
              level="component"
            >
              <FooterSaveButton
                pending={saving}
                disabled={saveDisabled}
                onClick={requestSave}
              />
            </RequireScope>
          </SettingsSection.FooterActions>
        </SettingsSection.Footer>
      </SettingsSection.Panel>
      {confirmSave
        ? confirmSave({
            value: pending.value,
            open: confirmOpen,
            onOpenChange: (open) => {
              if (!open && !saving) setConfirmOpen(false);
            },
            onConfirm: () => {
              // The setting changed underneath the confirmation, e.g. from
              // another tab: saving now would overwrite a value never shown.
              if (stored !== pending.base) {
                setConfirmOpen(false);
                setError(STORED_VALUE_CHANGED_MESSAGE);
                return;
              }
              void handleSave(pending.value).then((saved) => {
                if (saved) setConfirmOpen(false);
              });
            },
            isPending: saving,
            errorMessage: error,
          })
        : null}
    </SettingsSection>
  );
}
