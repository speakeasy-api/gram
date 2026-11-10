import { Badge } from "@/components/ui/Badge";

/**
 * The tag on an option that is shown but cannot be chosen yet: "Coming soon"
 * for a pathway the backend does not support, or a short reason such as
 * "Requires KMS" for one this organization is not entitled to.
 */
export function AvailabilityBadge({
  children = "Coming soon",
}: {
  children?: string;
}): JSX.Element {
  return (
    <Badge variant="neutral" size="sm">
      <Badge.Text>{children}</Badge.Text>
    </Badge>
  );
}
