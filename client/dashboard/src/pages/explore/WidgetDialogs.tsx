import { Button } from "@/components/ui/Button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { Icon } from "@/components/ui/Icon";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { TextArea } from "@/components/ui/Textarea";
import type { WidgetDashboard } from "@gram/client/models/components/widgetdashboard.js";
import { useState, type FormEvent, type JSX, type ReactNode } from "react";
import { describeDashboards } from "./widgetUsage";
import { MAX_NAME_LENGTH } from "./widgetNames";

const MAX_DESCRIPTION_LENGTH = 2000;

/** What the details dialog submits. */
export interface Details {
  name: string;
  /** Omitted when left blank. */
  description?: string | undefined;
}

/** What a details dialog names, so its copy fits the thing being named. */
export interface DetailsSubject {
  /** "Widget" or "Dashboard": how the fields are labelled. */
  noun: string;
  /** Under the title: who sees the thing, and what its name is for. */
  blurb: string;
  /** A name to suggest, as the name field's placeholder. */
  example: string;
}

interface DetailsDialogProps {
  open: boolean;
  title: string;
  confirm: string;
  initial: Details;
  pending: boolean;
  /** Controls under an Advanced disclosure, for what few need to set. */
  advanced?: ReactNode;
  onCancel: () => void;
  onSubmit: (details: Details) => void;
}

const WIDGET_SUBJECT: DetailsSubject = {
  noun: "Widget",
  blurb:
    "Widgets are shared with everyone in this project. Names need not be unique; each shows who saved it.",
  example: "Cost by model",
};

/**
 * Names a widget and says what it is for: used to save one, to save the
 * builder as a new one, and to rename one. Remount it per opening (key it) so
 * the fields start from the right values.
 */
export function WidgetDetailsDialog(props: DetailsDialogProps): JSX.Element {
  return <DetailsDialog {...props} subject={WIDGET_SUBJECT} />;
}

/** Names something saved to the project, and says what it is for. */
export function DetailsDialog({
  open,
  title,
  confirm,
  subject,
  initial,
  pending,
  advanced,
  onCancel,
  onSubmit,
}: DetailsDialogProps & { subject: DetailsSubject }): JSX.Element {
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
            <Dialog.Description>{subject.blurb}</Dialog.Description>
          </Dialog.Header>
          <Input
            value={name}
            onChange={setName}
            maxLength={MAX_NAME_LENGTH}
            placeholder={subject.example}
            aria-label={`${subject.noun} name`}
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
            aria-label={`${subject.noun} description`}
            rows={2}
          />
          {advanced ? (
            <Collapsible>
              <CollapsibleTrigger className="text-muted-foreground hover:text-foreground flex items-center gap-1 text-sm transition-colors [&[data-state=open]>svg]:rotate-90">
                <Icon
                  name="chevron-right"
                  className="size-4 transition-transform"
                />
                Advanced
              </CollapsibleTrigger>
              <CollapsibleContent className="pt-3">
                {advanced}
              </CollapsibleContent>
            </Collapsible>
          ) : null}
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

/**
 * Confirms deleting a widget, which removes it for everyone, and from every
 * dashboard it is on.
 */
export function DeleteWidgetDialog({
  name,
  dashboards,
  open,
  pending,
  onCancel,
  onConfirm,
}: {
  name: string;
  /** The dashboards the widget is on, whose cards go with it. */
  dashboards: WidgetDashboard[];
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
            It leaves this project's widgets for everyone.{" "}
            {dashboards.length > 0
              ? `Its card goes from ${describeDashboards(dashboards)} too. `
              : ""}
            Anything open in the builder stays on screen, as a link, until you
            move on.
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
