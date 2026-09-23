import { AnyField } from "@/components/moon/any-field";
import { InputField } from "@/components/moon/input-field";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { TextArea } from "@/components/ui/Textarea";
import type { SigintSignal } from "@gram/client/models/components/sigintsignal.js";
import { useState, type FormEvent } from "react";

export interface SignalDraft {
  name: string;
  description: string;
  classifierCriteria: string;
}

interface SignalEditorDialogProps {
  open: boolean;
  canWrite: boolean;
  signal: SigintSignal | null;
  pending: boolean;
  error: string | null;
  onOpenChange: (open: boolean) => void;
  onSave: (draft: SignalDraft) => void;
}

function nameError(name: string): string | null {
  const length = Array.from(name.trim()).length;
  if (length === 0) return "Enter a name.";
  if (length > 200) return "Use 200 characters or fewer.";
  return null;
}

export function SignalEditorDialog({
  open,
  canWrite,
  signal,
  pending,
  error,
  onOpenChange,
  onSave,
}: SignalEditorDialogProps): JSX.Element {
  const [name, setName] = useState(signal?.name ?? "");
  const [description, setDescription] = useState(signal?.description ?? "");
  const [classifierCriteria, setClassifierCriteria] = useState(
    signal?.classifierCriteria ?? "",
  );
  const [submitted, setSubmitted] = useState(false);
  const validationError = nameError(name);

  const submit = (event: FormEvent<HTMLFormElement>): void => {
    event.preventDefault();
    setSubmitted(true);
    if (!canWrite || pending || validationError) return;
    onSave({
      name: name.trim(),
      description,
      classifierCriteria,
    });
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!pending) onOpenChange(next);
      }}
    >
      <Dialog.Content className="max-h-[90vh] overflow-y-auto sm:max-w-xl">
        <Dialog.Header>
          <Dialog.Title>
            {signal ? "Edit signal" : "Create signal"}
          </Dialog.Title>
          <Dialog.Description>
            Define reusable classification criteria. This only saves
            configuration; it does not run a classifier.
          </Dialog.Description>
        </Dialog.Header>
        <form onSubmit={submit} className="space-y-5">
          <InputField
            label="Name"
            value={name}
            onChange={(event) => setName(event.target.value)}
            required
            autoFocus
            error={submitted ? validationError : null}
            hint="A trimmed name between 1 and 200 Unicode characters."
          />
          <AnyField
            label="Description"
            hint="Optional context for people who reuse this signal."
            render={(props) => (
              <TextArea
                {...props}
                value={description}
                onChange={setDescription}
                rows={3}
              />
            )}
          />
          <AnyField
            label="Classifier criteria"
            hint="Describe the content that should match this signal."
            render={(props) => (
              <TextArea
                {...props}
                value={classifierCriteria}
                onChange={setClassifierCriteria}
                rows={6}
              />
            )}
          />
          {error ? <Alert variant="error">{error}</Alert> : null}
          <Dialog.Footer>
            <Button
              type="button"
              variant="secondary"
              onClick={() => onOpenChange(false)}
              disabled={pending}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={
                !canWrite || pending || (submitted && !!validationError)
              }
            >
              {pending ? "Saving…" : "Save signal"}
            </Button>
          </Dialog.Footer>
        </form>
      </Dialog.Content>
    </Dialog>
  );
}
