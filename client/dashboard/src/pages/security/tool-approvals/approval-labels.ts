import type { BadgeProps } from "@/components/ui/Badge";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { RiskPolicyApproval } from "@gram/client/models/components/riskpolicyapproval.js";
import { memberDisplayName } from "../audience-principals";

export type ApprovalStatus = RiskPolicyApproval["status"];

type ApprovalStatusBadge = {
  label: string;
  variant: NonNullable<BadgeProps["variant"]>;
};

/** The badge for a request's status. A status this dashboard does not know
 *  yet reads as Unknown instead of breaking the queue. */
export function approvalStatusBadge(
  status: ApprovalStatus,
): ApprovalStatusBadge {
  switch (status) {
    case "blocked":
      return { label: "Not requested", variant: "neutral" };
    case "pending":
      return { label: "Pending", variant: "warning" };
    case "approved":
      return { label: "Approved", variant: "success" };
    case "consumed":
      return { label: "Used", variant: "success" };
    case "denied":
      return { label: "Denied", variant: "destructive" };
    case "expired":
      return { label: "Expired", variant: "neutral" };
  }
  return { label: "Unknown", variant: "neutral" };
}

/** Who made the held call: a person by name when they are a member, else the
 *  agent or user id from the principal URN. */
export function requesterLabel(
  requesterUrn: string,
  members: AccessMember[],
): string {
  const member = members.find(
    (candidate) => candidate.principalUrn === requesterUrn,
  );
  if (member) return memberDisplayName(member);
  const [kind, id] = requesterUrn.split(":", 2);
  if (kind === "agent" && id) return `Agent ${id.slice(0, 8)}`;
  return requesterUrn;
}
