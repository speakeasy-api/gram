import { Badge } from "@speakeasy-api/moonshine";
import type { PlatformSetupStatus } from "../types";

export function platformStatusBadge(
  status: PlatformSetupStatus,
): JSX.Element | null {
  switch (status) {
    case "complete":
      return (
        <Badge variant="success" background>
          <Badge.Text>Complete</Badge.Text>
        </Badge>
      );
    case "in_progress":
      return (
        <Badge variant="neutral" background>
          <Badge.Text>In progress</Badge.Text>
        </Badge>
      );
    case "blocked":
      return (
        <Badge variant="destructive" background>
          <Badge.Text>Not eligible</Badge.Text>
        </Badge>
      );
    case "not_started":
    default:
      return null;
  }
}
