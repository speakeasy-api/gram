import { useId, type ReactNode } from "react";
import { Check } from "lucide-react";
import { Badge } from "@/components/ui/Badge";
import { cn } from "@/lib/utils";
import {
  useIsActiveJourneyStep,
  useRegisterJourneyStep,
} from "./journey-steps";

interface StepSectionProps {
  index: number;
  title: string;
  description?: ReactNode;
  /** Swaps the number for a check mark once the sub-step's outcome is met. */
  complete?: boolean;
  /** Rendered at the end of the heading row, e.g. a status badge. */
  aside?: ReactNode;
  /** Short marker after the title, e.g. "Recommended" or "Optional". */
  badge?: string;
  /** Green for a recommendation, neutral for anything else. */
  badgeVariant?: "success" | "neutral";
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
  badge,
  badgeVariant = "neutral",
  children,
}: StepSectionProps): JSX.Element {
  const headingId = useId();
  // The task page's rail lists whatever sections the task renders, and shows
  // one at a time; the rest stay mounted but hidden so their state survives.
  useRegisterJourneyStep(headingId, { index, title, complete, badge });
  const active = useIsActiveJourneyStep(index);

  return (
    <section aria-labelledby={headingId} className="space-y-3" hidden={!active}>
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
          <div className="flex items-center gap-2">
            <h3
              id={headingId}
              className="text-foreground text-sm leading-7 font-semibold"
            >
              {title}
            </h3>
            {badge ? (
              <Badge variant={badgeVariant} background size="sm">
                <Badge.Text>{badge}</Badge.Text>
              </Badge>
            ) : null}
          </div>
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
