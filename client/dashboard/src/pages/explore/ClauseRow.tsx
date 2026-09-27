import { Button } from "@/components/ui/Button";
import type { JSX, ReactNode } from "react";

/** One clause of the builder: an uppercase mono label beside its controls. */
export function ClauseRow({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="flex flex-col gap-1.5 sm:flex-row sm:items-start sm:gap-4">
      <span className="text-eyebrow w-24 shrink-0 sm:pt-3">{label}</span>
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  );
}

/** A labelled control in the builder's presentation strip. */
export function BuilderField({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <span className="text-eyebrow">{label}</span>
      {children}
    </div>
  );
}

/** The "+" that appends another row to a clause. */
export function AddRowButton({
  label,
  onClick,
}: {
  label: string;
  onClick: () => void;
}): JSX.Element {
  return (
    <Button
      variant="tertiary"
      size="sm"
      icon="plus"
      aria-label={label}
      onClick={onClick}
    />
  );
}
