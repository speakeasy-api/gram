import { Dialog } from "@/components/ui/Dialog";
import { Icon } from "@/components/ui/Icon";
import { Label } from "@/components/ui/Label";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import type { ReactNode } from "react";

/**
 * One setting per row: name and one-clause hint on the left, the control on
 * the right. Three bordered cards, each with its own heading, paragraph and
 * footer bar, turned three small settings into a page of chrome — this is
 * the same density as every other settings panel in the dashboard.
 */
export function AuthRow({
  label,
  hint,
  htmlFor,
  children,
  className,
}: {
  label: ReactNode;
  /** One clause. Anything longer belongs behind a popover. */
  hint?: ReactNode;
  htmlFor?: string;
  children: ReactNode;
  className?: string;
}): JSX.Element {
  return (
    <div
      className={cn(
        "grid gap-2 px-6 py-5 sm:grid-cols-[minmax(200px,280px)_1fr] sm:gap-8",
        className,
      )}
    >
      <div className="space-y-1">
        <Label htmlFor={htmlFor} className="text-sm font-medium">
          {label}
        </Label>
        {hint ? (
          <Text muted small className="block">
            {hint}
          </Text>
        ) : null}
      </div>
      <div className="min-w-0 space-y-3">{children}</div>
    </div>
  );
}

/**
 * A save that exists only while there is something to save. A permanently
 * mounted, permanently disabled button reads as broken chrome, and three of
 * them stacked down a panel was most of what made this surface feel heavy.
 */
export function RowSave({
  visible,
  children,
}: {
  visible: boolean;
  children: ReactNode;
}): JSX.Element | null {
  if (!visible) return null;
  return <div className="flex items-center gap-3 pt-1">{children}</div>;
}

/**
 * The "What is this?" affordance a row's hint hands the terminology to. A
 * modal rather than a popover: these run to a couple of paragraphs, and a
 * floating panel that size covered the rows beneath the one being read.
 */
export function ExplainerDialog({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <Dialog>
      <Dialog.Trigger asChild>
        <button
          type="button"
          className="text-muted-foreground hover:text-foreground mt-2 flex cursor-pointer items-center gap-1 underline underline-offset-2"
        >
          <Icon name="info" className="size-3" />
          What is this?
        </button>
      </Dialog.Trigger>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>{title}</Dialog.Title>
        </Dialog.Header>
        <div className="space-y-3">{children}</div>
      </Dialog.Content>
    </Dialog>
  );
}
