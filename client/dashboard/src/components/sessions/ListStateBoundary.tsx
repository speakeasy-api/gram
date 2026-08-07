import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";

/**
 * The pending / error / empty triad shared by the user-session and
 * user-session-client listings. Extracted so the two lists cannot drift, and
 * so neither has to express four render states as a nested ternary.
 */
export function ListStateBoundary({
  isPending,
  isError,
  isEmpty,
  errorMessage,
  emptyMessage,
  onRetry,
  skeletonRows = 3,
  children,
}: {
  isPending: boolean;
  isError: boolean;
  isEmpty: boolean;
  errorMessage: string;
  emptyMessage: string;
  onRetry: () => void;
  skeletonRows?: number;
  children: React.ReactNode;
}): JSX.Element {
  if (isPending) {
    return (
      <div className="space-y-2">
        {Array.from({ length: skeletonRows }).map((_, i) => (
          <Skeleton key={i} className="h-12 w-full" />
        ))}
      </div>
    );
  }

  if (isError) {
    return (
      <div role="alert" className="flex items-center justify-between gap-3">
        <p className="text-destructive text-sm">{errorMessage}</p>
        <Button variant="tertiary" size="sm" onClick={onRetry}>
          Retry
        </Button>
      </div>
    );
  }

  if (isEmpty) {
    return <p className="text-muted-foreground text-sm">{emptyMessage}</p>;
  }

  return <>{children}</>;
}
