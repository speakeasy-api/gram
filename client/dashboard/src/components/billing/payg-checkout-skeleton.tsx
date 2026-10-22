import { Skeleton } from "@/components/ui/Skeleton";

/** Reserve the major sections of the initial checkout, without imitating every field. */
export function PaygCheckoutSkeleton(): JSX.Element {
  return (
    <div className="w-full" role="status" aria-label="Loading checkout">
      <span className="sr-only">Loading checkout</span>
      <div
        aria-hidden="true"
        className="grid w-full grid-cols-1 gap-8 lg:block"
      >
        <div className="contents pb-6 lg:grid lg:grid-cols-2 lg:items-start lg:gap-10">
          <div className="order-1 flex min-w-0 items-center justify-between gap-4 border-b border-border pb-2">
            {/* Small pricing heading. */}
            <Skeleton className="h-7 w-20" />
            <Skeleton className="h-7 w-32" />
          </div>
          <div className="order-3 w-full max-w-lg border-b border-border pb-2 lg:justify-self-end">
            <Skeleton className="h-7 w-36" />
          </div>
        </div>
        <div className="contents lg:grid lg:grid-cols-2 lg:items-start lg:gap-10">
          <div className="order-2 min-w-0 space-y-6">
            <div className="divide-y divide-border border-b border-border">
              {[0, 1, 2].map((item) => (
                <div key={item} className="py-4 first:pt-0">
                  <Skeleton className="h-12 w-full" />
                </div>
              ))}
            </div>
            <Skeleton className="h-28 w-full" />
            <div className="border-t border-border pt-4">
              <Skeleton className="h-8 w-full" />
            </div>
          </div>
          <div className="order-4 w-full max-w-lg space-y-8 lg:justify-self-end">
            {/* Broad wallet, payment, totals, and confirmation shapes. */}
            <Skeleton className="h-11 w-full" />
            <Skeleton className="h-36 w-full" />
            <Skeleton className="h-16 w-full" />
            <Skeleton className="h-11 w-full" />
          </div>
        </div>
      </div>
    </div>
  );
}
