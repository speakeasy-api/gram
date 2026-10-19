import type { BadgeProps } from "@/components/ui/Badge";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { RiskPolicyApproval } from "@gram/client/models/components/riskpolicyapproval.js";

export type ApprovalStatus = RiskPolicyApproval["status"];

export const APPROVAL_STATUS_BADGE: Record<
  ApprovalStatus,
  { label: string; variant: NonNullable<BadgeProps["variant"]> }
> = {
  blocked: { label: "Not requested", variant: "neutral" },
  pending: { label: "Pending", variant: "warning" },
  approved: { label: "Approved", variant: "success" },
  consumed: { label: "Used", variant: "success" },
  denied: { label: "Denied", variant: "destructive" },
  expired: { label: "Expired", variant: "neutral" },
};

/** Who made the held call: a person by name when they are a member, else the
 *  agent or user id from the principal URN. */
export function requesterLabel(
  requesterUrn: string,
  members: AccessMember[],
): string {
  const member = members.find(
    (candidate) => candidate.principalUrn === requesterUrn,
  );
  if (member) return member.name || member.email;
  const [kind, id] = requesterUrn.split(":", 2);
  if (kind === "agent" && id) return `Agent ${id.slice(0, 8)}`;
  return requesterUrn;
}
