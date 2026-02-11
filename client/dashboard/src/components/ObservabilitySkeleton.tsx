import { Skeleton } from "@/components/ui/skeleton";

/**
 * Shared skeleton component used across all observability pages
 * (Insights, Chat Sessions, Logs) when logs are disabled.
 */
export function ObservabilitySkeleton() {
  return (
    <div className="flex flex-col gap-6 p-6 overflow-hidden">
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4 shrink-0">
        {[1, 2, 3, 4].map((i) => (
          <div key={i} className="rounded-lg border border-border bg-card p-5">
            <Skeleton className="h-4 w-24 mb-3" />
            <Skeleton className="h-9 w-32" />
          </div>
        ))}
      </div>
      <div className="rounded-lg border border-border bg-card p-6 flex-1 min-h-[120px]">
        <Skeleton className="h-full w-full" />
      </div>
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6 shrink-0">
        <div className="rounded-lg border border-border bg-card p-6">
          <Skeleton className="h-32 w-full" />
        </div>
        <div className="rounded-lg border border-border bg-card p-6">
          <Skeleton className="h-32 w-full" />
        </div>
      </div>
    </div>
  );
}
