import type { BadgeProps } from "@/components/ui/Badge";
import type { InternalSessionIssuer } from "@gram/client/models/components/internalsessionissuer.js";
import type { InternalSessionIssuerKey } from "@gram/client/models/components/internalsessionissuerkey.js";

type BadgeVariant = NonNullable<BadgeProps["variant"]>;

export const ISSUER_STATE_BADGE: Record<
  InternalSessionIssuer["state"],
  { label: string; variant: BadgeVariant }
> = {
  enabled: { label: "Enabled", variant: "success" },
  disabled: { label: "Off", variant: "neutral" },
  setup_required: { label: "Off", variant: "neutral" },
  verified_domain_required: {
    label: "Needs a verified domain",
    variant: "neutral",
  },
  signing_paused: { label: "Paused", variant: "warning" },
  keyless: { label: "Needs attention", variant: "destructive" },
  unavailable: { label: "Unavailable", variant: "neutral" },
};

export const KEY_STATE_BADGE: Record<
  InternalSessionIssuerKey["state"],
  { label: string; variant: BadgeVariant }
> = {
  active: { label: "Active", variant: "success" },
  pending: { label: "Pending", variant: "neutral" },
  retired: { label: "Retired", variant: "neutral" },
};
