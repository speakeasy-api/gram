import { CopyButton } from "@/components/ui/copy-button";
import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { TextArea } from "@/components/ui/textarea";
import { Type } from "@/components/ui/type";
import { useTelemetry } from "@/contexts/Telemetry";
import { useUpdateEnvironmentMutation } from "@gram/client/react-query/updateEnvironment.js";
import { Button } from "@speakeasy-api/moonshine";
import { AlertCircle } from "lucide-react";
import { useEffect, useState } from "react";

const NAME_PATTERN = /^[-_.a-zA-Z][-_.a-zA-Z0-9]*$/;

export interface EnvironmentVariableDraft {
  name: string;
  value: string;
  isSecret: boolean;
}

interface EnvironmentVariableDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  environmentSlug: string;
  /** Omitted when creating a new variable. */
  entry?: EnvironmentVariableDraft;
  /**
   * False when `entry` names a variable this environment does not store yet, as
   * with an MCP variable advertised by a toolset but never given a value. The
   * name is still fixed, but there is no stored value to preserve, so one has
   * to be sent.
   */
  entryStored?: boolean;
  /** Names already in use, so create mode can reject duplicates. */
  existingNames: string[];
  onSaved: () => void;
}

function validateName(name: string, existingNames: string[]): string | null {
  if (name.length === 0) return "Enter a variable name.";
  if (!NAME_PATTERN.test(name)) {
    return "Name must start with a letter, underscore, dash, or period and contain only letters, numbers, underscores, dashes, or periods.";
  }
  if (existingNames.includes(name)) return `${name} already exists.`;
  return null;
}

export function EnvironmentVariableDialog({
  open,
  onOpenChange,
  environmentSlug,
  entry,
  entryStored = true,
  existingNames,
  onSaved,
}: EnvironmentVariableDialogProps): JSX.Element {
  const telemetry = useTelemetry();
  const isEdit = entry !== undefined;
  // Whether there is a stored value behind this entry. Only a stored value can
  // be preserved by omitting one, or protected by demanding a replacement.
  const isStored = isEdit && entryStored;

  const [name, setName] = useState("");
  const [value, setValue] = useState("");
  const [valueDirty, setValueDirty] = useState(false);
  const [isSecret, setIsSecret] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Reseed the form each time the dialog opens so it never shows a previous
  // variable's draft. A stored secret is never revealed, so its value field
  // starts empty and an untouched field means "keep the stored value".
  // Keyed on the entry's fields rather than the object: callers pass fresh
  // literals, and reseeding on reference identity would wipe the draft on any
  // parent rerender.
  const entryName = entry?.name;
  const entryValue = entry?.value;
  const entryIsSecret = entry?.isSecret;
  useEffect(() => {
    if (!open) return;
    setName(entryName ?? "");
    setValue(entryIsSecret === false ? (entryValue ?? "") : "");
    setValueDirty(false);
    setIsSecret(entryIsSecret ?? true);
    setError(null);
  }, [open, entryName, entryValue, entryIsSecret]);

  const { mutate: updateEnvironment, isPending: isSaving } =
    useUpdateEnvironmentMutation({
      onSuccess: () => {
        telemetry.capture("environment_event", {
          action: "environment_updated",
        });
        onSaved();
        onOpenChange(false);
      },
      onError: (err) => {
        console.error("Environment variable save failed:", err?.message || err);
        setError("Failed to save the variable. Please try again.");
      },
    });

  // Revealing a stored secret is impossible, so turning secrecy off has to come
  // with a replacement value. The server enforces this too. A variable with
  // nothing stored has no secret to protect, so the plain "a readable value
  // cannot be empty" rule below covers it instead.
  const needsNewValue = isStored && entry.isSecret && !isSecret;
  const trimmedValue = value.trim();

  const handleSave = () => {
    const trimmedName = isEdit ? entry.name : name.trim();

    if (!isEdit) {
      const nameError = validateName(trimmedName, existingNames);
      if (nameError) {
        setError(nameError);
        return;
      }
    }

    if (needsNewValue && trimmedValue === "") {
      setError(`Enter a new value to make ${trimmedName} readable.`);
      return;
    }

    // A non-secret value is stored as plaintext, and the column rejects an
    // empty string. Secret entries encrypt first, so empty is allowed there.
    // Omitting the value only means "keep what is stored", so an entry with
    // nothing stored must always send one or the server rejects the write.
    const willSendValue = !isStored || valueDirty || needsNewValue;
    if (!isSecret && willSendValue && trimmedValue === "") {
      setError("Enter a value, or turn on Secret to store it empty.");
      return;
    }

    updateEnvironment({
      request: {
        slug: environmentSlug,
        updateEnvironmentRequestBody: {
          entriesToUpdate: [
            {
              name: trimmedName,
              // Omitting the value on an existing entry preserves the stored
              // one, which is the only way to keep a secret you cannot read.
              ...(willSendValue ? { value } : {}),
              isSecret,
            },
          ],
          entriesToRemove: [],
        },
      },
    });
  };

  const title = getTitle(entry, isStored);
  const showCopy = isEdit && !entry.isSecret && !valueDirty && value !== "";

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>{title}</Dialog.Title>
          <Dialog.Description>
            {getDescription(isEdit, isStored)}
          </Dialog.Description>
        </Dialog.Header>

        <div className="grid gap-4 py-2">
          <div className="grid gap-2">
            <Type className="text-sm font-medium">Key</Type>
            {isEdit ? (
              <Type className="text-muted-foreground font-mono text-sm break-all">
                {entry.name}
              </Type>
            ) : (
              <Input
                value={name}
                onChange={(next) => setName(next.toUpperCase())}
                placeholder="NAME"
                className="w-full font-mono text-sm"
                disabled={isSaving}
                autoFocus
              />
            )}
          </div>

          <div className="grid gap-2">
            <div className="flex items-center justify-between">
              <Type className="text-sm font-medium">Value</Type>
              {showCopy && (
                <CopyButton text={value} tooltip={`Copy ${entry.name}`} />
              )}
            </div>
            <TextArea
              value={value}
              onChange={(next) => {
                setValue(next);
                setValueDirty(true);
                if (error) setError(null);
              }}
              placeholder={getValuePlaceholder(entry, needsNewValue, isStored)}
              className="font-mono text-sm"
              disabled={isSaving}
            />
            {isStored && entry.isSecret && !needsNewValue && (
              <Type className="text-muted-foreground text-xs">
                Leave blank to keep the current value. Secret values are never
                revealed.
              </Type>
            )}
            {needsNewValue && (
              <Type className="text-muted-foreground text-xs">
                Secret values are never revealed, so making {entry.name}{" "}
                readable requires a new value.
              </Type>
            )}
          </div>

          <label className="flex cursor-pointer items-start justify-between gap-4">
            <span className="min-w-0">
              <Type className="text-sm font-medium">Secret</Type>
              <Type className="text-muted-foreground text-xs">
                Secret values are encrypted and shown only as a redacted
                preview. Turn this off to keep the value readable.
              </Type>
            </span>
            <Switch
              checked={isSecret}
              onCheckedChange={(checked) => {
                setIsSecret(checked);
                if (error) setError(null);
              }}
              disabled={isSaving}
              aria-label="Secret"
              className="shrink-0"
            />
          </label>

          {error && (
            <div
              className="text-destructive flex items-center gap-2 text-sm"
              role="alert"
            >
              <AlertCircle className="h-4 w-4" aria-hidden="true" />
              {error}
            </div>
          )}
        </div>

        <Dialog.Footer>
          <Button
            variant="tertiary"
            onClick={() => onOpenChange(false)}
            disabled={isSaving}
          >
            Cancel
          </Button>
          <Button onClick={handleSave} disabled={isSaving}>
            {isSaving ? "Saving..." : "Save"}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

// Variable names run long enough to wrap a heading onto two lines (MCP header
// variables especially), and the Key field below spells the name out anyway.
function getTitle(
  entry: EnvironmentVariableDraft | undefined,
  isStored: boolean,
): string {
  if (!entry) return "Add Variable";
  return isStored ? "Edit Variable" : "Set Variable";
}

function getDescription(isEdit: boolean, isStored: boolean): string {
  if (!isEdit) return "Add an environment variable to this environment.";
  if (!isStored) {
    return "Give this variable a value, and choose whether it is secret.";
  }
  return "Update the value or change whether this variable is secret.";
}

// The note under the field already says a blank field keeps a stored secret, so
// the placeholder does not repeat the sentence back.
function getValuePlaceholder(
  entry: EnvironmentVariableDraft | undefined,
  needsNewValue: boolean,
  isStored: boolean,
): string {
  if (needsNewValue) return "Enter a new value";
  if (isStored && entry?.isSecret) return "Leave blank to keep";
  return "Enter value";
}
