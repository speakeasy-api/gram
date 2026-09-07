import {
  killswitchCreateHref,
  killswitchStatusHref,
} from "@/components/killswitch/KillswitchUserStatus";

export function getMemberKillswitchMenuModel(
  killswitchHref: string,
  memberId: string,
): { viewHref: string; newHref: string } {
  return {
    viewHref: killswitchStatusHref(killswitchHref, memberId),
    newHref: killswitchCreateHref(killswitchHref, { userId: memberId }),
  };
}
