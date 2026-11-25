import { Button } from "@/components/ui/Button";
import { Spinner } from "@/components/ui/Spinner";

/**
 * The Retry action under a failed read. While the retry is in flight it
 * spins and locks, so a second click does not queue another request and the
 * admin can see the click landed rather than wondering if it did.
 */
export function RetryButton({
  onRetry,
  isRetrying,
  label = "Retry",
  className,
}: {
  onRetry: () => void;
  isRetrying: boolean;
  label?: string;
  className?: string;
}): JSX.Element {
  return (
    <Button
      className={className}
      variant="secondary"
      size="sm"
      disabled={isRetrying}
      aria-busy={isRetrying}
      onClick={onRetry}
    >
      {isRetrying ? (
        <Button.LeftIcon>
          <Spinner className="mr-0" />
        </Button.LeftIcon>
      ) : null}
      <Button.Text>{isRetrying ? "Retrying…" : label}</Button.Text>
    </Button>
  );
}
