import { Skeleton } from "@/components/ui/Skeleton";

/** Reserve the major sections of the initial checkout, without imitating every field. */
export function PaygCheckoutSkeleton(): JSX.Element {
  return (
    <div className="w-full" role="status" aria-label="Loading checkout">
      <span className="sr-only">Loading checkout</span>
      <div
        aria-hidden="true"
        className="grid w-full grid-cols-1 items-start gap-10 lg:grid-cols-2 lg:gap-12"
      >
        <div className="lg:col-span-2">
          {/* Pricing heading and explanation. */}
          <Skeleton className="h-[92px] w-full max-w-lg" />
        </div>
        <div className="min-w-0 space-y-8">
          <div className="space-y-6">
            {[0, 1, 2].map((item) => (
              <Skeleton key={item} className="h-[52px] w-full" />
            ))}
          </div>
          <div className="border-t border-neutral-default pt-5">
            <Skeleton className="h-[60px] w-full" />
          </div>
        </div>
        <div className="w-full max-w-lg self-start pt-1 lg:justify-self-end">
          {/* Wallet button, payment choices, collapsed promo/totals, submit. */}
          <Skeleton className="h-11 w-full" />
          <Skeleton className="mt-[51px] h-[139px] w-full" />
          <Skeleton className="mt-[37px] h-[69px] w-full" />
          <Skeleton className="mt-[9px] h-[42px] w-full" />
        </div>
      </div>
    </div>
  );
}
