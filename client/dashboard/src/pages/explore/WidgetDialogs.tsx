import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { TextArea } from "@/components/ui/Textarea";
import { useState, type FormEvent, type JSX } from "react";
import { MAX_NAME_LENGTH } from "./widgetNames";

const MAX_DESCRIPTION_LENGTH = 2000;

/** What the details dialog submits. */
export interface WidgetDetails {
  name: string;
  /** Omitted when left blank. */
  description?: string | undefined;
}

/**
 * Names a widget and says what it is for: used to save one, to save the
 * builder as a new one, and to rename one. Remount it per opening (key it) so
 * the fields start from the right values.
 */
export function WidgetDetailsDialog({
  open,
  title,
  confirm,
  initial,
  pending,
  onCancel,
  onSubmit,
}: {
  open: boolean;
  title: string;
  confirm: string;
  initial: WidgetDetails;
  pending: boolean;
  onCancel: () => void;
  onSubmit: (details: WidgetDetails) => void;
}): JSX.Element {
  const [name, setName] = useState(initial.name);
  const [description, setDescription] = useState(initial.description ?? "");
  const trimmed = name.trim();
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (trimmed === "") return;
    const about = description.trim();
    onSubmit({ name: trimmed, description: about === "" ? undefined : about });
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next && !pending) onCancel();
      }}
    >
      <Dialog.Content closeable={!pending}>
        <form onSubmit={submit} className="flex flex-col gap-4">
          <Dialog.Header>
            <Dialog.Title>{title}</Dialog.Title>
            <Dialog.Description>
              Widgets are shared with everyone in this project. Names need not
              be unique; each shows who saved it.
            </Dialog.Description>
          </Dialog.Header>
          <Input
            value={name}
            onChange={setName}
            maxLength={MAX_NAME_LENGTH}
            placeholder="Cost by model"
            aria-label="Widget name"
            autoFocus
          />
          <TextArea
            value={description}
            onChange={(value) =>
              // The service counts code points, not UTF-16 units.
              setDescription(
                Array.from(value).slice(0, MAX_DESCRIPTION_LENGTH).join(""),
              )
            }
            placeholder="What it is for (optional)"
            aria-label="Widget description"
            rows={2}
          />
          <Dialog.Footer>
            <Button
              type="button"
              variant="tertiary"
              onClick={onCancel}
              disabled={pending}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              variant="primary"
              disabled={pending || trimmed === ""}
            >
              {confirm}
            </Button>
          </Dialog.Footer>
        </form>
      </Dialog.Content>
    </Dialog>
  );
}

/** Confirms deleting a widget, which removes it for everyone. */
export function DeleteWidgetDialog({
  name,
  open,
  pending,
  onCancel,
  onConfirm,
}: {
  name: string;
  open: boolean;
  pending: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}): JSX.Element {
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next && !pending) onCancel();
      }}
    >
      <Dialog.Content closeable={!pending}>
        <Dialog.Header>
          <Dialog.Title>Delete “{name}”?</Dialog.Title>
          <Dialog.Description>
            It leaves this project's widgets for everyone. Anything open in the
            builder stays on screen, as a link, until you move on.
          </Dialog.Description>
        </Dialog.Header>
        <Dialog.Footer>
          <Button variant="tertiary" onClick={onCancel} disabled={pending}>
            Cancel
          </Button>
          <Button
            variant="destructive-primary"
            onClick={onConfirm}
            disabled={pending}
          >
            Delete
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

/**
 * Confirms leaving the open widget while the builder holds edits not yet
 * saved to it.
 */
export function DiscardChangesDialog({
  name,
  open,
  onCancel,
  onConfirm,
}: {
  name: string;
  open: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}): JSX.Element {
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) onCancel();
      }}
    >
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Discard unsaved changes to “{name}”?</Dialog.Title>
          <Dialog.Description>
            The builder has edits not saved to this widget. Opening another
            replaces them.
          </Dialog.Description>
        </Dialog.Header>
        <Dialog.Footer>
          <Button variant="tertiary" onClick={onCancel}>
            Cancel
          </Button>
          <Button variant="destructive-primary" onClick={onConfirm}>
            Discard and open
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

// The editor's usual mark for work not yet saved.
export function UnsavedDot({
  label,
  tooltip,
}: {
  label: string;
  tooltip: string;
}): JSX.Element {
  return (
    <SimpleTooltip tooltip={tooltip}>
      <span
        role="img"
        aria-label={label}
        className="bg-warning-default size-2 shrink-0 self-center rounded-full"
      />
    </SimpleTooltip>
  );
}
