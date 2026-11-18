import { useIconDither } from "@/components/icon-dither";
import { cn } from "@/lib/utils";
import type { LucideIcon } from "lucide-react";
import { useEffect, type ReactNode } from "react";

/** Widths of the ghost rows, so the placeholder reads as a real list. */
const GHOST_ROWS = [
  [38, 72],
  [26, 58],
  [44, 80],
  [30, 64],
  [36, 70],
  [22, 52],
] as const;

/**
 * What the list area shows before there are tools: the outline of the list it
 * will become, faded, with the one thing to do next set over it. A small
 * halftone tile carries the same texture as the page header.
 */
export function InspectState({
  icon: Icon,
  tone = "neutral",
  heading,
  description,
  action,
}: {
  icon: LucideIcon;
  tone?: "neutral" | "warning";
  heading: string;
  description?: ReactNode;
  action?: ReactNode;
}): JSX.Element {
  return (
    <div className="relative min-h-[420px]">
      <ul
        aria-hidden="true"
        className="border-border pointer-events-none border-t [mask-image:linear-gradient(to_bottom,black,transparent_85%)] opacity-60"
      >
        {GHOST_ROWS.map(([name, description], index) => (
          <li
            key={index}
            className="border-border flex flex-col gap-2 border-b px-6 py-3.5"
          >
            <span className="bg-muted h-3" style={{ width: `${name}%` }} />
            <span
              className="bg-muted/70 h-2.5"
              style={{ width: `${description}%` }}
            />
          </li>
        ))}
      </ul>

      <div className="absolute inset-0 flex items-center justify-center px-6">
        <div className="flex max-w-[520px] flex-col items-center gap-4 bg-[radial-gradient(closest-side,var(--color-card)_70%,transparent)] px-16 py-14 text-center">
          <HalftoneTile>
            <Icon
              className={cn(
                "size-5",
                tone === "warning"
                  ? "text-warning-foreground"
                  : "text-foreground",
              )}
            />
          </HalftoneTile>
          <div className="flex flex-col gap-2">
            <h3 className="font-display text-[22px] leading-7 font-light">
              {heading}
            </h3>
            {description ? (
              <p className="text-muted-foreground text-sm leading-5">
                {description}
              </p>
            ) : null}
          </div>
          {action ? (
            <div className="flex flex-wrap items-center justify-center gap-3">
              {action}
            </div>
          ) : null}
        </div>
      </div>
    </div>
  );
}

/** A square of the header's halftone sea with an icon set on white in it. */
function HalftoneTile({ children }: { children: ReactNode }): JSX.Element {
  const { canvasRef, start } = useIconDither({
    speed: 0.25,
    stillWhenReduced: true,
    exclusive: false,
  });
  useEffect(() => {
    const frame = requestAnimationFrame(start);
    return () => cancelAnimationFrame(frame);
  }, [start]);
  return (
    <span className="border-border relative isolate flex size-16 items-center justify-center border">
      <canvas
        ref={canvasRef}
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 -z-10 size-full"
      />
      <span className="bg-card border-border flex size-9 items-center justify-center border">
        {children}
      </span>
    </span>
  );
}

/** The run panel's companion to a list state: how Inspect works, in order. */
export function InspectSteps(): JSX.Element {
  const steps = [
    "Sign in with your own account, the way an agent would.",
    "See every tool agents get here, and why any are missing.",
    "Run one as you and read the raw request and response.",
  ];
  return (
    <div className="flex flex-col gap-4 pt-2">
      <span className="text-eyebrow">How Inspect works</span>
      <ol className="flex flex-col gap-3">
        {steps.map((step, index) => (
          <li key={step} className="flex gap-3 text-sm">
            <span className="border-border text-muted-foreground flex size-6 shrink-0 items-center justify-center border font-mono text-xs">
              {index + 1}
            </span>
            <span className="text-muted-foreground pt-0.5 leading-5">
              {step}
            </span>
          </li>
        ))}
      </ol>
    </div>
  );
}
