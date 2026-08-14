import { MoreHorizontalIcon } from "lucide-react";
import {
  createContext,
  useContext,
  useId,
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
 * The list owns the only live region on the page, and these controls are drawn
 * inside table cells the list cannot pass props to, so the announcer reaches
 * them through context the way the peek controls do.
 *
 * The default is a no-op rather than a throw: the peek panel renders these
 * actions too and it is mounted on its own in tests.
 */
const AnnounceContext = createContext<(text: string) => void>(() => {});

export function AnnounceProvider({
  value,
  children,
}: {
  value: (text: string) => void;
  children: ReactNode;
}): JSX.Element {
  return (
    <AnnounceContext.Provider value={value}>
      {children}
    </AnnounceContext.Provider>
  );
}

type OpenDialog = "disable" | "extend";

/**
 * Disable, re-enable and extend, in the row menu and in the peek panel footer.
 *
 * One component for both surfaces, because they are the same three actions
 * against the same record: two implementations would be two answers to
 * "can this trial be extended" and two confirmations to keep in step.
 */
export function OrganizationActions({
  org,
  layout,
}: {
  org: AdminOrganization;
  layout: "menu" | "footer";
}): JSX.Element {
  const announce = useContext(AnnounceContext);
  const [open, setOpen] = useState<OpenDialog>();

  // Held here rather than in the dialogs. A dialog unmounts as it closes, and
  // the write it started has to keep its cache update and its announcement.
  const disable = useDisableOrganization();
  const enable = useEnableOrganization();
  const extend = useExtendTrial();

  const isDisabled = Boolean(org.disabled_at);
  const busy = disable.isPending || enable.isPending || extend.isPending;

  const openDialog = (dialog: OpenDialog): void => {
    // A failure the operator cancelled out of is not one the next attempt
    // should open holding.
    disable.reset();
    extend.reset();
    setOpen(dialog);
  };

  const runDisable = (): void => {
    disable.mutate(org.id, {
      onSuccess: () => {
        setOpen(undefined);
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
      onSuccess: () => announce(`${org.name} is enabled.`),
      onError: (error) =>
        announce(`Could not re-enable ${org.name}: ${errorMessage(error)}`),
    });
  };

  const runExtend = (days: number): void => {
    extend.mutate(
      { id: org.id, days },
      {
        onSuccess: () => {
          setOpen(undefined);
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
          onConfirm={runDisable}
        />
      )}
      {open === "extend" && (
        <ExtendTrial
          org={org}
          pending={extend.isPending}
          failure={extend.error}
          onCancel={() => setOpen(undefined)}
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

  if (layout === "footer") {
    return contain(
      <>
        {/* Live while a write is in flight, and marked busy instead. Disabling
            the control the operator just pressed drops the keyboard onto the
            body, and re-enabling is idempotent, so a second press costs a
            request and nothing else. */}
        {isDisabled ? (
          <Button
            variant="outline"
            size="xs"
            aria-busy={busy}
            onClick={runEnable}
          >
            Re-enable
          </Button>
        ) : (
          <Button
            variant="outline"
            size="xs"
            aria-busy={busy}
            onClick={() => openDialog("disable")}
          >
            Disable
          </Button>
        )}
        {canExtendTrial(org) && (
          <Button
            variant="outline"
            size="xs"
            aria-busy={busy}
            onClick={() => openDialog("extend")}
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
              onSelect={() => openDialog("disable")}
            >
              Disable
            </DropdownMenuItem>
          )}
          {canExtendTrial(org) && (
            <DropdownMenuItem onSelect={() => openDialog("extend")}>
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
function Failure({ children }: { children: ReactNode }): JSX.Element {
  return (
    <p role="alert" className="text-destructive text-sm">
      {children}
    </p>
  );
}

function ConfirmDisable({
  org,
  pending,
  failure,
  onCancel,
  onConfirm,
}: {
  org: AdminOrganization;
  pending: boolean;
  failure: Error | null;
  onCancel: () => void;
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
      <DialogContent>
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
  onSubmit,
}: {
  org: AdminOrganization;
  pending: boolean;
  failure: Error | null;
  onCancel: () => void;
  onSubmit: (days: number) => void;
}): JSX.Element {
  const [days, setDays] = useState(String(DEFAULT_EXTENSION_DAYS));
  const [rejected, setRejected] = useState(false);
  const fieldID = useId();

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
      <DialogContent>
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
              onChange={(event) => setDays(event.target.value)}
              className="w-24"
            />
          </div>

          {rejected && <Failure>{BOUNDS_HINT}</Failure>}
          {!rejected && failure && <Failure>{errorMessage(failure)}</Failure>}

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
