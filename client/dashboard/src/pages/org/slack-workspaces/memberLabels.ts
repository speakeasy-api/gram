import type { SlackDirectoryMember } from "@gram/client/models/components/slackdirectorymember.js";
export const typeLabels: Record<SlackDirectoryMember["memberType"], string> = {
  person: "Member",
  guest: "Guest",
  single_channel_guest: "Single-channel guest",
  bot: "Bot / app",
  unknown: "Unknown",
};
export const stateLabels: Record<SlackDirectoryMember["status"], string> = {
  active: "Active",
  deactivated: "Deactivated",
  invited: "Invited",
  unknown: "Unknown",
};
