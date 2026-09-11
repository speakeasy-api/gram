import type { ReactNode } from "react";
import { Skeleton } from "@/components/ui/Skeleton";

interface JourneyLayoutProps {
  /** The timeline down the left: a stepper or a status list. */
  rail: ReactNode;
  /** Swap both columns for skeletons while the journey is being resolved. */
  loading?: boolean;
  /** How many rail rows the loading skeleton should stand in for. */
  skeletonRows?: number;
  children: ReactNode;
}

// The linear onboarding frame: a narrow rail on the left for where you are in
// the journey and the current step's content on the right. Shared by the
// wizard and by each setup task's page so the two never drift apart.
export function JourneyLayout({
  rail,
  loading = false,
  skeletonRows = 7,
  children,
}: JourneyLayoutProps): JSX.Element {
  return (
    <main className="flex min-h-0 flex-1 items-start justify-center overflow-y-auto px-4 py-8 md:px-8 md:py-16">
      <div className="flex w-full max-w-5xl gap-24">
        <div className="order-first hidden w-64 flex-shrink-0 md:block">
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

        <div className="order-last min-w-0 flex-1">
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
