import type { ReactNode } from "react";
import { Skeleton } from "@/components/ui/Skeleton";
import { cn } from "@/lib/utils";
import { SETUP_CONTAINER } from "./setup-container";

interface JourneyLayoutProps {
  /** The timeline down the left: a stepper or a status list. */
  rail: ReactNode;
  /** Swap both columns for skeletons while the journey is being resolved. */
  loading?: boolean;
  /** How many rail rows the loading skeleton should stand in for. */
  skeletonRows?: number;
  children: ReactNode;
}

// The linear onboarding frame: a rail on the left (2/12 of the width) for where
// you are in the journey and the current step's content on the right (10/12).
// Shared by the wizard and by each setup task's page so the two never drift
// apart.
export function JourneyLayout({
  rail,
  loading = false,
  skeletonRows = 7,
  children,
}: JourneyLayoutProps): JSX.Element {
  return (
    <main className="min-h-0 flex-1 overflow-y-auto py-8 md:py-16">
      {/* Same frame as the header, so the rail starts under the logo and the
          content ends under the header actions. */}
      <div className={cn(SETUP_CONTAINER, "md:grid md:grid-cols-12 md:gap-14")}>
        <div className="hidden md:col-span-2 md:block">
          {loading ? (
            <Skeleton>
              {Array.from({ length: skeletonRows }, (_, index) => (
                <div key={index} className="h-8 w-full" />
              ))}
            </Skeleton>
          ) : (
            rail
          )}
        </div>

        <div className="min-w-0 md:col-span-10">
          {loading ? (
            <Skeleton>
              <div className="h-12 w-2/3" />
              <div className="h-5 w-full" />
              <div className="h-64 w-full" />
            </Skeleton>
          ) : (
            children
          )}
        </div>
      </div>
    </main>
  );
}
