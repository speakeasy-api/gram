import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";

/**
 * Shared skeleton component used across all observability pages
 * (Insights, Agent Sessions, Logs) when logs are disabled.
 */
export function ObservabilitySkeleton(): JSX.Element {
  return (
    <div className="flex flex-col gap-6 overflow-hidden p-6">
      <div className="grid shrink-0 grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-4">
        {[1, 2, 3, 4].map((i) => (
          <Card key={i} className="p-5">
            <Skeleton className="mb-3 h-4 w-24" />
            <Skeleton className="h-9 w-32" />
          </Card>
        ))}
      </div>
      <Card className="min-h-[120px] flex-1 p-6">
        <Skeleton className="h-full w-full" />
      </Card>
      <div className="grid shrink-0 grid-cols-1 gap-6 lg:grid-cols-2">
        <Card className="p-6">
          <Skeleton className="h-32 w-full" />
        </Card>
        <Card className="p-6">
          <Skeleton className="h-32 w-full" />
        </Card>
      </div>
    </div>
  );
}
