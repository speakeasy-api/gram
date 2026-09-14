import { Badge, type BadgeProps } from "@/components/ui/Badge";
import { scopeTier, type ScopeTier } from "../model/provider";

// ScopeBadge labels a remote identity provider or session client with its
// tenancy tier — project-specific, organizational, or platform — derived from
// the owning ids on the entity. Shared by the issuer and client detail headers
// and the org-admin listing so the three tiers never render inconsistently.
const TIER_BADGE: Record<
  ScopeTier,
  { label: string; variant: BadgeProps["variant"] }
> = {
  project: { label: "Project-Specific", variant: "neutral" },
  organization: { label: "Organizational", variant: "information" },
  platform: { label: "Platform", variant: "success" },
};

export function ScopeBadge({
  projectId,
  organizationId,
}: {
  projectId?: string | null;
  organizationId?: string | null;
}): JSX.Element {
  const { label, variant } =
    TIER_BADGE[scopeTier({ projectId, organizationId })];
  return <Badge variant={variant}>{label}</Badge>;
}
