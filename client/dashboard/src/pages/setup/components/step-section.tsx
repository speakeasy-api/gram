import { useId, type ReactNode } from "react";
import { Check } from "lucide-react";
import { cn } from "@/lib/utils";

interface StepSectionProps {
  index: number;
  title: string;
  description?: ReactNode;
  /** Swaps the number for a check mark once the sub-step's outcome is met. */
  complete?: boolean;
  /** Rendered at the end of the heading row, e.g. a status badge. */
  aside?: ReactNode;
  children: ReactNode;
}

// A numbered sub-step inside an onboarding card. Cards map to outcomes, so
// the mechanical steps on the way to one (publish the marketplace, connect the
// platform, confirm traffic) stack as sections instead of each taking a card.
export function StepSection({
  index,
  title,
  description,
  complete = false,
  aside,
  children,
}: StepSectionProps): JSX.Element {
  const headingId = useId();

  return (
    <section aria-labelledby={headingId} className="space-y-3">
      <div className="flex items-start gap-3">
        <div
          aria-hidden="true"
          className={cn(
            "flex h-7 w-7 flex-shrink-0 items-center justify-center border text-xs font-semibold",
            complete
              ? "border-success-default text-default-success"
              : "border-border text-muted-foreground",
          )}
        >
          {complete ? <Check className="h-3.5 w-3.5" strokeWidth={3} /> : index}
        </div>
        <div className="min-w-0 flex-1">
          <h3
            id={headingId}
            className="text-foreground text-sm leading-7 font-semibold"
          >
            {title}
          </h3>
          {description ? (
            <p className="text-muted-foreground text-sm leading-relaxed">
              {description}
            </p>
          ) : null}
        </div>
        {aside ? <div className="flex-shrink-0 pt-1">{aside}</div> : null}
      </div>
      <div className="sm:pl-10">{children}</div>
    </section>
  );
}
