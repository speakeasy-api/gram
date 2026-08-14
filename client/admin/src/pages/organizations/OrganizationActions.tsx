import { MoreHorizontalIcon } from "lucide-react";
import {
  createContext,
  useContext,
  useId,
  useRef,
  useState,
  type FormEvent,
  type JSX,
  type ReactNode,
} from "react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import {
  errorMessage,
  MAX_TRIAL_EXTENSION_DAYS,
  MIN_TRIAL_EXTENSION_DAYS,
  type AdminOrganization,
} from "@/lib/gramAdminApi";

import {
  canExtendTrial,
  useDisableOrganization,
  useEnableOrganization,
  useExtendTrial,
} from "./rowActions";

// The trial length the rest of the system assumes, so the operator extending a
// trial by the usual amount types nothing.
const DEFAULT_EXTENSION_DAYS = 14;

const BOUNDS_HINT = `Enter a whole number of days between ${MIN_TRIAL_EXTENSION_DAYS} and ${MAX_TRIAL_EXTENSION_DAYS}.`;

/**
 * How these controls report a write.
 *
 * The list owns both surfaces a write can report on, the live region and the
 * failure banner, and these controls are drawn inside table cells the list
 * cannot pass props to, so they reach them through context the way the peek
 * controls do.
 *
 * The default is a no-op rather than a throw: the peek panel renders these
 * actions too and it is mounted on its own in tests.
 */
export type WriteReporter = {
  // Speaks. Every write ends in one of these, whether it succeeded or not.
  announce: (text: string) => void;
  // Shows, and only for a failure with no dialog of its own to report in.
  // `null` clears whatever is showing.
  showFailure: (text: string | null) => void;
};

const NO_REPORTER: WriteReporter = {
  announce: () => {},
  showFailure: () => {},
};

const WriteReportContext = createContext<WriteReporter>(NO_REPORTER);

export function WriteReportProvider({
  value,
  children,
}: {
  value: WriteReporter;
  children: ReactNode;
}): JSX.Element {
  return (
    <WriteReportContext.Provider value={value}>
      {children}
    </WriteReportContext.Provider>
  );
}

type OpenDialog = "disable" | "extend";

/**
 * Disable, re-enable and extend, wherever the record is on screen: the row
 * menu, the peek panel footer and the record header.
 *
 * One component for all three, because they are the same three actions
 * against the same record: two implementations would be two answers to
 * "can this trial be extended" and two confirmations to keep in step.
 *
 * `buttons` names the shape rather than the place. It was `footer` while the
 * peek panel was the only surface that drew it that way.
 */
export function OrganizationActions({
  org,
  layout,
}: {
  org: AdminOrganization;
  layout: "menu" | "buttons";
}): JSX.Element {
  const { announce, showFailure } = useContext(WriteReportContext);
  const [open, setOpen] = useState<OpenDialog>();

  // Held here rather than in the dialogs. A dialog unmounts as it closes, and
  // the write it started has to keep its cache update and its announcement.
  const disable = useDisableOrganization();
  const enable = useEnableOrganization();
  const extend = useExtendTrial();

  const isDisabled = Boolean(org.disabled_at);
  const busy = disable.isPending || enable.isPending || extend.isPending;

  const menuTrigger = useRef<HTMLButtonElement>(null);

  // Where the keyboard goes when a dialog closes. Radix restores focus to a
  // `DialogTrigger`, and these dialogs have none: its `onCloseAutoFocus`
  // cancels FocusScope's own restore and then focuses `triggerRef.current`,
  // which is null here, so without this every close drops the keyboard onto
  // `document.body`. The control that opened the dialog is still mounted on
  // all six exit paths, the peek footer button being the same node after the
  // write it started, so one handler covers success, Escape, Cancel, the
  // backdrop and the X.
  const openedFrom = useRef<HTMLElement | null>(null);

  const openDialog = (dialog: OpenDialog, from: HTMLElement | null): void => {
    openedFrom.current = from;
    // A failure the operator cancelled out of is not one the next attempt
    // should open holding.
    disable.reset();
    extend.reset();
    setOpen(dialog);
  };

  const restoreFocus = (event: Event): void => {
    const control = openedFrom.current;
    // A control that has left the page is not somewhere to put the keyboard.
    // Radix's own restore is no better in that case, so this leaves it alone.
    if (!control?.isConnected) return;
    event.preventDefault();
    control.focus();
  };

  const runDisable = (): void => {
    disable.mutate(org.id, {
      onSuccess: () => {
        setOpen(undefined);
        showFailure(null);
        announce(`${org.name} is disabled.`);
      },
      // The dialog stays open and carries the same failure, because that is
      // where the operator is looking.
      onError: (error) =>
        announce(`Could not disable ${org.name}: ${errorMessage(error)}`),
    });
  };

  const runEnable = (): void => {
    enable.mutate(org.id, {
      onSuccess: () => {
        showFailure(null);
        announce(`${org.name} is enabled.`);
      },
      // The one write with no dialog, so its failure is shown as well as
      // spoken. Without the banner the only account of it on the page is
      // inside a region no sighted operator reads.
      onError: (error) => {
        const text = `Could not re-enable ${org.name}: ${errorMessage(error)}`;
        announce(text);
        showFailure(text);
      },
    });
  };

  const runExtend = (days: number): void => {
    extend.mutate(
      { id: org.id, days },
      {
        onSuccess: () => {
          setOpen(undefined);
          showFailure(null);
          announce(`${org.name} trial extended by ${dayCount(days)}.`);
        },
        onError: (error) =>
          announce(
            `Could not extend the trial for ${org.name}: ${errorMessage(error)}`,
          ),
      },
    );
  };

  const dialogs = (
    <>
      {open === "disable" && (
        <ConfirmDisable
          org={org}
          pending={disable.isPending}
          failure={disable.error}
          onCancel={() => setOpen(undefined)}
          onCloseAutoFocus={restoreFocus}
          onConfirm={runDisable}
        />
      )}
      {open === "extend" && (
        <ExtendTrial
          org={org}
          pending={extend.isPending}
          failure={extend.error}
          onCancel={() => setOpen(undefined)}
          onCloseAutoFocus={restoreFocus}
          onSubmit={runExtend}
        />
      )}
    </>
  );

  // Everything this component draws is inside a table row that opens the
  // organization when it is clicked, and a portal's events travel up the React
  // tree rather than the DOM: a menu item, a dialog body and the dialog's own
  // backdrop all reach that handler from a portal at the end of the document.
  // None of them is a click on the row. `contents` keeps the box itself out of
  // both layouts.
  const contain = (children: ReactNode): JSX.Element => (
    <div className="contents" onClick={(event) => event.stopPropagation()}>
      {children}
    </div>
  );

  if (layout === "buttons") {
    return contain(
      <>
        {/* Named for the record, the same way the row menu trigger is. The
            panel's own name is the constant "Organization peek" and it swaps
            records under itself, so a bare "Disable" reaches a screen reader
            with nothing saying which organization it acts on.

            Live while a write is in flight, and marked busy instead. Disabling
            the control the operator just pressed drops the keyboard onto the
            body, and re-enabling is idempotent, so a second press costs a
            request and nothing else. */}
        {isDisabled ? (
          <Button
            variant="outline"
            size="xs"
            aria-label={`Re-enable ${org.name}`}
            aria-busy={busy}
            onClick={runEnable}
          >
            Re-enable
          </Button>
        ) : (
          <Button
            variant="outline"
            size="xs"
            aria-label={`Disable ${org.name}`}
            aria-busy={busy}
            onClick={(event) => openDialog("disable", event.currentTarget)}
          >
            Disable
          </Button>
        )}
        {canExtendTrial(org) && (
          <Button
            variant="outline"
            size="xs"
            aria-label={`Extend trial for ${org.name}`}
            aria-busy={busy}
            onClick={(event) => openDialog("extend", event.currentTarget)}
          >
            Extend trial
          </Button>
        )}
        {dialogs}
      </>,
    );
  }

  return contain(
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          {/* Named for the record, because a table full of identical "Actions"
              buttons tells a screen reader nothing about which row it is on. */}
          <Button
            ref={menuTrigger}
            variant="ghost"
            size="icon-xs"
            aria-label={`Actions for ${org.name}`}
            aria-busy={busy}
          >
            <MoreHorizontalIcon />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start">
          {isDisabled ? (
            <DropdownMenuItem onSelect={runEnable}>Re-enable</DropdownMenuItem>
          ) : (
            <DropdownMenuItem
              variant="destructive"
              // Opens the confirmation. The write waits for it: disabling cuts
              // a customer off, and this menu sits one row away from four
              // others.
              //
              // The trigger, not the item this fires on: the menu closes with
              // the dialog opening and takes the item down with it, and the
              // dialog has to give the keyboard back to something still on the
              // page.
              onSelect={() => openDialog("disable", menuTrigger.current)}
            >
              Disable
            </DropdownMenuItem>
          )}
          {canExtendTrial(org) && (
            <DropdownMenuItem
              onSelect={() => openDialog("extend", menuTrigger.current)}
            >
              Extend trial
            </DropdownMenuItem>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
      {dialogs}
    </>,
  );
}

function dayCount(days: number): string {
  return `${days} ${days === 1 ? "day" : "days"}`;
}

// A modal takes the rest of the page out of the accessibility tree, the live
// region included, so a failure the operator is looking at needs its own.
function Failure({
  id,
  children,
}: {
  id?: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <p id={id} role="alert" className="text-destructive text-sm">
      {children}
    </p>
  );
}

function ConfirmDisable({
  org,
  pending,
  failure,
  onCancel,
  onCloseAutoFocus,
  onConfirm,
}: {
  org: AdminOrganization;
  pending: boolean;
  failure: Error | null;
  onCancel: () => void;
  onCloseAutoFocus: (event: Event) => void;
  onConfirm: () => void;
}): JSX.Element {
  return (
    <Dialog
      open
      onOpenChange={(next) => {
        // Not while the write is in flight: the answer decides what the row
        // says next, and dismissing the dialog would leave the operator with
        // no account of how it ended.
        if (!next && !pending) onCancel();
      }}
    >
      <DialogContent onCloseAutoFocus={onCloseAutoFocus}>
        <DialogHeader>
          <DialogTitle>Disable {org.name}?</DialogTitle>
          <DialogDescription>
            Every member loses access to Gram until the organization is
            re-enabled.
          </DialogDescription>
        </DialogHeader>
        {failure && <Failure>{errorMessage(failure)}</Failure>}
        <DialogFooter>
          <Button
            variant="ghost"
            size="sm"
            disabled={pending}
            onClick={onCancel}
          >
            Cancel
          </Button>
          <Button
            variant="destructive"
            size="sm"
            disabled={pending}
            onClick={onConfirm}
          >
            {pending ? "Disabling..." : "Disable"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ExtendTrial({
  org,
  pending,
  failure,
  onCancel,
  onCloseAutoFocus,
  onSubmit,
}: {
  org: AdminOrganization;
  pending: boolean;
  failure: Error | null;
  onCancel: () => void;
  onCloseAutoFocus: (event: Event) => void;
  onSubmit: (days: number) => void;
}): JSX.Element {
  const { announce } = useContext(WriteReportContext);
  const [days, setDays] = useState(String(DEFAULT_EXTENSION_DAYS));
  const [rejected, setRejected] = useState(false);
  const fieldID = useId();
  const messageID = useId();

  const submit = (event: FormEvent): void => {
    event.preventDefault();
    const parsed = Number(days);
    // The server's own bounds, refused here so a request that cannot succeed
    // never leaves the browser. A whole number, because the interval the
    // server adds is a count of days.
    if (
      !Number.isInteger(parsed) ||
      parsed < MIN_TRIAL_EXTENSION_DAYS ||
      parsed > MAX_TRIAL_EXTENSION_DAYS
    ) {
      setRejected(true);
      // Spoken as well as shown, because showing it a second time shows
      // nothing: the state and the text are both unchanged, so the same press
      // repeated leaves the DOM untouched and a `role="alert"` announces only
      // what is inserted or changed. The live region alternates a zero-width
      // space, so it speaks every time.
      announce(`Could not extend the trial for ${org.name}: ${BOUNDS_HINT}`);
      return;
    }
    setRejected(false);
    onSubmit(parsed);
  };

  return (
    <Dialog
      open
      onOpenChange={(next) => {
        if (!next && !pending) onCancel();
      }}
    >
      <DialogContent onCloseAutoFocus={onCloseAutoFocus}>
        {/* noValidate, so a day count outside the bounds is refused in one
            place and reported in one voice. The browser's own bubble says
            something different in every browser, disappears on the next key
            and is not part of the accessibility tree. */}
        <form noValidate onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>Extend the trial for {org.name}?</DialogTitle>
            {/* Radix points the dialog's description at this, so the bounds
                are announced with the title rather than only after a refusal. */}
            <DialogDescription>
              The days are added to the date the trial ends on now, not to
              today. {BOUNDS_HINT}
            </DialogDescription>
          </DialogHeader>

          <div className="my-4 flex items-center gap-2">
            <label htmlFor={fieldID} className="text-sm">
              Days
            </label>
            <Input
              id={fieldID}
              type="number"
              min={MIN_TRIAL_EXTENSION_DAYS}
              max={MAX_TRIAL_EXTENSION_DAYS}
              step={1}
              value={days}
              disabled={pending}
              aria-invalid={rejected}
              // Pointed at whichever message is under the field. Without it a
              // user who tabs back to the input is told it is invalid and not
              // what would make it valid: the bounds are in the dialog
              // description and in the alert, and neither is the field's.
              aria-describedby={rejected || failure ? messageID : undefined}
              onChange={(event) => setDays(event.target.value)}
              className="w-24"
            />
          </div>

          {/* One at a time. The bounds refusal is the newer of the two and it
              is about the value now in the field, so a stale server failure
              underneath it would give the operator two reasons and no way to
              tell which one the next press answers. */}
          {rejected && <Failure id={messageID}>{BOUNDS_HINT}</Failure>}
          {!rejected && failure && (
            <Failure id={messageID}>{errorMessage(failure)}</Failure>
          )}

          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              disabled={pending}
              onClick={onCancel}
            >
              Cancel
            </Button>
            <Button type="submit" size="sm" disabled={pending}>
              {pending ? "Extending..." : "Extend"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
