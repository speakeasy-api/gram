import { useId, useRef, useState, type JSX } from "react";
import { AlertTriangle } from "lucide-react";

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
import { errorMessage } from "@/lib/gramAdminApi";
import { pluralize, labelForUseCase } from "@/lib/organizationEncryption";

export interface FinalizeEncryptionProps {
  organizationName: string;
  useCase: string;
  /** Unreadable values the server counted; finalize destroys them and the admin must acknowledge the number. */
  unreadableValues: number;
  /** Platform-key copies still stored, every one of which finalize destroys. */
  platformCopies: number;
  /** The phrase the server requires, typed by the admin. */
  confirmation: string;
  disabled: boolean;
  /** Resolve once finalized; reject with an error the dialog shows so the admin can retry. */
  onSubmit: (submission: {
    confirmation: string;
    acknowledgedUnreadableValues: number;
  }) => Promise<unknown>;
}

export function FinalizeEncryption({
  organizationName,
  useCase,
  unreadableValues,
  platformCopies,
  confirmation,
  disabled,
  onSubmit,
}: FinalizeEncryptionProps): JSX.Element {
  const id = useId();
  const [open, setOpen] = useState(false);
  const [typed, setTyped] = useState("");
  // The count the admin acknowledged, not a flag: when a refetch changes the
  // count while the dialog is open, the acknowledgement lapses and must be
  // given again for the new number.
  const [acknowledgedCount, setAcknowledgedCount] = useState<number | null>(
    null,
  );
  const acknowledged = acknowledgedCount === unreadableValues;
  const [pending, setPending] = useState(false);
  const submitting = useRef(false);
  const [error, setError] = useState<string | null>(null);
  const heading = useRef<HTMLHeadingElement>(null);
  const label = labelForUseCase(useCase);
  // Blockers or a busy page can appear while the dialog is open, so the
  // dialog's own Finalize button follows `disabled` too.
  const valid = !disabled && typed === confirmation && acknowledged;

  function changeOpen(next: boolean) {
    if (submitting.current) return;
    if (next) {
      setTyped("");
      setAcknowledgedCount(null);
      setError(null);
    }
    setOpen(next);
  }

  async function submit() {
    if (!valid || acknowledgedCount === null || submitting.current) return;
    submitting.current = true;
    setPending(true);
    setError(null);
    try {
      await onSubmit({
        confirmation: typed,
        acknowledgedUnreadableValues: acknowledgedCount,
      });
      setOpen(false);
    } catch (failure) {
      setError(errorMessage(failure));
    } finally {
      submitting.current = false;
      setPending(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={changeOpen}>
      <DialogTrigger asChild>
        <Button variant="destructive" size="sm" disabled={disabled}>
          Finalize
        </Button>
      </DialogTrigger>
      <DialogContent
        className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-xl"
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
          className="space-y-5"
          aria-busy={pending}
          onSubmit={(event) => {
            event.preventDefault();
            void submit();
          }}
        >
          <DialogHeader className="pr-6 text-left">
            <DialogTitle ref={heading} tabIndex={-1} className="outline-none">
              Finalize {label} for {organizationName}?
            </DialogTitle>
            <DialogDescription>
              The organization&apos;s values will be readable only with its own
              key.
            </DialogDescription>
          </DialogHeader>

          <div className="flex gap-3 rounded-lg border border-destructive/30 bg-destructive/5 p-4 text-sm">
            <AlertTriangle
              aria-hidden="true"
              className="mt-0.5 size-4 shrink-0 text-destructive"
            />
            <div className="space-y-2">
              <p className="font-medium">This cannot be undone</p>
              <ul className="text-muted-foreground list-disc space-y-1 pl-4">
                <li>
                  Every platform-key copy of this organization&apos;s {label} is
                  destroyed, currently{" "}
                  {pluralize(platformCopies, "copy", "copies")}.
                </li>
                <li>
                  Rolling back to the platform key is no longer possible
                  afterwards.
                </li>
                <li>
                  {unreadableValues === 0
                    ? "No value is recorded as unreadable."
                    : `${pluralize(unreadableValues, "unreadable value")} ${unreadableValues === 1 ? "has" : "have"} no organization-key copy and ${unreadableValues === 1 ? "is" : "are"} destroyed with the rest.`}
                </li>
              </ul>
            </div>
          </div>

          <fieldset disabled={pending} className="space-y-4">
            <div className="flex items-start gap-2">
              <Checkbox
                id={`${id}-acknowledge`}
                checked={acknowledged}
                onCheckedChange={(checked) =>
                  setAcknowledgedCount(
                    checked === true ? unreadableValues : null,
                  )
                }
              />
              <label htmlFor={`${id}-acknowledge`} className="text-sm">
                I understand that{" "}
                <strong>
                  {pluralize(unreadableValues, "unreadable value")}
                </strong>{" "}
                will be destroyed.
              </label>
            </div>
            <div className="space-y-2">
              <label
                htmlFor={`${id}-confirmation`}
                className="text-sm font-medium"
              >
                Type <strong className="font-bold">{confirmation}</strong>
              </label>
              <Input
                id={`${id}-confirmation`}
                autoComplete="off"
                spellCheck={false}
                value={typed}
                onChange={(event) => setTyped(event.target.value)}
              />
            </div>
          </fieldset>

          {error && (
            <p
              role="alert"
              className="rounded-lg border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive"
            >
              {error}
            </p>
          )}

          <DialogFooter>
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
              {pending ? "Finalizing…" : "Finalize"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
