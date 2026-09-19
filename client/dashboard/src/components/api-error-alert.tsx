import { Alert } from "@/components/ui/Alert";
import { Text } from "@/components/ui/Text";
import { apiErrorAlertVariant, describeApiError } from "@/lib/api-error";

/** Inline surface for a failed request: the API's message under a short title. */
export function ApiErrorAlert({
  error,
  className,
}: {
  error: unknown;
  className?: string;
}): JSX.Element | null {
  if (error == null) return null;
  const described = describeApiError(error);
  return (
    <Alert
      variant={apiErrorAlertVariant(described.kind)}
      alignTop
      className={className}
    >
      <div className="flex flex-col gap-0.5">
        <Text variant="small" className="font-medium">
          {described.title}
        </Text>
        <Text variant="small">{described.message}</Text>
      </div>
    </Alert>
  );
}
