import { useId, useRef, useState, type JSX } from "react";
import { AlertTriangle, Info } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";

export interface RepairKeyCause {
  cause: string;
  label: string;
  description?: string;
  removable: boolean;
  blockedReason?: string;
}
export interface RepairKeySubmission {
  removeCauses: string[];
  reason: string;
  confirmation: string;
}
export interface RepairInferenceKeyProps {
  keyType: string;
  keyName: string;
  causes: readonly RepairKeyCause[];
  classified: boolean;
  disabled: boolean;
  /** Resolve once accepted; reject with a safe, staff-facing error to allow retry. */
  onSubmit: (submission: RepairKeySubmission) => Promise<unknown>;
}

const CONFIRMATION = "I know what I'm doing";
const DESCRIPTIONS: Record<string, string> = {
  trial_demotion:
    "The trial lifecycle disabled this key when trial access ended.",
  billing_inactive:
    "The billing lifecycle disabled this key because billing was not eligible.",
  admin_lock:
    "A staff member deliberately locked this key. Remove only after checking why.",
};

export function RepairInferenceKey({
  keyType,
  keyName,
  causes,
  classified,
  disabled,
  onSubmit,
}: RepairInferenceKeyProps): JSX.Element {
  const id = useId();
  const [open, setOpen] = useState(false);
  const [selected, setSelected] = useState<string[]>([]);
  const [reason, setReason] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [pending, setPending] = useState(false);
  const submitting = useRef(false);
  const [error, setError] = useState<string | null>(null);
  const [accepted, setAccepted] = useState(false);
  const heading = useRef<HTMLHeadingElement>(null);
  const unclassified = !classified || (disabled && causes.length === 0);
  const known = (cause: string) => Object.hasOwn(DESCRIPTIONS, cause);
  const canRemove = (cause: RepairKeyCause) =>
    !unclassified && known(cause.cause) && cause.removable;
  // Re-evaluate current metadata, not only the selections made when the dialog opened.
  const removals = causes.filter(
    (cause) => canRemove(cause) && selected.includes(cause.cause),
  );
  const remaining = causes.filter((cause) => !removals.includes(cause));
  const reasonTooLong = new TextEncoder().encode(reason.trim()).length > 2000;
  const valid =
    removals.length > 0 &&
    reason.trim().length > 0 &&
    !reasonTooLong &&
    confirmation === CONFIRMATION;

  function changeOpen(next: boolean) {
    if (submitting.current) return;
    if (next) {
      setSelected([]);
      setReason("");
      setConfirmation("");
      setError(null);
      setAccepted(false);
    }
    setOpen(next);
  }

  async function submit() {
    if (!valid || submitting.current) return;
    submitting.current = true;
    setPending(true);
    setError(null);
    try {
      await onSubmit({
        removeCauses: removals.map((cause) => cause.cause),
        reason: reason.trim(),
        confirmation,
      });
      setAccepted(true);
      setOpen(false);
    } catch (failure) {
      setError(
        failure instanceof Error
          ? failure.message
          : "Could not repair this key. Please try again.",
      );
    } finally {
      submitting.current = false;
      setPending(false);
    }
  }

  return (
    <TooltipProvider>
      <Dialog open={open} onOpenChange={changeOpen}>
        <DialogTrigger asChild>
          <Button variant="outline" size="sm">
            Repair key locks
          </Button>
        </DialogTrigger>
        {!open && accepted && (
          <p role="status" className="mt-2 text-sm text-muted-foreground">
            Repair accepted; upstream reconciliation pending; refresh key
            diagnostics to verify.
          </p>
        )}
        <DialogContent
          className="flex max-h-[calc(100dvh-2rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-xl"
          showCloseButton={!pending}
          onOpenAutoFocus={(event) => {
            event.preventDefault();
            heading.current?.focus();
          }}
          onEscapeKeyDown={(event) => {
            if (submitting.current) event.preventDefault();
          }}
          onInteractOutside={(event) => {
            if (submitting.current) event.preventDefault();
          }}
        >
          <form
            className="flex min-h-0 flex-col"
            aria-busy={pending}
            onSubmit={(event) => {
              event.preventDefault();
              void submit();
            }}
          >
            <div className="min-h-0 space-y-6 overflow-y-auto p-6">
              <DialogHeader className="pr-6 text-left">
                <DialogTitle
                  ref={heading}
                  tabIndex={-1}
                  className="outline-none"
                >
                  Repair inference key locks
                </DialogTitle>
                <DialogDescription>
                  Remove specific stale locks from one key. Tier, credits and
                  runtime are not changed.
                </DialogDescription>
              </DialogHeader>
              <div className="rounded-lg border bg-muted/30 px-4 py-3">
                <p className="font-medium break-words">{keyName}</p>
                <p className="mt-1 text-sm text-muted-foreground">
                  Key scope:{" "}
                  <span className="font-mono break-all">{keyType}</span> ·{" "}
                  {disabled ? "Disabled" : "Enabled"}
                </p>
              </div>
              <div className="flex gap-3 rounded-lg border border-amber-500/30 bg-amber-500/10 p-4 text-sm">
                <AlertTriangle
                  aria-hidden="true"
                  className="mt-0.5 size-4 shrink-0 text-amber-600 dark:text-amber-400"
                />
                <div className="space-y-1">
                  <p className="font-medium">
                    An exception, not a lifecycle override
                  </p>
                  <p className="text-muted-foreground">
                    Trial, billing and tier changes control keys automatically.
                    Use this only for a stale edge case, and file a bug so the
                    lifecycle can be fixed.
                  </p>
                </div>
              </div>
              <fieldset disabled={pending} className="space-y-3">
                <legend className="mb-2 text-sm font-semibold">
                  Choose locks to remove
                </legend>
                {unclassified && (
                  <p className="text-sm text-muted-foreground">
                    This key has an unclassified disable. Repair is unavailable;
                    investigate and file a bug rather than clearing an unknown
                    lock.
                  </p>
                )}
                {causes.map((cause) => {
                  const allowed = canRemove(cause);
                  const blocked =
                    cause.blockedReason ||
                    (!known(cause.cause)
                      ? "This lock is not recognized. It cannot be removed here; investigate and file a bug."
                      : unclassified
                        ? "Unclassified key state must be investigated before repair."
                        : "Current lifecycle policy does not allow this lock to be removed.");
                  const causeId = `${id}-${cause.cause}`;
                  return (
                    <div
                      key={cause.cause}
                      className="flex items-start gap-3 rounded-lg border p-4"
                    >
                      <Checkbox
                        id={causeId}
                        className="mt-0.5"
                        aria-describedby={`${causeId}-description${!allowed ? ` ${causeId}-blocked` : ""}`}
                        disabled={!allowed || pending}
                        checked={removals.includes(cause)}
                        onCheckedChange={(checked) =>
                          setSelected((current) =>
                            checked === true
                              ? [...current, cause.cause]
                              : current.filter(
                                  (value) => value !== cause.cause,
                                ),
                          )
                        }
                      />
                      <div className="min-w-0 flex-1 space-y-1">
                        <label
                          htmlFor={causeId}
                          className="text-sm font-medium"
                        >
                          {cause.label}
                        </label>
                        <p
                          id={`${causeId}-description`}
                          className="text-sm text-muted-foreground"
                        >
                          {cause.description ||
                            (known(cause.cause)
                              ? DESCRIPTIONS[cause.cause]
                              : "Unrecognized lock. It will be preserved.")}
                        </p>
                        {!allowed && (
                          <p
                            id={`${causeId}-blocked`}
                            className="text-xs leading-relaxed text-muted-foreground [overflow-wrap:anywhere]"
                          >
                            {blocked}
                          </p>
                        )}
                      </div>
                      {!allowed && (
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <button
                              type="button"
                              aria-label={`Why ${cause.label} cannot be removed`}
                              className="shrink-0 rounded p-1 text-muted-foreground focus-visible:outline-2 focus-visible:outline-ring"
                            >
                              <Info aria-hidden="true" className="size-4" />
                            </button>
                          </TooltipTrigger>
                          <TooltipContent className="max-w-[min(20rem,calc(100vw-2rem))] [overflow-wrap:anywhere]">
                            {blocked}
                          </TooltipContent>
                        </Tooltip>
                      )}
                    </div>
                  );
                })}
                {causes.length === 0 && !unclassified && (
                  <p className="text-sm text-muted-foreground">
                    There are no locks to remove.
                  </p>
                )}
              </fieldset>
              <div
                role="status"
                aria-live="polite"
                aria-atomic="true"
                className="space-y-1 rounded-lg bg-muted/50 p-4 text-sm"
              >
                <p className="font-medium">
                  {pending
                    ? "Removing selected locks…"
                    : (disabled && unclassified) || remaining.length > 0
                      ? "Still disabled after repair"
                      : "Enabled after repair"}
                </p>
                {remaining.length > 0 && (
                  <p>
                    Remaining locks:{" "}
                    {remaining.map((cause) => cause.label).join(", ")}
                  </p>
                )}
                <p className="text-muted-foreground">
                  Preview only. The server revalidates current locks and
                  billing; upstream application may be pending after acceptance.
                </p>
              </div>
              <div className="space-y-2">
                <label htmlFor={`${id}-reason`} className="text-sm font-medium">
                  Reason or bug ticket
                </label>
                <Textarea
                  id={`${id}-reason`}
                  disabled={pending}
                  value={reason}
                  onChange={(event) => setReason(event.target.value)}
                  placeholder="Explain the stale lock and link the bug"
                  rows={2}
                  maxLength={2000}
                  aria-invalid={reasonTooLong}
                  aria-describedby={
                    reasonTooLong ? `${id}-reason-error` : undefined
                  }
                />
              </div>
              {reasonTooLong && (
                <p
                  id={`${id}-reason-error`}
                  role="alert"
                  className="text-sm text-destructive"
                >
                  Reason exceeds 2000 UTF-8 bytes. Shorten it before submitting.
                </p>
              )}
              <div className="space-y-2">
                <label
                  htmlFor={`${id}-confirmation`}
                  className="text-sm font-medium"
                >
                  Type <strong className="font-bold">{CONFIRMATION}</strong>
                </label>
                <Input
                  id={`${id}-confirmation`}
                  disabled={pending}
                  autoComplete="off"
                  spellCheck={false}
                  value={confirmation}
                  onChange={(event) => setConfirmation(event.target.value)}
                />
              </div>
              {error && (
                <p
                  role="alert"
                  className="rounded-lg border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive"
                >
                  {error}
                </p>
              )}
            </div>
            <DialogFooter className="shrink-0 border-t bg-background p-4 sm:px-6">
              <Button
                type="button"
                variant="outline"
                disabled={pending}
                onClick={() => changeOpen(false)}
              >
                Cancel
              </Button>
              <Button
                type="submit"
                variant="destructive"
                disabled={!valid || pending}
              >
                {pending ? "Removing locks…" : "Remove selected locks"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </TooltipProvider>
  );
}
