import { Badge } from "@/components/ui/badge";

// ScopeBadge labels a remote identity provider or session client as
// organization-wide or scoped to a single project, based on whether it carries
// an owning project id. Shared by the issuer and client detail headers so the
// two never drift.
export function ScopeBadge({
  projectScoped,
}: {
  projectScoped: boolean;
}): JSX.Element {
  return projectScoped ? (
    <Badge background={false}>Project-Specific</Badge>
  ) : (
    <Badge background={false}>Organizational</Badge>
  );
}
