import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Icon } from "@/components/ui/Icon";
import { Input } from "@/components/ui/Input";
import type { Widget } from "@gram/client/models/components/widget.js";
import { useState, type JSX } from "react";
import {
  DetailsDialog,
  type Details,
  type DetailsSubject,
} from "./WidgetDialogs";

const DASHBOARD_SUBJECT: DetailsSubject = {
  noun: "Dashboard",
  blurb:
    "Dashboards are shared with everyone in this project. Names need not be unique; each shows who made it.",
  example: "Agent activity",
};

/**
 * Names a dashboard and says what it is for: used to make one and to rename
 * one. Remount it per opening (key it) so the fields start from the right
 * values.
 */
export function DashboardDetailsDialog(props: {
  open: boolean;
  title: string;
  confirm: string;
  initial: Details;
  pending: boolean;
  onCancel: () => void;
  onSubmit: (details: Details) => void;
}): JSX.Element {
  return <DetailsDialog {...props} subject={DASHBOARD_SUBJECT} />;
}

/** Confirms deleting a dashboard, which removes it for everyone. */
export function DeleteDashboardDialog({
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
            It leaves this project's dashboards for everyone. The widgets on it
            stay saved; only this layout of them goes.
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
 * Picks one of the project's saved widgets to place on a dashboard. The
 * same widget may be placed more than once, so nothing is greyed out.
 */
export function AddWidgetDialog({
  open,
  widgets,
  pending,
  onCancel,
  onAdd,
}: {
  open: boolean;
  widgets: Widget[];
  pending: boolean;
  onCancel: () => void;
  onAdd: (widget: Widget) => void;
}): JSX.Element {
  const [search, setSearch] = useState("");
  const needle = search.trim().toLowerCase();
  const matches = widgets.filter(
    (widget) => needle === "" || widget.name.toLowerCase().includes(needle),
  );
  let list: JSX.Element;
  if (widgets.length === 0) {
    list = (
      <p className="text-muted-foreground text-sm">
        No widgets saved yet. Build a question in Explore and save it as a
        widget first.
      </p>
    );
  } else if (matches.length === 0) {
    list = <p className="text-muted-foreground text-sm">No widgets match.</p>;
  } else {
    list = (
      <ul className="border-border max-h-80 divide-y overflow-y-auto border">
        {matches.map((widget) => (
          <li key={widget.id}>
            <button
              type="button"
              className="hover:bg-muted flex w-full items-center justify-between gap-3 px-3 py-2 text-left disabled:opacity-50"
              disabled={pending}
              onClick={() => onAdd(widget)}
            >
              <span className="flex min-w-0 flex-col">
                <span className="truncate text-sm font-medium">
                  {widget.name}
                </span>
                <span className="text-muted-foreground truncate text-xs">
                  {widget.dataset}
                  {widget.description ? ` · ${widget.description}` : ""}
                </span>
              </span>
              <Icon
                name="plus"
                className="text-muted-foreground size-4 shrink-0"
                aria-hidden
              />
            </button>
          </li>
        ))}
      </ul>
    );
  }
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next && !pending) onCancel();
      }}
    >
      <Dialog.Content closeable={!pending}>
        <Dialog.Header>
          <Dialog.Title>Add a widget</Dialog.Title>
          <Dialog.Description>
            Any of this project's saved widgets. A widget is linked, not copied:
            editing it changes it on every dashboard it is on.
          </Dialog.Description>
        </Dialog.Header>
        <Input
          value={search}
          onChange={setSearch}
          placeholder="Search widgets"
          aria-label="Search widgets"
          autoFocus
        />
        {list}
      </Dialog.Content>
    </Dialog>
  );
}
