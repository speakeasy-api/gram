import { InlineEmptyState } from "@/components/inline-empty-state";
import { Button } from "@/components/ui/Button";

/**
 * A section's read failed. Rendered ahead of the section's empty state, so a
 * dropped request never reads as "nothing here".
 */
export function SourceSectionError({
  heading,
  description,
  onRetry,
}: {
  heading: string;
  description: string;
  onRetry?: () => void;
}): JSX.Element {
  return (
    <InlineEmptyState
      icon="triangle-alert"
      heading={heading}
      description={description}
      action={
        onRetry ? (
          <Button variant="secondary" size="sm" onClick={onRetry}>
            <Button.Text>Retry</Button.Text>
          </Button>
        ) : undefined
      }
    />
  );
}
