import { Badge } from "@/components/ui/badge";
import {
  remoteSessionScopeTier,
  type RemoteSessionScopeTier,
} from "@/lib/sources";

// ScopeBadge labels a remote identity provider or session client with its
// tenancy tier — project-specific, organizational, or platform — derived from
// the owning ids on the entity. Shared by the issuer and client detail headers
// and the org-admin listing so the three tiers never render inconsistently.
const TIER_BADGE: Record<
  RemoteSessionScopeTier,
  { label: string; variant: "outline" | "secondary" | "default" }
> = {
  project: { label: "Project-Specific", variant: "outline" },
  organization: { label: "Organizational", variant: "secondary" },
  platform: { label: "Platform", variant: "default" },
};

export function ScopeBadge({
  projectId,
  organizationId,
}: {
  projectId?: string | null;
  organizationId?: string | null;
}): JSX.Element {
  const { label, variant } =
    TIER_BADGE[remoteSessionScopeTier({ projectId, organizationId })];
  return <Badge variant={variant}>{label}</Badge>;
}
