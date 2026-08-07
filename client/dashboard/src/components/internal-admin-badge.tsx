import { Badge } from "@/components/ui/Badge";

// Amber "Internal Admin" pill marking UI that only Speakeasy platform admins
// (or local dev builds) can see. Composes the design-system Badge — like
// ReleaseStageBadge does — so the developer toolbar and dev-only form fields
// render the marker from one source of truth.
export function InternalAdminBadge(): JSX.Element {
  return (
    <Badge size="sm" variant="warning" background>
      <Badge.Text>Internal Admin</Badge.Text>
    </Badge>
  );
}
