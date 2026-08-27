import { CalendarIcon, MoreHorizontalIcon } from "lucide-react";
import {
  useContext,
  useId,
  useRef,
  useState,
  type FormEvent,
  type JSX,
  type ReactNode,
} from "react";

import { Button } from "@/components/ui/button";
import { Calendar } from "@/components/ui/calendar";
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
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  errorMessage,
  MAX_TRIAL_EXTENSION_DAYS,
  MAX_TRIAL_REARM_DAYS,
  MAX_TRIAL_START_DAYS,
  MIN_TRIAL_EXTENSION_DAYS,
  MIN_TRIAL_REARM_DAYS,
  MIN_TRIAL_START_DAYS,
  type AdminOrganization,
} from "@/lib/gramAdminApi";
import {
  calendarDate,
  dayISO,
  dayOf,
  trialEndDay,
  utcTodayDay,
} from "@/lib/trialDates";
import { fmtDateShort } from "@/lib/utils";

import { PEEK_PANEL_ID } from "./PeekPanel";
import {
  canExtendTrial,
  canRearmTrial,
  canStartTrial,
  useDisableOrganization,
  useEnableOrganization,
  useExtendTrial,
  useRearmTrial,
  useStartTrial,
} from "./rowActions";
import { WriteReportContext, type WriteReporter } from "./writeReport";

export type { WriteReporter } from "./writeReport";

// The trial length the rest of the system assumes, so the operator doing the
// usual thing picks nothing. It is the starting value of both day counts.
const DEFAULT_TRIAL_DAYS = 14;

// Each action carries the bounds of its own endpoint. They hold the same pair
// today and they are separate on the server so they can stop doing that.
type DayBounds = { min: number; max: number };

const EXTEND_BOUNDS: DayBounds = {
  min: MIN_TRIAL_EXTENSION_DAYS,
  max: MAX_TRIAL_EXTENSION_DAYS,
};

const REARM_BOUNDS: DayBounds = {
  min: MIN_TRIAL_REARM_DAYS,
  max: MAX_TRIAL_REARM_DAYS,
};

const START_BOUNDS: DayBounds = {
  min: MIN_TRIAL_START_DAYS,
  max: MAX_TRIAL_START_DAYS,
};

function boundsHint({ min, max }: DayBounds): string {
  return `Enter a whole number of days between ${min} and ${max}.`;
}

type DayRange = { anchor: number; earliest: number; latest: number };

// The days the server would accept, as the calendar's own range. `undefined`
// where the record carries no end date to add days to.
function extensionRange(org: AdminOrganization): DayRange | undefined {
  const anchor = trialEndDay(org.trial_ends_at);
  if (anchor === undefined) return undefined;
  return {
    anchor,
    earliest: anchor + MIN_TRIAL_EXTENSION_DAYS,
    latest: anchor + MAX_TRIAL_EXTENSION_DAYS,
  };
}

function startTrialRange(): DayRange {
  const anchor = utcTodayDay();
  return {
    anchor,
    earliest: anchor + MIN_TRIAL_START_DAYS,
    latest: anchor + MAX_TRIAL_START_DAYS,
  };
}

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

type OpenDialog = "disable" | "extend" | "rearm" | "start";

/**
 * Disable, re-enable, extend, re-arm and start, wherever the record is on
 * screen: the row menu, the peek panel footer, the record header and the
 * overview trial row.
 *
 * One component for all of them, because they are the same actions against the
 * same record: two implementations would be two answers to "can this trial be
 * started" and two confirmations to keep in step.
 *
 * `buttons` names the shape rather than the place. It was `footer` while the
 * peek panel was the only surface that drew it that way.
 */
export function OrganizationActions({
  org,
  layout,
  actions = "all",
  buttonClassName,
}: {
  org: AdminOrganization;
  layout: "menu" | "buttons";
  // Which of the record's actions this instance draws. The record shows two
  // bars at once: lifecycle in the header, the trial's own resolution beside
  // the facts it acts on. `all` is every other surface.
  actions?: "all" | "lifecycle" | "trial";
  // For a surface that is not the page's own background. A stock outline
  // button brings the page's border and fill with it, which inside a toned
  // panel reads as a control belonging to something else.
  buttonClassName?: string;
}): JSX.Element {
  const { announce, showFailure } = useContext(WriteReportContext);
  const [open, setOpen] = useState<OpenDialog>();

  // Held here rather than in the dialogs. A dialog unmounts as it closes, and
  // the write it started has to keep its cache update and its announcement.
  const disable = useDisableOrganization();
  const enable = useEnableOrganization();
  const extend = useExtendTrial();
  const rearm = useRearmTrial();
  const start = useStartTrial();

  const isDisabled = Boolean(org.disabled_at);
  const busy =
    disable.isPending ||
    enable.isPending ||
    extend.isPending ||
    rearm.isPending ||
    start.isPending;

  // The two failures a trial dialog reports, written once so the bounds refusal
  // and the server's own refusal are led by the same words.
  const extendFailureLead = `Could not extend the trial for ${org.name}`;
  const rearmFailureLead = `Could not re-arm the trial for ${org.name}`;
  const startFailureLead = `Could not start the trial for ${org.name}`;

  // Only extend has a date to add days to. Re-arm counts from now, so it gets
  // no calendar and its dialog falls back to a day count.
  const extendRange = extensionRange(org);
  const startRange = startTrialRange();

  // Read once and used by both layouts, so a menu caller cannot get a
  // different answer from a buttons caller passing the same `actions`.
  const showLifecycle = actions !== "trial";
  const showExtend = actions !== "lifecycle" && canExtendTrial(org);
  const showStart = actions !== "lifecycle" && canStartTrial(org);

  const menuTrigger = useRef<HTMLButtonElement>(null);

  // Where the keyboard goes when a dialog closes. Radix restores focus to a
  // `DialogTrigger`, and these dialogs have none: its `onCloseAutoFocus`
  // cancels FocusScope's own restore and then focuses `triggerRef.current`,
  // which is null here, so without this every close drops the keyboard onto
  // `document.body`. One handler covers all five exits: success, Escape,
  // Cancel, the backdrop and the X.
  const openedFrom = useRef<HTMLElement | null>(null);

  const openDialog = (dialog: OpenDialog, from: HTMLElement | null): void => {
    openedFrom.current = from;
    // A failure the operator cancelled out of is not one the next attempt
    // should open holding.
    disable.reset();
    extend.reset();
    rearm.reset();
    start.reset();
    setOpen(dialog);
  };

  const restoreFocus = (event: Event): void => {
    const control = openedFrom.current;
    if (control?.isConnected) {
      event.preventDefault();
      control.focus();
      return;
    }

    // A re-armed record is running rather than demoted, so the bar takes the
    // Re-arm button down and mounts an Extend button in a sibling slot rather
    // than reusing the node. The peek panel is already focusable, so the
    // keyboard goes there. Nothing to fall back to on any other surface.
    if (layout !== "buttons") return;
    const panel = document.getElementById(PEEK_PANEL_ID);
    if (!panel) return;
    event.preventDefault();
    panel.focus();
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
          announce(`${extendFailureLead}: ${errorMessage(error)}`),
      },
    );
  };

  const runRearm = (days: number): void => {
    rearm.mutate(
      { id: org.id, days },
      {
        onSuccess: () => {
          setOpen(undefined);
          showFailure(null);
          announce(`${org.name} trial re-armed for ${dayCount(days)}.`);
        },
        onError: (error) =>
          announce(`${rearmFailureLead}: ${errorMessage(error)}`),
      },
    );
  };

  const runStart = (days: number): void => {
    start.mutate(
      { id: org.id, days },
      {
        onSuccess: () => {
          setOpen(undefined);
          showFailure(null);
          announce(`${org.name} trial started for ${dayCount(days)}.`);
        },
        onError: (error) =>
          announce(`${startFailureLead}: ${errorMessage(error)}`),
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
        <TrialDaysDialog
          bounds={EXTEND_BOUNDS}
          range={extendRange}
          title={`Extend the trial for ${org.name}?`}
          description={
            extendRange
              ? `The trial ends on ${fmtDateShort(org.trial_ends_at)} now.`
              : "The days are added to the date the trial ends on now, not to today."
          }
          submitLabel="Extend"
          pendingLabel="Extending..."
          failureLead={extendFailureLead}
          pending={extend.isPending}
          failure={extend.error}
          onCancel={() => setOpen(undefined)}
          onCloseAutoFocus={restoreFocus}
          onSubmit={runExtend}
        />
      )}
      {open === "rearm" && (
        <TrialDaysDialog
          bounds={REARM_BOUNDS}
          range={undefined}
          title={`Re-arm the trial for ${org.name}?`}
          // Everything the write does, because an operator who reads "days" and
          // expects only a new date has been told less than half of it. The
          // gate is what the whitelist flag is called outside the schema.
          description="Restores the account type, brings the model provider keys back, and takes the organization out from behind the book-a-demo gate. The trial then runs for the days below, counted from now rather than from the date the old one ended."
          submitLabel="Re-arm"
          pendingLabel="Re-arming..."
          failureLead={rearmFailureLead}
          pending={rearm.isPending}
          failure={rearm.error}
          onCancel={() => setOpen(undefined)}
          onCloseAutoFocus={restoreFocus}
          onSubmit={runRearm}
        />
      )}
      {open === "start" && (
        <TrialDaysDialog
          bounds={START_BOUNDS}
          range={startRange}
          title={`Start a trial for ${org.name}?`}
          description="Sets the account type, brings the model provider keys up, and takes the organization out from behind the book-a-demo gate. The trial then runs until the date below, counted from today."
          submitLabel="Start trial"
          pendingLabel="Starting..."
          failureLead={startFailureLead}
          pending={start.isPending}
          failure={start.error}
          onCancel={() => setOpen(undefined)}
          onCloseAutoFocus={restoreFocus}
          onSubmit={runStart}
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
        {showLifecycle &&
          (isDisabled ? (
            <Button
              variant="outline"
              size="xs"
              aria-label={`Re-enable ${org.name}`}
              aria-busy={busy}
              className={buttonClassName}
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
              className={buttonClassName}
              onClick={(event) => openDialog("disable", event.currentTarget)}
            >
              Disable
            </Button>
          ))}
        {showExtend && (
          <Button
            variant="outline"
            size="xs"
            aria-label={`Extend trial for ${org.name}`}
            aria-busy={busy}
            className={buttonClassName}
            onClick={(event) => openDialog("extend", event.currentTarget)}
          >
            Extend trial
          </Button>
        )}
        {canRearmTrial(org) && (
          <Button
            variant="outline"
            size="xs"
            aria-label={`Re-arm trial for ${org.name}`}
            aria-busy={busy}
            onClick={(event) => openDialog("rearm", event.currentTarget)}
          >
            Re-arm trial
          </Button>
        )}
        {showStart && (
          <Button
            variant="outline"
            size="xs"
            aria-label={`Start trial for ${org.name}`}
            aria-busy={busy}
            className={buttonClassName}
            onClick={(event) => openDialog("start", event.currentTarget)}
          >
            Start trial
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
          {showLifecycle &&
            (isDisabled ? (
              <DropdownMenuItem onSelect={runEnable}>
                Re-enable
              </DropdownMenuItem>
            ) : (
              <DropdownMenuItem
                variant="destructive"
                // Opens the confirmation. The write waits for it: disabling
                // cuts a customer off, and this menu sits one row away from
                // four others.
                //
                // The trigger, not the item this fires on: the menu closes
                // with the dialog opening and takes the item down with it, and
                // the dialog has to give the keyboard back to something still
                // on the page.
                onSelect={() => openDialog("disable", menuTrigger.current)}
              >
                Disable
              </DropdownMenuItem>
            ))}
          {showExtend && (
            <DropdownMenuItem
              onSelect={() => openDialog("extend", menuTrigger.current)}
            >
              Extend trial
            </DropdownMenuItem>
          )}
          {canRearmTrial(org) && (
            <DropdownMenuItem
              onSelect={() => openDialog("rearm", menuTrigger.current)}
            >
              Re-arm trial
            </DropdownMenuItem>
          )}
          {showStart && (
            <DropdownMenuItem
              onSelect={() => openDialog("start", menuTrigger.current)}
            >
              Start trial
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

// The dialog's own account of a failure, beside the field it is about. The
// page's region does survive an open modal, because `hideOthers` exempts a node
// that carries `aria-live` by name, but it is sr-only and polite: on its own it
// leaves a sighted operator reading a dialog that reports nothing.
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

// One day-count dialog for both trial writes. Extend and re-arm ask the same
// question of the operator, refuse the same values and report a failure the
// same way; only their bounds and their words differ. Two copies would be two
// places to keep the refusal path honest.
//
// Exported for a test that renders it on a bounds pair neither endpoint uses.
// Both real pairs hold 1 and 365, so nothing else can tell a dialog that reads
// the `bounds` prop from one that hardcodes the extension bounds.
export function TrialDaysDialog({
  bounds,
  range,
  title,
  description,
  submitLabel,
  pendingLabel,
  failureLead,
  pending,
  failure,
  onCancel,
  onCloseAutoFocus,
  onSubmit,
}: {
  bounds: DayBounds;
  // The dates the operator can pick between, where the write has an end date to
  // add days to. Without one there is nothing to pick against, so the dialog
  // falls back to a day count. Re-arm is always that case.
  range: DayRange | undefined;
  title: string;
  description: string;
  submitLabel: string;
  pendingLabel: string;
  failureLead: string;
  pending: boolean;
  failure: Error | null;
  onCancel: () => void;
  onCloseAutoFocus: (event: Event) => void;
  onSubmit: (days: number) => void;
}): JSX.Element {
  const { announce } = useContext(WriteReportContext);
  const [days, setDays] = useState(String(DEFAULT_TRIAL_DAYS));
  const [endsOn, setEndsOn] = useState<Date | undefined>(
    () => range && calendarDate(range.anchor + DEFAULT_TRIAL_DAYS),
  );
  const [calendarOpen, setCalendarOpen] = useState(false);
  const [rejected, setRejected] = useState(false);
  const fieldID = useId();
  const messageID = useId();

  const hint = range
    ? `Pick a date between ${fmtDateShort(dayISO(range.earliest))} and ${fmtDateShort(dayISO(range.latest))}.`
    : boundsHint(bounds);

  // What the picked date is worth as the request the server takes. NaN where
  // nothing is picked, so the guard below refuses it rather than sending it.
  const picked = endsOn && range ? dayOf(endsOn) - range.anchor : Number.NaN;

  const submit = (event: FormEvent): void => {
    event.preventDefault();
    // A disabled day is not an enforced value: the calendar can still be left
    // holding nothing, and the day count has no calendar at all.
    const parsed = range ? picked : Number(days);
    // The endpoint's own bounds, refused here so a request that cannot succeed
    // never leaves the browser. A whole number, because the interval the
    // server works in is a count of days.
    if (
      !Number.isInteger(parsed) ||
      parsed < bounds.min ||
      parsed > bounds.max
    ) {
      setRejected(true);
      // Spoken as well as shown, because showing it a second time shows
      // nothing: the state and the text are both unchanged, so the same press
      // repeated leaves the DOM untouched and a `role="alert"` announces only
      // what is inserted or changed. The live region alternates a zero-width
      // space, so it speaks every time.
      announce(`${failureLead}: ${hint}`);
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
            <DialogTitle>{title}</DialogTitle>
            {/* Radix points the dialog's description at this, so the bounds
                are announced with the title rather than only after a refusal. */}
            <DialogDescription>
              {description} {hint}
            </DialogDescription>
          </DialogHeader>

          <div className="my-4 flex items-center gap-2">
            <label htmlFor={fieldID} className="text-sm">
              {range ? "Ends on" : "Days"}
            </label>
            {range ? (
              <Popover open={calendarOpen} onOpenChange={setCalendarOpen}>
                <PopoverTrigger asChild>
                  <Button
                    id={fieldID}
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={pending}
                    aria-invalid={rejected}
                    aria-describedby={
                      rejected || failure ? messageID : undefined
                    }
                  >
                    <CalendarIcon />
                    {endsOn
                      ? fmtDateShort(dayISO(dayOf(endsOn)))
                      : "Pick a date"}
                  </Button>
                </PopoverTrigger>
                <PopoverContent align="start" className="w-auto p-0">
                  {/* Bounded rather than validated afterwards, so a day the
                      server would refuse cannot be pressed at all. The months
                      are bounded too, or the operator can page through years
                      of days that are all dead. */}
                  <Calendar
                    mode="single"
                    autoFocus
                    selected={endsOn}
                    defaultMonth={endsOn}
                    startMonth={calendarDate(range.earliest)}
                    endMonth={calendarDate(range.latest)}
                    disabled={{
                      before: calendarDate(range.earliest),
                      after: calendarDate(range.latest),
                    }}
                    onSelect={(date) => {
                      setEndsOn(date);
                      setCalendarOpen(false);
                    }}
                  />
                </PopoverContent>
              </Popover>
            ) : (
              <Input
                id={fieldID}
                type="number"
                min={bounds.min}
                max={bounds.max}
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
            )}
          </div>

          {/* The operator picks a date and the request sends a count, so the
              dialog says the date that count reaches. */}
          {range && endsOn && (
            <p className="text-muted-foreground text-sm">
              The trial will end on {fmtDateShort(dayISO(dayOf(endsOn)))},{" "}
              {dayCount(picked)} later than it does now.
            </p>
          )}

          {/* One at a time. The bounds refusal is the newer of the two and it
              is about the value now in the field, so a stale server failure
              underneath it would give the operator two reasons and no way to
              tell which one the next press answers. */}
          {rejected && <Failure id={messageID}>{hint}</Failure>}
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
              {pending ? pendingLabel : submitLabel}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
