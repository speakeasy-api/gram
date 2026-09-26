import { Bot, CircleSlash, UserRound } from "lucide-react";
import type { ReactNode } from "react";

type IdentityMode = "user" | "agent" | "none";

export type IdentityModeCard = {
  value: IdentityMode;
  title: string;
  description: string;
  icon: ReactNode;
};

/**
 * The three identity choices, in the order AIM-230 fixes, defined once so the
 * server settings panel and both creation flows cannot drift apart. The
 * descriptions name the upstream service rather than talking about "the
 * upstream", so the choice reads as a decision about Linear (or whatever this
 * server fronts) rather than about Speakeasy's plumbing.
 *
 * Icons are muted: these mark a choice, not a status. The colored dot on the
 * server details pill is what reports which one is in force.
 */
export function identityModeCards(upstreamName: string): IdentityModeCard[] {
  return [
    {
      value: "user",
      title: "User Identity",
      description: `Each user signs in to ${upstreamName} as themselves and keeps their own permissions.`,
      icon: <UserRound aria-hidden="true" className="size-4" />,
    },
    {
      value: "agent",
      title: "Agent Identity",
      description:
        "Every caller acts as one service account. Manage what it may do in the control plane.",
      icon: <Bot aria-hidden="true" className="size-4" />,
    },
    {
      value: "none",
      title: "No Identity",
      description:
        "Speakeasy will manage no identity and users will manage their own static headers.",
      icon: <CircleSlash aria-hidden="true" className="size-4" />,
    },
  ];
}
