import { Button } from "@/components/ui/Button";
import type { JSX, ReactNode } from "react";

/**
 * One clause of the builder, read as part of a sentence: a short mono
 * label, then its controls on the same line.
 */
export function ClauseRow({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="flex flex-col gap-1 sm:flex-row sm:items-start sm:gap-3">
      <span className="text-eyebrow w-16 shrink-0 sm:pt-2">{label}</span>
      <div className="min-w-0 flex-1">{children}</div>
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
