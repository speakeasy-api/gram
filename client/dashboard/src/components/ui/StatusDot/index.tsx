import { cn } from "@/lib/utils";

export type StatusDotTone =
  | "success"
  | "warning"
  | "destructive"
  | "information"
  | "neutral";

const TONE_CLASS: Record<StatusDotTone, string> = {
  success: "bg-[var(--fill-success-default)]",
  warning: "bg-[var(--fill-warning-default)]",
  destructive: "bg-[var(--fill-destructive-default)]",
  information: "bg-[var(--fill-information-default)]",
  neutral: "bg-muted-foreground/40",
};

/** A small status dot. Decorative: always pair it with a text label. */
export function StatusDot({
  tone,
  className,
}: {
  tone: StatusDotTone;
  className?: string;
}): JSX.Element {
  return (
    <span
      aria-hidden="true"
      data-tone={tone}
      className={cn(
        "inline-block size-2 shrink-0 rounded-full",
        TONE_CLASS[tone],
        className,
      )}
    />
  );
}
