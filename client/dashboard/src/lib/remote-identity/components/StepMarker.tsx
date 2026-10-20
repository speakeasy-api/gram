import { Check } from "lucide-react";

/** A numbered step's circle, or a filled check once the step is done. */
export function StepMarker({
  index,
  done,
}: {
  index: number;
  done: boolean;
}): JSX.Element {
  return done ? (
    <span
      role="img"
      aria-label="Done"
      className="bg-primary text-primary-foreground flex size-6 shrink-0 items-center justify-center rounded-full"
    >
      <Check aria-hidden="true" className="size-3" strokeWidth={3} />
    </span>
  ) : (
    <span className="text-muted-foreground flex size-6 shrink-0 items-center justify-center rounded-full border font-mono text-xs">
      {index}
    </span>
  );
}
